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
  isGalleryDlAvailable,
  canStartJob,
  registerClient,
  linkJobToClient,
  getClientJobCount,
  releaseJob,
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

    let imageCount = 0;
    let title = 'Image';
    let images = [];
    let dirMeta = null;

    // try parsing as a JSON array first (twitter/x and some other extractors)
    try {
      const data = JSON.parse(stdout);
      if (Array.isArray(data)) {
        for (const entry of data) {
          if (!Array.isArray(entry) || entry.length < 2) continue;
          // skip error entries (e.g. [-1, {error: "KeyError"}])
          if (entry[0] < 0) continue;
          if (typeof entry[1] === 'string' && entry[1].startsWith('http')) {
            const meta = (entry.length >= 3 && entry[2] && typeof entry[2] === 'object') ? entry[2] : {};
            imageCount++;
            images.push({
              filename: meta.filename || `image_${imageCount}`,
              extension: meta.extension || 'jpg',
              url: entry[1]
            });
            if (title === 'Image') {
              title = meta.subcategory || meta.category || meta.gallery || 'Image';
            }
          } else if (entry[1] && typeof entry[1] === 'object') {
            const meta = entry[1];
            if (!dirMeta) dirMeta = meta;
            if (title === 'Image') {
              title = meta.subcategory || meta.category || meta.gallery || 'Image';
            }
          }
        }
      }
    } catch {}

    // fall back to line-by-line parsing (most extractors)
    if (imageCount === 0) {
      const lines = stdout.trim().split('\n').filter(l => l.trim());
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
          if (title === 'Image') {
            title = item.subcategory || item.category || item.gallery || 'Image';
          }
        } catch { }
      }
    }

    if (imageCount === 0) {
      return res.status(500).json({ error: 'No images found in this link' });
    }

    const hostname = new URL(url).hostname.replace('www.', '');

    const result = {
      title,
      imageCount: imageCount || images.length,
      images: images.slice(0, 10),
      site: hostname,
      isGallery: true
    };

    // detect tiktok carousel (photo post with audio)
    if (dirMeta && dirMeta.category === 'tiktok' && dirMeta.post_type === 'image') {
      result.isTikTokCarousel = true;
      const music = dirMeta.music || {};
      result.hasAudio = !!(music.playUrl || music.play_url);
      result.musicTitle = music.title || null;
      result.musicDuration = music.duration || null;
    }

    res.json(result);
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

  const jobCheck = canStartJob('download');
  if (!jobCheck.ok) {
    sendProgress(downloadId, 'error', jobCheck.reason);
    return res.status(503).json({ error: jobCheck.reason });
  }
  const galleryDir = path.join(TEMP_DIRS.gallery, `gallery-${downloadId}`);

  if (!fs.existsSync(galleryDir)) {
    fs.mkdirSync(galleryDir, { recursive: true });
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  const processInfo = { cancelled: false, process: null, tempDir: galleryDir, jobType: 'download' };
  activeProcesses.set(downloadId, processInfo);

  console.log(`[Queue] Gallery download started.`);

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
      releaseJob(downloadId);
      console.log(`[Queue] Gallery finished.`);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    }

  } catch (err) {
    console.error(`[${downloadId}] Gallery error:`, err.message);
    discordAlerts.galleryError('Gallery Download Error', 'Gallery download failed.', { jobId: downloadId, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Gallery download failed');
    }

    releaseJob(downloadId);
    console.log(`[Queue] Gallery error.`);
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

const IMAGE_EXTS = new Set(['.jpg', '.jpeg', '.png', '.webp', '.gif', '.bmp', '.tiff']);
const AUDIO_EXTS = new Set(['.mp3', '.m4a', '.wav', '.ogg', '.aac', '.opus']);

function getAudioDuration(filePath) {
  return new Promise((resolve, reject) => {
    const proc = spawn('ffprobe', [
      '-v', 'error', '-show_entries', 'format=duration',
      '-of', 'default=noprint_wrappers=1:nokey=1', filePath
    ]);
    let out = '';
    proc.stdout.on('data', (d) => { out += d.toString(); });
    proc.on('close', (code) => {
      if (code !== 0) return reject(new Error('ffprobe failed'));
      const dur = parseFloat(out.trim());
      if (isNaN(dur)) return reject(new Error('Could not parse duration'));
      resolve(dur);
    });
    proc.on('error', reject);
  });
}

function getImageDimensions(filePath) {
  return new Promise((resolve, reject) => {
    const proc = spawn('ffprobe', [
      '-v', 'error', '-select_streams', 'v:0',
      '-show_entries', 'stream=width,height',
      '-of', 'csv=p=0:s=x', filePath
    ]);
    let out = '';
    proc.stdout.on('data', (d) => { out += d.toString(); });
    proc.on('close', (code) => {
      if (code !== 0) return resolve({ width: 1080, height: 1920 });
      const [w, h] = out.trim().split('x').map(Number);
      resolve({ width: w || 1080, height: h || 1920 });
    });
    proc.on('error', () => resolve({ width: 1080, height: 1920 }));
  });
}

router.get('/slideshow', async (req, res) => {
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

  const jobCheck = canStartJob('download');
  if (!jobCheck.ok) {
    sendProgress(downloadId, 'error', jobCheck.reason);
    return res.status(503).json({ error: jobCheck.reason });
  }

  const galleryDir = path.join(TEMP_DIRS.gallery, `gallery-${downloadId}`);
  if (!fs.existsSync(galleryDir)) {
    fs.mkdirSync(galleryDir, { recursive: true });
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  const processInfo = { cancelled: false, process: null, tempDir: galleryDir, jobType: 'download' };
  activeProcesses.set(downloadId, processInfo);

  console.log(`[Queue] Slideshow download started.`);
  sendProgress(downloadId, 'starting', 'Starting slideshow download...');

  req.on('close', () => {
    if (!res.writableEnded) {
      processInfo.cancelled = true;
      if (processInfo.process) {
        try { processInfo.process.kill('SIGTERM'); } catch {}
      }
    }
  });

  function cleanup() {
    activeDownloads.delete(downloadId);
    releaseJob(downloadId);
    console.log(`[Queue] Slideshow finished.`);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);
  }

  try {
    // step 1: download all files via gallery-dl
    await runGalleryDl(url, galleryDir, downloadId, processInfo, req);

    const allFiles = collectDownloadedFiles(galleryDir);
    if (allFiles.length === 0) {
      throw new Error('No files were downloaded');
    }

    // step 2: separate images and audio
    const imageFiles = allFiles.filter(f => IMAGE_EXTS.has(path.extname(f).toLowerCase()));
    const audioFiles = allFiles.filter(f => AUDIO_EXTS.has(path.extname(f).toLowerCase()));

    if (imageFiles.length === 0) {
      throw new Error('No images found in download');
    }

    // no audio? fall back to regular gallery download (photos only)
    if (audioFiles.length === 0) {
      console.log(`[${downloadId}] No audio found, falling back to gallery download`);
      if (allFiles.length === 1) {
        await sendSingleFile(res, allFiles[0], filename, downloadId, url, req, cleanup);
      } else {
        await sendZipFile(res, allFiles, filename, url, downloadId, cleanup);
      }
      return;
    }

    // step 3: get audio duration
    const audioFile = audioFiles[0];
    sendProgress(downloadId, 'processing', 'Analyzing audio...', 30);
    const audioDuration = await getAudioDuration(audioFile);
    console.log(`[${downloadId}] Audio duration: ${audioDuration}s, Images: ${imageFiles.length}`);

    // step 4: determine orientation from first image
    const firstDims = await getImageDimensions(imageFiles[0]);
    const isVertical = firstDims.height >= firstDims.width;
    const targetW = isVertical ? 1080 : 1920;
    const targetH = isVertical ? 1920 : 1080;

    // step 5: build ffmpeg command
    const outputFile = path.join(galleryDir, `${downloadId}-slideshow.mp4`);
    sendProgress(downloadId, 'processing', 'Creating slideshow video...', 50);

    const crossfadeDur = 0.5;

    if (imageFiles.length === 1) {
      // single image: loop for audio duration
      const ffArgs = [
        '-loop', '1', '-t', String(audioDuration),
        '-i', imageFiles[0],
        '-i', audioFile,
        '-filter_complex',
        `[0]scale=w='if(gt(iw,ih),${targetW},-2)':h='if(gt(iw,ih),-2,${targetH})',pad=${targetW}:${targetH}:(ow-iw)/2:(oh-ih)/2:black,setsar=1[v]`,
        '-map', '[v]', '-map', '1:a',
        '-shortest', '-r', '30', '-pix_fmt', 'yuv420p',
        '-c:v', 'libx264', '-preset', 'fast', '-crf', '23',
        '-c:a', 'aac', '-b:a', '192k',
        outputFile
      ];

      await runFfmpeg(ffArgs, downloadId, processInfo);
    } else {
      // multiple images: crossfade slideshow
      const secPerImage = audioDuration / imageFiles.length;
      const imgDuration = Math.max(secPerImage, crossfadeDur + 0.1);

      const ffArgs = [];
      // input args: each image looped for imgDuration
      for (const img of imageFiles) {
        ffArgs.push('-loop', '1', '-t', String(imgDuration), '-i', img);
      }
      // audio input
      ffArgs.push('-i', audioFile);
      const audioIdx = imageFiles.length;

      // build filter_complex
      let filterParts = [];
      // scale each image
      for (let i = 0; i < imageFiles.length; i++) {
        filterParts.push(
          `[${i}]scale=w='if(gt(iw,ih),${targetW},-2)':h='if(gt(iw,ih),-2,${targetH})',pad=${targetW}:${targetH}:(ow-iw)/2:(oh-ih)/2:black,setsar=1[s${i}]`
        );
      }

      // chain xfade transitions
      let prevLabel = 's0';
      for (let i = 1; i < imageFiles.length; i++) {
        const offset = Math.max(0, imgDuration * i - crossfadeDur * i);
        const outLabel = i === imageFiles.length - 1 ? 'v' : `x${i}`;
        filterParts.push(
          `[${prevLabel}][s${i}]xfade=transition=fade:duration=${crossfadeDur}:offset=${offset.toFixed(3)}[${outLabel}]`
        );
        prevLabel = outLabel;
      }

      ffArgs.push('-filter_complex', filterParts.join(';'));
      ffArgs.push('-map', '[v]', '-map', `${audioIdx}:a`);
      ffArgs.push('-shortest', '-r', '30', '-pix_fmt', 'yuv420p');
      ffArgs.push('-c:v', 'libx264', '-preset', 'fast', '-crf', '23');
      ffArgs.push('-c:a', 'aac', '-b:a', '192k');
      ffArgs.push(outputFile);

      await runFfmpeg(ffArgs, downloadId, processInfo);
    }

    if (!fs.existsSync(outputFile)) {
      throw new Error('Slideshow creation failed - output file not found');
    }

    // step 6: send the mp4
    await sendSingleFile(res, outputFile, filename, downloadId, url, req, cleanup);

  } catch (err) {
    console.error(`[${downloadId}] Slideshow error:`, err.message);
    discordAlerts.galleryError('Slideshow Error', 'Slideshow creation failed.', { jobId: downloadId, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Slideshow creation failed');
    }

    releaseJob(downloadId);
    console.log(`[Queue] Slideshow error.`);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Slideshow creation failed' });
    }
  }
});

function runFfmpeg(args, downloadId, processInfo) {
  return new Promise((resolve, reject) => {
    console.log(`[${downloadId}] ffmpeg starting slideshow render`);
    const proc = spawn('ffmpeg', ['-y', ...args]);
    processInfo.process = proc;

    let stderr = '';
    proc.stderr.on('data', (data) => {
      stderr += data.toString();
      if (stderr.length > 10000) stderr = stderr.slice(-5000);
    });

    const timeout = setTimeout(() => {
      proc.kill('SIGTERM');
      reject(new Error('ffmpeg slideshow timeout (5 min)'));
    }, 300000);

    proc.on('close', (code) => {
      clearTimeout(timeout);
      if (processInfo.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code === 0) {
        resolve();
      } else {
        console.error(`[${downloadId}] ffmpeg error: ${stderr.slice(-500)}`);
        reject(new Error('Failed to create slideshow video'));
      }
    });

    proc.on('error', (err) => {
      clearTimeout(timeout);
      reject(err);
    });
  });
}

module.exports = router;
