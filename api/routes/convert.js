const express = require('express');
const router = express.Router();
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');
const { v4: uuidv4 } = require('uuid');
const multer = require('multer');

const {
  TEMP_DIRS,
  SAFETY_LIMITS,
  FILE_SIZE_LIMIT,
  CONTAINER_MIMES,
  AUDIO_MIMES,
  ALLOWED_MODES,
  ALLOWED_QUALITIES,
  ALLOWED_PRESETS,
  ALLOWED_DENOISE,
  ALLOWED_FORMATS,
  ALLOWED_REENCODES,
  ASYNC_JOB_TIMEOUT,
  CHUNK_TIMEOUT
} = require('../config/constants');

const {
  activeProcesses,
  activeJobsByType,
  asyncJobs,
  canStartJob,
  registerClient,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount,
  sendProgress
} = require('../services/state');

const { validateVideoFile, validateTimeParam } = require('../utils/validation');
const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');
const {
  COMPRESSION_CONFIG,
  selectResolution,
  getDenoiseFilter,
  getDownscaleResolution,
  buildVideoFilters,
  calculateTargetBitrate,
  formatETA
} = require('../utils/ffmpeg');
const discordAlerts = require('../discord-alerts');

const upload = multer({
  dest: TEMP_DIRS.upload,
  limits: { fileSize: FILE_SIZE_LIMIT }
});

const chunkedUploads = new Map();

setInterval(() => {
  const now = Date.now();
  for (const [uploadId, data] of chunkedUploads.entries()) {
    if (now - data.lastActivity > CHUNK_TIMEOUT) {
      console.log(`[Chunk] Upload ${uploadId} timed out, cleaning up`);
      try {
        const files = fs.readdirSync(TEMP_DIRS.upload).filter(f => f.startsWith(`chunk-${uploadId}-`));
        for (const f of files) {
          fs.unlinkSync(path.join(TEMP_DIRS.upload, f));
        }
      } catch { }
      chunkedUploads.delete(uploadId);
    }
  }
}, 60000);

setInterval(() => {
  const now = Date.now();
  for (const [jobId, job] of asyncJobs.entries()) {
    if (now - job.createdAt > ASYNC_JOB_TIMEOUT) {
      console.log(`[AsyncJob] Job ${jobId} expired, cleaning up`);
      if (job.outputPath) {
        fs.unlink(job.outputPath, () => { });
      }
      asyncJobs.delete(jobId);
    }
  }
}, 60000);

router.post('/api/convert', upload.single('file'), (req, res) => handleConvert(req, res));
router.post('/api/compress', upload.single('file'), (req, res) => handleCompress(req, res));

router.post('/api/upload/init', express.json(), (req, res) => {
  const { fileName, fileSize, totalChunks } = req.body;

  if (!fileName || !fileSize || !totalChunks) {
    return res.status(400).json({ error: 'Missing fileName, fileSize, or totalChunks' });
  }

  const numericFileSize = Number(fileSize);
  if (!Number.isFinite(numericFileSize) || numericFileSize <= 0) {
    return res.status(400).json({ error: 'fileSize must be a positive number' });
  }
  if (numericFileSize > FILE_SIZE_LIMIT) {
    return res.status(400).json({ error: `File too large. Maximum size is ${FILE_SIZE_LIMIT / (1024 * 1024 * 1024)}GB` });
  }

  if (totalChunks > 200) {
    return res.status(400).json({ error: 'Too many chunks (max 200)' });
  }

  const uploadId = uuidv4();
  chunkedUploads.set(uploadId, {
    fileName,
    fileSize: numericFileSize,
    totalChunks,
    receivedChunks: new Set(),
    lastActivity: Date.now()
  });

  console.log(`[Chunk] Initialized upload ${uploadId}: (${(fileSize / 1024 / 1024).toFixed(1)}MB, ${totalChunks} chunks)`);
  res.json({ uploadId });
});

router.post('/api/upload/chunk/:uploadId/:chunkIndex', upload.single('chunk'), (req, res) => {
  const { uploadId, chunkIndex } = req.params;
  const index = parseInt(chunkIndex, 10);

  const uploadData = chunkedUploads.get(uploadId);
  if (!uploadData) {
    if (req.file) fs.unlink(req.file.path, () => { });
    return res.status(404).json({ error: 'Upload not found or expired' });
  }

  if (!req.file) {
    return res.status(400).json({ error: 'No chunk data' });
  }

  if (index < 0 || index >= uploadData.totalChunks) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: 'Invalid chunk index' });
  }

  const chunkPath = path.join(TEMP_DIRS.upload, `chunk-${uploadId}-${String(index).padStart(4, '0')}`);

  try {
    fs.renameSync(req.file.path, chunkPath);
  } catch (err) {
    console.error(`[Chunk] Failed to save chunk ${index} for upload ${uploadId}:`, err.message);
    try { fs.unlinkSync(req.file.path); } catch { }
    return res.status(500).json({ error: 'Failed to save chunk. Disk may be full or permissions issue.' });
  }

  uploadData.receivedChunks.add(index);
  uploadData.lastActivity = Date.now();

  const received = uploadData.receivedChunks.size;
  const total = uploadData.totalChunks;
  console.log(`[Chunk] Upload ${uploadId}: chunk ${index + 1}/${total}`);

  res.json({ received, total, complete: received === total });
});

router.post('/api/upload/complete/:uploadId', express.json(), async (req, res) => {
  const { uploadId } = req.params;

  const uploadData = chunkedUploads.get(uploadId);
  if (!uploadData) {
    return res.status(404).json({ error: 'Upload not found or expired' });
  }

  if (uploadData.receivedChunks.size !== uploadData.totalChunks) {
    return res.status(400).json({
      error: `Missing chunks: received ${uploadData.receivedChunks.size}/${uploadData.totalChunks}`
    });
  }

  const assembledPath = path.join(TEMP_DIRS.upload, `assembled-${uploadId}-${sanitizeFilename(uploadData.fileName)}`);

  try {
    const writeStream = fs.createWriteStream(assembledPath);

    for (let i = 0; i < uploadData.totalChunks; i++) {
      const chunkPath = path.join(TEMP_DIRS.upload, `chunk-${uploadId}-${String(i).padStart(4, '0')}`);

      await new Promise((resolve, reject) => {
        const readStream = fs.createReadStream(chunkPath);
        readStream.on('error', reject);
        readStream.on('end', () => {
          fs.unlink(chunkPath, () => { });
          resolve();
        });
        readStream.pipe(writeStream, { end: false });
      });
    }

    await new Promise((resolve, reject) => {
      writeStream.on('finish', resolve);
      writeStream.on('error', reject);
      writeStream.end();
    });

    chunkedUploads.delete(uploadId);

    console.log(`[Chunk] Upload ${uploadId} assembled: ${assembledPath}`);
    res.json({ success: true, filePath: assembledPath, fileName: uploadData.fileName });

  } catch (err) {
    console.error(`[Chunk] Assembly failed for ${uploadId}:`, err);
    chunkedUploads.delete(uploadId);
    res.status(500).json({ error: 'Failed to assemble file' });
  }
});

router.get('/api/job/:jobId/status', (req, res) => {
  const { jobId } = req.params;
  const job = asyncJobs.get(jobId);

  if (!job) {
    return res.status(404).json({ error: 'Job not found or expired' });
  }

  res.json({
    status: job.status,
    progress: job.progress || 0,
    message: job.message || '',
    error: job.error || null
  });
});

router.get('/api/job/:jobId/download', (req, res) => {
  const { jobId } = req.params;
  const job = asyncJobs.get(jobId);

  if (!job) {
    return res.status(404).json({ error: 'Job not found or expired' });
  }

  if (job.status !== 'complete') {
    return res.status(400).json({ error: 'Job not complete yet' });
  }

  if (!job.outputPath || !fs.existsSync(job.outputPath)) {
    return res.status(404).json({ error: 'Output file not found' });
  }

  const stat = fs.statSync(job.outputPath);

  res.setHeader('Content-Type', job.mimeType || 'video/mp4');
  res.setHeader('Content-Length', stat.size);
  res.setHeader('Content-Disposition', `attachment; filename="${job.outputFilename}"; filename*=UTF-8''${encodeURIComponent(job.outputFilename)}`);

  const stream = fs.createReadStream(job.outputPath);
  stream.pipe(res);

  stream.on('close', () => {
    setTimeout(() => {
      fs.unlink(job.outputPath, () => { });
      asyncJobs.delete(jobId);
    }, 5000);
  });
});

function validateChunkedFilePath(filePath) {
  if (!filePath || typeof filePath !== 'string') return null;
  const resolved = path.resolve(filePath);
  if (!resolved.startsWith(TEMP_DIRS.upload)) return null;
  return resolved;
}

router.post('/api/compress-chunked', express.json(), async (req, res) => {
  const { filePath, fileName, clientId, ...options } = req.body;

  const validPath = validateChunkedFilePath(filePath);
  if (!validPath) {
    return res.status(400).json({ error: 'Invalid file path' });
  }

  if (!fs.existsSync(validPath)) {
    return res.status(400).json({ error: 'File not found. Complete chunked upload first.' });
  }

  req.file = { path: validPath, originalname: fileName || 'video.mp4' };
  req.body = { ...options, clientId };

  const jobId = uuidv4();

  asyncJobs.set(jobId, {
    status: 'processing',
    progress: 0,
    message: 'Starting compression...',
    createdAt: Date.now(),
    outputPath: null,
    outputFilename: null,
    mimeType: null
  });

  res.json({ jobId });

  handleCompressAsync(req, jobId).catch(err => {
    console.error(`[AsyncJob] Job ${jobId} failed:`, err);
    const job = asyncJobs.get(jobId);
    if (job) {
      job.status = 'error';
      job.error = err.message || 'Compression failed';
    }
  });
});

router.post('/api/convert-chunked', express.json(), async (req, res) => {
  const {
    filePath,
    fileName,
    format = 'mp4',
    clientId,
    quality = 'medium',
    reencode = 'auto',
    startTime,
    endTime,
    audioBitrate = '192'
  } = req.body;

  const validPath = validateChunkedFilePath(filePath);
  if (!validPath) {
    return res.status(400).json({ error: 'Invalid file path' });
  }

  if (!fs.existsSync(validPath)) {
    return res.status(400).json({ error: 'File not found. Complete chunked upload first.' });
  }

  if (!ALLOWED_FORMATS.includes(format)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid format. Allowed: ${ALLOWED_FORMATS.join(', ')}` });
  }
  if (!ALLOWED_REENCODES.includes(reencode)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid reencode option. Allowed: ${ALLOWED_REENCODES.join(', ')}` });
  }
  if (!ALLOWED_QUALITIES.includes(quality)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid quality. Allowed: ${ALLOWED_QUALITIES.join(', ')}` });
  }

  req.file = { path: validPath, originalname: fileName || 'video.mp4' };
  req.body.format = format;
  req.body.clientId = clientId;
  req.body.quality = quality;
  req.body.reencode = reencode;
  req.body.startTime = startTime;
  req.body.endTime = endTime;
  req.body.audioBitrate = audioBitrate;

  const jobId = uuidv4();

  asyncJobs.set(jobId, {
    status: 'processing',
    progress: 0,
    message: 'Starting conversion...',
    createdAt: Date.now(),
    outputPath: null,
    outputFilename: null,
    mimeType: null
  });

  res.json({ jobId });

  handleConvertAsync(req, jobId).catch(err => {
    console.error(`[AsyncJob] Convert job ${jobId} failed:`, err);
    const job = asyncJobs.get(jobId);
    if (job) {
      job.status = 'error';
      job.error = err.message || 'Conversion failed';
    }
  });
});

async function handleConvert(req, res) {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const {
    format = 'mp4',
    clientId,
    quality = 'medium',
    reencode = 'auto',
    startTime,
    endTime,
    audioBitrate = '192'
  } = req.body;

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => { });
      return res.status(429).json({
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.`
      });
    }
  }

  const convertJobCheck = canStartJob('convert');
  if (!convertJobCheck.ok) {
    fs.unlink(req.file.path, () => {});
    return res.status(503).json({ error: convertJobCheck.reason });
  }

  const convertId = uuidv4();
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIRS.convert, `${convertId}-converted.${format}`);

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(convertId, clientId);
  }

  activeJobsByType.convert++;
  console.log(`[Queue] Convert started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${convertId}] Converting to ${format}`);

  try {
    const isAudioFormat = ['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format);

    const validStartTime = validateTimeParam(startTime);
    const validEndTime = validateTimeParam(endTime);

    if (startTime && validStartTime === null) {
      fs.unlink(inputPath, () => { });
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      return res.status(400).json({ error: 'Invalid startTime format. Use seconds or HH:MM:SS' });
    }
    if (endTime && validEndTime === null) {
      fs.unlink(inputPath, () => { });
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      return res.status(400).json({ error: 'Invalid endTime format. Use seconds or HH:MM:SS' });
    }

    const ffmpegArgs = ['-y'];

    if (validStartTime) ffmpegArgs.push('-ss', validStartTime);
    if (validEndTime) ffmpegArgs.push('-to', validEndTime);

    ffmpegArgs.push('-i', inputPath, '-threads', '0');

    if (isAudioFormat) {
      if (format === 'mp3') {
        ffmpegArgs.push('-codec:a', 'libmp3lame', '-b:a', `${audioBitrate}k`);
      } else if (format === 'm4a') {
        ffmpegArgs.push('-codec:a', 'aac', '-b:a', `${audioBitrate}k`);
      } else if (format === 'opus') {
        ffmpegArgs.push('-codec:a', 'libopus', '-b:a', '128k');
      } else if (format === 'wav') {
        ffmpegArgs.push('-codec:a', 'pcm_s16le');
      } else if (format === 'flac') {
        ffmpegArgs.push('-codec:a', 'flac');
      }
      ffmpegArgs.push('-vn');
    } else {
      const codecCompatibility = {
        'mp4': ['h264', 'avc', 'hevc', 'h265'],
        'webm': ['vp8', 'vp9', 'av1'],
        'mkv': ['*'],
        'mov': ['h264', 'hevc', 'prores']
      };

      const probeCodec = await new Promise((resolve) => {
        const ffprobe = spawn('ffprobe', [
          '-v', 'error', '-select_streams', 'v:0',
          '-show_entries', 'stream=codec_name', '-of', 'csv=p=0', inputPath
        ]);
        let out = '';
        ffprobe.stdout.on('data', (d) => { out += d.toString(); });
        ffprobe.on('close', () => resolve(out.trim().toLowerCase()));
        ffprobe.on('error', () => resolve('unknown'));
      });

      const compat = codecCompatibility[format] || [];
      const isCompatible = compat.includes('*') || compat.some(c => probeCodec.includes(c));
      const needsReencode = reencode === 'always' || (reencode === 'auto' && !isCompatible);

      if (needsReencode) {
        const crfValues = { high: 18, medium: 23, low: 28 };
        const crf = crfValues[quality] || 23;
        console.log(`[${convertId}] Re-encoding video (${probeCodec} → h264, CRF ${crf})`);
        ffmpegArgs.push(
          '-c:v', 'libx264',
          '-preset', 'medium',
          '-crf', String(crf),
          '-pix_fmt', 'yuv420p',
          '-c:a', 'aac', '-b:a', '128k'
        );
      } else {
        ffmpegArgs.push('-codec', 'copy');
      }

      if (format === 'mp4' || format === 'mov') {
        ffmpegArgs.push('-movflags', '+faststart');
      }
    }

    ffmpegArgs.push(outputPath);

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', ffmpegArgs);

      ffmpeg.stderr.on('data', (data) => {
        const msg = data.toString();
        if (msg.includes('Error')) {
          console.error(`[${convertId}] ffmpeg: ${msg.trim()}`);
        }
      });

      ffmpeg.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Conversion failed with code ${code}`));
      });

      ffmpeg.on('error', reject);
    });

    try { fs.unlinkSync(inputPath); } catch { }

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}.${format}`;
    const mimeType = isAudioFormat
      ? (AUDIO_MIMES[format] || 'audio/mpeg')
      : (CONTAINER_MIMES[format] || 'video/mp4');

    res.setHeader('Content-Type', mimeType);
    res.setHeader('Content-Length', stat.size);
    res.setHeader('Content-Disposition', `attachment; filename="${outputFilename}"; filename*=UTF-8''${encodeURIComponent(outputFilename)}`);

    const stream = fs.createReadStream(outputPath);
    stream.pipe(res);

    stream.on('close', () => {
      console.log(`[${convertId}] Conversion complete`);
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);

      console.log(`[Queue] Convert finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(convertId), 2000);
    });

    stream.on('error', () => {
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      console.log(`[Queue] Convert failed. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(convertId), 2000);
    });

  } catch (err) {
    console.error(`[${convertId}] Error:`, err);
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    console.log(`[Queue] Convert error. Active: ${JSON.stringify(activeJobsByType)}`);
    setTimeout(() => cleanupJobFiles(convertId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Conversion failed' });
    }
  }
}

async function handleConvertAsync(req, jobId) {
  const job = asyncJobs.get(jobId);
  if (!job) return;

  const {
    format = 'mp4',
    clientId,
    quality = 'medium',
    reencode = 'auto',
    startTime,
    endTime,
    audioBitrate = '192'
  } = req.body;

  const convertId = jobId;
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIRS.convert, `${convertId}-converted.${format}`);

  const asyncConvertCheck = canStartJob('convert');
  if (!asyncConvertCheck.ok) {
    try { fs.unlinkSync(inputPath); } catch {}
    job.status = 'error';
    job.error = asyncConvertCheck.reason;
    return;
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(convertId, clientId);
  }

  activeJobsByType.convert++;
  console.log(`[Queue] Async convert started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${convertId}] Converting to ${format} (async)`);

  if (startTime && !validateTimeParam(startTime)) {
    try { fs.unlinkSync(inputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    job.status = 'error';
    job.error = 'Invalid startTime format';
    job.progress = 0;
    return;
  }
  if (endTime && !validateTimeParam(endTime)) {
    try { fs.unlinkSync(inputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    job.status = 'error';
    job.error = 'Invalid endTime format';
    job.progress = 0;
    return;
  }

  try {
    const isAudioFormat = ['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format);
    const validStartTime = validateTimeParam(startTime);
    const validEndTime = validateTimeParam(endTime);

    job.message = 'Analyzing file...';
    job.progress = 5;

    const ffmpegArgs = ['-y'];

    if (validStartTime) ffmpegArgs.push('-ss', validStartTime);
    if (validEndTime) ffmpegArgs.push('-to', validEndTime);

    ffmpegArgs.push('-i', inputPath, '-threads', '0');

    if (isAudioFormat) {
      if (format === 'mp3') {
        ffmpegArgs.push('-codec:a', 'libmp3lame', '-b:a', `${audioBitrate}k`);
      } else if (format === 'm4a') {
        ffmpegArgs.push('-codec:a', 'aac', '-b:a', `${audioBitrate}k`);
      } else if (format === 'opus') {
        ffmpegArgs.push('-codec:a', 'libopus', '-b:a', '128k');
      } else if (format === 'wav') {
        ffmpegArgs.push('-codec:a', 'pcm_s16le');
      } else if (format === 'flac') {
        ffmpegArgs.push('-codec:a', 'flac');
      }
      ffmpegArgs.push('-vn');
    } else {
      const codecCompatibility = {
        'mp4': ['h264', 'avc', 'hevc', 'h265'],
        'webm': ['vp8', 'vp9', 'av1'],
        'mkv': ['*'],
        'mov': ['h264', 'hevc', 'prores']
      };

      const probeCodec = await new Promise((resolve) => {
        const ffprobe = spawn('ffprobe', [
          '-v', 'error', '-select_streams', 'v:0',
          '-show_entries', 'stream=codec_name', '-of', 'csv=p=0', inputPath
        ]);
        let out = '';
        ffprobe.stdout.on('data', (d) => { out += d.toString(); });
        ffprobe.on('close', () => resolve(out.trim().toLowerCase()));
        ffprobe.on('error', () => resolve('unknown'));
      });

      const compat = codecCompatibility[format] || [];
      const isCompatible = compat.includes('*') || compat.some(c => probeCodec.includes(c));
      const needsReencode = reencode === 'always' || (reencode === 'auto' && !isCompatible);

      if (needsReencode) {
        const crfValues = { high: 18, medium: 23, low: 28 };
        const crf = crfValues[quality] || 23;
        ffmpegArgs.push('-c:v', 'libx264', '-preset', 'medium', '-crf', String(crf), '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-b:a', '128k');
      } else {
        ffmpegArgs.push('-codec', 'copy');
      }

      if (format === 'mp4' || format === 'mov') {
        ffmpegArgs.push('-movflags', '+faststart');
      }
    }

    ffmpegArgs.push(outputPath);

    job.message = 'Converting...';
    job.progress = 10;

    const duration = await new Promise((resolve) => {
      const ffprobe = spawn('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', inputPath]);
      let out = '';
      ffprobe.stdout.on('data', (d) => { out += d.toString(); });
      ffprobe.on('close', () => resolve(parseFloat(out) || 60));
      ffprobe.on('error', () => resolve(60));
    });

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', ffmpegArgs);

      ffmpeg.stderr.on('data', (data) => {
        const msg = data.toString();
        const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
        if (timeMatch) {
          const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
          const progress = Math.min(95, 10 + (currentTime / duration) * 85);

          const speedMatch = msg.match(/speed=\s*([\d.]+)x/);
          const speed = speedMatch ? parseFloat(speedMatch[1]) : null;
          const eta = speed ? formatETA((duration - currentTime) / speed) : null;

          job.progress = Math.round(progress);
          job.message = eta ? `Converting... ${Math.round(progress)}% (ETA: ${eta})` : `Converting... ${Math.round(progress)}%`;
        }
      });

      ffmpeg.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Conversion failed with code ${code}`));
      });

      ffmpeg.on('error', reject);
    });

    try { fs.unlinkSync(inputPath); } catch { }

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}.${format}`;
    const mimeType = isAudioFormat
      ? (AUDIO_MIMES[format] || 'audio/mpeg')
      : (CONTAINER_MIMES[format] || 'video/mp4');

    console.log(`[${convertId}] Async conversion complete`);

    job.status = 'complete';
    job.progress = 100;
    job.message = 'Conversion complete!';
    job.outputPath = outputPath;
    job.outputFilename = outputFilename;
    job.mimeType = mimeType;

    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);

    console.log(`[Queue] Async convert finished. Active: ${JSON.stringify(activeJobsByType)}`);

  } catch (err) {
    console.error(`[${convertId}] Async convert error:`, err);
    try { fs.unlinkSync(inputPath); } catch { }
    try { fs.unlinkSync(outputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);

    job.status = 'error';
    job.error = err.message || 'Conversion failed';
  }
}

async function handleCompress(req, res) {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const {
    targetSize = '50',
    duration = '0',
    progressId,
    clientId,
    mode = 'size',
    quality = 'medium',
    preset = 'balanced',
    denoise = 'auto',
    downscale = false
  } = req.body;

  const shouldDownscale = downscale === true || downscale === 'true';

  if (!ALLOWED_MODES.includes(mode)) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: `Invalid mode. Allowed: ${ALLOWED_MODES.join(', ')}` });
  }
  if (!ALLOWED_QUALITIES.includes(quality)) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: `Invalid quality. Allowed: ${ALLOWED_QUALITIES.join(', ')}` });
  }
  if (!ALLOWED_PRESETS.includes(preset)) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: `Invalid preset. Allowed: ${ALLOWED_PRESETS.join(', ')}` });
  }
  if (!ALLOWED_DENOISE.includes(denoise)) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: `Invalid denoise. Allowed: ${ALLOWED_DENOISE.join(', ')}` });
  }

  const targetMB = parseFloat(targetSize);
  const videoDuration = parseFloat(duration);

  if (videoDuration > SAFETY_LIMITS.maxVideoDuration) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({
      error: `Video too long. Maximum duration is ${SAFETY_LIMITS.maxVideoDuration / 3600} hours.`
    });
  }

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => { });
      return res.status(429).json({
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.`
      });
    }
  }

  const compressJobCheck = canStartJob('compress');
  if (!compressJobCheck.ok) {
    fs.unlink(req.file.path, () => {});
    return res.status(503).json({ error: compressJobCheck.reason });
  }

  const compressId = progressId || uuidv4();
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIRS.compress, `${compressId}-compressed.mp4`);
  const passLogFile = path.join(TEMP_DIRS.compress, `${compressId}-pass`);

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(compressId, clientId);
  }

  activeJobsByType.compress++;
  console.log(`[Queue] Compress started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${compressId}] Compressing | Mode: ${mode} | Preset: ${preset}`);

  const processInfo = { cancelled: false, process: null, tempFile: outputPath };
  activeProcesses.set(compressId, processInfo);

  try {
    sendProgress(compressId, 'compressing', 'Analyzing video...', 0);

    if (!validateVideoFile(inputPath)) {
      throw new Error('File does not contain valid video');
    }

    const probeResult = await probeVideo(inputPath);
    const actualDuration = videoDuration > 0 ? videoDuration : probeResult.duration;
    const sourceWidth = probeResult.width;
    const sourceHeight = probeResult.height;
    const sourceFileSizeMB = fs.statSync(inputPath).size / (1024 * 1024);
    const sourceBitrateMbps = (sourceFileSizeMB * 8) / actualDuration;

    const presetConfig = COMPRESSION_CONFIG.presets[preset];
    const denoiseFilter = getDenoiseFilter(denoise, sourceHeight, sourceBitrateMbps, presetConfig.denoise);
    const downscaleWidth = shouldDownscale ? getDownscaleResolution(sourceWidth, sourceHeight) : null;

    if (denoiseFilter) {
      console.log(`[${compressId}] Denoise: ${denoise === 'auto' ? 'auto-detected' : denoise}`);
    }
    if (downscaleWidth) {
      console.log(`[${compressId}] Downscaling to ${downscaleWidth}p`);
    }

    if (mode === 'quality') {
      const crf = presetConfig.crf[quality];
      const vfArg = buildVideoFilters(denoiseFilter, downscaleWidth, sourceWidth);

      console.log(`[${compressId}] CRF mode: preset=${preset}, quality=${quality}, crf=${crf}`);
      sendProgress(compressId, 'compressing', `Encoding (${preset})...`, 5);

      await runCrfEncode({
        inputPath,
        outputPath,
        crf,
        ffmpegPreset: presetConfig.ffmpegPreset,
        vfArg,
        x264Params: presetConfig.x264Params,
        processInfo,
        compressId,
        actualDuration
      });

    } else {
      if (sourceFileSizeMB <= targetMB) {
        console.log(`[${compressId}] Already under target, remuxing`);
        sendProgress(compressId, 'compressing', 'File already small enough...', 50);

        await new Promise((resolve, reject) => {
          const ffmpeg = spawn('ffmpeg', ['-y', '-i', inputPath, '-c:v', 'copy', '-c:a', 'copy', '-movflags', '+faststart', outputPath]);
          processInfo.process = ffmpeg;
          ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error('Remux failed')));
          ffmpeg.on('error', reject);
        });
      } else {
        const videoBitrateK = calculateTargetBitrate(targetMB, actualDuration, 96);
        const resolution = selectResolution(sourceWidth, sourceHeight, videoBitrateK);
        const scaleWidth = downscaleWidth || (resolution.needsScale ? resolution.width : null);
        const vfArg = buildVideoFilters(denoiseFilter, scaleWidth, sourceWidth);

        console.log(`[${compressId}] Two-pass: target=${targetMB}MB, bitrate=${videoBitrateK}k, res=${resolution.width}x${resolution.height}`);

        await runTwoPassEncode({
          inputPath,
          outputPath,
          passLogFile,
          videoBitrateK,
          ffmpegPreset: presetConfig.ffmpegPreset,
          vfArg,
          x264Params: presetConfig.x264Params,
          processInfo,
          compressId,
          actualDuration
        });
      }
    }

    try { fs.unlinkSync(inputPath); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log`); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log.mbtree`); } catch { }

    sendProgress(compressId, 'compressing', 'Sending file...', 98);

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}_compressed.mp4`;

    console.log(`[${compressId}] Complete: ${(stat.size / 1024 / 1024).toFixed(2)}MB`);

    res.setHeader('Content-Type', 'video/mp4');
    res.setHeader('Content-Length', stat.size);
    res.setHeader('Content-Disposition', `attachment; filename="${outputFilename}"; filename*=UTF-8''${encodeURIComponent(outputFilename)}`);

    const stream = fs.createReadStream(outputPath);
    stream.pipe(res);

    stream.on('close', () => {
      sendProgress(compressId, 'complete', 'Compression complete!', 100);
      activeProcesses.delete(compressId);
      activeJobsByType.compress--;
      unlinkJobFromClient(compressId);
      console.log(`[Queue] Compress finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(compressId), 2000);
    });

    stream.on('error', () => {
      activeJobsByType.compress--;
      unlinkJobFromClient(compressId);
      setTimeout(() => cleanupJobFiles(compressId), 2000);
    });

  } catch (err) {
    console.error(`[${compressId}] Error:`, err.message);
    discordAlerts.compressionError('Compression Error', 'Video compression failed.', { jobId: compressId, error: err.message });
    activeProcesses.delete(compressId);
    activeJobsByType.compress--;
    unlinkJobFromClient(compressId);

    setTimeout(() => cleanupJobFiles(compressId), 2000);

    if (!processInfo.cancelled) {
      sendProgress(compressId, 'error', err.message || 'Compression failed');
    }

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Compression failed' });
    }
  }
}

async function handleCompressAsync(req, jobId) {
  const job = asyncJobs.get(jobId);
  if (!job) return;

  const {
    targetSize = '50',
    duration = '0',
    clientId,
    mode = 'size',
    quality = 'medium',
    preset = 'balanced',
    denoise = 'auto',
    downscale = false
  } = req.body;

  const shouldDownscale = downscale === true || downscale === 'true';
  const targetMB = parseFloat(targetSize);
  const videoDuration = parseFloat(duration);

  const compressId = jobId;
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIRS.compress, `${compressId}-compressed.mp4`);
  const passLogFile = path.join(TEMP_DIRS.compress, `${compressId}-pass`);

  if (isNaN(targetMB) || targetMB <= 0) {
    try { fs.unlinkSync(inputPath); } catch { }
    job.status = 'error';
    job.error = 'Invalid target size';
    return;
  }

  if (isNaN(videoDuration) || videoDuration <= 0) {
    try { fs.unlinkSync(inputPath); } catch { }
    job.status = 'error';
    job.error = 'Invalid video duration';
    return;
  }

  const asyncCompressCheck = canStartJob('compress');
  if (!asyncCompressCheck.ok) {
    try { fs.unlinkSync(inputPath); } catch {}
    job.status = 'error';
    job.error = asyncCompressCheck.reason;
    return;
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(compressId, clientId);
  }

  activeJobsByType.compress++;
  console.log(`[${compressId}] Async compress | Mode: ${mode} | Preset: ${preset}`);

  const processInfo = { cancelled: false, process: null, tempFile: outputPath };
  activeProcesses.set(compressId, processInfo);

  try {
    job.message = 'Analyzing video...';

    if (!validateVideoFile(inputPath)) {
      throw new Error('File does not contain valid video');
    }

    const probeResult = await probeVideo(inputPath);
    const actualDuration = videoDuration > 0 ? videoDuration : probeResult.duration;
    const sourceWidth = probeResult.width;
    const sourceHeight = probeResult.height;
    const sourceFileSizeMB = fs.statSync(inputPath).size / (1024 * 1024);
    const sourceBitrateMbps = (sourceFileSizeMB * 8) / actualDuration;

    const presetConfig = COMPRESSION_CONFIG.presets[preset] || COMPRESSION_CONFIG.presets.balanced;
    const denoiseFilter = getDenoiseFilter(denoise, sourceHeight, sourceBitrateMbps, presetConfig.denoise);
    const downscaleWidth = shouldDownscale ? getDownscaleResolution(sourceWidth, sourceHeight) : null;

    if (mode === 'quality') {
      const crf = presetConfig.crf[quality];
      const vfArg = buildVideoFilters(denoiseFilter, downscaleWidth, sourceWidth);

      job.message = `Encoding (${preset})...`;
      job.progress = 5;

      await runCrfEncodeAsync({
        inputPath,
        outputPath,
        crf,
        ffmpegPreset: presetConfig.ffmpegPreset,
        vfArg,
        x264Params: presetConfig.x264Params,
        processInfo,
        actualDuration,
        job
      });

    } else {
      if (sourceFileSizeMB <= targetMB) {
        job.message = 'Already under target...';
        job.progress = 50;

        await new Promise((resolve, reject) => {
          const ffmpeg = spawn('ffmpeg', ['-y', '-i', inputPath, '-c:v', 'copy', '-c:a', 'copy', '-movflags', '+faststart', outputPath]);
          processInfo.process = ffmpeg;
          ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error('Remux failed')));
          ffmpeg.on('error', reject);
        });
      } else {
        const videoBitrateK = calculateTargetBitrate(targetMB, actualDuration, 96);
        const resolution = selectResolution(sourceWidth, sourceHeight, videoBitrateK);
        const scaleWidth = downscaleWidth || (resolution.needsScale ? resolution.width : null);
        const vfArg = buildVideoFilters(denoiseFilter, scaleWidth, sourceWidth);

        await runTwoPassEncodeAsync({
          inputPath,
          outputPath,
          passLogFile,
          videoBitrateK,
          ffmpegPreset: presetConfig.ffmpegPreset,
          vfArg,
          x264Params: presetConfig.x264Params,
          processInfo,
          actualDuration,
          job
        });
      }
    }

    try { fs.unlinkSync(inputPath); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log`); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log.mbtree`); } catch { }

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}_compressed.mp4`;

    console.log(`[${compressId}] Complete: ${(stat.size / 1024 / 1024).toFixed(2)}MB`);

    job.status = 'complete';
    job.progress = 100;
    job.message = 'Complete!';
    job.outputPath = outputPath;
    job.outputFilename = outputFilename;
    job.mimeType = 'video/mp4';

    activeProcesses.delete(compressId);
    activeJobsByType.compress--;
    unlinkJobFromClient(compressId);
    trackCompress();

  } catch (err) {
    console.error(`[${compressId}] Error:`, err.message);
    activeProcesses.delete(compressId);
    activeJobsByType.compress--;
    unlinkJobFromClient(compressId);

    try { fs.unlinkSync(inputPath); } catch { }
    try { fs.unlinkSync(outputPath); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log`); } catch { }
    try { fs.unlinkSync(`${passLogFile}-0.log.mbtree`); } catch { }

    job.status = 'error';
    job.error = err.message || 'Compression failed';
  }
}

async function probeVideo(inputPath) {
  return new Promise((resolve) => {
    const ffprobe = spawn('ffprobe', [
      '-v', 'error',
      '-select_streams', 'v:0',
      '-show_entries', 'stream=width,height,bit_rate,codec_name,r_frame_rate:format=duration,bit_rate',
      '-of', 'json',
      inputPath
    ]);

    let output = '';
    ffprobe.stdout.on('data', (data) => { output += data.toString(); });
    ffprobe.on('close', () => {
      try {
        const parsed = JSON.parse(output);
        const stream = parsed.streams?.[0] || {};
        const format = parsed.format || {};
        let fps = 30;
        if (stream.r_frame_rate) {
          const [num, den] = stream.r_frame_rate.split('/').map(Number);
          if (isFinite(num) && isFinite(den) && den !== 0) {
            fps = num / den || 30;
          }
        }
        resolve({
          duration: parseFloat(format.duration) || 60,
          width: parseInt(stream.width) || 1920,
          height: parseInt(stream.height) || 1080,
          videoBitrate: parseInt(stream.bit_rate) || parseInt(format.bit_rate) || 0,
          codec: stream.codec_name || 'unknown',
          fps
        });
      } catch {
        resolve({ duration: 60, width: 1920, height: 1080, videoBitrate: 0, codec: 'unknown', fps: 30 });
      }
    });
    ffprobe.on('error', () => resolve({ duration: 60, width: 1920, height: 1080, videoBitrate: 0, codec: 'unknown', fps: 30 }));
  });
}

async function runCrfEncode({ inputPath, outputPath, crf, ffmpegPreset, vfArg, x264Params, processInfo, compressId, actualDuration }) {
  const args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) args.push('-vf', vfArg);
  args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-crf', String(crf),
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-c:a', 'aac', '-b:a', '128k',
    '-movflags', '+faststart',
    outputPath
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', args);
    processInfo.process = ffmpeg;

    let lastProgress = 0;
    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = Math.min(95, (currentTime / actualDuration) * 95);

        const speedMatch = msg.match(/speed=\s*([\d.]+)x/);
        const encSpeed = speedMatch ? parseFloat(speedMatch[1]) : null;
        const eta = encSpeed ? formatETA((actualDuration - currentTime) / encSpeed) : null;

        if (progress > lastProgress + 2) {
          lastProgress = progress;
          const statusMsg = eta ? `Encoding... ${Math.round(progress)}% (ETA: ${eta})` : `Encoding... ${Math.round(progress)}%`;
          sendProgress(compressId, 'compressing', statusMsg, progress);
        }
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Encoding failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });
}

async function runCrfEncodeAsync({ inputPath, outputPath, crf, ffmpegPreset, vfArg, x264Params, processInfo, actualDuration, job }) {
  const args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) args.push('-vf', vfArg);
  args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-crf', String(crf),
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-c:a', 'aac', '-b:a', '128k',
    '-movflags', '+faststart',
    outputPath
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', args);
    processInfo.process = ffmpeg;

    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = Math.min(95, (currentTime / actualDuration) * 95);
        job.progress = Math.round(progress);
        job.message = `Encoding... ${Math.round(progress)}%`;
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Encoding failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });
}

async function runTwoPassEncode({ inputPath, outputPath, passLogFile, videoBitrateK, ffmpegPreset, vfArg, x264Params, processInfo, compressId, actualDuration }) {
  const maxrateK = Math.floor(videoBitrateK * 1.5);
  const bufsizeK = Math.floor(videoBitrateK * 2);

  sendProgress(compressId, 'compressing', 'Pass 1/2 - Analyzing...', 5);

  const pass1Args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) pass1Args.push('-vf', vfArg);
  pass1Args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-b:v', `${videoBitrateK}k`,
    '-maxrate', `${maxrateK}k`,
    '-bufsize', `${bufsizeK}k`,
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-pass', '1',
    '-passlogfile', passLogFile,
    '-an',
    '-f', 'null',
    process.platform === 'win32' ? 'NUL' : '/dev/null'
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', pass1Args);
    processInfo.process = ffmpeg;

    let lastProgress = 0;
    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = Math.min(45, (currentTime / actualDuration) * 45);

        const speedMatch = msg.match(/speed=\s*([\d.]+)x/);
        const encSpeed = speedMatch ? parseFloat(speedMatch[1]) : null;
        const eta = encSpeed ? formatETA((actualDuration - currentTime) / encSpeed) : null;

        if (progress > lastProgress + 2) {
          lastProgress = progress;
          const statusMsg = eta ? `Pass 1/2 - ${Math.round(progress / 45 * 100)}% (ETA: ${eta})` : `Pass 1/2 - ${Math.round(progress / 45 * 100)}%`;
          sendProgress(compressId, 'compressing', statusMsg, progress);
        }
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Pass 1 failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });

  if (processInfo.cancelled) throw new Error('Cancelled');

  sendProgress(compressId, 'compressing', 'Pass 2/2 - Encoding...', 50);

  const pass2Args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) pass2Args.push('-vf', vfArg);
  pass2Args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-b:v', `${videoBitrateK}k`,
    '-maxrate', `${maxrateK}k`,
    '-bufsize', `${bufsizeK}k`,
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-pass', '2',
    '-passlogfile', passLogFile,
    '-c:a', 'aac', '-b:a', '128k',
    '-movflags', '+faststart',
    outputPath
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', pass2Args);
    processInfo.process = ffmpeg;

    let lastProgress = 50;
    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = 50 + Math.min(45, (currentTime / actualDuration) * 45);

        const speedMatch = msg.match(/speed=\s*([\d.]+)x/);
        const encSpeed = speedMatch ? parseFloat(speedMatch[1]) : null;
        const eta = encSpeed ? formatETA((actualDuration - currentTime) / encSpeed) : null;

        if (progress > lastProgress + 2) {
          lastProgress = progress;
          const statusMsg = eta ? `Pass 2/2 - ${Math.round((progress - 50) / 45 * 100)}% (ETA: ${eta})` : `Pass 2/2 - ${Math.round((progress - 50) / 45 * 100)}%`;
          sendProgress(compressId, 'compressing', statusMsg, progress);
        }
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Pass 2 failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });
}

async function runTwoPassEncodeAsync({ inputPath, outputPath, passLogFile, videoBitrateK, ffmpegPreset, vfArg, x264Params, processInfo, actualDuration, job }) {
  const maxrateK = Math.floor(videoBitrateK * 1.5);
  const bufsizeK = Math.floor(videoBitrateK * 2);

  job.message = 'Pass 1/2 - Analyzing...';
  job.progress = 5;

  const pass1Args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) pass1Args.push('-vf', vfArg);
  pass1Args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-b:v', `${videoBitrateK}k`,
    '-maxrate', `${maxrateK}k`,
    '-bufsize', `${bufsizeK}k`,
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-pass', '1',
    '-passlogfile', passLogFile,
    '-an',
    '-f', 'null',
    process.platform === 'win32' ? 'NUL' : '/dev/null'
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', pass1Args);
    processInfo.process = ffmpeg;

    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = Math.min(45, (currentTime / actualDuration) * 45);
        job.progress = Math.round(progress);
        job.message = `Pass 1/2 - ${Math.round(progress / 45 * 100)}%`;
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Pass 1 failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });

  if (processInfo.cancelled) throw new Error('Cancelled');

  job.message = 'Pass 2/2 - Encoding...';
  job.progress = 50;

  const pass2Args = ['-y', '-i', inputPath, '-threads', '0'];
  if (vfArg) pass2Args.push('-vf', vfArg);
  pass2Args.push(
    '-c:v', 'libx264',
    '-preset', ffmpegPreset,
    '-b:v', `${videoBitrateK}k`,
    '-maxrate', `${maxrateK}k`,
    '-bufsize', `${bufsizeK}k`,
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'high',
    '-level:v', '4.2',
    '-x264-params', x264Params,
    '-pass', '2',
    '-passlogfile', passLogFile,
    '-c:a', 'aac', '-b:a', '128k',
    '-movflags', '+faststart',
    outputPath
  );

  await new Promise((resolve, reject) => {
    const ffmpeg = spawn('ffmpeg', pass2Args);
    processInfo.process = ffmpeg;

    ffmpeg.stderr.on('data', (data) => {
      const msg = data.toString();
      const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
      if (timeMatch) {
        const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
        const progress = 50 + Math.min(45, (currentTime / actualDuration) * 45);
        job.progress = Math.round(progress);
        job.message = `Pass 2/2 - ${Math.round((progress - 50) / 45 * 100)}%`;
      }
    });

    ffmpeg.on('close', (code) => {
      if (processInfo.cancelled) reject(new Error('Cancelled'));
      else if (code === 0) resolve();
      else reject(new Error(`Pass 2 failed with code ${code}`));
    });
    ffmpeg.on('error', reject);
  });
}

module.exports = router;
