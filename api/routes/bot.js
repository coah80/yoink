const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { spawnSync } = require('child_process');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const {
  TEMP_DIRS, SAFETY_LIMITS, CONTAINER_MIMES, AUDIO_MIMES,
  BOT_SECRET, BOT_DOWNLOAD_EXPIRY, ASYNC_JOB_TIMEOUT
} = require('../config/constants');

const { asyncJobs, botDownloads } = require('../services/state');
const { downloadViaCobalt } = require('../services/cobalt');
const { downloadViaYtdlp, getPlaylistInfo, handleClipDownload } = require('../services/downloader');
const { processVideo } = require('../services/processor');
const { parseYouTubeClip } = require('../services/youtube');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { sanitizeFilename } = require('../utils/files');

setInterval(() => {
  const now = Date.now();
  for (const [token, data] of botDownloads.entries()) {
    if (now - data.createdAt > BOT_DOWNLOAD_EXPIRY) {
      console.log(`[Bot] Download token ${token.slice(0, 8)}... expired`);
      if (data.filePath && fs.existsSync(data.filePath)) {
        fs.unlink(data.filePath, () => {});
      }
      botDownloads.delete(token);
    }
  }
  for (const [jobId, job] of asyncJobs.entries()) {
    if (now - job.createdAt > ASYNC_JOB_TIMEOUT) {
      console.log(`[Bot] Job ${jobId.slice(0, 8)}... expired (${job.status})`);
      asyncJobs.delete(jobId);
    }
  }
}, 30000);

function checkBotAuth(req) {
  const authHeader = req.headers.authorization;
  return authHeader && authHeader === `Bearer ${BOT_SECRET}`;
}

router.post('/api/bot/download', express.json(), async (req, res) => {
  if (!checkBotAuth(req)) return res.status(401).json({ error: 'Unauthorized' });

  const { url, format = 'video', quality = '1080p', container = 'mp4', audioFormat = 'mp3', playlist = false } = req.body;
  if (!url) return res.status(400).json({ error: 'URL required' });

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) return res.status(400).json({ error: urlCheck.error });

  const jobId = uuidv4();
  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;

  const job = {
    status: 'starting', progress: 0, message: 'Initializing download...',
    createdAt: Date.now(), url, format: outputExt,
    filePath: null, fileName: null, fileSize: null, downloadToken: null
  };
  asyncJobs.set(jobId, job);
  res.json({ jobId });

  processBotDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, playlist);
});

async function processBotDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, playlist) {
  try {
    job.status = 'downloading';
    job.message = 'Downloading from source...';

    const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');
    let downloadedPath = null;
    let downloadedExt = null;

    if (isYouTube) {
      const isClip = url.includes('/clip/');

      if (isClip) {
        job.message = 'Parsing clip...';
        const clipData = await parseYouTubeClip(url);
        job.message = 'Stream trimming clip...';
        const result = await handleClipDownload(clipData, jobId, {
          tempDir: TEMP_DIRS.bot,
          onProgress: (progress) => {
            job.progress = progress;
            job.message = `Trimming... ${progress}%`;
          }
        });
        downloadedPath = result.path;
        downloadedExt = result.ext;
        job.progress = 100;
      } else {
        job.message = 'Downloading via Cobalt...';
        const cobaltResult = await downloadViaCobalt(url, jobId, isAudio, (progress) => {
          job.progress = progress;
        });
        downloadedPath = cobaltResult.filePath;
        downloadedExt = cobaltResult.ext;
        job.progress = 100;
      }
    } else {
      const result = await downloadViaYtdlp(url, jobId, {
        isAudio, audioFormat, quality, container,
        tempDir: TEMP_DIRS.bot,
        filePrefix: 'bot-',
        playlist,
        onProgress: (progress, speed, eta) => {
          job.progress = progress;
          job.message = `Downloading... ${progress.toFixed(0)}%`;
          if (speed) job.speed = speed;
          if (eta) job.eta = eta;
        }
      });
      downloadedPath = result.path;
      downloadedExt = result.ext;
    }

    if (!downloadedPath || !fs.existsSync(downloadedPath)) {
      throw new Error('Downloaded file not found');
    }

    job.status = 'processing';
    job.progress = 100;
    job.message = 'Processing...';

    const finalFile = path.join(TEMP_DIRS.bot, `bot-${jobId}-final.${outputExt}`);
    const processed = await processVideo(downloadedPath, finalFile, {
      isAudio, audioFormat, container, jobId
    });

    const actualFinalFile = processed.skipped ? processed.path : finalFile;
    if (!processed.skipped) {
      try { fs.unlinkSync(downloadedPath); } catch {}
    }

    if (!fs.existsSync(actualFinalFile)) {
      throw new Error('Downloaded file not found after processing');
    }

    const stat = fs.statSync(actualFinalFile);
    const downloadToken = crypto.randomBytes(32).toString('hex');

    let title = 'download';
    try {
      const infoResult = spawnSync('yt-dlp', ['--print', 'title', '--no-playlist', url], { timeout: 10000 });
      if (infoResult.status === 0) title = infoResult.stdout.toString().trim().slice(0, 100);
    } catch {}

    const fileName = sanitizeFilename(title) + '.' + (processed.skipped ? downloadedExt : outputExt);

    botDownloads.set(downloadToken, {
      filePath: actualFinalFile, fileName, fileSize: stat.size,
      mimeType: isAudio ? (AUDIO_MIMES[audioFormat] || 'audio/mpeg') : (CONTAINER_MIMES[container] || 'video/mp4'),
      createdAt: Date.now(), downloaded: false
    });

    job.status = 'complete';
    job.progress = 100;
    job.message = 'Ready for download';
    job.filePath = actualFinalFile;
    job.fileName = fileName;
    job.fileSize = stat.size;
    job.downloadToken = downloadToken;

    console.log(`[Bot] Job ${jobId} complete, token: ${downloadToken.slice(0, 8)}...`);

  } catch (err) {
    console.error(`[Bot] Job ${jobId} failed:`, err.message);
    job.status = 'error';
    job.message = toUserError(err.message);
    job.debugError = err.message;

    const files = fs.readdirSync(TEMP_DIRS.bot);
    files.filter(f => f.includes(jobId)).forEach(f => {
      try { fs.unlinkSync(path.join(TEMP_DIRS.bot, f)); } catch {}
    });
  }
}

router.post('/api/bot/download-playlist', express.json(), async (req, res) => {
  if (!checkBotAuth(req)) return res.status(401).json({ error: 'Unauthorized' });

  const { url, format = 'video', quality = '1080p', container = 'mp4', audioFormat = 'mp3', audioBitrate = '320' } = req.body;
  if (!url) return res.status(400).json({ error: 'URL required' });

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) return res.status(400).json({ error: urlCheck.error });

  const jobId = uuidv4();
  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;

  const job = {
    status: 'starting', progress: 0, message: 'Getting playlist info...',
    createdAt: Date.now(), url, format: outputExt,
    filePath: null, fileName: null, fileSize: null, downloadToken: null,
    playlistInfo: null, videosCompleted: 0, totalVideos: 0, failedVideos: []
  };
  asyncJobs.set(jobId, job);
  res.json({ jobId });

  processBotPlaylistDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate);
});

async function processBotPlaylistDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate) {
  const playlistDir = path.join(TEMP_DIRS.bot, `playlist-${jobId}`);

  try {
    if (!fs.existsSync(playlistDir)) fs.mkdirSync(playlistDir, { recursive: true });

    const playlistInfo = await getPlaylistInfo(url);
    if (playlistInfo.count > SAFETY_LIMITS.maxPlaylistVideos) {
      throw new Error(`Playlist too large. Maximum ${SAFETY_LIMITS.maxPlaylistVideos} videos allowed. This playlist has ${playlistInfo.count} videos.`);
    }

    job.totalVideos = playlistInfo.count;
    job.playlistInfo = { title: playlistInfo.title, count: playlistInfo.count };
    job.message = `Found ${playlistInfo.count} videos`;
    job.status = 'downloading';

    const downloadedFiles = [];
    const failedVideos = [];

    for (let i = 0; i < playlistInfo.entries.length; i++) {
      const entry = playlistInfo.entries[i];
      const videoNum = i + 1;
      const videoTitle = entry.title || `Video ${videoNum}`;
      const videoUrl = entry.url || (entry.id ? `https://www.youtube.com/watch?v=${entry.id}` : null);
      if (!videoUrl && !entry.id) continue;

      job.message = `Downloading ${videoNum}/${playlistInfo.count}: ${videoTitle}`;
      job.progress = Math.round((videoNum / playlistInfo.count) * 90);

      const safeTitle = sanitizeFilename(videoTitle).substring(0, 100);
      const videoFile = path.join(playlistDir, `${String(videoNum).padStart(3, '0')} - ${safeTitle}.${outputExt}`);

      try {
        const actualVideoUrl = videoUrl || `https://www.youtube.com/watch?v=${entry.id}`;
        const isYouTubeVideo = actualVideoUrl.includes('youtube.com') || actualVideoUrl.includes('youtu.be');
        let tempPath = null;

        if (isYouTubeVideo) {
          const videoJobId = `${jobId}-v${videoNum}`;
          try {
            const cobaltResult = await downloadViaCobalt(actualVideoUrl, videoJobId, isAudio, null, null, { outputDir: playlistDir, maxRetries: 2, retryDelay: 1000 });
            tempPath = cobaltResult.filePath;
          } catch (cobaltErr) {
            failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(cobaltErr.message) });
            job.failedVideos = failedVideos.slice(-50);
            continue;
          }
        }

        if (!tempPath && !isYouTubeVideo) {
          const result = await downloadViaYtdlp(actualVideoUrl, `temp_${videoNum}`, {
            isAudio, quality, container,
            tempDir: playlistDir,
            onProgress: (progress, speed, eta) => {
              job.progress = Math.round(((videoNum - 1 + progress / 100) / playlistInfo.count) * 90);
              if (speed) job.speed = speed;
              if (eta) job.eta = eta;
            }
          });
          tempPath = result.path;
        }

        if (tempPath && fs.existsSync(tempPath)) {
          const processed = await processVideo(tempPath, videoFile, {
            isAudio, audioFormat, audioBitrate, container, jobId
          });
          if (processed.skipped && tempPath !== videoFile) {
            fs.renameSync(tempPath, videoFile);
          }
          downloadedFiles.push(videoFile);
          job.videosCompleted = downloadedFiles.length;
        }
      } catch (err) {
        failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(err.message) });
        job.failedVideos = failedVideos.slice(-50);
      }
    }

    if (downloadedFiles.length === 0) throw new Error('No videos were successfully downloaded');

    job.message = 'Creating zip file...';
    job.progress = 95;

    const zipPath = path.join(TEMP_DIRS.bot, `playlist-${jobId}.zip`);
    const safePlaylistName = sanitizeFilename(playlistInfo.title || 'playlist');

    await new Promise((resolve, reject) => {
      const output = fs.createWriteStream(zipPath);
      const archive = archiver('zip', { zlib: { level: 5 } });
      output.on('close', resolve);
      archive.on('error', reject);
      archive.pipe(output);
      downloadedFiles.forEach(filePath => archive.file(filePath, { name: path.basename(filePath) }));
      archive.finalize();
    });

    const stat = fs.statSync(zipPath);
    const downloadToken = crypto.randomBytes(32).toString('hex');
    const fileName = `${safePlaylistName}.zip`;

    botDownloads.set(downloadToken, {
      filePath: zipPath, fileName, fileSize: stat.size,
      mimeType: 'application/zip', createdAt: Date.now(), downloaded: false, isPlaylist: true
    });

    job.status = 'complete';
    job.progress = 100;
    job.message = `Ready for download (${downloadedFiles.length} videos)`;
    job.filePath = zipPath;
    job.fileName = fileName;
    job.fileSize = stat.size;
    job.downloadToken = downloadToken;
    job.videosCompleted = downloadedFiles.length;

    console.log(`[Bot] Playlist job ${jobId} complete, token: ${downloadToken.slice(0, 8)}...`);

    try {
      if (fs.existsSync(playlistDir)) fs.rmSync(playlistDir, { recursive: true, force: true });
    } catch {}

  } catch (err) {
    console.error(`[Bot] Playlist job ${jobId} failed:`, err.message);
    job.status = 'error';
    job.message = toUserError(err.message);
    job.debugError = err.message;

    try {
      if (fs.existsSync(playlistDir)) fs.rmSync(playlistDir, { recursive: true, force: true });
    } catch {}
  }
}

router.get('/api/download/:token', (req, res) => {
  const { token } = req.params;
  const data = botDownloads.get(token);

  if (!data) {
    return res.status(404).send(`<!DOCTYPE html><html><head><title>Download Not Found</title>
      <style>body{font-family:-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:linear-gradient(135deg,#667eea,#764ba2);color:white}
      .container{text-align:center;padding:2rem}h1{font-size:3rem;margin:0}p{font-size:1.2rem;opacity:0.9}</style></head>
      <body><div class="container"><h1>X</h1><h2>Download Not Found</h2><p>This download link has expired or is invalid.</p></div></body></html>`);
  }

  const downloadUrl = `/api/bot/download/${token}`;
  res.send(`<!DOCTYPE html><html><head><title>Downloading...</title><meta name="viewport" content="width=device-width,initial-scale=1">
    <style>body{font-family:-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:linear-gradient(135deg,#667eea,#764ba2);color:white}
    .container{text-align:center;padding:2rem}.spinner{border:4px solid rgba(255,255,255,0.3);border-radius:50%;border-top:4px solid white;width:50px;height:50px;animation:spin 1s linear infinite;margin:0 auto 1.5rem}
    @keyframes spin{0%{transform:rotate(0deg)}100%{transform:rotate(360deg)}}h1{font-size:2rem;margin:0 0 0.5rem}p{font-size:1.1rem;opacity:0.9;margin:0.5rem 0}
    .filename{font-size:0.9rem;opacity:0.7;margin-top:1rem;word-break:break-all;max-width:400px;margin-left:auto;margin-right:auto}</style></head>
    <body><div class="container"><div class="spinner"></div><h1>Downloading...</h1><p>Your download should start automatically.</p>
    <p class="filename">${data.fileName}</p><p style="margin-top:2rem;font-size:0.85rem">This page will close automatically.</p></div>
    <iframe id="downloadFrame" style="display:none"></iframe>
    <script>document.getElementById('downloadFrame').src='${downloadUrl}';setTimeout(()=>{window.close();setTimeout(()=>{document.body.innerHTML='<div class="container"><h1>Done</h1><h2>Download Started</h2><p>You can close this page now.</p></div>'},100)},2000)</script></body></html>`);
});

router.get('/api/bot/download/:token', (req, res) => {
  const { token } = req.params;
  const data = botDownloads.get(token);

  if (!data) return res.status(404).json({ error: 'Download not found or expired' });
  if (!fs.existsSync(data.filePath)) {
    botDownloads.delete(token);
    return res.status(404).json({ error: 'File no longer available' });
  }

  const stat = fs.statSync(data.filePath);
  const asciiFilename = data.fileName.replace(/[^\x20-\x7E]/g, '_');

  res.setHeader('Content-Type', data.mimeType);
  res.setHeader('Content-Length', stat.size);
  res.setHeader('Content-Disposition', `attachment; filename="${asciiFilename}"; filename*=UTF-8''${encodeURIComponent(data.fileName)}`);

  const stream = fs.createReadStream(data.filePath);
  stream.pipe(res);

  stream.on('close', () => {
    data.downloaded = true;
    setTimeout(() => {
      if (botDownloads.has(token)) {
        fs.unlink(data.filePath, () => {});
        botDownloads.delete(token);
        console.log(`[Bot] Token ${token.slice(0, 8)}... cleaned up after download`);
      }
    }, 30000);
  });
});

router.get('/api/bot/status/:jobId', (req, res) => {
  if (!checkBotAuth(req)) return res.status(401).json({ error: 'Unauthorized' });

  const { jobId } = req.params;
  const job = asyncJobs.get(jobId);
  if (!job) return res.status(404).json({ error: 'Job not found' });

  res.json({
    status: job.status, progress: job.progress, message: job.message,
    debugError: job.debugError, fileName: job.fileName, fileSize: job.fileSize,
    downloadToken: job.downloadToken, speed: job.speed, eta: job.eta
  });
});

module.exports = router;
