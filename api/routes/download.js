const express = require('express');
const router = express.Router();
const { spawn, execSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const { v4: uuidv4 } = require('uuid');

const { TEMP_DIRS, SAFETY_LIMITS, QUALITY_HEIGHT, CONTAINER_MIMES, AUDIO_MIMES } = require('../config/constants');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { hasCookiesFile, getCookiesArgs, needsCookiesRetry } = require('../utils/cookies');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const { getClientIp, getCountryFromIP } = require('../utils/ip');
const { parseYouTubeClip, getRandomMullvadProxy } = require('../services/youtube');
const { fetchMetadataViaCobalt, downloadViaCobalt } = require('../services/cobalt');
const discordAlerts = require('../discord-alerts');
const { trackDownload } = require('../services/analyticsOptional');

const {
  activeDownloads,
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
      console.log('[Metadata] Detected YouTube clip, parsing...');
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
      console.error('[Metadata] Clip parsing failed:', clipErr.message);
      return res.status(400).json({
        error: 'Could not parse YouTube clip. Try using the full video URL instead.',
        clipUnsupported: true
      });
    }
  }

  if (isYouTube && !downloadPlaylist) {
    console.log('[Metadata] YouTube detected, using Cobalt directly...');

    try {
      const cobaltMeta = await fetchMetadataViaCobalt(url);
      return res.json({ ...cobaltMeta, usingCookies: false });
    } catch (cobaltErr) {
      console.error('[Metadata] Cobalt failed for YouTube:', cobaltErr.message);
      return res.status(500).json({ error: 'Failed to fetch YouTube metadata via Cobalt' });
    }
  }

  const usingCookies = hasCookiesFile();
  const ytdlpArgs = [...getCookiesArgs(), '-t', 'sleep'];

  if (!downloadPlaylist) {
    ytdlpArgs.push('--no-playlist');
    ytdlpArgs.push(
      '--print', '%(title)s',
      '--print', '%(ext)s',
      '--print', '%(id)s',
      '--print', '%(uploader)s',
      '--print', '%(duration)s',
      '--print', '%(thumbnail)s',
      url
    );
  } else {
    ytdlpArgs.push(
      '--yes-playlist',
      '--flat-playlist',
      '--print', '%(playlist_title)s',
      '--print', '%(playlist_count)s',
      '--print', '%(title)s',
      url
    );
  }

  const ytdlp = spawn('yt-dlp', ytdlpArgs);

  const timeoutMs = 30000;
  const timeoutId = setTimeout(() => {
    if (ytdlp.exitCode === null) {
      console.log(`[Metadata] Timeout reached (${timeoutMs}ms), killing yt-dlp process`);
      ytdlp.kill('SIGKILL');
    }
  }, timeoutMs);

  let output = '';
  let errorOutput = '';

  ytdlp.stdout.on('data', (data) => {
    output += data.toString();
  });

  ytdlp.stderr.on('data', (data) => {
    errorOutput += data.toString();
  });

  ytdlp.on('close', async (code) => {
    clearTimeout(timeoutId);

    if (code !== 0) {
      if (ytdlp.killed) {
        return res.status(504).json({ error: 'Metadata fetch timed out (30s)' });
      }

      console.error('yt-dlp metadata error:', errorOutput);

      const isBotBlocked = needsCookiesRetry(errorOutput);

      if (isBotBlocked && !usingCookies) {
        console.error('[Cookies] Hey, I can\'t find cookies.txt! YouTube is blocking requests.');
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

      res.json({
        title: playlistTitle,
        isPlaylist: true,
        videoCount: videoCount || videoTitles.length,
        videoTitles: videoTitles.slice(0, 50),
        usingCookies
      });
    } else {
      const title = lines[0] || 'download';
      const ext = lines[1] || 'mp4';
      const id = lines[2] || '';
      const uploader = lines[3] || '';
      const duration = lines[4] || '';
      const thumbnail = lines[5] || '';

      res.json({ title, ext, id, uploader, duration, thumbnail, isPlaylist: false, usingCookies });
    }
  });

  ytdlp.on('error', (err) => {
    console.error('Failed to spawn yt-dlp:', err);
    res.status(500).json({ error: 'yt-dlp not found. Please install yt-dlp.' });
  });
});

router.get('/api/download', async (req, res) => {
  const {
    url,
    format = 'video',
    filename,
    quality = '1080p',
    container = 'mp4',
    audioFormat = 'mp3',
    audioBitrate = '320',
    progressId,
    playlist = 'false',
    clientId,
    twitterGifs = 'true',
    clipMethod = 'cobalt'
  } = req.query;

  const convertTwitterGifs = twitterGifs === 'true';
  const downloadPlaylist = playlist === 'true';

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

  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;
  const tempFile = path.join(TEMP_DIRS.download, `${downloadId}.%(ext)s`);
  const finalFile = path.join(TEMP_DIRS.download, `${downloadId}-final.${outputExt}`);

  const abortController = new AbortController();
  const processInfo = { cancelled: false, process: null, tempFile: finalFile, abortController };
  activeProcesses.set(downloadId, processInfo);

  registerPendingJob(downloadId, {
    type: 'download',
    url,
    options: { format, quality, container, audioFormat, audioBitrate, twitterGifs, downloadPlaylist },
    clientId,
    status: 'starting',
    tempFiles: [tempFile, finalFile]
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
        console.log(`[${downloadId}] YouTube clip detected, clip method: ${clipMethod}`);

        try {
          const clipData = await parseYouTubeClip(url);
          console.log(`[${downloadId}] Clip parsed: video ${clipData.videoId}, ${clipData.startTimeMs}-${clipData.endTimeMs}ms`);

          if (clipMethod === 'ytdlp') {
            const result = await downloadClipViaYtdlp(clipData, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req);
            downloadedPath = result.path;
            downloadedExt = result.ext;
          } else {
            const result = await downloadClipViaCobalt(clipData, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req);
            downloadedPath = result.path;
            downloadedExt = result.ext;
          }
        } catch (clipErr) {
          console.error(`[${downloadId}] Clip download failed:`, clipErr.message);
          throw new Error(`Clip download failed: ${clipErr.message}`);
        }
      } else {
        console.log(`[${downloadId}] YouTube detected, using Cobalt...`);
        sendProgress(downloadId, 'downloading', 'Downloading via Cobalt...', 0);

        try {
          const cobaltResult = await downloadViaCobalt(url, downloadId, isAudio, (progress, downloaded, total) => {
            sendProgress(downloadId, 'downloading', `Downloading... ${progress}%`, progress);
            updatePendingJob(downloadId, { progress, status: 'downloading' });
          }, processInfo.abortController.signal);
          downloadedPath = cobaltResult.filePath;
          downloadedExt = cobaltResult.ext;

          updatePendingJob(downloadId, {
            cobaltUrl: cobaltResult.downloadUrl,
            tempFiles: [cobaltResult.filePath, cobaltResult.filePath + '.part']
          });

          sendProgress(downloadId, 'downloading', 'Download complete', 100);
        } catch (cobaltErr) {
          console.error(`[${downloadId}] Cobalt failed:`, cobaltErr.message);
          
          if (cobaltErr.message === 'Cancelled' || cobaltErr.message.includes('Download cancelled')) {
            throw cobaltErr;
          }
          
          console.log(`[${downloadId}] Falling back to yt-dlp with proxy...`);
          const result = await downloadViaYtdlp(url, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req);
          downloadedPath = result.path;
          downloadedExt = result.ext;
        }
      }
    } else {
      const result = await downloadViaYtdlp(url, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req, downloadPlaylist);
      downloadedPath = result.path;
      downloadedExt = result.ext;
    }

    if (!downloadedPath || !fs.existsSync(downloadedPath)) {
      console.error(`[${downloadId}] Downloaded path missing: ${downloadedPath}`);
      throw new Error('Downloaded file not found');
    }

    const isTwitter = url.includes('twitter.com') || url.includes('x.com');
    let isGif = false;

    if (isTwitter && !isAudio && convertTwitterGifs) {
      try {
        const probeResult = execSync(
          `ffprobe -v quiet -print_format json -show_streams -show_format "${downloadedPath}"`,
          { encoding: 'utf8' }
        );
        const probe = JSON.parse(probeResult);
        const hasAudio = probe.streams?.some(s => s.codec_type === 'audio');
        const duration = parseFloat(probe.format?.duration || '999');
        isGif = !hasAudio && duration < 60;
      } catch (e) {
        console.log(`[${downloadId}] Could not probe for GIF detection: ${e.message}`);
      }
    }

    const actualOutputExt = isGif ? 'gif' : outputExt;
    const actualFinalFile = isGif ? path.join(TEMP_DIRS.download, `${downloadId}-final.gif`) : finalFile;

    sendProgress(downloadId, 'processing', isGif ? 'Converting to GIF...' : 'Processing video...', 100);

    await processWithFfmpeg(downloadedPath, actualFinalFile, isAudio, isGif, audioFormat, audioBitrate, container, downloadId);

    try { fs.unlinkSync(downloadedPath); } catch { }

    if (!fs.existsSync(actualFinalFile)) {
      console.error(`[${downloadId}] Final file not found after ffmpeg: ${actualFinalFile}`);
      throw new Error('Processing failed - output file not created');
    }

    await sendFileToClient(res, req, actualFinalFile, filename, actualOutputExt, isAudio, isGif, audioFormat, container, downloadId, url);

  } catch (err) {
    console.error(`[${downloadId}] Error:`, err.message);
    discordAlerts.downloadFailed('Download Error', 'Video download failed.', { jobId: downloadId, url, format: outputExt, error: err.message });

    sendProgress(downloadId, 'error', toUserError(err.message || 'Download failed'));

    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Download error. Active: ${JSON.stringify(activeJobsByType)}`);

    const files = fs.readdirSync(TEMP_DIRS.download);
    files.filter(f => f.startsWith(downloadId)).forEach(f => {
      try { fs.unlinkSync(path.join(TEMP_DIRS.download, f)); } catch { }
    });

    if (!res.headersSent) {
      res.status(500).json({ error: toUserError(err.message || 'Download failed') });
    }
  }
});

async function downloadClipViaYtdlp(clipData, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req) {
  console.log(`[${downloadId}] User prefers yt-dlp, downloading clip section...`);
  sendProgress(downloadId, 'downloading', 'Downloading clip section...', 0);

  const startTime = clipData.startTimeMs / 1000;
  const endTime = clipData.endTimeMs / 1000;
  const proxy = getRandomMullvadProxy();

  const ytdlpArgs = [
    ...getCookiesArgs(),
    '--download-sections', `*${startTime}-${endTime}`,
    '--force-keyframes-at-cuts',
    '-t', 'sleep',
    '--no-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s',
    '-o', tempFile,
    '--ffmpeg-location', '/usr/bin/ffmpeg',
  ];

  if (proxy) {
    ytdlpArgs.push('--proxy', proxy.url);
    console.log(`[${downloadId}] Using Mullvad proxy: ${proxy.server}`);
  }

  if (isAudio) {
    ytdlpArgs.push('-x', '--audio-format', audioFormat, '--audio-quality', '320');
  } else {
    ytdlpArgs.push('-f', `bestvideo[height<=${quality.replace('p', '')}]+bestaudio/best[height<=${quality.replace('p', '')}]`);
    ytdlpArgs.push('--merge-output-format', container);
  }

  ytdlpArgs.push(clipData.fullVideoUrl);

  await new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);
    processInfo.process = ytdlp;

    let lastProgress = 0;
    ytdlp.stdout.on('data', (data) => {
      const output = data.toString();
      const match = output.match(/(\d+\.?\d*)%/);
      if (match) {
        const progress = parseFloat(match[1]);
        if (progress > lastProgress) {
          lastProgress = progress;
          sendProgress(downloadId, 'downloading', `Downloading clip... ${Math.round(progress)}%`, Math.round(progress));
          updatePendingJob(downloadId, { progress: Math.round(progress), status: 'downloading' });
        }
      }
    });

    ytdlp.stderr.on('data', (data) => {
      console.log(`[${downloadId}] yt-dlp: ${data.toString().trim()}`);
    });

    ytdlp.on('close', (code) => {
      if (processInfo.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code !== 0) {
        reject(new Error('yt-dlp failed to download clip'));
      } else {
        resolve();
      }
    });

    ytdlp.on('error', reject);

    req.on('close', () => {
      if (!processInfo.cancelled) {
        ytdlp.kill('SIGTERM');
      }
    });
  });

  const files = fs.readdirSync(TEMP_DIRS.download);
  const downloadedFile = files.find(f => 
    f.startsWith(downloadId) && 
    !f.includes('-final') && 
    !f.includes('-cobalt') &&
    !f.includes('-trimmed') &&
    !f.endsWith('.part') &&
    !f.includes('.part-Frag')
  );

  if (!downloadedFile) {
    throw new Error('Clip download incomplete');
  }

  return {
    path: path.join(TEMP_DIRS.download, downloadedFile),
    ext: path.extname(downloadedFile).slice(1)
  };
}

async function downloadClipViaCobalt(clipData, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req) {
  console.log(`[${downloadId}] Using Cobalt to download full video for trimming...`);
  sendProgress(downloadId, 'downloading', 'Downloading full video...', 0);

  try {
    const cobaltResult = await downloadViaCobalt(clipData.fullVideoUrl, downloadId, isAudio, (progress, downloaded, total) => {
      sendProgress(downloadId, 'downloading', `Downloading... ${progress}%`, progress);
      updatePendingJob(downloadId, { progress, status: 'downloading' });
    }, null, { quality, container });

    let downloadedPath = cobaltResult.filePath;
    let downloadedExt = cobaltResult.ext;
    console.log(`[${downloadId}] Full video downloaded via Cobalt: ${path.basename(downloadedPath)}`);
  
    sendProgress(downloadId, 'processing', 'Trimming clip...', 95);
    updatePendingJob(downloadId, { progress: 95, status: 'processing' });

    const startTime = clipData.startTimeMs / 1000;
    const endTime = clipData.endTimeMs / 1000;
    const duration = endTime - startTime;

    const trimmedFile = path.join(TEMP_DIRS.download, `${downloadId}-trimmed.${downloadedExt}`);

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', [
        '-ss', startTime.toString(),
        '-i', downloadedPath,
        '-t', duration.toString(),
        '-c:v', 'libx264',
        '-c:a', 'copy',
        '-preset', 'fast',
        '-avoid_negative_ts', 'make_zero',
        trimmedFile
      ]);

      processInfo.process = ffmpeg;
      let stderrOutput = '';

      ffmpeg.stderr.on('data', (data) => {
        stderrOutput += data.toString();
      });

      ffmpeg.on('close', (code) => {
        if (processInfo.cancelled) {
          reject(new Error('Download cancelled'));
        } else if (code !== 0) {
          console.error(`[${downloadId}] ffmpeg trim error:`, stderrOutput);
          reject(new Error('Failed to trim clip'));
        } else {
          resolve();
        }
      });

      ffmpeg.on('error', reject);

      req.on('close', () => {
        if (!processInfo.cancelled) {
          ffmpeg.kill('SIGTERM');
        }
      });
    });

    fs.unlinkSync(downloadedPath);
    return { path: trimmedFile, ext: downloadedExt };
  } catch (cobaltErr) {
    console.error(`[${downloadId}] Cobalt failed for clip:`, cobaltErr.message);
    console.log(`[${downloadId}] Falling back to yt-dlp for clip...`);
    return await downloadClipViaYtdlp(clipData, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req);
  }
}

async function downloadViaYtdlp(url, downloadId, isAudio, audioFormat, quality, container, tempFile, processInfo, req, downloadPlaylist = false) {
  sendProgress(downloadId, 'downloading', 'Retrying with fallback method...', 0);
  
  const proxy = getRandomMullvadProxy();
  const ytdlpArgs = [
    ...getCookiesArgs(),
    '--continue',
    '-t', 'sleep',
    downloadPlaylist ? '--yes-playlist' : '--no-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s',
    '-o', tempFile,
    '--ffmpeg-location', '/usr/bin/ffmpeg',
  ];
  
  if (proxy) {
    ytdlpArgs.push('--proxy', proxy.url);
    console.log(`[${downloadId}] Using Mullvad proxy: ${proxy.server}`);
  }
  
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
  
  ytdlpArgs.push(url);
  
  let lastProgress = 0;
  let stderrOutput = '';
  
  await new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);
    processInfo.process = ytdlp;

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString().trim();
      const match = msg.match(/([\d.]+)%/);
      if (match) {
        const progress = parseFloat(match[1]);
        if (progress > lastProgress + 5 || progress >= 100) {
          lastProgress = progress;
          sendProgress(downloadId, 'downloading', `Downloading... ${progress.toFixed(0)}%`, progress);
          updatePendingJob(downloadId, { progress, status: 'downloading' });
        }
      }
    });

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (msg.includes('[download]') && msg.includes('%')) {
        const match = msg.match(/([\d.]+)%/);
        if (match) {
          const progress = parseFloat(match[1]);
          if (progress > lastProgress + 5 || progress >= 100) {
            lastProgress = progress;
            sendProgress(downloadId, 'downloading', `Downloading... ${progress.toFixed(0)}%`, progress);
            updatePendingJob(downloadId, { progress, status: 'downloading' });
          }
        }
      }
    });

    ytdlp.on('close', (code) => {
      if (processInfo.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code !== 0) {
        const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        const errorMessage = errorMatch ? errorMatch[1].trim() : 'Download failed';
        reject(new Error(errorMessage));
      } else {
        resolve();
      }
    });

    ytdlp.on('error', reject);

    req.on('close', () => {
      if (!processInfo.cancelled) {
        ytdlp.kill('SIGTERM');
      }
    });
  });
  
  const files = fs.readdirSync(TEMP_DIRS.download);
  const downloadedFile = files.find(f => 
    f.startsWith(downloadId) && 
    !f.includes('-final') && 
    !f.includes('-cobalt') &&
    !f.endsWith('.part') &&
    !f.includes('.part-Frag')
  );

  if (!downloadedFile) {
    throw new Error('Downloaded file not found');
  }

  return {
    path: path.join(TEMP_DIRS.download, downloadedFile),
    ext: path.extname(downloadedFile).slice(1)
  };
}

async function processWithFfmpeg(inputPath, outputPath, isAudio, isGif, audioFormat, audioBitrate, container, downloadId) {
  const ffmpegArgs = [
    '-y',
    '-i', inputPath,
    '-progress', 'pipe:1',
  ];

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
  } else if (isGif) {
    ffmpegArgs.length = 0;
    ffmpegArgs.push(
      '-y',
      '-i', inputPath,
      '-vf', 'fps=15,scale=480:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse',
      '-loop', '0'
    );
  } else {
    ffmpegArgs.push('-codec', 'copy');
    if (container === 'mp4' || container === 'mov') {
      ffmpegArgs.push('-movflags', '+faststart');
    }
  }

  ffmpegArgs.push(outputPath);

  sendProgress(downloadId, 'remuxing', isGif ? 'Creating GIF...' : 'Preparing file...');

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', ffmpegArgs);
    let ffmpegStderr = '';

    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      ffmpegStderr += msg;
      if (msg.includes('Error') || msg.includes('error')) {
        console.error(`[${downloadId}] ffmpeg: ${msg.trim()}`);
      }
    });

    ffmpeg.on('close', (code) => {
      if (code === 0) resolve();
      else {
        console.error(`[${downloadId}] FFmpeg failed with code ${code}. Last 500 chars: ${ffmpegStderr.slice(-500)}`);
        reject(new Error(`Encoding failed (code ${code})`));
      }
    });

    ffmpeg.on('error', (err) => {
      console.error(`[${downloadId}] FFmpeg spawn error: ${err.message}`);
      reject(err);
    });
  });
}

async function sendFileToClient(res, req, filePath, filename, outputExt, isAudio, isGif, audioFormat, container, downloadId, url) {
  sendProgress(downloadId, 'sending', 'Sending file to you...');

  const stat = fs.statSync(filePath);
  const safeFilename = sanitizeFilename(filename || 'download');
  const fullFilename = `${safeFilename}.${outputExt}`;
  const asciiFilename = safeFilename.replace(/[^\x20-\x7E]/g, '_') + '.' + outputExt;
  const mimeType = isGif
    ? 'image/gif'
    : isAudio
      ? (AUDIO_MIMES[audioFormat] || 'audio/mpeg')
      : (CONTAINER_MIMES[container] || 'video/mp4');

  res.setHeader('Content-Type', mimeType);
  res.setHeader('Content-Length', stat.size);
  res.setHeader('Content-Disposition', `attachment; filename="${asciiFilename}"; filename*=UTF-8''${encodeURIComponent(fullFilename)}`);

  const stream = fs.createReadStream(filePath);
  let finished = false;

  stream.pipe(res);

  const cleanup = () => {
    if (finished) return;
    finished = true;
    sendProgress(downloadId, 'complete', 'Download complete!');
    activeDownloads.delete(downloadId);
    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);

    try {
      const site = new URL(url).hostname.replace('www.', '');
      trackDownload(outputExt, site, getCountryFromIP(getClientIp(req)));
    } catch (e) { }

    console.log(`[Queue] Download finished. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);
  };

  stream.on('close', cleanup);
  res.on('finish', cleanup);

  stream.on('error', (err) => {
    if (finished) return;
    finished = true;
    console.error(`[${downloadId}] Stream error:`, err);
    sendProgress(downloadId, 'error', 'Failed to send file');
    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);
  });

  req.on('close', () => {
    if (finished) return;
    finished = true;
    stream.destroy();
    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Download cancelled. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => {
      try { fs.unlinkSync(filePath); } catch { }
    }, 1000);
  });
}

module.exports = router;
