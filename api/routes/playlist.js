const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const {
  TEMP_DIRS,
  SAFETY_LIMITS,
  QUALITY_HEIGHT
} = require('../config/constants');

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
const { getCookiesArgs } = require('../utils/cookies');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const discordAlerts = require('../discord-alerts');

router.get('/api/download-playlist', async (req, res) => {
  const {
    url,
    format = 'video',
    filename,
    quality = '1080p',
    container = 'mp4',
    audioFormat = 'mp3',
    audioBitrate = '320',
    progressId,
    clientId
  } = req.query;

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

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  activeJobsByType.playlist++;
  console.log(`[Queue] Playlist started. Active: ${JSON.stringify(activeJobsByType)}`);

  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;
  const playlistDir = path.join(TEMP_DIRS.playlist, downloadId);

  if (!fs.existsSync(playlistDir)) {
    fs.mkdirSync(playlistDir, { recursive: true });
  }

  const processInfo = { cancelled: false, process: null, tempDir: playlistDir };
  activeProcesses.set(downloadId, processInfo);

  sendProgress(downloadId, 'starting', 'Getting playlist info...');

  try {
    const playlistInfo = await getPlaylistInfo(url);

    if (playlistInfo.count > SAFETY_LIMITS.maxPlaylistVideos) {
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      activeProcesses.delete(downloadId);
      return res.status(400).json({
        error: `Playlist too large. Maximum ${SAFETY_LIMITS.maxPlaylistVideos} videos allowed. This playlist has ${playlistInfo.count} videos.`
      });
    }

    const totalVideos = playlistInfo.count;
    const playlistTitle = playlistInfo.title;
    const isChunked = totalVideos > SAFETY_LIMITS.playlistChunkSize;
    const totalChunks = Math.ceil(totalVideos / SAFETY_LIMITS.playlistChunkSize);

    sendProgress(downloadId, 'playlist-info', `Found ${totalVideos} videos in playlist${isChunked ? ` (processing in ${totalChunks} chunks of ${SAFETY_LIMITS.playlistChunkSize})` : ''}`, 0, {
      playlistTitle,
      totalVideos,
      currentVideo: 0,
      currentVideoTitle: '',
      format: isAudio ? audioFormat : `${quality} ${container}`,
      isChunked,
      totalChunks,
      currentChunk: isChunked ? 1 : null
    });

    const downloadedFiles = [];
    const failedVideos = [];

    for (let i = 0; i < playlistInfo.entries.length; i++) {
      if (isChunked && i > 0 && i % SAFETY_LIMITS.playlistChunkSize === 0) {
        const currentChunk = Math.floor(i / SAFETY_LIMITS.playlistChunkSize) + 1;
        sendProgress(downloadId, 'chunk-pause', `Chunk ${currentChunk - 1}/${totalChunks} complete. Starting chunk ${currentChunk}...`,
          (i / totalVideos) * 100, {
          playlistTitle,
          totalVideos,
          currentVideo: i,
          currentChunk,
          totalChunks
        });
        await new Promise(resolve => setTimeout(resolve, 2000));
      }

      if (processInfo.cancelled) {
        throw new Error('Download cancelled');
      }

      if (processInfo.finishEarly) {
        console.log(`[${downloadId}] Finishing early after ${downloadedFiles.length} videos`);
        break;
      }

      const entry = playlistInfo.entries[i];
      const videoNum = i + 1;
      const videoTitle = entry.title || `Video ${videoNum}`;
      const videoUrl = entry.url || entry.id ? `https://www.youtube.com/watch?v=${entry.id}` : null;

      if (!videoUrl && !entry.id) {
        console.log(`[${downloadId}] Skipping video ${videoNum}: no URL`);
        continue;
      }

      const safeTitle = sanitizeFilename(videoTitle).substring(0, 100);
      const videoFile = path.join(playlistDir, `${String(videoNum).padStart(3, '0')} - ${safeTitle}.${outputExt}`);
      const tempVideoFile = path.join(playlistDir, `temp_${videoNum}.%(ext)s`);

      const currentChunk = isChunked ? Math.floor(i / SAFETY_LIMITS.playlistChunkSize) + 1 : null;
      const chunkLabel = isChunked ? `[Chunk ${currentChunk}/${totalChunks}] ` : '';

      sendProgress(downloadId, 'downloading', `${chunkLabel}Downloading ${videoNum}/${totalVideos}: ${videoTitle}`,
        ((videoNum - 1) / totalVideos) * 100, {
        playlistTitle,
        totalVideos,
        currentVideo: videoNum,
        currentVideoTitle: videoTitle,
        format: isAudio ? audioFormat : `${quality} ${container}`,
        isChunked,
        currentChunk,
        totalChunks
      });

      try {
        const actualVideoUrl = videoUrl || `https://www.youtube.com/watch?v=${entry.id}`;
        const isYouTubeVideo = actualVideoUrl.includes('youtube.com') || actualVideoUrl.includes('youtu.be');

        let tempPath = null;

        if (isYouTubeVideo) {
          tempPath = await downloadVideoViaCobalt(
            actualVideoUrl, downloadId, videoNum, totalVideos, 
            isAudio, videoTitle, playlistTitle, playlistDir, 
            failedVideos, processInfo, audioFormat, quality, container
          );
          if (!tempPath) continue;
        }

        if (!tempPath && !isYouTubeVideo) {
          tempPath = await downloadVideoViaYtdlp(
            actualVideoUrl, downloadId, videoNum, totalVideos,
            isAudio, videoTitle, playlistTitle, playlistDir,
            tempVideoFile, quality, container, processInfo
          );
        }

        if (tempPath && fs.existsSync(tempPath)) {
          await processVideoWithFfmpeg(
            tempPath, videoFile, downloadId, videoNum, totalVideos,
            isAudio, videoTitle, playlistTitle, audioFormat, audioBitrate, container
          );
          downloadedFiles.push(videoFile);
          console.log(`[${downloadId}] Video ${videoNum} complete: ${safeTitle}`);
        } else {
          console.error(`[${downloadId}] No temp file found for video ${videoNum}`);
        }

      } catch (err) {
        console.error(`[${downloadId}] Error downloading video ${videoNum}:`, err.message);
        const reason = toUserError(err.message);
        failedVideos.push({ num: videoNum, title: videoTitle, reason });
        sendProgress(downloadId, 'downloading',
          `Skipped ${videoNum}/${totalVideos}: ${videoTitle}`,
          ((videoNum - 1) / totalVideos) * 100, {
          playlistTitle,
          totalVideos,
          currentVideo: videoNum,
          currentVideoTitle: videoTitle,
          format: isAudio ? audioFormat : `${quality} ${container}`,
          failedVideos: failedVideos.slice(-50),
          failedCount: failedVideos.length
        });
        if (err.message === 'Cancelled' || err.message === 'Download cancelled') {
          throw err;
        }
      }
    }

    if (downloadedFiles.length === 0) {
      console.error(`[${downloadId}] Playlist failed completely. Errors: ${JSON.stringify(failedVideos)}`);
      throw new Error('No videos were successfully downloaded');
    }

    if (failedVideos.length > 0) {
      console.log(`[${downloadId}] Playlist partially complete: ${downloadedFiles.length}/${totalVideos} videos (${failedVideos.length} failed)`);
    }

    await sendPlaylistZip(
      res, downloadId, downloadedFiles, playlistDir, 
      playlistTitle, filename, totalVideos, isAudio, 
      audioFormat, quality, container, failedVideos
    );

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

async function getPlaylistInfo(url) {
  return new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', [
      ...getCookiesArgs(),
      '-t', 'sleep',
      '--yes-playlist',
      '--flat-playlist',
      '-J',
      url
    ]);

    let output = '';
    let errorOutput = '';

    ytdlp.stdout.on('data', (data) => {
      output += data.toString();
    });

    ytdlp.stderr.on('data', (data) => {
      errorOutput += data.toString();
    });

    ytdlp.on('close', (code) => {
      if (code !== 0) {
        const errorMatch = errorOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        const errorMessage = errorMatch ? errorMatch[1].trim() : 'Failed to get playlist info';
        reject(new Error(errorMessage));
        return;
      }
      try {
        const data = JSON.parse(output);
        resolve({
          title: data.title || 'Playlist',
          entries: data.entries || [],
          count: data.playlist_count || (data.entries ? data.entries.length : 0)
        });
      } catch (e) {
        reject(new Error('Failed to parse playlist info'));
      }
    });

    ytdlp.on('error', reject);
  });
}

async function downloadVideoViaCobalt(
  videoUrl, downloadId, videoNum, totalVideos,
  isAudio, videoTitle, playlistTitle, playlistDir,
  failedVideos, processInfo, audioFormat, quality, container
) {
  const videoJobId = `${downloadId}-v${videoNum}`;
  console.log(`[${downloadId}] Video ${videoNum}: Using Cobalt for YouTube video`);

  const abortController = new AbortController();
  processInfo.abortController = abortController;

  try {
    const cobaltResult = await downloadViaCobalt(
      videoUrl,
      videoJobId,
      isAudio,
      (progress, downloaded, total) => {
        const overallProgress = ((videoNum - 1) / totalVideos * 100) + (progress / totalVideos);
        sendProgress(downloadId, 'downloading',
          `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${progress}%)`,
          overallProgress, {
          playlistTitle,
          totalVideos,
          currentVideo: videoNum,
          currentVideoTitle: videoTitle,
          videoProgress: progress,
          format: isAudio ? audioFormat : `${quality} ${container}`,
          failedVideos: failedVideos.slice(-50),
          failedCount: failedVideos.length
        });
      },
      abortController.signal,
      { outputDir: playlistDir, maxRetries: 3, retryDelay: 2000 }
    );
    return cobaltResult.filePath;
  } catch (cobaltErr) {
    if (cobaltErr.message === 'Cancelled') throw cobaltErr;
    const reason = toUserError(cobaltErr.message);
    failedVideos.push({ num: videoNum, title: videoTitle, reason });
    sendProgress(downloadId, 'downloading',
      `Skipped ${videoNum}/${totalVideos}: ${videoTitle}`,
      ((videoNum - 1) / totalVideos) * 100, {
      playlistTitle,
      totalVideos,
      currentVideo: videoNum,
      currentVideoTitle: videoTitle,
      format: isAudio ? audioFormat : `${quality} ${container}`,
      failedVideos: failedVideos.slice(-50),
      failedCount: failedVideos.length
    });
    console.error(`[${downloadId}] Cobalt failed for video ${videoNum}:`, cobaltErr.message);
    return null;
  }
}

async function downloadVideoViaYtdlp(
  videoUrl, downloadId, videoNum, totalVideos,
  isAudio, videoTitle, playlistTitle, playlistDir,
  tempVideoFile, quality, container, processInfo
) {
  const ytdlpArgs = [
    ...getCookiesArgs(),
    '-t', 'sleep',
    '--no-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s',
    '-o', tempVideoFile,
    '--ffmpeg-location', '/usr/bin/ffmpeg',
  ];

  if (isAudio) {
    ytdlpArgs.push('-f', 'bestaudio/best');
  } else {
    const maxHeight = QUALITY_HEIGHT[quality];
    if (maxHeight) {
      ytdlpArgs.push('-f', `bv[vcodec^=avc][height<=${maxHeight}]+ba[acodec^=mp4a]/bv[height<=${maxHeight}]+ba/b`);
    } else {
      ytdlpArgs.push('-f', 'bv[vcodec^=avc]+ba[acodec^=mp4a]/bv+ba/b');
    }
    ytdlpArgs.push('--merge-output-format', container);
  }

  ytdlpArgs.push(videoUrl);

  await new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);
    processInfo.process = ytdlp;

    let stderrOutput = '';

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString().trim();
      const match = msg.match(/([\d.]+)%/);
      if (match) {
        const videoProgress = parseFloat(match[1]);
        const overallProgress = ((videoNum - 1) / totalVideos * 100) + (videoProgress / totalVideos);
        sendProgress(downloadId, 'downloading',
          `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${videoProgress.toFixed(0)}%)`,
          overallProgress, {
          playlistTitle,
          totalVideos,
          currentVideo: videoNum,
          currentVideoTitle: videoTitle,
          videoProgress
        });
      }
    });

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (msg.includes('[download]') && msg.includes('%')) {
        const match = msg.match(/([\d.]+)%/);
        if (match) {
          const videoProgress = parseFloat(match[1]);
          const overallProgress = ((videoNum - 1) / totalVideos * 100) + (videoProgress / totalVideos);
          sendProgress(downloadId, 'downloading',
            `Downloading ${videoNum}/${totalVideos}: ${videoTitle} (${videoProgress.toFixed(0)}%)`,
            overallProgress, {
            playlistTitle,
            totalVideos,
            currentVideo: videoNum,
            currentVideoTitle: videoTitle,
            videoProgress
          });
        }
      }
    });

    ytdlp.on('close', (code) => {
      if (code === 0) resolve();
      else {
        const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        const errorMessage = errorMatch ? errorMatch[1].trim() : `Failed to download video ${videoNum}`;
        reject(new Error(errorMessage));
      }
    });

    ytdlp.on('error', reject);
  });

  const tempFiles = fs.readdirSync(playlistDir);
  const downloadedTemp = tempFiles.find(f => f.startsWith(`temp_${videoNum}.`));
  if (downloadedTemp) {
    return path.join(playlistDir, downloadedTemp);
  }
  return null;
}

async function processVideoWithFfmpeg(
  tempPath, videoFile, downloadId, videoNum, totalVideos,
  isAudio, videoTitle, playlistTitle, audioFormat, audioBitrate, container
) {
  sendProgress(downloadId, 'processing',
    `Processing ${videoNum}/${totalVideos}: ${videoTitle}`,
    ((videoNum - 0.5) / totalVideos) * 100, {
    playlistTitle,
    totalVideos,
    currentVideo: videoNum,
    currentVideoTitle: videoTitle,
    format: isAudio ? audioFormat : container
  });

  const ffmpegArgs = ['-y', '-i', tempPath];

  if (isAudio) {
    if (audioFormat === 'mp3') {
      ffmpegArgs.push('-codec:a', 'libmp3lame', '-b:a', `${audioBitrate}k`);
    } else if (audioFormat === 'm4a') {
      ffmpegArgs.push('-codec:a', 'aac', '-b:a', `${audioBitrate}k`);
    } else if (audioFormat === 'opus') {
      ffmpegArgs.push('-codec:a', 'libopus', '-b:a', `${audioBitrate}k`);
    } else if (audioFormat === 'wav') {
      ffmpegArgs.push('-codec:a', 'pcm_s16le');
    } else if (audioFormat === 'flac') {
      ffmpegArgs.push('-codec:a', 'flac');
    } else {
      ffmpegArgs.push('-codec:a', 'copy');
    }
  } else {
    ffmpegArgs.push('-codec', 'copy');
    if (container === 'mp4' || container === 'mov') {
      ffmpegArgs.push('-movflags', '+faststart');
    }
  }

  ffmpegArgs.push(videoFile);

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', ffmpegArgs);
    let ffmpegError = '';
    ffmpeg.stderr.on('data', (data) => {
      ffmpegError += data.toString();
    });
    ffmpeg.on('close', (code) => {
      if (code === 0) resolve();
      else {
        console.error(`[${downloadId}] FFmpeg failed for video ${videoNum} (code ${code}):`, ffmpegError.substring(0, 500));
        reject(new Error(`Processing failed for video ${videoNum} (code ${code})`));
      }
    });
    ffmpeg.on('error', reject);
  });

  try { fs.unlinkSync(tempPath); } catch { }
}

async function sendPlaylistZip(
  res, downloadId, downloadedFiles, playlistDir,
  playlistTitle, filename, totalVideos, isAudio,
  audioFormat, quality, container, failedVideos
) {
  sendProgress(downloadId, 'zipping', `Creating zip file with ${downloadedFiles.length} videos...`, 95, {
    playlistTitle,
    totalVideos,
    downloadedCount: downloadedFiles.length,
    format: isAudio ? audioFormat : `${quality} ${container}`
  });

  const zipPath = path.join(TEMP_DIRS.playlist, `${downloadId}.zip`);
  const safePlaylistName = sanitizeFilename(playlistTitle || filename || 'playlist');

  await new Promise((resolve, reject) => {
    const output = fs.createWriteStream(zipPath);
    const archive = archiver('zip', { zlib: { level: 5 } });

    output.on('close', resolve);
    archive.on('error', reject);

    archive.pipe(output);

    downloadedFiles.forEach(filePath => {
      const fileName = path.basename(filePath);
      archive.file(filePath, { name: fileName });
    });

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
      playlistTitle,
      totalVideos,
      downloadedCount: downloadedFiles.length,
      failedVideos: failedVideos.slice(-50),
      failedCount: failedVideos.length
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
    console.log(`[Queue] Playlist failed. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);
  });
}

module.exports = router;
