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
  FILE_SIZE_LIMIT
} = require('../config/constants');

const {
  activeProcesses,
  activeJobsByType,
  asyncJobs,
  canStartJob,
  registerClient,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount
} = require('../services/state');

const { cleanupJobFiles, sanitizeFilename } = require('../utils/files');

const upload = multer({
  dest: TEMP_DIRS.upload,
  limits: { fileSize: FILE_SIZE_LIMIT }
});

const ALLOWED_OUTPUT_MODES = ['subtitles', 'captions', 'text'];
const ALLOWED_MODELS = ['tiny', 'base', 'small', 'medium'];
const ALLOWED_SUB_FORMATS = ['srt', 'ass'];

const WHISPER_SCRIPT = path.join(__dirname, '..', 'utils', 'whisper.py');

function validateChunkedFilePath(filePath) {
  if (!filePath || typeof filePath !== 'string') return null;
  const resolved = path.resolve(filePath);
  const uploadDir = TEMP_DIRS.upload + path.sep;
  if (!resolved.startsWith(uploadDir)) return null;
  try {
    if (fs.lstatSync(resolved).isSymbolicLink()) return null;
  } catch {
    return null;
  }
  return resolved;
}

// Direct upload
router.post('/api/transcribe', upload.single('file'), (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const { clientId } = req.body;

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => {});
      return res.status(429).json({
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.`
      });
    }
  }

  const jobId = uuidv4();

  asyncJobs.set(jobId, {
    status: 'processing',
    progress: 0,
    message: 'Starting transcription...',
    createdAt: Date.now(),
    outputPath: null,
    outputFilename: null,
    mimeType: null,
    textContent: null
  });

  res.json({ jobId });

  handleTranscribeAsync(req, jobId).catch(err => {
    console.error(`[AsyncJob] Transcribe job ${jobId} failed:`, err);
    const job = asyncJobs.get(jobId);
    if (job) {
      job.status = 'error';
      job.error = err.message || 'Transcription failed';
    }
  });
});

// Chunked upload
router.post('/api/transcribe-chunked', express.json(), async (req, res) => {
  const { filePath, fileName, clientId, ...options } = req.body;

  const validPath = validateChunkedFilePath(filePath);
  if (!validPath) {
    return res.status(400).json({ error: 'Invalid file path' });
  }

  if (!fs.existsSync(validPath)) {
    return res.status(400).json({ error: 'File not found. Complete chunked upload first.' });
  }

  req.file = { path: validPath, originalname: fileName || 'media' };
  req.body = { ...options, clientId };

  const jobId = uuidv4();

  asyncJobs.set(jobId, {
    status: 'processing',
    progress: 0,
    message: 'Starting transcription...',
    createdAt: Date.now(),
    outputPath: null,
    outputFilename: null,
    mimeType: null,
    textContent: null
  });

  res.json({ jobId });

  handleTranscribeAsync(req, jobId).catch(err => {
    console.error(`[AsyncJob] Transcribe job ${jobId} failed:`, err);
    const job = asyncJobs.get(jobId);
    if (job) {
      job.status = 'error';
      job.error = err.message || 'Transcription failed';
    }
  });
});

async function handleTranscribeAsync(req, jobId) {
  const job = asyncJobs.get(jobId);
  if (!job) return;

  const {
    outputMode = 'text',
    model = 'base',
    subtitleFormat = 'srt',
    language = '',
    clientId,
    captionSize = 72,
    maxWordsPerCaption = 0,
    maxCharsPerLine = 0,
    minDuration = 0,
    captionGap = 0
  } = req.body;

  if (!ALLOWED_OUTPUT_MODES.includes(outputMode)) {
    job.status = 'error';
    job.error = `Invalid output mode. Allowed: ${ALLOWED_OUTPUT_MODES.join(', ')}`;
    return;
  }
  if (!ALLOWED_MODELS.includes(model)) {
    job.status = 'error';
    job.error = `Invalid model. Allowed: ${ALLOWED_MODELS.join(', ')}`;
    return;
  }
  if (outputMode === 'subtitles' && !ALLOWED_SUB_FORMATS.includes(subtitleFormat)) {
    job.status = 'error';
    job.error = `Invalid subtitle format. Allowed: ${ALLOWED_SUB_FORMATS.join(', ')}`;
    return;
  }
  if (language && !/^[a-z]{2,5}$/i.test(language)) {
    job.status = 'error';
    job.error = 'Invalid language code. Use 2-5 letter code (e.g. en, es, ja).';
    return;
  }

  // Parse caption formatting params once (FormData sends strings, JSON sends numbers)
  const cs = Number(captionSize) || 72;
  const mwpc = Number(maxWordsPerCaption) || 0;
  const mcpl = Number(maxCharsPerLine) || 0;
  const md = Number(minDuration) || 0;
  const cg = Number(captionGap) || 0;

  // Validate caption formatting params (only relevant for subtitles/captions)
  if (outputMode !== 'text') {
    if (cs !== 72 && (cs < 40 || cs > 120 || !Number.isInteger(cs))) {
      job.status = 'error';
      job.error = 'captionSize must be an integer between 40 and 120.';
      return;
    }
    if (mwpc && (mwpc < 1 || mwpc > 20 || !Number.isInteger(mwpc))) {
      job.status = 'error';
      job.error = 'maxWordsPerCaption must be an integer between 1 and 20.';
      return;
    }
    if (mcpl && (mcpl < 10 || mcpl > 80 || !Number.isInteger(mcpl))) {
      job.status = 'error';
      job.error = 'maxCharsPerLine must be an integer between 10 and 80.';
      return;
    }
    if (md && (md < 0.1 || md > 5)) {
      job.status = 'error';
      job.error = 'minDuration must be between 0.1 and 5 seconds.';
      return;
    }
    if (cg && (cg < 0 || cg > 1)) {
      job.status = 'error';
      job.error = 'captionGap must be between 0 and 1 seconds.';
      return;
    }
  }

  const transcribeCheck = canStartJob('transcribe');
  if (!transcribeCheck.ok) {
    try { fs.unlinkSync(req.file.path); } catch {}
    job.status = 'error';
    job.error = transcribeCheck.reason;
    return;
  }

  const transcribeId = jobId;
  const inputPath = req.file.path;

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(transcribeId, clientId);
  }

  console.log(`[Queue] Transcribe started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${transcribeId}] Transcribing | Mode: ${outputMode} | Model: ${model}`);

  const processInfo = { cancelled: false, process: null };
  activeProcesses.set(transcribeId, processInfo);

  const wavPath = path.join(TEMP_DIRS.transcribe, `${transcribeId}.wav`);
  let whisperOutputFormat;
  if (outputMode === 'text') {
    whisperOutputFormat = 'txt';
  } else if (outputMode === 'subtitles') {
    whisperOutputFormat = subtitleFormat;
  } else {
    // captions mode: need ASS for burn-in
    whisperOutputFormat = 'ass';
  }
  const whisperOutputPath = path.join(TEMP_DIRS.transcribe, `${transcribeId}.${whisperOutputFormat}`);
  const captionedPath = path.join(TEMP_DIRS.transcribe, `${transcribeId}-captioned.mp4`);

  try {
    // Stage 1: Check if file has video (for captions mode) and extract audio
    job.message = 'Analyzing file...';
    job.progress = 1;

    const hasVideo = await probeHasVideo(inputPath);
    const hasAudio = await probeHasAudio(inputPath);

    if (!hasAudio) {
      throw new Error('No audio found in file');
    }

    if (outputMode === 'captions' && !hasVideo) {
      throw new Error('Captions mode requires a video file (no video stream found)');
    }

    // Extract audio as 16kHz mono WAV
    job.message = 'Extracting audio...';
    job.progress = 2;

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', [
        '-y', '-i', inputPath,
        '-vn', '-acodec', 'pcm_s16le', '-ar', '16000', '-ac', '1',
        wavPath
      ]);
      processInfo.process = ffmpeg;

      ffmpeg.on('close', (code) => {
        if (processInfo.cancelled) reject(new Error('Cancelled'));
        else if (code === 0) resolve();
        else reject(new Error(`Audio extraction failed with code ${code}`));
      });
      ffmpeg.on('error', reject);
    });

    // Check WAV file exists and has content
    const wavStat = fs.statSync(wavPath);
    if (wavStat.size < 1000) {
      throw new Error('No audio found in file');
    }

    job.progress = 5;

    // Stage 2: Run whisper transcription
    job.message = 'Starting transcription...';

    const whisperArgs = [
      WHISPER_SCRIPT,
      '--input', wavPath,
      '--model', model,
      '--output-format', whisperOutputFormat,
      '--output', whisperOutputPath
    ];

    if (language) {
      whisperArgs.push('--language', language);
    }

    if (outputMode !== 'text') {
      if (cs !== 72) whisperArgs.push('--font-size', String(cs));
      if (mwpc > 0) whisperArgs.push('--max-words-per-caption', String(mwpc));
      if (mcpl > 0) whisperArgs.push('--max-chars-per-line', String(mcpl));
      if (md > 0) whisperArgs.push('--min-duration', String(md));
      if (cg > 0) whisperArgs.push('--gap', String(cg));
    }

    const whisperResult = await new Promise((resolve, reject) => {
      const python = spawn('python3', whisperArgs);
      processInfo.process = python;

      let stdout = '';
      let stderrBuffer = '';

      python.stdout.on('data', (data) => {
        stdout += data.toString();
      });

      python.stderr.on('data', (data) => {
        stderrBuffer += data.toString();

        // Parse progress JSON lines from stderr
        const lines = stderrBuffer.split('\n');
        stderrBuffer = lines.pop(); // keep incomplete line in buffer

        for (const line of lines) {
          if (!line.trim()) continue;
          try {
            const progress = JSON.parse(line);
            if (progress.progress !== undefined) {
              // Map whisper progress (0-95) to our range (5-85)
              const mapped = 5 + (progress.progress / 95) * 80;
              job.progress = Math.round(Math.min(85, mapped));
              job.message = progress.message || 'Transcribing...';
            }
          } catch {
            // not JSON, ignore
          }
        }
      });

      python.on('close', (code) => {
        if (processInfo.cancelled) {
          reject(new Error('Cancelled'));
          return;
        }

        // Parse final result from stdout
        try {
          const result = JSON.parse(stdout.trim());
          if (result.success) {
            resolve(result);
          } else {
            reject(new Error(result.error || 'Transcription failed'));
          }
        } catch {
          if (code !== 0) {
            reject(new Error(`Whisper process exited with code ${code}`));
          } else {
            reject(new Error('Failed to parse whisper output'));
          }
        }
      });

      python.on('error', (err) => {
        reject(new Error(`Failed to start whisper: ${err.message}`));
      });
    });

    console.log(`[${transcribeId}] Whisper done: ${whisperResult.segmentCount} segments, language: ${whisperResult.language}`);

    // Stage 3: Handle output based on mode
    if (outputMode === 'text') {
      // Read text content and store inline
      job.message = 'Preparing transcript...';
      job.progress = 90;

      const textContent = fs.readFileSync(whisperOutputPath, 'utf-8');

      job.status = 'complete';
      job.progress = 100;
      job.message = 'Transcription complete!';
      job.textContent = textContent;
      job.outputPath = whisperOutputPath;
      job.outputFilename = `${sanitizeFilename(path.parse(req.file.originalname).name)}_transcript.txt`;
      job.mimeType = 'text/plain';

    } else if (outputMode === 'subtitles') {
      // Return the subtitle file
      job.message = 'Preparing subtitles...';
      job.progress = 90;

      const ext = subtitleFormat;
      const mimeType = ext === 'srt' ? 'application/x-subrip' : 'text/x-ssa';

      job.status = 'complete';
      job.progress = 100;
      job.message = 'Transcription complete!';
      job.outputPath = whisperOutputPath;
      job.outputFilename = `${sanitizeFilename(path.parse(req.file.originalname).name)}.${ext}`;
      job.mimeType = mimeType;

    } else if (outputMode === 'captions') {
      // Burn ASS subtitles into video
      job.message = 'Burning captions into video...';
      job.progress = 86;

      const duration = await probeDuration(inputPath);

      await new Promise((resolve, reject) => {
        const ffmpeg = spawn('ffmpeg', [
          '-y', '-i', inputPath,
          '-vf', `ass=${whisperOutputPath.replace(/([\\:'])/g, '\\$1')}`,
          '-c:v', 'libx264', '-preset', 'medium', '-crf', '23',
          '-pix_fmt', 'yuv420p',
          '-c:a', 'aac', '-b:a', '128k',
          '-movflags', '+faststart',
          captionedPath
        ]);
        processInfo.process = ffmpeg;

        ffmpeg.stderr.on('data', (data) => {
          const msg = data.toString();
          const timeMatch = msg.match(/time=(\d+):(\d+):(\d+\.?\d*)/);
          if (timeMatch && duration > 0) {
            const currentTime = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseFloat(timeMatch[3]);
            const progress = 86 + Math.min(13, (currentTime / duration) * 13);
            job.progress = Math.round(progress);
            job.message = `Burning captions... ${Math.round((currentTime / duration) * 100)}%`;
          }
        });

        ffmpeg.on('close', (code) => {
          if (processInfo.cancelled) reject(new Error('Cancelled'));
          else if (code === 0) resolve();
          else reject(new Error(`Caption burn-in failed with code ${code}`));
        });
        ffmpeg.on('error', reject);
      });

      job.status = 'complete';
      job.progress = 100;
      job.message = 'Captions burned in!';
      job.outputPath = captionedPath;
      job.outputFilename = `${sanitizeFilename(path.parse(req.file.originalname).name)}_captioned.mp4`;
      job.mimeType = 'video/mp4';
    }

    // Cleanup intermediate files
    try { fs.unlinkSync(wavPath); } catch {}
    try { fs.unlinkSync(inputPath); } catch {}
    if (outputMode === 'captions') {
      try { fs.unlinkSync(whisperOutputPath); } catch {}
    }

    activeProcesses.delete(transcribeId);
    activeJobsByType.transcribe--;
    unlinkJobFromClient(transcribeId);

    console.log(`[Queue] Transcribe finished. Active: ${JSON.stringify(activeJobsByType)}`);

  } catch (err) {
    console.error(`[${transcribeId}] Transcribe error:`, err.message);
    activeProcesses.delete(transcribeId);
    activeJobsByType.transcribe--;
    unlinkJobFromClient(transcribeId);

    try { fs.unlinkSync(inputPath); } catch {}
    try { fs.unlinkSync(wavPath); } catch {}
    try { fs.unlinkSync(whisperOutputPath); } catch {}
    try { fs.unlinkSync(captionedPath); } catch {}

    job.status = 'error';
    job.error = err.message || 'Transcription failed';
  }
}

function probeHasVideo(inputPath) {
  return new Promise((resolve) => {
    const ffprobe = spawn('ffprobe', [
      '-v', 'error', '-select_streams', 'v:0',
      '-show_entries', 'stream=codec_type',
      '-of', 'csv=p=0', inputPath
    ]);
    let out = '';
    ffprobe.stdout.on('data', (d) => { out += d.toString(); });
    ffprobe.on('close', () => resolve(out.trim().includes('video')));
    ffprobe.on('error', () => resolve(false));
  });
}

function probeHasAudio(inputPath) {
  return new Promise((resolve) => {
    const ffprobe = spawn('ffprobe', [
      '-v', 'error', '-select_streams', 'a:0',
      '-show_entries', 'stream=codec_type',
      '-of', 'csv=p=0', inputPath
    ]);
    let out = '';
    ffprobe.stdout.on('data', (d) => { out += d.toString(); });
    ffprobe.on('close', () => resolve(out.trim().includes('audio')));
    ffprobe.on('error', () => resolve(false));
  });
}

function probeDuration(inputPath) {
  return new Promise((resolve) => {
    const ffprobe = spawn('ffprobe', [
      '-v', 'error', '-show_entries', 'format=duration',
      '-of', 'csv=p=0', inputPath
    ]);
    let out = '';
    ffprobe.stdout.on('data', (d) => { out += d.toString(); });
    ffprobe.on('close', () => resolve(parseFloat(out) || 0));
    ffprobe.on('error', () => resolve(0));
  });
}

module.exports = router;
