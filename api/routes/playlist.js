const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const { TEMP_DIRS, SAFETY_LIMITS } = require('../config/constants');
const {
  activeDownloads,
  activeProcesses,
  activeJobsByType,
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

router.get('/api/download-playlist', async (req, res) => {
  const {
    url, format = 'video', filename, quality = '1080p', container = 'mp4',
    audioFormat = 'mp3', audioBitrate = '320', progressId, clientId
  } = req.query;

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) return res.status(400).json({ error: urlCheck.error });

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      return res.status(429).json({ error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.` });
    }
  }

  const downloadId = progressId || uuidv4();
  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  activeJobsByType.playlist++;
  console.log(`[Queue] Playlist started. Active: ${JSON.stringify(activeJobsByType)}`);

  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;
  const playlistDir = path.join(TEMP_DIRS.playlist, downloadId);
  if (!fs.existsSync(playlistDir)) fs.mkdirSync(playlistDir, { recursive: true });

  const processInfo = { cancelled: false, process: null, tempDir: playlistDir };
  activeProcesses.set(downloadId, processInfo);
  sendProgress(downloadId, 'starting', 'Getting playlist info...');

  try {
    const playlistInfo = await getPlaylistInfo(url);

    if (playlistInfo.count > SAFETY_LIMITS.maxPlaylistVideos) {
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      activeProcesses.delete(downloadId);
      return res.status(400).json({ error: `Playlist too large. Maximum ${SAFETY_LIMITS.maxPlaylistVideos} videos allowed. This playlist has ${playlistInfo.count} videos.` });
    }

    const totalVideos = playlistInfo.count;
    const playlistTitle = playlistInfo.title;
    const isChunked = totalVideos > SAFETY_LIMITS.playlistChunkSize;
    const totalChunks = Math.ceil(totalVideos / SAFETY_LIMITS.playlistChunkSize);

    sendProgress(downloadId, 'playlist-info', `Found ${totalVideos} videos in playlist${isChunked ? ` (processing in ${totalChunks} chunks of ${SAFETY_LIMITS.playlistChunkSize})` : ''}`, 0, {
      playlistTitle, totalVideos, currentVideo: 0, currentVideoTitle: '',
      format: isAudio ? audioFormat : `${quality} ${container}`,
      isChunked, totalChunks, currentChunk: isChunked ? 1 : null
    });

    const downloadedFiles = [];
    const failedVideos = [];

    for (let i = 0; i < playlistInfo.entries.length; i++) {
      if (isChunked && i > 0 && i % SAFETY_LIMITS.playlistChunkSize === 0) {
        const currentChunk = Math.floor(i / SAFETY_LIMITS.playlistChunkSize) + 1;
        sendProgress(downloadId, 'chunk-pause', `Chunk ${currentChunk - 1}/${totalChunks} complete. Starting chunk ${currentChunk}...`,
          (i / totalVideos) * 100, { playlistTitle, totalVideos, currentVideo: i, currentChunk, totalChunks });
        await new Promise(resolve => setTimeout(resolve, 2000));
      }

      if (processInfo.cancelled) throw new Error('Download cancelled');
      if (processInfo.finishEarly) {
        console.log(`[${downloadId}] Finishing early after ${downloadedFiles.length} videos`);
        break;
      }

      const entry = playlistInfo.entries[i];
      const videoNum = i + 1;
      const videoTitle = entry.title || `Video ${videoNum}`;
      const videoUrl = entry.url || (entry.id ? `https://www.youtube.com/watch?v=${entry.id}` : null);
      if (!videoUrl && !entry.id) continue;

      const safeTitle = sanitizeFilename(videoTitle).substring(0, 100);
      const videoFile = path.join(playlistDir, `${String(videoNum).padStart(3, '0')} - ${safeTitle}.${outputExt}`);
      const currentChunk = isChunked ? Math.floor(i / SAFETY_LIMITS.playlistChunkSize) + 1 : null;
      const chunkLabel = isChunked ? `[Chunk ${currentChunk}/${totalChunks}] ` : '';

      sendProgress(downloadId, 'downloading', `${chunkLabel}Downloading ${videoNum}/${totalVideos}: ${videoTitle}`,
        ((videoNum - 1) / totalVideos) * 100, {
          playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle,
          format: isAudio ? audioFormat : `${quality} ${container}`,
          isChunked, currentChunk, totalChunks
        });

      try {
        const actualVideoUrl = videoUrl || `https://www.youtube.com/watch?v=${entry.id}`;
        const isYouTubeVideo = actualVideoUrl.includes('youtube.com') || actualVideoUrl.includes('youtu.be');
        let tempPath = null;

        if (isYouTubeVideo) {
          const videoJobId = `${downloadId}-v${videoNum}`;
          const abortController = new AbortController();
          processInfo.abortController = abortController;

          try {
            const cobaltResult = await downloadViaCobalt(actualVideoUrl, videoJobId, isAudio,
              (progress) => {
                const overallProgress = ((videoNum - 1) / totalVideos * 100) + (progress / totalVideos);
                sendProgress(downloadId, 'downloading', `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${progress}%)`,
                  overallProgress, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle, videoProgress: progress,
                    format: isAudio ? audioFormat : `${quality} ${container}`, failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length });
              }, abortController.signal, { outputDir: playlistDir, maxRetries: 3, retryDelay: 2000 });
            tempPath = cobaltResult.filePath;
          } catch (cobaltErr) {
            if (cobaltErr.message === 'Cancelled') throw cobaltErr;
            failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(cobaltErr.message) });
            sendProgress(downloadId, 'downloading', `Skipped ${videoNum}/${totalVideos}: ${videoTitle}`,
              ((videoNum - 1) / totalVideos) * 100, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle,
                format: isAudio ? audioFormat : `${quality} ${container}`, failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length });
            continue;
          }
        }

        if (!tempPath && !isYouTubeVideo) {
          const tempVideoFile = path.join(playlistDir, `temp_${videoNum}.%(ext)s`);
          const result = await downloadViaYtdlp(actualVideoUrl, `temp_${videoNum}`, {
            isAudio, quality, container,
            tempDir: playlistDir,
            processInfo,
            onProgress: (progress, speed, eta) => {
              const overallProgress = ((videoNum - 1) / totalVideos * 100) + (progress / totalVideos);
              sendProgress(downloadId, 'downloading', `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${progress.toFixed(0)}%)`,
                overallProgress, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle, videoProgress: progress, speed, eta });
            }
          });
          tempPath = result.path;
        }

        if (tempPath && fs.existsSync(tempPath)) {
          sendProgress(downloadId, 'processing', `Processing ${videoNum}/${totalVideos}: ${videoTitle}`,
            ((videoNum - 0.5) / totalVideos) * 100, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle, format: isAudio ? audioFormat : container });

          const processed = await processVideo(tempPath, videoFile, {
            isAudio, audioFormat, audioBitrate, container, jobId: downloadId
          });

          if (processed.skipped && tempPath !== videoFile) {
            fs.renameSync(tempPath, videoFile);
          }

          downloadedFiles.push(videoFile);
          console.log(`[${downloadId}] Video ${videoNum} complete: ${safeTitle}`);
        }
      } catch (err) {
        console.error(`[${downloadId}] Error downloading video ${videoNum}:`, err.message);
        failedVideos.push({ num: videoNum, title: videoTitle, reason: toUserError(err.message) });
        sendProgress(downloadId, 'downloading', `Skipped ${videoNum}/${totalVideos}: ${videoTitle}`,
          ((videoNum - 1) / totalVideos) * 100, { playlistTitle, totalVideos, currentVideo: videoNum, currentVideoTitle: videoTitle,
            format: isAudio ? audioFormat : `${quality} ${container}`, failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length });
        if (err.message === 'Cancelled' || err.message === 'Download cancelled') throw err;
      }
    }

    if (downloadedFiles.length === 0) throw new Error('No videos were successfully downloaded');

    sendProgress(downloadId, 'zipping', `Creating zip file with ${downloadedFiles.length} videos...`, 95, {
      playlistTitle, totalVideos, downloadedCount: downloadedFiles.length, format: isAudio ? audioFormat : `${quality} ${container}`
    });

    const zipPath = path.join(TEMP_DIRS.playlist, `${downloadId}.zip`);
    const safePlaylistName = sanitizeFilename(playlistTitle || filename || 'playlist');

    await new Promise((resolve, reject) => {
      const output = fs.createWriteStream(zipPath);
      const archive = archiver('zip', { zlib: { level: 5 } });
      output.on('close', resolve);
      archive.on('error', reject);
      archive.pipe(output);
      downloadedFiles.forEach(filePath => archive.file(filePath, { name: path.basename(filePath) }));
      archive.finalize();
    });

    sendProgress(downloadId, 'sending', 'Sending zip file to you...');

    const stat = fs.statSync(zipPath);
    const zipFilename = `${safePlaylistName}.zip`;

    res.setHeader('Content-Type', 'application/zip');
    res.setHeader('Content-Length', stat.size);
    res.setHeader('Content-Disposition', `attachment; filename="${zipFilename}"; filename*=UTF-8''${encodeURIComponent(zipFilename)}`);

    const stream = fs.createReadStream(zipPath);
    stream.pipe(res);

    stream.on('close', () => {
      sendProgress(downloadId, 'complete', `Downloaded ${downloadedFiles.length} videos!`, 100, {
        playlistTitle, totalVideos, downloadedCount: downloadedFiles.length, failedVideos: failedVideos.slice(-50), failedCount: failedVideos.length
      });
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Playlist finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    });

    stream.on('error', (err) => {
      console.error(`[${downloadId}] Stream error:`, err);
      discordAlerts.fileSendFailed('Playlist Stream Error', 'Failed to send zip file to client.', { jobId: downloadId, error: err.message });
      sendProgress(downloadId, 'error', 'Failed to send zip file');
      activeProcesses.delete(downloadId);
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    });

  } catch (err) {
    console.error(`[${downloadId}] Playlist error:`, err.message);
    discordAlerts.downloadFailed('Playlist Download Error', 'Playlist download failed.', { jobId: downloadId, url, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', toUserError(err.message || 'Playlist download failed'));
    }

    activeProcesses.delete(downloadId);
    activeJobsByType.playlist--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Playlist error. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: toUserError(err.message || 'Playlist download failed') });
    }
  }
});

module.exports = router;
