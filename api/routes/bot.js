const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { spawn, spawnSync } = require('child_process');
const { v4: uuidv4 } = require('uuid');
const archiver = require('archiver');

const {
  TEMP_DIRS,
  SAFETY_LIMITS,
  QUALITY_HEIGHT,
  CONTAINER_MIMES,
  AUDIO_MIMES,
  BOT_SECRET,
  BOT_DOWNLOAD_EXPIRY
} = require('../config/constants');

const {
  asyncJobs,
  botDownloads
} = require('../services/state');

const { downloadViaCobalt } = require('../services/cobalt');
const { parseYouTubeClip } = require('../services/youtube');
const { getCookiesArgs } = require('../utils/cookies');
const { validateUrl } = require('../utils/validation');
const { toUserError } = require('../utils/errors');
const { sanitizeFilename } = require('../utils/files');

setInterval(() => {
  const now = Date.now();
  for (const [token, data] of botDownloads.entries()) {
    if (now - data.createdAt > BOT_DOWNLOAD_EXPIRY) {
      console.log(`[Bot] Download token ${token.slice(0, 8)}... expired`);
      if (data.filePath && fs.existsSync(data.filePath)) {
        fs.unlink(data.filePath, () => { });
      }
      botDownloads.delete(token);
    }
  }
}, 30000);

function checkBotAuth(req) {
  const authHeader = req.headers.authorization;
  return authHeader && authHeader === `Bearer ${BOT_SECRET}`;
}

router.post('/api/bot/download', express.json(), async (req, res) => {
  if (!checkBotAuth(req)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const { url, format = 'video', quality = '1080p', container = 'mp4', audioFormat = 'mp3', playlist = false, clipMethod = 'cobalt' } = req.body;

  if (!url) {
    return res.status(400).json({ error: 'URL required' });
  }

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const jobId = uuidv4();
  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;
  const tempFile = path.join(TEMP_DIRS.bot, `bot-${jobId}.%(ext)s`);
  const finalFile = path.join(TEMP_DIRS.bot, `bot-${jobId}-final.${outputExt}`);

  const job = {
    status: 'starting',
    progress: 0,
    message: 'Initializing download...',
    createdAt: Date.now(),
    url,
    format: outputExt,
    filePath: null,
    fileName: null,
    fileSize: null,
    downloadToken: null
  };
  asyncJobs.set(jobId, job);

  res.json({ jobId });

  processBotDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, tempFile, finalFile, playlist);
});

async function processBotDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, tempFile, finalFile, playlist) {
  try {
    job.status = 'downloading';
    job.message = 'Downloading from source...';

    const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');
    let downloadedPath = null;
    let downloadedExt = null;
    let usedCobalt = false;

    if (isYouTube) {
      const isClip = url.includes('/clip/');

      if (isClip) {
        console.log(`[Bot] YouTube clip detected for ${jobId}, parsing...`);
        job.message = 'Parsing clip...';

        try {
          const clipData = await parseYouTubeClip(url);
          console.log(`[Bot] Clip parsed: video ${clipData.videoId}, ${clipData.startTimeMs}-${clipData.endTimeMs}ms`);

          job.message = 'Downloading full video...';

          const cobaltResult = await downloadViaCobalt(clipData.fullVideoUrl, jobId, isAudio, (progress) => {
            job.progress = Math.floor(progress * 0.7);
          });

          const fullVideoPath = cobaltResult.filePath;
          const ext = cobaltResult.ext;

          console.log(`[Bot] Full video downloaded, trimming to clip...`);
          job.message = 'Trimming clip...';
          job.progress = 75;

          const clipPath = path.join(TEMP_DIRS.bot, `bot-${jobId}-clip.${ext}`);
          const startTime = clipData.startTimeMs / 1000;
          const duration = (clipData.endTimeMs - clipData.startTimeMs) / 1000;

          await new Promise((resolve, reject) => {
            const ffmpeg = spawn('ffmpeg', [
              '-ss', startTime.toString(),
              '-i', fullVideoPath,
              '-t', duration.toString(),
              '-c', 'copy',
              '-avoid_negative_ts', 'make_zero',
              '-y',
              clipPath
            ]);
            ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error(`ffmpeg exited ${code}`)));
            ffmpeg.on('error', reject);
          });

          try { fs.unlinkSync(fullVideoPath); } catch (e) {}

          downloadedPath = clipPath;
          downloadedExt = ext;
          usedCobalt = true;
          job.progress = 100;

        } catch (clipErr) {
          console.error(`[Bot] Clip processing failed:`, clipErr.message);
          throw new Error(`Clip download failed: ${clipErr.message}`);
        }

      } else {
        console.log(`[Bot] YouTube detected, using Cobalt directly for ${jobId}`);
        job.message = 'Downloading via Cobalt...';

        try {
          const cobaltResult = await downloadViaCobalt(url, jobId, isAudio, (progress) => {
            job.progress = progress;
          });
          downloadedPath = cobaltResult.filePath;
          downloadedExt = cobaltResult.ext;
          usedCobalt = true;
          job.progress = 100;
        } catch (cobaltErr) {
          console.error(`[Bot] Cobalt failed for YouTube:`, cobaltErr.message);

          let userFriendlyError = 'YouTube download failed';
          if (cobaltErr.message.includes('content.video.unavailable')) {
            userFriendlyError = 'Video unavailable or private';
          } else if (cobaltErr.message.includes('content.video.live')) {
            userFriendlyError = 'Live streams cannot be downloaded';
          } else if (cobaltErr.message.includes('content.video.age')) {
            userFriendlyError = 'Age-restricted video';
          } else if (cobaltErr.message.includes('rate')) {
            userFriendlyError = 'Rate limited - try again later';
          } else if (cobaltErr.message.includes('api.link.unsupported')) {
            userFriendlyError = 'Link processing failed';
          }

          throw new Error(userFriendlyError);
        }
      }
    } else {
      const ytdlpArgs = [
        ...getCookiesArgs(),
        playlist ? '--yes-playlist' : '--no-playlist',
        '--newline',
        '-o', tempFile,
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

      ytdlpArgs.push(url);

      let lastProgress = 0;

      await new Promise((resolve, reject) => {
        const ytdlp = spawn('yt-dlp', ytdlpArgs);
        let stderrOutput = '';

        ytdlp.stdout.on('data', (data) => {
          const msg = data.toString().trim();
          const match = msg.match(/([\d.]+)%/);
          if (match) {
            const progress = parseFloat(match[1]);
            if (progress > lastProgress + 2 || progress >= 100) {
              lastProgress = progress;
              job.progress = progress;
              job.message = `Downloading... ${progress.toFixed(0)}%`;
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
              if (progress > lastProgress + 2 || progress >= 100) {
                lastProgress = progress;
                job.progress = progress;
                job.message = `Downloading... ${progress.toFixed(0)}%`;
              }
            }
          }
        });

        ytdlp.on('close', (code) => {
          if (code === 0) resolve();
          else reject(new Error('Download failed'));
        });
        ytdlp.on('error', reject);
      });

      const files = fs.readdirSync(TEMP_DIRS.bot);
      const downloadedFile = files.find(f =>
        f.startsWith(`bot-${jobId}`) &&
        !f.includes('-final') &&
        !f.includes('-cobalt') &&
        !f.endsWith('.part') &&
        !f.includes('.part-Frag')
      );

      if (!downloadedFile) {
        const partialFiles = files.filter(f => f.startsWith(`bot-${jobId}`));
        console.error(`[Bot ${jobId}] No complete file. Partials: ${partialFiles.join(', ') || 'none'}`);
        throw new Error('Downloaded file not found');
      }

      downloadedPath = path.join(TEMP_DIRS.bot, downloadedFile);
      downloadedExt = path.extname(downloadedFile).slice(1);
    }

    if (!downloadedPath || !fs.existsSync(downloadedPath)) {
      console.error(`[Bot ${jobId}] File missing: ${downloadedPath}`);
      throw new Error('Downloaded file not found');
    }

    job.status = 'processing';
    job.progress = 100;
    job.message = 'Processing...';

    let actualFinalFile = finalFile;
    let actualOutputExt = outputExt;

    if (downloadedExt !== outputExt && !isAudio) {
      await new Promise((resolve, reject) => {
        const ffmpeg = spawn('ffmpeg', [
          '-i', downloadedPath,
          '-c:v', 'copy',
          '-c:a', 'copy',
          '-y',
          finalFile
        ]);
        ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error('Processing failed')));
        ffmpeg.on('error', reject);
      });
      try { fs.unlinkSync(downloadedPath); } catch { }
    } else {
      actualFinalFile = downloadedPath;
      actualOutputExt = downloadedExt;
    }

    if (!fs.existsSync(actualFinalFile)) {
      throw new Error('Downloaded file not found after processing');
    }
    const stat = fs.statSync(actualFinalFile);
    const downloadToken = crypto.randomBytes(32).toString('hex');

    let title = 'download';
    try {
      const infoResult = spawnSync('yt-dlp', ['--print', 'title', '--no-playlist', url], { timeout: 10000 });
      if (infoResult.status === 0) {
        title = infoResult.stdout.toString().trim().slice(0, 100);
      }
    } catch { }

    const fileName = sanitizeFilename(title) + '.' + actualOutputExt;

    botDownloads.set(downloadToken, {
      filePath: actualFinalFile,
      fileName,
      fileSize: stat.size,
      mimeType: isAudio ? (AUDIO_MIMES[audioFormat] || 'audio/mpeg') : (CONTAINER_MIMES[container] || 'video/mp4'),
      createdAt: Date.now(),
      downloaded: false
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
      try { fs.unlinkSync(path.join(TEMP_DIRS.bot, f)); } catch { }
    });
  }
}

router.post('/api/bot/download-playlist', express.json(), async (req, res) => {
  if (!checkBotAuth(req)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const { url, format = 'video', quality = '1080p', container = 'mp4', audioFormat = 'mp3', audioBitrate = '320' } = req.body;

  if (!url) {
    return res.status(400).json({ error: 'URL required' });
  }

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const jobId = uuidv4();
  const isAudio = format === 'audio';
  const outputExt = isAudio ? audioFormat : container;

  const job = {
    status: 'starting',
    progress: 0,
    message: 'Getting playlist info...',
    createdAt: Date.now(),
    url,
    format: outputExt,
    filePath: null,
    fileName: null,
    fileSize: null,
    downloadToken: null,
    playlistInfo: null,
    videosCompleted: 0,
    totalVideos: 0,
    failedVideos: []
  };
  asyncJobs.set(jobId, job);

  res.json({ jobId });

  processBotPlaylistDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate);
});

async function processBotPlaylistDownload(jobId, job, url, isAudio, audioFormat, outputExt, quality, container, audioBitrate) {
  const playlistDir = path.join(TEMP_DIRS.bot, `playlist-${jobId}`);
  
  try {
    if (!fs.existsSync(playlistDir)) {
      fs.mkdirSync(playlistDir, { recursive: true });
    }

    const playlistInfo = await getPlaylistInfo(url);

    if (playlistInfo.count > SAFETY_LIMITS.maxPlaylistVideos) {
      throw new Error(`Playlist too large. Maximum ${SAFETY_LIMITS.maxPlaylistVideos} videos allowed. This playlist has ${playlistInfo.count} videos.`);
    }

    job.totalVideos = playlistInfo.count;
    job.playlistInfo = {
      title: playlistInfo.title,
      count: playlistInfo.count
    };
    job.message = `Found ${playlistInfo.count} videos`;
    job.status = 'downloading';

    console.log(`[Bot] Playlist ${jobId}: ${playlistInfo.count} videos`);

    const downloadedFiles = [];
    const failedVideos = [];

    for (let i = 0; i < playlistInfo.entries.length; i++) {
      const entry = playlistInfo.entries[i];
      const videoNum = i + 1;
      const videoTitle = entry.title || `Video ${videoNum}`;
      const videoUrl = entry.url || entry.id ? `https://www.youtube.com/watch?v=${entry.id}` : null;

      if (!videoUrl && !entry.id) {
        console.log(`[Bot ${jobId}] Skipping video ${videoNum}: no URL`);
        continue;
      }

      job.message = `Downloading ${videoNum}/${playlistInfo.count}: ${videoTitle}`;
      job.progress = Math.round((videoNum / playlistInfo.count) * 90);

      const safeTitle = sanitizeFilename(videoTitle).substring(0, 100);
      const videoFile = path.join(playlistDir, `${String(videoNum).padStart(3, '0')} - ${safeTitle}.${outputExt}`);
      const tempVideoFile = path.join(playlistDir, `temp_${videoNum}.%(ext)s`);

      try {
        const actualVideoUrl = videoUrl || `https://www.youtube.com/watch?v=${entry.id}`;
        const isYouTubeVideo = actualVideoUrl.includes('youtube.com') || actualVideoUrl.includes('youtu.be');

        let tempPath = null;

        if (isYouTubeVideo) {
          const videoJobId = `${jobId}-v${videoNum}`;
          console.log(`[Bot ${jobId}] Video ${videoNum}: Using Cobalt`);

          try {
            const cobaltResult = await downloadViaCobalt(
              actualVideoUrl,
              videoJobId,
              isAudio,
              null,
              null,
              { outputDir: playlistDir, maxRetries: 2, retryDelay: 1000 }
            );
            tempPath = cobaltResult.filePath;
          } catch (cobaltErr) {
            const reason = toUserError(cobaltErr.message);
            failedVideos.push({ num: videoNum, title: videoTitle, reason });
            job.failedVideos = failedVideos.slice(-50);
            console.error(`[Bot ${jobId}] Cobalt failed for video ${videoNum}:`, cobaltErr.message);
            continue;
          }
        }

        if (!tempPath && !isYouTubeVideo) {
          tempPath = await downloadViaYtdlp(actualVideoUrl, tempVideoFile, playlistDir, videoNum, isAudio, quality, container);
        }

        if (tempPath && fs.existsSync(tempPath)) {
          await processVideoWithFfmpeg(tempPath, videoFile, isAudio, audioFormat, audioBitrate, container, jobId, videoNum);
          downloadedFiles.push(videoFile);
          job.videosCompleted = downloadedFiles.length;
          console.log(`[Bot ${jobId}] Video ${videoNum} complete`);
        }

      } catch (err) {
        console.error(`[Bot ${jobId}] Error downloading video ${videoNum}:`, err.message);
        const reason = toUserError(err.message);
        failedVideos.push({ num: videoNum, title: videoTitle, reason });
        job.failedVideos = failedVideos.slice(-50);
      }
    }

    if (downloadedFiles.length === 0) {
      throw new Error('No videos were successfully downloaded');
    }

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

      downloadedFiles.forEach(filePath => {
        const fileName = path.basename(filePath);
        archive.file(filePath, { name: fileName });
      });

      archive.finalize();
    });

    const stat = fs.statSync(zipPath);
    const downloadToken = crypto.randomBytes(32).toString('hex');
    const fileName = `${safePlaylistName}.zip`;

    botDownloads.set(downloadToken, {
      filePath: zipPath,
      fileName,
      fileSize: stat.size,
      mimeType: 'application/zip',
      createdAt: Date.now(),
      downloaded: false,
      isPlaylist: true
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

  } catch (err) {
    console.error(`[Bot] Playlist job ${jobId} failed:`, err.message);
    job.status = 'error';
    job.message = toUserError(err.message);
    job.debugError = err.message;

    try {
      if (fs.existsSync(playlistDir)) {
        fs.rmSync(playlistDir, { recursive: true, force: true });
      }
    } catch (cleanupErr) {
      console.error(`[Bot] Cleanup error for ${jobId}:`, cleanupErr);
    }
  }
}

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

async function downloadViaYtdlp(videoUrl, tempVideoFile, playlistDir, videoNum, isAudio, quality, container) {
  const ytdlpArgs = [
    ...getCookiesArgs(),
    '-t', 'sleep',
    '--no-playlist',
    '--newline',
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
    let stderrOutput = '';

    ytdlp.stderr.on('data', (data) => {
      stderrOutput += data.toString();
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

async function processVideoWithFfmpeg(tempPath, videoFile, isAudio, audioFormat, audioBitrate, container, jobId, videoNum) {
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
        console.error(`[Bot ${jobId}] FFmpeg failed for video ${videoNum} (code ${code}):`, ffmpegError.substring(0, 500));
        reject(new Error(`Processing failed for video ${videoNum}`));
      }
    });
    ffmpeg.on('error', reject);
  });

  try { fs.unlinkSync(tempPath); } catch { }
}

router.get('/api/download/:token', (req, res) => {
  const { token } = req.params;
  const data = botDownloads.get(token);

  if (!data) {
    return res.status(404).send(`
      <!DOCTYPE html>
      <html>
      <head>
        <title>Download Not Found</title>
        <style>
          body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            display: flex;
            align-items: center;
            justify-content: center;
            height: 100vh;
            margin: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
          }
          .container {
            text-align: center;
            padding: 2rem;
          }
          h1 { font-size: 3rem; margin: 0; }
          p { font-size: 1.2rem; opacity: 0.9; }
        </style>
      </head>
      <body>
        <div class="container">
          <h1>X</h1>
          <h2>Download Not Found</h2>
          <p>This download link has expired or is invalid.</p>
        </div>
      </body>
      </html>
    `);
  }

  const downloadUrl = `/api/bot/download/${token}`;

  res.send(`
    <!DOCTYPE html>
    <html>
    <head>
      <title>Downloading...</title>
      <meta name="viewport" content="width=device-width, initial-scale=1">
      <style>
        body {
          font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
          display: flex;
          align-items: center;
          justify-content: center;
          height: 100vh;
          margin: 0;
          background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
          color: white;
        }
        .container {
          text-align: center;
          padding: 2rem;
        }
        .spinner {
          border: 4px solid rgba(255, 255, 255, 0.3);
          border-radius: 50%;
          border-top: 4px solid white;
          width: 50px;
          height: 50px;
          animation: spin 1s linear infinite;
          margin: 0 auto 1.5rem;
        }
        @keyframes spin {
          0% { transform: rotate(0deg); }
          100% { transform: rotate(360deg); }
        }
        h1 {
          font-size: 2rem;
          margin: 0 0 0.5rem;
        }
        p {
          font-size: 1.1rem;
          opacity: 0.9;
          margin: 0.5rem 0;
        }
        .filename {
          font-size: 0.9rem;
          opacity: 0.7;
          margin-top: 1rem;
          word-break: break-all;
          max-width: 400px;
          margin-left: auto;
          margin-right: auto;
        }
      </style>
    </head>
    <body>
      <div class="container">
        <div class="spinner"></div>
        <h1>Downloading...</h1>
        <p>Your download should start automatically.</p>
        <p class="filename">${data.fileName}</p>
        <p style="margin-top: 2rem; font-size: 0.85rem;">This page will close automatically.</p>
      </div>
      <iframe id="downloadFrame" style="display:none;"></iframe>
      <script>
        document.getElementById('downloadFrame').src = '${downloadUrl}';

        setTimeout(() => {
          window.close();
          setTimeout(() => {
            document.body.innerHTML = \`
              <div class="container">
                <h1>Done</h1>
                <h2>Download Started</h2>
                <p>You can close this page now.</p>
              </div>
            \`;
          }, 100);
        }, 2000);
      </script>
    </body>
    </html>
  `);
});

router.get('/api/bot/download/:token', (req, res) => {
  const { token } = req.params;
  const data = botDownloads.get(token);

  if (!data) {
    return res.status(404).json({ error: 'Download not found or expired' });
  }

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
        fs.unlink(data.filePath, () => { });
        botDownloads.delete(token);
        console.log(`[Bot] Token ${token.slice(0, 8)}... cleaned up after download`);
      }
    }, 30000);
  });
});

router.get('/api/bot/status/:jobId', (req, res) => {
  if (!checkBotAuth(req)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const { jobId } = req.params;
  const job = asyncJobs.get(jobId);

  if (!job) {
    return res.status(404).json({ error: 'Job not found' });
  }

  res.json({
    status: job.status,
    progress: job.progress,
    message: job.message,
    debugError: job.debugError,
    fileName: job.fileName,
    fileSize: job.fileSize,
    downloadToken: job.downloadToken
  });
});

module.exports = router;
