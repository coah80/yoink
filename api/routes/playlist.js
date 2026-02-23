const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const { TEMP_DIRS, SAFETY_LIMITS, PLAYLIST_DOWNLOAD_EXPIRY } = require('../config/constants');
const {
  activeDownloads,
  activeProcesses,
  activeJobsByType,
  asyncJobs,
  botDownloads,
  registerClient,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount,
  sendProgress
} = require('../services/state');

const { downloadViaCobalt } = require('../services/cobalt');
const { downloadViaYtdlp, getPlaylistInfo } = require('../services/downloader');
const { processVideo } = require('../services/processor');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const discordAlerts = require('../discord-alerts');

router.post('/api/playlist/start', express.json(), async (req, res) => {
  const {
    url, format = 'video', quality = '1080p', container = 'mp4',
    audioFormat = 'mp3', audioBitrate = '320', clientId
  } = req.body;

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) return res.status(400).json({ error: urlCheck.error });

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      return res.status(429).json({ error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.` });
    }
  }

  const jobId = uuidv4();
  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(jobId, clientId);
  }

  const job = {
    status: 'starting',
    progress: 0,
    message: 'getting playlist info...',
    createdAt: Date.now(),
    url,
    format: outputExt,
    type: 'playlist',
    playlistTitle: null,
    totalVideos: 0,
    videosCompleted: 0,
    currentVideo: 0,
    currentVideoTitle: '',
    failedVideos: [],
    failedCount: 0,
    downloadToken: null,
    fileName: null,
    fileSize: null
  };

  asyncJobs.set(jobId, job);
  res.json({ jobId });

  processPlaylistAsync(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate);
});

async function processPlaylistAsync(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate) {
  const playlistDir = path.join(TEMP_DIRS.playlist, jobId);
  if (!fs.existsSync(playlistDir)) fs.mkdirSync(playlistDir, { recursive: true });

  const processInfo = { cancelled: false, process: null, tempDir: playlistDir };
  activeProcesses.set(jobId, processInfo);
  activeJobsByType.playlist++;
  console.log(`[Queue] Async playlist started. Active: ${JSON.stringify(activeJobsByType)}`);

  try {
    const playlistInfo = await getPlaylistInfo(url);

    if (playlistInfo.count > SAFETY_LIMITS.maxPlaylistVideos) {
      throw new Error(`Playlist too large. Maximum ${SAFETY_LIMITS.maxPlaylistVideos} videos allowed. This playlist has ${playlistInfo.count} videos.`);
    }

    const totalVideos = playlistInfo.count;
    const playlistTitle = playlistInfo.title;

    job.status = 'downloading';
    job.playlistTitle = playlistTitle;
    job.totalVideos = totalVideos;
    job.message = `found ${totalVideos} videos`;

    sendProgress(jobId, 'playlist-info', `Found ${totalVideos} videos in playlist`, 0, {
      playlistTitle, totalVideos, currentVideo: 0, currentVideoTitle: '',
      format: isAudio ? audioFormat : `${quality} ${container}`
    });

    const downloadedFiles = [];
    const failedVideos = [];

    for (let i = 0; i < playlistInfo.entries.length; i++) {
      if (processInfo.cancelled) throw new Error('Download cancelled');
      if (processInfo.finishEarly) {
        console.log(`[${jobId}] Finishing early after ${downloadedFiles.length} videos`);
        break;
      }

      const entry = playlistInfo.entries[i];
      const videoNum = i + 1;
      const videoTitle = entry.title || `Video ${videoNum}`;
      const videoUrl = entry.url || (entry.id ? `https://www.youtube.com/watch?v=${entry.id}` : null);
      if (!videoUrl && !entry.id) continue;

      const safeTitle = sanitizeFilename(videoTitle).substring(0, 100);
      const videoFile = path.join(playlistDir, `${String(videoNum).padStart(3, '0')} - ${safeTitle}.${outputExt}`);

      job.currentVideo = videoNum;
      job.currentVideoTitle = videoTitle;
      job.progress = ((videoNum - 1) / totalVideos) * 100;
      job.message = `downloading ${videoNum}/${totalVideos}: ${videoTitle}`;

      sendProgress(jobId, 'downloading', `Downloading ${videoNum}/${totalVideos}: ${videoTitle}`,
        job.progress, {
          playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle,
          format: isAudio ? audioFormat : `${quality} ${container}`,
          failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length
        });

      try {
        const actualVideoUrl = videoUrl || `https://www.youtube.com/watch?v=${entry.id}`;
        const isYouTubeVideo = actualVideoUrl.includes('youtube.com') || actualVideoUrl.includes('youtu.be');
        let tempPath = null;

        if (isYouTubeVideo) {
          const videoJobId = `${jobId}-v${videoNum}`;
          const abortController = new AbortController();
          processInfo.abortController = abortController;

          try {
            const cobaltResult = await downloadViaCobalt(actualVideoUrl, videoJobId, isAudio,
              (progress) => {
                const overallProgress = ((videoNum - 1) / totalVideos * 100) + (progress / totalVideos);
                job.progress = overallProgress;
                sendProgress(jobId, 'downloading', `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${progress}%)`,
                  overallProgress, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle, videoProgress: progress,
                    format: isAudio ? audioFormat : `${quality} ${container}`, failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length });
              }, abortController.signal, { outputDir: playlistDir, maxRetries: 3, retryDelay: 2000 });
            tempPath = cobaltResult.filePath;
          } catch (cobaltErr) {
            if (cobaltErr.message === 'Cancelled') throw cobaltErr;
            failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(cobaltErr.message) });
            job.failedVideos = failedVideos.slice(-50);
            job.failedCount = failedVideos.length;
            continue;
          }
        }

        if (!tempPath && !isYouTubeVideo) {
          const result = await downloadViaYtdlp(actualVideoUrl, `temp_${videoNum}`, {
            isAudio, quality, container,
            tempDir: playlistDir,
            processInfo,
            onProgress: (progress, speed, eta) => {
              const overallProgress = ((videoNum - 1) / totalVideos * 100) + (progress / totalVideos);
              job.progress = overallProgress;
              if (speed) job.speed = speed;
              if (eta) job.eta = eta;
              sendProgress(jobId, 'downloading', `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${progress.toFixed(0)}%)`,
                overallProgress, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle, videoProgress: progress, speed, eta });
            }
          });
          tempPath = result.path;
        }

        if (tempPath && fs.existsSync(tempPath)) {
          sendProgress(jobId, 'processing', `Processing ${videoNum}/${totalVideos}: ${videoTitle}`,
            ((videoNum - 0.5) / totalVideos) * 100, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle });

          const processed = await processVideo(tempPath, videoFile, {
            isAudio, audioFormat, audioBitrate, container, jobId
          });

          if (processed.skipped && tempPath !== videoFile) {
            fs.renameSync(tempPath, videoFile);
          }

          downloadedFiles.push(videoFile);
          job.videosCompleted = downloadedFiles.length;
          console.log(`[${jobId}] Video ${videoNum} complete`);
        }
      } catch (err) {
        console.error(`[${jobId}] Error downloading video ${videoNum}:`, err.message);
        failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(err.message) });
        job.failedVideos = failedVideos.slice(-50);
        job.failedCount = failedVideos.length;
        if (err.message === 'Cancelled' || err.message === 'Download cancelled') throw err;
      }
    }

    if (downloadedFiles.length === 0) throw new Error('No videos were successfully downloaded');

    job.status = 'zipping';
    job.progress = 95;
    job.message = `creating zip with ${downloadedFiles.length} videos...`;

    sendProgress(jobId, 'zipping', `Creating zip file with ${downloadedFiles.length} videos...`, 95, {
      playlistTitle, totalVideos, downloadedCount: downloadedFiles.length
    });

    const zipPath = path.join(TEMP_DIRS.playlist, `${jobId}.zip`);
    const safePlaylistName = sanitizeFilename(playlistTitle || 'playlist');

    await new Promise((resolve, reject) => {
      const output = fs.createWriteStream(zipPath);
      const archive = archiver('zip', { zlib: { level: 5 } });
      output.on('close', resolve);
      archive.on('error', reject);
      archive.pipe(output);
      downloadedFiles.forEach(filePath => archive.file(filePath, { name: path.basename(filePath) }));
      archive.finalize();
    });

    try {
      if (fs.existsSync(playlistDir)) fs.rmSync(playlistDir, { recursive: true, force: true });
    } catch {}

    const stat = fs.statSync(zipPath);
    const downloadToken = crypto.randomBytes(32).toString('hex');
    const fileName = `${safePlaylistName}.zip`;

    botDownloads.set(downloadToken, {
      filePath: zipPath, fileName, fileSize: stat.size,
      mimeType: 'application/zip', createdAt: Date.now(),
      downloaded: false, isWebPlaylist: true
    });

    job.status = 'complete';
    job.progress = 100;
    job.message = `${downloadedFiles.length} videos ready to download`;
    job.downloadToken = downloadToken;
    job.fileName = fileName;
    job.fileSize = stat.size;
    job.failedVideos = failedVideos.slice(-50);
    job.failedCount = failedVideos.length;

    sendProgress(jobId, 'complete', `${downloadedFiles.length} videos ready!`, 100, {
      playlistTitle, totalVideos, downloadedCount: downloadedFiles.length,
      failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length,
      downloadToken
    });

    activeProcesses.delete(jobId);
    activeJobsByType.playlist--;
    unlinkJobFromClient(jobId);
    console.log(`[Queue] Async playlist complete. Active: ${JSON.stringify(activeJobsByType)}`);

  } catch (err) {
    console.error(`[${jobId}] Async playlist error:`, err.message);
    discordAlerts.downloadFailed('Playlist Download Error', 'Playlist download failed.', { jobId, error: err.message });

    job.status = 'error';
    job.message = toUserError(err.message || 'Playlist download failed');

    if (!processInfo.cancelled) {
      sendProgress(jobId, 'error', toUserError(err.message || 'Playlist download failed'));
    }

    activeProcesses.delete(jobId);
    activeJobsByType.playlist--;
    unlinkJobFromClient(jobId);
    console.log(`[Queue] Async playlist error. Active: ${JSON.stringify(activeJobsByType)}`);

    try {
      if (fs.existsSync(playlistDir)) fs.rmSync(playlistDir, { recursive: true, force: true });
    } catch {}
  }
}

router.get('/api/playlist/status/:jobId', (req, res) => {
  const { jobId } = req.params;
  const job = asyncJobs.get(jobId);
  if (!job) return res.status(404).json({ error: 'Job not found' });

  res.json({
    status: job.status,
    progress: job.progress,
    message: job.message,
    playlistTitle: job.playlistTitle,
    totalVideos: job.totalVideos,
    videosCompleted: job.videosCompleted,
    currentVideo: job.currentVideo,
    currentVideoTitle: job.currentVideoTitle,
    failedVideos: job.failedVideos,
    failedCount: job.failedCount,
    downloadToken: job.downloadToken,
    fileName: job.fileName,
    fileSize: job.fileSize,
    speed: job.speed,
    eta: job.eta
  });
});

router.get('/api/playlist/download/:token', (req, res) => {
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
        console.log(`[Playlist] Token ${token.slice(0, 8)}... cleaned up after download`);
      }
    }, 30000);
  });
});

setInterval(() => {
  const now = Date.now();
  for (const [token, data] of botDownloads.entries()) {
    if (data.isWebPlaylist && now - data.createdAt > PLAYLIST_DOWNLOAD_EXPIRY) {
      console.log(`[Playlist] Download token ${token.slice(0, 8)}... expired`);
      if (data.filePath && fs.existsSync(data.filePath)) {
        fs.unlink(data.filePath, () => {});
      }
      botDownloads.delete(token);
    }
  }
}, 60000);

module.exports = router;
