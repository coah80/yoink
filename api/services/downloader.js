const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const { QUALITY_HEIGHT } = require('../config/constants');
const { getCookiesArgs } = require('../utils/cookies');
const { downloadViaCobalt, streamClipFromCobalt } = require('./cobalt');

function parseYtdlpProgress(text) {
  const percentMatch = text.match(/([\d.]+)%/);
  const speedMatch = text.match(/at\s+([\d.]+\s*\w+\/s)/);
  const etaMatch = text.match(/ETA\s+(\S+)/);

  return {
    percent: percentMatch ? parseFloat(percentMatch[1]) : null,
    speed: speedMatch ? speedMatch[1] : null,
    eta: etaMatch ? etaMatch[1] : null
  };
}

async function downloadViaYtdlp(url, jobId, opts = {}) {
  const {
    isAudio = false,
    audioFormat = 'mp3',
    quality = '1080p',
    container = 'mp4',
    tempDir,
    filePrefix = '',
    processInfo = null,
    onProgress = null,
    playlist = false,
    onCancel = null
  } = opts;

  const tempFile = path.join(tempDir, `${filePrefix}${jobId}.%(ext)s`);

  const ytdlpArgs = [
    ...getCookiesArgs(),
    '--continue',
    '-t', 'sleep',
    playlist ? '--yes-playlist' : '--no-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s',
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
    if (processInfo) processInfo.process = ytdlp;

    let stderrOutput = '';

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString().trim();
      const parsed = parseYtdlpProgress(msg);
      if (parsed.percent !== null && (parsed.percent > lastProgress + 2 || parsed.percent >= 100)) {
        lastProgress = parsed.percent;
        if (onProgress) onProgress(parsed.percent, parsed.speed, parsed.eta);
      }
    });

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (msg.includes('[download]') && msg.includes('%')) {
        const parsed = parseYtdlpProgress(msg);
        if (parsed.percent !== null && (parsed.percent > lastProgress + 2 || parsed.percent >= 100)) {
          lastProgress = parsed.percent;
          if (onProgress) onProgress(parsed.percent, parsed.speed, parsed.eta);
        }
      }
    });

    ytdlp.on('close', (code) => {
      if (processInfo?.cancelled) {
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

    if (onCancel) onCancel(() => ytdlp.kill('SIGTERM'));
  });

  const files = fs.readdirSync(tempDir);
  const prefix = `${filePrefix}${jobId}`;
  const downloadedFile = files.find(f =>
    f.startsWith(prefix) &&
    !f.includes('-final') &&
    !f.includes('-cobalt') &&
    !f.includes('-clip') &&
    !f.includes('-trimmed') &&
    !f.endsWith('.part') &&
    !f.includes('.part-Frag')
  );

  if (!downloadedFile) {
    throw new Error('Downloaded file not found');
  }

  return {
    path: path.join(tempDir, downloadedFile),
    ext: path.extname(downloadedFile).slice(1)
  };
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

async function handleClipDownload(clipData, jobId, opts = {}) {
  const { tempDir, onProgress = null } = opts;

  const startTime = clipData.startTimeMs / 1000;
  const endTime = clipData.endTimeMs / 1000;
  const duration = (clipData.endTimeMs - clipData.startTimeMs) / 1000;
  const clipFile = path.join(tempDir, `${jobId}-clip.mp4`);

  try {
    const result = await streamClipFromCobalt(
      clipData.fullVideoUrl,
      jobId,
      startTime,
      endTime,
      clipFile,
      (progress) => {
        if (onProgress) onProgress(progress, null, null);
      }
    );
    return { path: result.filePath, ext: result.ext };
  } catch (streamErr) {
    console.error(`[${jobId}] Stream trim failed:`, streamErr.message);
    console.log(`[${jobId}] Falling back to full download + trim...`);
  }

  const cobaltResult = await downloadViaCobalt(clipData.fullVideoUrl, jobId, false, (progress) => {
    if (onProgress) onProgress(Math.floor(progress * 0.8), null, null);
  });

  const trimmedFile = path.join(tempDir, `${jobId}-trimmed.${cobaltResult.ext}`);

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', [
      '-ss', startTime.toString(),
      '-i', cobaltResult.filePath,
      '-t', duration.toString(),
      '-c', 'copy',
      '-avoid_negative_ts', 'make_zero',
      '-y',
      trimmedFile
    ]);
    ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error('Trim failed')));
    ffmpeg.on('error', reject);
  });

  try { fs.unlinkSync(cobaltResult.filePath); } catch {}

  return { path: trimmedFile, ext: cobaltResult.ext };
}

module.exports = {
  downloadViaYtdlp,
  getPlaylistInfo,
  handleClipDownload,
  parseYtdlpProgress
};
