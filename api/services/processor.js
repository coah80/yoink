const { spawn, execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const { CONTAINER_MIMES, AUDIO_MIMES } = require('../config/constants');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const {
  activeDownloads,
  activeProcesses,
  activeJobsByType,
  removePendingJob,
  unlinkJobFromClient,
  sendProgress
} = require('./state');

async function processVideo(inputPath, outputPath, opts = {}) {
  const {
    isAudio = false,
    isGif = false,
    audioFormat = 'mp3',
    audioBitrate = '320',
    container = 'mp4',
    jobId = ''
  } = opts;

  const inputExt = path.extname(inputPath).slice(1).toLowerCase();
  const outputExt = isGif ? 'gif' : (isAudio ? audioFormat : container);

  if (!isAudio && !isGif && inputExt === outputExt) {
    console.log(`[${jobId}] Format match (${inputExt}), skipping ffmpeg`);
    return { path: inputPath, ext: inputExt, skipped: true };
  }

  const ffmpegArgs = ['-y', '-i', inputPath];

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

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', ffmpegArgs);
    let ffmpegStderr = '';

    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      ffmpegStderr += msg;
      if (msg.includes('Error') || msg.includes('error')) {
        console.error(`[${jobId}] ffmpeg: ${msg.trim()}`);
      }
    });

    ffmpeg.on('close', (code) => {
      if (code === 0) resolve();
      else {
        console.error(`[${jobId}] FFmpeg failed with code ${code}. Last 500 chars: ${ffmpegStderr.slice(-500)}`);
        reject(new Error(`Encoding failed (code ${code})`));
      }
    });

    ffmpeg.on('error', (err) => {
      console.error(`[${jobId}] FFmpeg spawn error: ${err.message}`);
      reject(err);
    });
  });

  return { path: outputPath, ext: outputExt, skipped: false };
}

async function streamFile(res, req, filePath, opts = {}) {
  const {
    filename = 'download',
    ext = 'mp4',
    mimeType = 'video/mp4',
    downloadId,
    url = '',
    jobType = 'download',
    onCleanup = null
  } = opts;

  const stat = fs.statSync(filePath);
  const safeFilename = sanitizeFilename(filename);
  const fullFilename = `${safeFilename}.${ext}`;
  const asciiFilename = safeFilename.replace(/[^\x20-\x7E]/g, '_') + '.' + ext;

  sendProgress(downloadId, 'sending', 'Sending file to you...');

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
    activeJobsByType[jobType]--;
    unlinkJobFromClient(downloadId);

    console.log(`[Queue] ${jobType} finished. Active: ${JSON.stringify(activeJobsByType)}`);
    if (onCleanup) onCleanup();
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
    activeJobsByType[jobType]--;
    unlinkJobFromClient(downloadId);
    setTimeout(() => cleanupJobFiles(downloadId), 2000);
  });

  req.on('close', () => {
    if (finished) return;
    finished = true;
    stream.destroy();
    activeProcesses.delete(downloadId);
    removePendingJob(downloadId);
    activeJobsByType[jobType]--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] ${jobType} cancelled. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => {
      try { fs.unlinkSync(filePath); } catch {}
    }, 1000);
  });
}

function probeForGif(filePath) {
  try {
    const probeResult = execSync(
      `ffprobe -v quiet -print_format json -show_streams -show_format "${filePath}"`,
      { encoding: 'utf8' }
    );
    const probe = JSON.parse(probeResult);
    const hasAudio = probe.streams?.some(s => s.codec_type === 'audio');
    const duration = parseFloat(probe.format?.duration || '999');
    return !hasAudio && duration < 60;
  } catch (e) {
    return false;
  }
}

function getMimeType(ext, isAudio, isGif) {
  if (isGif) return 'image/gif';
  if (isAudio) return AUDIO_MIMES[ext] || 'audio/mpeg';
  return CONTAINER_MIMES[ext] || 'video/mp4';
}

module.exports = {
  processVideo,
  streamFile,
  probeForGif,
  getMimeType
};
