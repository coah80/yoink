const express = require('express');
const router = express.Router();
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const { v4: uuidv4 } = require('uuid');

const { TEMP_DIRS, SAFETY_LIMITS } = require('../config/constants');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { hasCookiesFile, getCookiesArgs, needsCookiesRetry } = require('../utils/cookies');
const { cleanupJobFiles } = require('../utils/files');
const { parseYouTubeClip } = require('../services/youtube');
const { fetchMetadataViaCobalt, downloadViaCobalt } = require('../services/cobalt');
const { downloadViaYtdlp, handleClipDownload } = require('../services/downloader');
const { processVideo, streamFile, probeForGif, getMimeType } = require('../services/processor');
const discordAlerts = require('../discord-alerts');
const { trackDownload } = require('../services/analyticsOptional');

const {
  activeProcesses,
  activeJobsByType,
  registerClient,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount,
  registerPendingJob,
  updatePendingJob,
  removePendingJob,
  sendProgress
} = require('../services/state');

router.get('/api/metadata', async (req, res) => {
  const { url, playlist } = req.query;
  const downloadPlaylist = playlist === 'true';

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');
  const isClip = url.includes('/clip/');

  if (isClip) {
    try {
      const clipData = await parseYouTubeClip(url);
      const clipDuration = (clipData.endTimeMs - clipData.startTimeMs) / 1000;

      try {
        const cobaltMeta = await fetchMetadataViaCobalt(clipData.fullVideoUrl);
        const originalDurationSec = parseInt(cobaltMeta.duration) || 0;

        let clipWarning = 'Clip will download full video then trim to clip portion.';
        if (originalDurationSec > 1800) {
          clipWarning = `Warning: Full video is ${Math.round(originalDurationSec / 60)} min. This will take a long time.`;
        } else if (originalDurationSec > 600) {
          clipWarning = `Full video is ${Math.round(originalDurationSec / 60)} min. Download may take a while.`;
        }

        return res.json({
          ...cobaltMeta,
          isClip: true,
          clipStartTime: clipData.startTimeMs / 1000,
          clipEndTime: clipData.endTimeMs / 1000,
          clipDuration,
          duration: clipDuration,
          originalVideoId: clipData.videoId,
          originalDuration: cobaltMeta.duration,
          fullVideoUrl: clipData.fullVideoUrl,
          usingCookies: false,
          clipNote: clipWarning
        });
      } catch (cobaltErr) {
        return res.json({
          isClip: true,
          clipStartTime: clipData.startTimeMs / 1000,
          clipEndTime: clipData.endTimeMs / 1000,
          clipDuration,
          duration: clipDuration,
          originalVideoId: clipData.videoId,
          fullVideoUrl: clipData.fullVideoUrl,
          title: 'YouTube Clip',
          thumbnail: `https://i.ytimg.com/vi/${clipData.videoId}/maxresdefault.jpg`,
          usingCookies: false,
          clipNote: 'Clip will download full video then trim to clip portion.'
        });
      }
    } catch (clipErr) {
      return res.json({
        isClip: true,
        title: 'YouTube Clip',
        usingCookies: false,
        clipNote: 'Clip will be downloaded via yt-dlp.'
      });
    }
  }

  if (isYouTube && !downloadPlaylist) {
    try {
      const cobaltMeta = await fetchMetadataViaCobalt(url);
      return res.json({ ...cobaltMeta, usingCookies: false });
    } catch (cobaltErr) {
      return res.status(500).json({ error: 'Failed to fetch YouTube metadata via Cobalt' });
    }
  }

  const usingCookies = hasCookiesFile();
  const ytdlpArgs = [...getCookiesArgs(), '-t', 'sleep'];

  if (!downloadPlaylist) {
    ytdlpArgs.push('--no-playlist',
      '--print', '%(title)s', '--print', '%(ext)s', '--print', '%(id)s',
      '--print', '%(uploader)s', '--print', '%(duration)s', '--print', '%(thumbnail)s',
      url
    );
  } else {
    ytdlpArgs.push('--yes-playlist', '--flat-playlist',
      '--print', '%(playlist_title)s', '--print', '%(playlist_count)s', '--print', '%(title)s',
      url
    );
  }

  const ytdlp = spawn('yt-dlp', ytdlpArgs);
  const timeoutId = setTimeout(() => {
    if (ytdlp.exitCode === null) ytdlp.kill('SIGKILL');
  }, 30000);

  let output = '';
  let errorOutput = '';

  ytdlp.stdout.on('data', (data) => { output += data.toString(); });
  ytdlp.stderr.on('data', (data) => { errorOutput += data.toString(); });

  ytdlp.on('close', (code) => {
    clearTimeout(timeoutId);
    if (code !== 0) {
      if (ytdlp.killed) return res.status(504).json({ error: 'Metadata fetch timed out (30s)' });
      if (needsCookiesRetry(errorOutput) && !usingCookies) {
        discordAlerts.cookieIssue('YouTube Bot Detection', 'YouTube is blocking requests - cookies.txt may be stale or missing.', { url, error: errorOutput.slice(0, 500) });
        return res.status(500).json({ error: 'YouTube requires authentication. Please add cookies.txt to the server.' });
      }
      return res.status(500).json({ error: 'Failed to fetch metadata', details: errorOutput });
    }

    const lines = output.trim().split('\n');
    if (downloadPlaylist) {
      const playlistTitle = lines[0] || 'Playlist';
      const videoCount = parseInt(lines[1]) || lines.length - 2;
      const videoTitles = lines.slice(2).filter(t => t.trim());
      res.json({ title: playlistTitle, isPlaylist: true, videoCount: videoCount || videoTitles.length, videoTitles: videoTitles.slice(0, 50), usingCookies });
    } else {
      res.json({ title: lines[0] || 'download', ext: lines[1] || 'mp4', id: lines[2] || '', uploader: lines[3] || '', duration: lines[4] || '', thumbnail: lines[5] || '', isPlaylist: false, usingCookies });
    }
  });

  ytdlp.on('error', () => res.status(500).json({ error: 'yt-dlp not found. Please install yt-dlp.' }));
});

router.get('/api/download', async (req, res) => {
  const {
    url, format = 'video', filename, quality = '1080p', container = 'mp4',
    audioFormat = 'mp3', audioBitrate = '320', progressId, playlist = 'false',
    clientId, twitterGifs = 'true'
  } = req.query;

  const convertTwitterGifs = twitterGifs === 'true';
  const downloadPlaylist = playlist === 'true';

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) return res.status(400).json({ error: urlCheck.error });

  if (clientId) {
    if (getClientJobCount(clientId) >= SAFETY_LIMITS.maxJobsPerClient) {
      return res.status(429).json({ error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.` });
    }
  }

  const downloadId = progressId || uuidv4();
  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;
  const finalFile = path.join(TEMP_DIRS.download, `${downloadId}-final.${outputExt}`);

  const abortController = new AbortController();
  const processInfo = { cancelled: false, process: null, tempFile: finalFile, abortController };
  activeProcesses.set(downloadId, processInfo);

  registerPendingJob(downloadId, {
    type: 'download', url,
    options: { format, quality, container, audioFormat, audioBitrate, twitterGifs, downloadPlaylist },
    clientId, status: 'starting'
  });

  activeJobsByType.download++;
  console.log(`[Queue] Download started. Active: ${JSON.stringify(activeJobsByType)}`);
  sendProgress(downloadId, 'starting', 'Initializing download...');

  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');

  try {
    sendProgress(downloadId, 'downloading', 'Downloading from source...', 0);

    let downloadedPath = null;
    let downloadedExt = null;

    if (isYouTube && !downloadPlaylist) {
      const isClip = url.includes('/clip/');

      if (isClip) {
        const clipData = await parseYouTubeClip(url);
        sendProgress(downloadId, 'downloading', 'Trimming clip from stream...', 0);
        const result = await handleClipDownload(clipData, downloadId, {
          tempDir: TEMP_DIRS.download,
          onProgress: (progress, speed, eta) => {
            sendProgress(downloadId, 'downloading', `Trimming... ${progress}%`, progress, { speed, eta });
            updatePendingJob(downloadId, { progress, status: 'downloading' });
          }
        });
        downloadedPath = result.path;
        downloadedExt = result.ext;
      } else {
        sendProgress(downloadId, 'downloading', 'Downloading via Cobalt...', 0);
        const cobaltResult = await downloadViaCobalt(url, downloadId, isAudio, (progress, downloaded, total) => {
          sendProgress(downloadId, 'downloading', `Downloading... ${progress}%`, progress);
          updatePendingJob(downloadId, { progress, status: 'downloading' });
        }, processInfo.abortController.signal);
        downloadedPath = cobaltResult.filePath;
        downloadedExt = cobaltResult.ext;
        sendProgress(downloadId, 'downloading', 'Download complete', 100);
      }
    } else {
      const result = await downloadViaYtdlp(url, downloadId, {
        isAudio, audioFormat, quality, container,
        tempDir: TEMP_DIRS.download,
        processInfo,
        playlist: downloadPlaylist,
        onProgress: (progress, speed, eta) => {
          sendProgress(downloadId, 'downloading', `Downloading... ${progress.toFixed(0)}%`, progress, { speed, eta });
          updatePendingJob(downloadId, { progress, status: 'downloading' });
        },
        onCancel: (kill) => req.on('close', kill)
      });
      downloadedPath = result.path;
      downloadedExt = result.ext;
    }

    if (!downloadedPath || !fs.existsSync(downloadedPath)) {
      throw new Error('Downloaded file not found');
    }

    const isTwitter = url.includes('twitter.com') || url.includes('x.com');
    let isGif = false;
    if (isTwitter && !isAudio && convertTwitterGifs) {
      isGif = probeForGif(downloadedPath);
    }

    const actualOutputExt = isGif ? 'gif' : outputExt;
    const actualFinalFile = isGif ? path.join(TEMP_DIRS.download, `${downloadId}-final.gif`) : finalFile;

    sendProgress(downloadId, 'processing', isGif ? 'Converting to GIF...' : 'Processing video...', 100);

    const processed = await processVideo(downloadedPath, actualFinalFile, {
      isAudio, isGif, audioFormat, audioBitrate, container, jobId: downloadId
    });

    if (!processed.skipped) {
      try { fs.unlinkSync(downloadedPath); } catch {}
    }

    const streamPath = processed.skipped ? processed.path : actualFinalFile;

    if (!fs.existsSync(streamPath)) {
      throw new Error('Processing failed - output file not created');
    }

    await streamFile(res, req, streamPath, {
      filename, ext: actualOutputExt,
      mimeType: getMimeType(actualOutputExt, isAudio, isGif),
      downloadId, url, jobType: 'download',
      trackFn: trackDownload
    });

  } catch (err) {
    console.error(`[${downloadId}] Error:`, err.message);
    discordAlerts.downloadFailed('Download Error', 'Video download failed.', { jobId: downloadId, url, format: outputExt, error: err.message });
    sendProgress(downloadId, 'error', toUserError(err.message || 'Download failed'));

    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);

    const files = fs.readdirSync(TEMP_DIRS.download);
    files.filter(f => f.startsWith(downloadId)).forEach(f => {
      try { fs.unlinkSync(path.join(TEMP_DIRS.download, f)); } catch {}
    });

    if (!res.headersSent) {
      res.status(500).json({ error: toUserError(err.message || 'Download failed') });
    }
  }
});

module.exports = router;
