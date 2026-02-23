const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const { TEMP_DIRS, SAFETY_LIMITS } = require('../config/constants');

const {
  activeDownloads,
  activeProcesses,
  activeJobsByType,
  isGalleryDlAvailable,
  registerClient,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount,
  sendProgress
} = require('../services/state');

const { validateUrl } = require('../utils/validation');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const { hasCookiesFile, COOKIES_FILE } = require('../utils/cookies');
const { rateLimitMiddleware } = require('../middleware/rateLimit');
const discordAlerts = require('../discord-alerts');

router.use(rateLimitMiddleware);

router.get('/status', (req, res) => {
  res.json({ available: isGalleryDlAvailable() });
});

router.get('/metadata', async (req, res) => {
  const { url } = req.query;

  if (!isGalleryDlAvailable()) {
    return res.status(503).json({ error: 'gallery-dl not installed on server' });
  }

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  try {
    const { stdout, stderr, exitCode } = await new Promise((resolve, reject) => {
      const proc = spawn('gallery-dl', ['--dump-json', '--range', '1-10', url]);
      let stdoutData = '';
      let stderrData = '';

      const timeout = setTimeout(() => {
        proc.kill('SIGTERM');
        reject(new Error('gallery-dl metadata timeout (30s)'));
      }, 30000);

      proc.stdout.on('data', (data) => {
        stdoutData += data.toString();
        if (stdoutData.length > 10 * 1024 * 1024) {
          proc.kill('SIGTERM');
          reject(new Error('Output too large'));
        }
      });

      proc.stderr.on('data', (data) => {
        stderrData += data.toString();
      });

      proc.on('close', (code) => {
        clearTimeout(timeout);
        resolve({ stdout: stdoutData, stderr: stderrData, exitCode: code });
      });

      proc.on('error', (err) => {
        clearTimeout(timeout);
        reject(err);
      });
    });

    if (exitCode !== 0 && !stdout.trim()) {
      console.error('[gallery-dl] metadata error:', stderr);
      return res.status(500).json({
        error: 'Could not fetch gallery info',
        details: stderr.substring(0, 200)
      });
    }

    const lines = stdout.trim().split('\n').filter(l => l.trim());
    let imageCount = 0;
    let title = 'Gallery';
    let images = [];

    for (const line of lines) {
      try {
        const item = JSON.parse(line);
        imageCount++;
        if (item.filename) {
          images.push({
            filename: item.filename,
            extension: item.extension || 'jpg',
            url: item.url
          });
        }
        if (!title || title === 'Gallery') {
          title = item.subcategory || item.category || item.gallery || 'Gallery';
        }
      } catch { }
    }

    const hostname = new URL(url).hostname.replace('www.', '');

    res.json({
      title,
      imageCount: imageCount || images.length,
      images: images.slice(0, 10),
      site: hostname,
      isGallery: true
    });
  } catch (err) {
    console.error('[gallery-dl] metadata error:', err);
    res.status(500).json({ error: 'Failed to get gallery info' });
  }
});

router.get('/download', async (req, res) => {
  const { url, progressId, clientId, filename } = req.query;

  if (!isGalleryDlAvailable()) {
    return res.status(503).json({ error: 'gallery-dl not installed on server' });
  }

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      return res.status(429).json({
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.`
      });
    }
  }

  const downloadId = progressId || uuidv4();
  const galleryDir = path.join(TEMP_DIRS.gallery, `gallery-${downloadId}`);

  if (!fs.existsSync(galleryDir)) {
    fs.mkdirSync(galleryDir, { recursive: true });
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  const processInfo = { cancelled: false, process: null, tempDir: galleryDir };
  activeProcesses.set(downloadId, processInfo);

  activeJobsByType.download++;
  console.log(`[Queue] Gallery download started. Active: ${JSON.stringify(activeJobsByType)}`);

  sendProgress(downloadId, 'starting', 'Starting gallery download...');

  try {
    await runGalleryDl(url, galleryDir, downloadId, processInfo, req);

    const allFiles = collectDownloadedFiles(galleryDir);

    if (allFiles.length === 0) {
      throw new Error('No images were downloaded');
    }

    if (allFiles.length === 1) {
      await sendSingleFile(res, allFiles[0], filename, downloadId, url, req, cleanup);
    } else {
      await sendZipFile(res, allFiles, filename, url, downloadId, cleanup);
    }

    function cleanup() {
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Gallery finished. Active: ${JSON.stringify(activeJobsByType)}`);

      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    }

  } catch (err) {
    console.error(`[${downloadId}] Gallery error:`, err.message);
    discordAlerts.galleryError('Gallery Download Error', 'Gallery download failed.', { jobId: downloadId, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Gallery download failed');
    }

    activeProcesses.delete(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Gallery error. Active: ${JSON.stringify(activeJobsByType)}`);

    setTimeout(() => cleanupJobFiles(downloadId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Gallery download failed' });
    }
  }
});

async function runGalleryDl(url, galleryDir, downloadId, processInfo, req) {
  const galleryArgs = [
    '-d', galleryDir,
    '--filename', '{num:03d}_{filename}.{extension}',
    '--write-metadata',
    url
  ];

  if (hasCookiesFile()) {
    galleryArgs.unshift('--cookies', COOKIES_FILE);
  }

  console.log(`[${downloadId}] gallery-dl starting`);

  return new Promise((resolve, reject) => {
    const galleryDl = spawn('gallery-dl', galleryArgs);
    processInfo.process = galleryDl;

    let downloadedCount = 0;
    let lastUpdate = Date.now();

    galleryDl.stdout.on('data', (data) => {
      const msg = data.toString();
      if (msg.includes('/') || msg.includes('.jpg') || msg.includes('.png') || msg.includes('.gif') || msg.includes('.webp')) {
        downloadedCount++;
        const now = Date.now();
        if (now - lastUpdate > 500) {
          lastUpdate = now;
          sendProgress(downloadId, 'downloading', `Downloaded ${downloadedCount} images...`, null, { downloadedCount });
        }
      }
    });

    let galleryStderr = '';
    galleryDl.stderr.on('data', (data) => {
      const msg = data.toString();
      galleryStderr += msg;
      if (msg.includes('ERROR')) {
        console.error(`[${downloadId}] gallery-dl error: ${msg.trim()}`);
      }
    });

    galleryDl.on('close', (code) => {
      if (processInfo.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code === 0) {
        resolve();
      } else {
        console.error(`[${downloadId}] gallery-dl exited with code ${code}: ${galleryStderr.trim()}`);
        reject(new Error(`gallery-dl failed with exit code ${code}: ${galleryStderr.trim().slice(0, 200)}`));
      }
    });

    galleryDl.on('error', reject);

    req.on('close', () => {
      processInfo.cancelled = true;
      galleryDl.kill('SIGTERM');
    });
  });
}

function collectDownloadedFiles(galleryDir) {
  const allFiles = [];
  function walkDir(dir) {
    const items = fs.readdirSync(dir);
    for (const item of items) {
      const fullPath = path.join(dir, item);
      const stat = fs.statSync(fullPath);
      if (stat.isDirectory()) {
        walkDir(fullPath);
      } else if (!item.endsWith('.json')) {
        allFiles.push(fullPath);
      }
    }
  }
  walkDir(galleryDir);
  return allFiles;
}

async function sendSingleFile(res, singleFile, filename, downloadId, url, req, cleanup) {
  const ext = path.extname(singleFile).toLowerCase();
  const mimeTypes = {
    '.jpg': 'image/jpeg',
    '.jpeg': 'image/jpeg',
    '.png': 'image/png',
    '.gif': 'image/gif',
    '.webp': 'image/webp',
    '.mp4': 'video/mp4',
    '.webm': 'video/webm'
  };
  const mimeType = mimeTypes[ext] || 'application/octet-stream';
  const stat = fs.statSync(singleFile);
  const safeFilename = sanitizeFilename(filename || path.basename(singleFile, ext)) + ext;

  sendProgress(downloadId, 'sending', 'Sending file...');

  res.setHeader('Content-Type', mimeType);
  res.setHeader('Content-Length', stat.size);
  res.setHeader('Content-Disposition', `attachment; filename="${safeFilename}"; filename*=UTF-8''${encodeURIComponent(safeFilename)}`);

  const stream = fs.createReadStream(singleFile);
  stream.pipe(res);

  stream.on('close', () => {
    sendProgress(downloadId, 'complete', 'Download complete!');
    cleanup();
  });

  stream.on('error', () => {
    sendProgress(downloadId, 'error', 'Failed to send file');
    cleanup();
  });
}

async function sendZipFile(res, allFiles, filename, url, downloadId, cleanup) {
  sendProgress(downloadId, 'zipping', `Creating zip with ${allFiles.length} images...`, 90);

  const zipPath = path.join(TEMP_DIRS.gallery, `${downloadId}.zip`);
  const hostname = new URL(url).hostname.replace('www.', '');
  const safeZipName = sanitizeFilename(filename || hostname || 'gallery');

  await new Promise((resolve, reject) => {
    const output = fs.createWriteStream(zipPath);
    const archive = archiver('zip', { zlib: { level: 5 } });

    output.on('close', resolve);
    archive.on('error', reject);

    archive.pipe(output);

    allFiles.forEach((filePath) => {
      const baseName = path.basename(filePath);
      archive.file(filePath, { name: baseName });
    });

    archive.finalize();
  });

  sendProgress(downloadId, 'sending', 'Sending zip file...');

  const stat = fs.statSync(zipPath);
  const zipFilename = `${safeZipName}.zip`;

  res.setHeader('Content-Type', 'application/zip');
  res.setHeader('Content-Length', stat.size);
  res.setHeader('Content-Disposition', `attachment; filename="${zipFilename}"; filename*=UTF-8''${encodeURIComponent(zipFilename)}`);

  const stream = fs.createReadStream(zipPath);
  stream.pipe(res);

  stream.on('close', () => {
    sendProgress(downloadId, 'complete', `Downloaded ${allFiles.length} images!`);
    cleanup();
  });

  stream.on('error', () => {
    sendProgress(downloadId, 'error', 'Failed to send zip');
    cleanup();
  });
}

module.exports = router;
