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
  ALLOWED_CROP_RATIOS,
  ALLOWED_AUDIO_BITRATES,
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

const { validateVideoFile, validateTimeParam, validateUrl } = require('../utils/validation');
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
const { downloadViaCobalt } = require('../services/cobalt');

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

  const response = {
    status: job.status,
    progress: job.progress || 0,
    message: job.message || '',
    error: job.error || null
  };
  if (job.textContent) response.textContent = job.textContent;
  res.json(response);
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

router.post('/api/fetch-url', express.json(), async (req, res) => {
  const { url } = req.body;

  if (!url || typeof url !== 'string' || !url.trim()) {
    return res.status(400).json({ error: 'Missing or invalid URL' });
  }

  const trimmedUrl = url.trim();
  const urlCheck = validateUrl(trimmedUrl);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const fetchCheck = canStartJob('fetchUrl');
  if (!fetchCheck.ok) {
    return res.status(503).json({ error: fetchCheck.reason });
  }

  const id = `fetch-${uuidv4()}`;
  const isYouTube = trimmedUrl.includes('youtube.com') || trimmedUrl.includes('youtu.be');
  console.log(`[${id}] Fetching URL (${isYouTube ? 'cobalt' : 'yt-dlp'})`);

  try {
    let filePath;

    const ytdlpFetch = () => new Promise((resolve, reject) => {
      const args = [
        '--no-playlist',
        '-f', 'bv*+ba/b',
        '-o', `${TEMP_DIRS.upload}/${id}-%(title)s.%(ext)s`,
        '--print', 'after_move:filepath',
        '--no-warnings',
        trimmedUrl
      ];

      const proc = spawn('yt-dlp', args);
      let stdout = '';
      let stderr = '';

      proc.stdout.on('data', (d) => { stdout += d.toString(); });
      proc.stderr.on('data', (d) => { stderr += d.toString(); });

      proc.on('close', (code) => {
        if (code !== 0) {
          reject(new Error(stderr.trim().split('\n').pop() || 'yt-dlp failed'));
          return;
        }
        const outputPath = stdout.trim().split('\n').pop();
        if (!outputPath || !fs.existsSync(outputPath)) {
          reject(new Error('yt-dlp did not produce a file'));
          return;
        }
        resolve(outputPath);
      });

      proc.on('error', (err) => reject(new Error(`Failed to run yt-dlp: ${err.message}`)));
    });

    if (isYouTube) {
      try {
        const cobaltResult = await downloadViaCobalt(trimmedUrl, id, false, null, null, {
          outputDir: TEMP_DIRS.upload
        });
        filePath = cobaltResult.filePath;
      } catch (cobaltErr) {
        console.log(`[${id}] Cobalt failed, falling back to yt-dlp: ${cobaltErr.message}`);
        filePath = await ytdlpFetch();
      }
    } else {
      filePath = await ytdlpFetch();
    }

    const stat = fs.statSync(filePath);
    if (stat.size > FILE_SIZE_LIMIT) {
      fs.unlink(filePath, () => {});
      activeJobsByType.fetchUrl--;
      return res.status(400).json({ error: `Downloaded file too large (${(stat.size / (1024 * 1024 * 1024)).toFixed(1)}GB). Maximum is ${FILE_SIZE_LIMIT / (1024 * 1024 * 1024)}GB.` });
    }

    let duration = 0, width = 0, height = 0;
    try {
      const probe = await new Promise((resolve) => {
        const ffprobe = spawn('ffprobe', [
          '-v', 'error',
          '-select_streams', 'v:0',
          '-show_entries', 'stream=width,height:format=duration',
          '-of', 'json',
          filePath
        ]);
        let out = '';
        ffprobe.stdout.on('data', (d) => { out += d.toString(); });
        ffprobe.on('close', () => {
          try {
            const parsed = JSON.parse(out);
            resolve({
              duration: parseFloat(parsed.format?.duration) || 0,
              width: parseInt(parsed.streams?.[0]?.width) || 0,
              height: parseInt(parsed.streams?.[0]?.height) || 0
            });
          } catch {
            resolve({ duration: 0, width: 0, height: 0 });
          }
        });
        ffprobe.on('error', () => resolve({ duration: 0, width: 0, height: 0 }));
      });
      duration = probe.duration;
      width = probe.width;
      height = probe.height;
    } catch {}

    const fileName = path.basename(filePath);
    console.log(`[${id}] Fetched: ${fileName} (${(stat.size / (1024 * 1024)).toFixed(1)}MB)`);

    activeJobsByType.fetchUrl--;
    res.json({ filePath, fileName, fileSize: stat.size, duration, width, height });
  } catch (err) {
    console.error(`[${id}] Fetch URL error:`, err.message);
    activeJobsByType.fetchUrl--;
    res.status(400).json({ error: err.message || 'Failed to download from URL' });
  }
});

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
    audioBitrate = '192',
    cropRatio,
    cropX,
    cropY,
    cropW,
    cropH,
    segments
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
  if (cropRatio && !ALLOWED_CROP_RATIOS.includes(cropRatio)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid crop ratio. Allowed: ${ALLOWED_CROP_RATIOS.join(', ')}` });
  }

  // validate raw crop params if provided
  const hasRawCrop = cropX !== undefined && cropY !== undefined && cropW !== undefined && cropH !== undefined;
  if (hasRawCrop) {
    const cx = parseInt(cropX), cy = parseInt(cropY), cw = parseInt(cropW), ch = parseInt(cropH);
    if (!Number.isFinite(cx) || !Number.isFinite(cy) || !Number.isFinite(cw) || !Number.isFinite(ch)) {
      fs.unlink(validPath, () => { });
      return res.status(400).json({ error: 'Invalid crop parameters: must be integers' });
    }
    if (cx < 0 || cy < 0 || cw <= 0 || ch <= 0) {
      fs.unlink(validPath, () => { });
      return res.status(400).json({ error: 'Invalid crop parameters: values must be positive' });
    }
    if (cw % 2 !== 0 || ch % 2 !== 0) {
      fs.unlink(validPath, () => { });
      return res.status(400).json({ error: 'Invalid crop parameters: width and height must be even' });
    }
  }

  // validate segments if provided
  if (segments) {
    if (!Array.isArray(segments) || segments.length === 0) {
      fs.unlink(validPath, () => { });
      return res.status(400).json({ error: 'Invalid segments: must be a non-empty array' });
    }
    if (segments.length > 20) {
      fs.unlink(validPath, () => { });
      return res.status(400).json({ error: 'Too many segments (max 20)' });
    }
    for (const seg of segments) {
      if (typeof seg.start !== 'number' || typeof seg.end !== 'number' || seg.end <= seg.start) {
        fs.unlink(validPath, () => { });
        return res.status(400).json({ error: 'Invalid segment: each must have numeric start < end' });
      }
    }
  }

  const safeBitrate = ALLOWED_AUDIO_BITRATES.includes(audioBitrate) ? audioBitrate : '192';

  req.file = { path: validPath, originalname: fileName || 'video.mp4' };
  req.body.format = format;
  req.body.clientId = clientId;
  req.body.quality = quality;
  req.body.reencode = reencode;
  req.body.startTime = startTime;
  req.body.endTime = endTime;
  req.body.audioBitrate = safeBitrate;
  req.body.cropRatio = cropRatio;
  req.body.cropX = cropX;
  req.body.cropY = cropY;
  req.body.cropW = cropW;
  req.body.cropH = cropH;
  req.body.segments = segments;

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

async function buildCropFilter(inputPath, cropOpts, logId) {
  // cropOpts can be a string (ratio like "16:9") or an object { cropX, cropY, cropW, cropH }
  if (typeof cropOpts === 'object' && cropOpts.cropW && cropOpts.cropH) {
    const { cropX: cx, cropY: cy, cropW: cw, cropH: ch } = cropOpts;
    const x = parseInt(cx), y = parseInt(cy), w = parseInt(cw), h = parseInt(ch);

    // validate against actual video dimensions
    const probeDims = await probeVideo(inputPath);
    if (x + w > probeDims.width || y + h > probeDims.height) {
      console.log(`[${logId}] Crop skipped: crop rect exceeds video bounds (${x}+${w} x ${y}+${h} vs ${probeDims.width}x${probeDims.height})`);
      return null;
    }

    const filter = `crop=${w}:${h}:${x}:${y}`;
    console.log(`[${logId}] Crop (raw): ${filter}`);
    return filter;
  }

  // string ratio path
  const cropRatio = typeof cropOpts === 'string' ? cropOpts : null;
  if (!cropRatio) return null;

  const probeDims = await probeVideo(inputPath);
  const videoW = probeDims.width;
  const videoH = probeDims.height;
  const [ratioW, ratioH] = cropRatio.split(':').map(Number);
  if (!videoW || !videoH || !ratioW || !ratioH) {
    console.log(`[${logId}] Crop skipped: invalid dimensions (${videoW}x${videoH}) or ratio (${ratioW}:${ratioH})`);
    return null;
  }
  let cropW, cropH, cropX, cropY;
  if (videoW / videoH > ratioW / ratioH) {
    cropH = videoH - (videoH % 2);
    cropW = Math.floor(cropH * ratioW / ratioH);
    cropW = cropW - (cropW % 2);
    cropX = Math.floor((videoW - cropW) / 2);
    cropY = Math.floor((videoH - cropH) / 2);
  } else {
    cropW = videoW - (videoW % 2);
    cropH = Math.floor(cropW * ratioH / ratioW);
    cropH = cropH - (cropH % 2);
    cropX = Math.floor((videoW - cropW) / 2);
    cropY = Math.floor((videoH - cropH) / 2);
  }
  const filter = `crop=${cropW}:${cropH}:${cropX}:${cropY}`;
  console.log(`[${logId}] Crop: ${cropRatio} -> ${filter}`);
  return filter;
}

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
    audioBitrate: rawBitrate = '192',
    cropRatio
  } = req.body;

  const audioBitrate = ALLOWED_AUDIO_BITRATES.includes(rawBitrate) ? rawBitrate : '192';

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => { });
      return res.status(429).json({
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.`
      });
    }
  }

  if (cropRatio && !ALLOWED_CROP_RATIOS.includes(cropRatio)) {
    fs.unlink(req.file.path, () => { });
    return res.status(400).json({ error: `Invalid crop ratio. Allowed: ${ALLOWED_CROP_RATIOS.join(', ')}` });
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
    if (validStartTime && validEndTime && parseFloat(validEndTime) <= parseFloat(validStartTime)) {
      fs.unlink(inputPath, () => { });
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      return res.status(400).json({ error: 'endTime must be greater than startTime' });
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
      const needsReencode = reencode === 'always' || (reencode === 'auto' && !isCompatible) || !!cropRatio;

      const cropFilter = cropRatio ? await buildCropFilter(inputPath, cropRatio, convertId) : null;

      if (needsReencode) {
        const crfValues = { high: 18, medium: 23, low: 28 };
        const crf = crfValues[quality] || 23;
        console.log(`[${convertId}] Re-encoding video (${probeCodec} → h264, CRF ${crf})`);
        if (cropFilter) {
          ffmpegArgs.push('-vf', cropFilter);
        }
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
    audioBitrate = '192',
    cropRatio,
    cropX,
    cropY,
    cropW,
    cropH,
    segments
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

  console.log(`[Queue] Async convert started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${convertId}] Converting to ${format} (async)`);

  if (cropRatio && !ALLOWED_CROP_RATIOS.includes(cropRatio)) {
    try { fs.unlinkSync(inputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    job.status = 'error';
    job.error = 'Invalid crop ratio';
    job.progress = 0;
    return;
  }
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

  // determine crop filter source: raw coords take priority over ratio
  const hasRawCrop = cropX !== undefined && cropY !== undefined && cropW !== undefined && cropH !== undefined;
  const cropOpts = hasRawCrop ? { cropX, cropY, cropW, cropH } : (cropRatio || null);
  const hasCrop = !!cropOpts;

  // determine if we have multiple segments
  const hasSegments = Array.isArray(segments) && segments.length > 1;

  const tempClips = []; // track temp files for cleanup

  try {
    const isAudioFormat = ['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format);

    job.message = 'Analyzing file...';
    job.progress = 5;

    const duration = await new Promise((resolve) => {
      const ffprobe = spawn('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', inputPath]);
      let out = '';
      ffprobe.stdout.on('data', (d) => { out += d.toString(); });
      ffprobe.on('close', () => resolve(parseFloat(out) || 60));
      ffprobe.on('error', () => resolve(60));
    });

    // build crop filter if needed
    const cropFilter = hasCrop ? await buildCropFilter(inputPath, cropOpts, convertId) : null;

    // probe codec for reencode decision
    // force reencode for multi-segment to avoid keyframe artifacts at concat boundaries
    let needsReencode = reencode === 'always' || hasCrop || hasSegments;
    if (!isAudioFormat && !needsReencode) {
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
      if (reencode === 'auto' && !isCompatible) needsReencode = true;
    }

    const crfValues = { high: 18, medium: 23, low: 28 };
    const crf = crfValues[quality] || 23;

    // helper to build ffmpeg args for a single segment
    function buildSegmentArgs(segStart, segEnd, outFile) {
      const args = ['-y'];
      if (segStart > 0) args.push('-ss', String(segStart));
      if (segEnd < duration) args.push('-to', String(segEnd));
      args.push('-i', inputPath, '-threads', '0');

      if (isAudioFormat) {
        if (format === 'mp3') args.push('-codec:a', 'libmp3lame', '-b:a', `${audioBitrate}k`);
        else if (format === 'm4a') args.push('-codec:a', 'aac', '-b:a', `${audioBitrate}k`);
        else if (format === 'opus') args.push('-codec:a', 'libopus', '-b:a', '128k');
        else if (format === 'wav') args.push('-codec:a', 'pcm_s16le');
        else if (format === 'flac') args.push('-codec:a', 'flac');
        args.push('-vn');
      } else if (needsReencode) {
        if (cropFilter) args.push('-vf', cropFilter);
        args.push('-c:v', 'libx264', '-preset', 'medium', '-crf', String(crf), '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-b:a', '128k');
      } else {
        args.push('-codec', 'copy');
      }

      if (!isAudioFormat && (format === 'mp4' || format === 'mov')) {
        args.push('-movflags', '+faststart');
      }
      args.push(outFile);
      return args;
    }

    if (hasSegments) {
      // multi-segment: encode each segment, then concat
      console.log(`[${convertId}] Processing ${segments.length} segments`);
      job.message = `Processing segment 1/${segments.length}...`;
      job.progress = 10;

      const clipPaths = [];
      const totalSegDuration = segments.reduce((sum, s) => sum + (s.end - s.start), 0);
      let processedDuration = 0;

      for (let i = 0; i < segments.length; i++) {
        const seg = segments[i];
        const clipPath = path.join(TEMP_DIRS.convert, `${convertId}-clip${i}.${format}`);
        tempClips.push(clipPath);
        clipPaths.push(clipPath);

        const segDuration = seg.end - seg.start;
        const segArgs = buildSegmentArgs(seg.start, seg.end, clipPath);

        job.message = `Processing segment ${i + 1}/${segments.length}...`;

        await new Promise((resolve, reject) => {
          const ffmpeg = spawn('ffmpeg', segArgs);
          ffmpeg.stderr.on('data', (data) => {
            const msg = data.toString();
            const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
            if (timeMatch) {
              const ct = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
              const segProgress = ct / segDuration;
              const overallProgress = 10 + ((processedDuration + segDuration * segProgress) / totalSegDuration) * 75;
              job.progress = Math.round(Math.min(85, overallProgress));
              job.message = `Segment ${i + 1}/${segments.length}... ${Math.round(segProgress * 100)}%`;
            }
          });
          ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error(`Segment ${i + 1} failed with code ${code}`)));
          ffmpeg.on('error', reject);
        });

        processedDuration += segDuration;
      }

      // write concat list (all temp files on encrypted volume via TEMP_DIRS.convert)
      const concatListPath = path.join(TEMP_DIRS.convert, `${convertId}-concat.txt`);
      tempClips.push(concatListPath);
      const concatContent = clipPaths.map(p => `file '${p.replace(/'/g, "'\\''")}'`).join('\n');
      fs.writeFileSync(concatListPath, concatContent);

      job.message = 'Joining segments...';
      job.progress = 90;

      // concat with stream copy
      await new Promise((resolve, reject) => {
        const concatArgs = ['-y', '-f', 'concat', '-safe', '0', '-i', concatListPath, '-c', 'copy'];
        if (format === 'mp4' || format === 'mov') concatArgs.push('-movflags', '+faststart');
        concatArgs.push(outputPath);

        const ffmpeg = spawn('ffmpeg', concatArgs);
        ffmpeg.stderr.on('data', () => {});
        ffmpeg.on('close', (code) => code === 0 ? resolve() : reject(new Error(`Concat failed with code ${code}`)));
        ffmpeg.on('error', reject);
      });

      // clean up temp clips
      for (const clip of tempClips) {
        try { fs.unlinkSync(clip); } catch { }
      }

    } else {
      // single segment / simple trim
      const validStartTime = validateTimeParam(startTime);
      const validEndTime = validateTimeParam(endTime);

      const finalArgs = ['-y'];
      if (validStartTime) finalArgs.push('-ss', validStartTime);
      if (validEndTime) finalArgs.push('-to', validEndTime);
      finalArgs.push('-i', inputPath, '-threads', '0');

      if (isAudioFormat) {
        if (format === 'mp3') finalArgs.push('-codec:a', 'libmp3lame', '-b:a', `${audioBitrate}k`);
        else if (format === 'm4a') finalArgs.push('-codec:a', 'aac', '-b:a', `${audioBitrate}k`);
        else if (format === 'opus') finalArgs.push('-codec:a', 'libopus', '-b:a', '128k');
        else if (format === 'wav') finalArgs.push('-codec:a', 'pcm_s16le');
        else if (format === 'flac') finalArgs.push('-codec:a', 'flac');
        finalArgs.push('-vn');
      } else if (needsReencode) {
        if (cropFilter) finalArgs.push('-vf', cropFilter);
        finalArgs.push('-c:v', 'libx264', '-preset', 'medium', '-crf', String(crf), '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-b:a', '128k');
      } else {
        finalArgs.push('-codec', 'copy');
      }

      if (!isAudioFormat && (format === 'mp4' || format === 'mov')) {
        finalArgs.push('-movflags', '+faststart');
      }
      finalArgs.push(outputPath);

      job.message = 'Converting...';
      job.progress = 10;

      await new Promise((resolve, reject) => {
        const ffmpeg = spawn('ffmpeg', finalArgs);

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
    }

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
    // clean up any temp clips
    for (const clip of tempClips) {
      try { fs.unlinkSync(clip); } catch { }
    }
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
