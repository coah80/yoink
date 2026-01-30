const express = require('express');
const cors = require('cors');
const { spawn, execSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');
const { v4: uuidv4 } = require('uuid');
const multer = require('multer');
const archiver = require('archiver');
const crypto = require('crypto');

let adminConfig = { ADMIN_PASSWORD: null, ADMIN_TOKEN_SECRET: 'default-secret' };
try {
  adminConfig = require('./admin-config.js');
} catch (e) {
  console.log('[Admin] No admin-config.js found, admin features disabled');
}

const ANALYTICS_FILE = path.join(__dirname, 'analytics.json');
const adminTokens = new Map();

function loadAnalytics() {
  try {
    if (fs.existsSync(ANALYTICS_FILE)) {
      return JSON.parse(fs.readFileSync(ANALYTICS_FILE, 'utf8'));
    }
  } catch (e) {
    console.error('[Analytics] Failed to load:', e.message);
  }
  return {
    totalDownloads: 0,
    totalConverts: 0,
    totalCompresses: 0,
    formats: {},
    sites: {},
    countries: {},
    dailyUsers: {},
    pageViews: {},
    popularUrls: [],
    peakUsers: { count: 0, timestamp: Date.now() },
    lastUpdated: Date.now()
  };
}

function saveAnalytics(data) {
  try {
    data.lastUpdated = Date.now();
    fs.writeFileSync(ANALYTICS_FILE, JSON.stringify(data, null, 2));
  } catch (e) {
    console.error('[Analytics] Failed to save:', e.message);
  }
}

let analytics = loadAnalytics();

function trackDownload(format, site, country) {
  analytics.totalDownloads++;
  analytics.formats[format] = (analytics.formats[format] || 0) + 1;
  if (site) {
    analytics.sites[site] = (analytics.sites[site] || 0) + 1;
  }
  if (country) {
    analytics.countries[country] = (analytics.countries[country] || 0) + 1;
  }
  saveAnalytics(analytics);
}

function trackConvert(fromFormat, toFormat) {
  analytics.totalConverts++;
  const key = `${fromFormat}->${toFormat}`;
  analytics.formats[key] = (analytics.formats[key] || 0) + 1;
  saveAnalytics(analytics);
}

function trackCompress() {
  analytics.totalCompresses++;
  saveAnalytics(analytics);
}

function trackPageView(page, country) {
  const today = new Date().toISOString().split('T')[0];
  if (!analytics.pageViews[today]) {
    analytics.pageViews[today] = {};
  }
  analytics.pageViews[today][page] = (analytics.pageViews[today][page] || 0) + 1;
  if (country) {
    analytics.countries[country] = (analytics.countries[country] || 0) + 1;
  }
  saveAnalytics(analytics);
}

function trackDailyUser(clientId, country) {
  const today = new Date().toISOString().split('T')[0];
  if (!analytics.dailyUsers[today]) {
    analytics.dailyUsers[today] = new Set();
  }
  if (typeof analytics.dailyUsers[today] === 'object' && !(analytics.dailyUsers[today] instanceof Set)) {
    analytics.dailyUsers[today] = new Set(analytics.dailyUsers[today]);
  }
  analytics.dailyUsers[today].add(clientId);
  if (country) {
    analytics.countries[country] = (analytics.countries[country] || 0) + 1;
  }
}

function getCountryFromIP(ip) {
  return null;
}

function generateAdminToken() {
  const token = crypto.randomBytes(32).toString('hex');
  adminTokens.set(token, { createdAt: Date.now() });
  return token;
}

function validateAdminToken(token) {
  return adminTokens.has(token);
}

const app = express();
const PORT = process.env.PORT || 3001;

const JOB_LIMITS = {
  download: 6,
  playlist: 2,
  convert: 2,
  compress: 1
};
const MAX_QUEUE_SIZE = 50;
const FILE_SIZE_LIMIT = 15 * 1024 * 1024 * 1024;
const FILE_RETENTION_MS = 20 * 60 * 1000;
const HEARTBEAT_TIMEOUT_MS = 45 * 1000;

const SAFETY_LIMITS = {
  playlistChunkSize: 50,
  maxPlaylistVideos: 1000,
  maxVideoDuration: 4 * 60 * 60,
  maxJobsPerClient: 5,
  rateLimitWindowMs: 60 * 1000,
  rateLimitMaxRequests: 60,
  maxUrlLength: 2048
};

const rateLimitStore = new Map();

const activeJobsByType = {
  download: 0,
  playlist: 0,
  convert: 0,
  compress: 0
};

const jobQueue = [];
const HEAVY_JOB_TYPES = ['playlist', 'convert', 'compress'];

const clientSessions = new Map();
const jobToClient = new Map();

function registerClient(clientId) {
  if (!clientSessions.has(clientId)) {
    clientSessions.set(clientId, { 
      lastHeartbeat: Date.now(), 
      activeJobs: new Set() 
    });
    console.log(`[Session] Client ${clientId.slice(0, 8)}... connected`);
    
    const currentCount = clientSessions.size;
    const now = Date.now();
    const dayAgo = now - 24 * 60 * 60 * 1000;
    if (!analytics.peakUsers) {
      analytics.peakUsers = { count: 0, timestamp: now };
    }
    if (currentCount > analytics.peakUsers.count || analytics.peakUsers.timestamp < dayAgo) {
      analytics.peakUsers = { count: currentCount, timestamp: now };
      saveAnalytics(analytics);
    }
  }
}

function updateHeartbeat(clientId) {
  const session = clientSessions.get(clientId);
  if (session) {
    session.lastHeartbeat = Date.now();
    return true;
  }
  return false;
}

function linkJobToClient(jobId, clientId) {
  if (clientId && clientSessions.has(clientId)) {
    const session = clientSessions.get(clientId);
    session.activeJobs.add(jobId);
    jobToClient.set(jobId, clientId);
  }
}

function unlinkJobFromClient(jobId) {
  const clientId = jobToClient.get(jobId);
  if (clientId) {
    const session = clientSessions.get(clientId);
    if (session) {
      session.activeJobs.delete(jobId);
    }
    jobToClient.delete(jobId);
  }
}

setInterval(() => {
  const now = Date.now();
  for (const [clientId, session] of clientSessions.entries()) {
    if (now - session.lastHeartbeat > HEARTBEAT_TIMEOUT_MS) {
      console.log(`[Session] Client ${clientId.slice(0, 8)}... timed out, cancelling ${session.activeJobs.size} jobs`);
      
      for (const jobId of session.activeJobs) {
        const processInfo = activeProcesses.get(jobId);
        if (processInfo) {
          processInfo.cancelled = true;
          if (processInfo.process) {
            processInfo.process.kill('SIGTERM');
          }
          sendProgress(jobId, 'cancelled', 'Connection lost - task cancelled');
        }
      }
      
      clientSessions.delete(clientId);
    }
  }
}, 10000);

function canStartJob(jobType) {
  if (HEAVY_JOB_TYPES.includes(jobType)) {
    const totalActive = Object.values(activeJobsByType).reduce((a, b) => a + b, 0);
    if (totalActive > 1) return false;
  }
  
  return activeJobsByType[jobType] < JOB_LIMITS[jobType];
}

function addToJobQueue(jobFn, jobType, jobId, clientId) {
  return new Promise((resolve, reject) => {
    const job = { 
      fn: jobFn, 
      resolve, 
      reject, 
      jobType, 
      jobId, 
      clientId,
      addedAt: Date.now() 
    };
    
    if (jobQueue.length >= MAX_QUEUE_SIZE) {
      reject(new Error('Server is too busy. Please try again later.'));
      return;
    }
    
    linkJobToClient(jobId, clientId);
    
    jobQueue.push(job);
    console.log(`[Queue] ${jobType} job ${jobId.slice(0, 8)}... added. Queue: ${jobQueue.length}`);
    processQueue();
  });
}

function processQueue() {
  jobQueue.sort((a, b) => {
    const aIsHeavy = HEAVY_JOB_TYPES.includes(a.jobType) ? 1 : 0;
    const bIsHeavy = HEAVY_JOB_TYPES.includes(b.jobType) ? 1 : 0;
    if (aIsHeavy !== bIsHeavy) return aIsHeavy - bIsHeavy;
    return a.addedAt - b.addedAt;
  });
  
  for (let i = 0; i < jobQueue.length; i++) {
    const job = jobQueue[i];
    
    if (canStartJob(job.jobType)) {
      jobQueue.splice(i, 1);
      activeJobsByType[job.jobType]++;
      
      console.log(`[Queue] Starting ${job.jobType} job ${job.jobId.slice(0, 8)}... Active: ${JSON.stringify(activeJobsByType)}`);
      
      job.fn()
        .then(result => {
          activeJobsByType[job.jobType]--;
          unlinkJobFromClient(job.jobId);
          job.resolve(result);
          processQueue();
        })
        .catch(err => {
          activeJobsByType[job.jobType]--;
          unlinkJobFromClient(job.jobId);
          job.reject(err);
          processQueue();
        });
      
      i = -1;
    }
  }
}

function getQueueStatus() {
  return {
    active: activeJobsByType,
    queued: jobQueue.length,
    limits: JOB_LIMITS
  };
}

function getClientIp(req) {
  return req.headers['x-forwarded-for']?.split(',')[0]?.trim() || 
         req.headers['x-real-ip'] || 
         req.socket?.remoteAddress || 
         'unknown';
}

function checkRateLimit(ip) {
  const now = Date.now();
  const windowStart = now - SAFETY_LIMITS.rateLimitWindowMs;
  
  if (!rateLimitStore.has(ip)) {
    rateLimitStore.set(ip, []);
  }
  
  const requests = rateLimitStore.get(ip).filter(t => t > windowStart);
  rateLimitStore.set(ip, requests);
  
  if (requests.length >= SAFETY_LIMITS.rateLimitMaxRequests) {
    return { allowed: false, remaining: 0, resetIn: Math.ceil((requests[0] + SAFETY_LIMITS.rateLimitWindowMs - now) / 1000) };
  }
  
  requests.push(now);
  return { allowed: true, remaining: SAFETY_LIMITS.rateLimitMaxRequests - requests.length };
}

function getClientJobCount(clientId) {
  const session = clientSessions.get(clientId);
  return session ? session.activeJobs.size : 0;
}

function validateUrl(url) {
  if (!url || typeof url !== 'string') {
    return { valid: false, error: 'URL is required' };
  }
  
  if (url.length > SAFETY_LIMITS.maxUrlLength) {
    return { valid: false, error: 'URL is too long' };
  }
  
  try {
    const parsed = new URL(url);
    
    if (!['http:', 'https:'].includes(parsed.protocol)) {
      return { valid: false, error: 'Only HTTP/HTTPS URLs are allowed' };
    }
    
    const hostname = parsed.hostname.toLowerCase();
    const privatePatterns = [
      /^localhost$/i,
      /^127\./,
      /^10\./,
      /^172\.(1[6-9]|2[0-9]|3[01])\./,
      /^192\.168\./,
      /^0\./,
      /^169\.254\./,
      /^\[::1\]$/,
      /^\[fe80:/i,
      /^\[fc00:/i,
      /^\[fd00:/i
    ];
    
    for (const pattern of privatePatterns) {
      if (pattern.test(hostname)) {
        return { valid: false, error: 'Private/local URLs are not allowed' };
      }
    }
    
    return { valid: true };
  } catch (e) {
    return { valid: false, error: 'Invalid URL format' };
  }
}

function rateLimitMiddleware(req, res, next) {
  const ip = getClientIp(req);
  const result = checkRateLimit(ip);
  
  res.setHeader('X-RateLimit-Limit', SAFETY_LIMITS.rateLimitMaxRequests);
  res.setHeader('X-RateLimit-Remaining', result.remaining);
  
  if (!result.allowed) {
    res.setHeader('X-RateLimit-Reset', result.resetIn);
    return res.status(429).json({ 
      error: 'Too many requests. Please slow down.',
      resetIn: result.resetIn
    });
  }
  
  next();
}

setInterval(() => {
  const now = Date.now();
  const windowStart = now - SAFETY_LIMITS.rateLimitWindowMs;
  for (const [ip, requests] of rateLimitStore.entries()) {
    const valid = requests.filter(t => t > windowStart);
    if (valid.length === 0) {
      rateLimitStore.delete(ip);
    } else {
      rateLimitStore.set(ip, valid);
    }
  }
}, 60000);

const ALLOWED_ORIGINS = [
  'http://localhost:3000',
  'http://localhost:5173',
  'http://127.0.0.1:3000',
  'http://127.0.0.1:5173',
  'https://yoink.tools',
  'https://www.yoink.tools',
  'https://yoink.pages.dev',
  'https://yoink-tools.pages.dev',
  /\.pages\.dev$/,
  /\.yoink\.tools$/,
];

app.use(cors({
  origin: true,
  credentials: true
}));

app.use(express.json({ limit: '500mb' }));

app.use('/api/download', rateLimitMiddleware);
app.use('/api/download-playlist', rateLimitMiddleware);
app.use('/api/convert', rateLimitMiddleware);
app.use('/api/compress', rateLimitMiddleware);

const TEMP_DIR = path.join(os.tmpdir(), 'yoink-downloads');
if (!fs.existsSync(TEMP_DIR)) {
  fs.mkdirSync(TEMP_DIR, { recursive: true });
}

function cleanupTempFiles() {
  try {
    const files = fs.readdirSync(TEMP_DIR);
    const now = Date.now();
    files.forEach(file => {
      const filePath = path.join(TEMP_DIR, file);
      const stat = fs.statSync(filePath);
      if (now - stat.mtimeMs > 60 * 60 * 1000) {
        fs.unlinkSync(filePath);
        console.log(`Cleaned up old temp file: ${file}`);
      }
    });
  } catch (err) {
    console.error('Cleanup error:', err);
  }
}

setInterval(cleanupTempFiles, 15 * 60 * 1000);
cleanupTempFiles();

function checkDependencies() {
  try {
    execSync('which yt-dlp', { stdio: 'ignore' });
    console.log('✓ yt-dlp found');
  } catch {
    console.error('✗ yt-dlp not found. Please install: brew install yt-dlp');
    process.exit(1);
  }
  
  try {
    execSync('which ffmpeg', { stdio: 'ignore' });
    console.log('✓ ffmpeg found');
  } catch {
    console.error('✗ ffmpeg not found. Please install: brew install ffmpeg');
    process.exit(1);
  }
}

checkDependencies();

function sanitizeFilename(filename) {
  return filename
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200);
}

const QUALITY_HEIGHT = {
  'best': null,  // No limit
  '4k': 2160,
  '1440p': 1440,
  '1080p': 1080,
  '720p': 720,
  '480p': 480,
  '360p': 360
};

const CONTAINER_MIMES = {
  'mp4': 'video/mp4',
  'webm': 'video/webm',
  'mkv': 'video/x-matroska',
  'mov': 'video/quicktime'
};

const AUDIO_MIMES = {
  'mp3': 'audio/mpeg',
  'm4a': 'audio/mp4',
  'opus': 'audio/opus',
  'wav': 'audio/wav',
  'flac': 'audio/flac'
};

app.get('/health', (req, res) => {
  res.json({ 
    status: 'ok', 
    version: '1.0.0',
    queue: getQueueStatus()
  });
});

app.post('/api/heartbeat/:clientId', (req, res) => {
  const { clientId } = req.params;
  
  if (!clientId) {
    return res.status(400).json({ error: 'Client ID required' });
  }
  
  registerClient(clientId);
  updateHeartbeat(clientId);
  
  const session = clientSessions.get(clientId);
  res.json({ 
    ok: true, 
    activeJobs: session ? session.activeJobs.size : 0,
    queue: getQueueStatus()
  });
});

app.post('/api/connect', (req, res) => {
  const clientId = uuidv4();
  registerClient(clientId);
  res.json({ clientId, queue: getQueueStatus() });
});

app.get('/api/queue-status', (req, res) => {
  const status = getQueueStatus();
  res.json(status);
});

app.get('/api/limits', (req, res) => {
  res.json({
    playlistChunkSize: SAFETY_LIMITS.playlistChunkSize,
    maxPlaylistVideos: SAFETY_LIMITS.maxPlaylistVideos,
    maxVideoDuration: SAFETY_LIMITS.maxVideoDuration,
    maxJobsPerClient: SAFETY_LIMITS.maxJobsPerClient,
    maxFileSizeMB: Math.floor(FILE_SIZE_LIMIT / (1024 * 1024)),
    rateLimitPerMinute: SAFETY_LIMITS.rateLimitMaxRequests
  });
});

app.get('/api/metadata', (req, res) => {
  const { url, playlist } = req.query;
  const downloadPlaylist = playlist === 'true';

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const ytdlpArgs = [];
  
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
      console.error('yt-dlp metadata error:', errorOutput);
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
        videoTitles: videoTitles.slice(0, 50)
      });
    } else {
      const title = lines[0] || 'download';
      const ext = lines[1] || 'mp4';
      const id = lines[2] || '';
      const uploader = lines[3] || '';
      const duration = lines[4] || '';
      const thumbnail = lines[5] || '';

      res.json({ title, ext, id, uploader, duration, thumbnail, isPlaylist: false });
    }
  });

  ytdlp.on('error', (err) => {
    console.error('Failed to spawn yt-dlp:', err);
    res.status(500).json({ error: 'yt-dlp not found. Please install yt-dlp.' });
  });
});

const activeDownloads = new Map();

const activeProcesses = new Map();

app.get('/api/progress/:id', (req, res) => {
  const { id } = req.params;
  
  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.flushHeaders();

  res.write(`data: ${JSON.stringify({ stage: 'connected', message: 'Connected to progress stream' })}\n\n`);

  activeDownloads.set(id, res);

  req.on('close', () => {
    activeDownloads.delete(id);
  });
});

app.post('/api/cancel/:id', (req, res) => {
  const { id } = req.params;
  
  const processInfo = activeProcesses.get(id);
  if (processInfo) {
    console.log(`[${id}] Cancelling download...`);
    processInfo.cancelled = true;
    
    if (processInfo.process) {
      try {
        processInfo.process.kill('SIGTERM');
      } catch (e) {
        console.error(`[${id}] Error killing process:`, e);
      }
    }
    
    if (processInfo.tempDir) {
      setTimeout(() => {
        try { fs.rmSync(processInfo.tempDir, { recursive: true }); } catch {}
      }, 1000);
    }
    if (processInfo.tempFile) {
      setTimeout(() => {
        try { fs.unlinkSync(processInfo.tempFile); } catch {}
      }, 1000);
    }
    
    activeProcesses.delete(id);
    sendProgress(id, 'cancelled', 'Download cancelled');
    res.json({ success: true, message: 'Download cancelled' });
  } else {
    res.json({ success: false, message: 'Download not found or already completed' });
  }
});

function sendProgress(downloadId, stage, message, progress = null, extra = null) {
  const res = activeDownloads.get(downloadId);
  if (res) {
    const data = { stage, message };
    if (progress !== null) data.progress = progress;
    if (extra !== null) Object.assign(data, extra);
    data.queueStatus = getQueueStatus();
    res.write(`data: ${JSON.stringify(data)}\n\n`);
  }
  console.log(`[${downloadId}] ${stage}: ${message}`);
}

function sendQueuePosition(progressId) {
  const position = jobQueue.findIndex(j => j.progressId === progressId);
  if (position >= 0) {
    sendProgress(progressId, 'queued', `You are #${position + 1} in queue`, 0, {
      queuePosition: position + 1,
      estimatedWait: (position + 1) * 30
    });
  }
}

app.get('/api/download', async (req, res) => {
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
    clientId
  } = req.query;
  
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
  const tempFile = path.join(TEMP_DIR, `${downloadId}.%(ext)s`);
  const finalFile = path.join(TEMP_DIR, `${downloadId}-final.${outputExt}`);

  const processInfo = { cancelled: false, process: null, tempFile: finalFile };
  activeProcesses.set(downloadId, processInfo);

  activeJobsByType.download++;
  console.log(`[Queue] Download started. Active: ${JSON.stringify(activeJobsByType)}`);

  sendProgress(downloadId, 'starting', 'Initializing download...');

  try {
    sendProgress(downloadId, 'downloading', 'Downloading from source...', 0);
    
    const ytdlpArgs = [
      downloadPlaylist ? '--yes-playlist' : '--no-playlist',
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
    
    console.log(`[${downloadId}] yt-dlp command: yt-dlp ${ytdlpArgs.join(' ')}`);

    let lastProgress = 0;
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
          }
        }
      });

      ytdlp.stderr.on('data', (data) => {
        const msg = data.toString();
        if (msg.includes('ERROR')) {
          console.error(`[${downloadId}] yt-dlp error: ${msg.trim()}`);
        } else if (msg.includes('[download]') && msg.includes('%')) {
          const match = msg.match(/([\d.]+)%/);
          if (match) {
            const progress = parseFloat(match[1]);
            if (progress > lastProgress + 5 || progress >= 100) {
              lastProgress = progress;
              sendProgress(downloadId, 'downloading', `Downloading... ${progress.toFixed(0)}%`, progress);
            }
          }
        }
      });

      ytdlp.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Download failed (code ${code}). Try again or use a different URL.`));
      });

      ytdlp.on('error', reject);

      req.on('close', () => {
        ytdlp.kill('SIGTERM');
      });
    });

    const files = fs.readdirSync(TEMP_DIR);
    const downloadedFile = files.find(f => f.startsWith(downloadId) && !f.includes('-final'));
    
    if (!downloadedFile) {
      throw new Error('Downloaded file not found');
    }

    const downloadedPath = path.join(TEMP_DIR, downloadedFile);
    
    const isTwitter = url.includes('twitter.com') || url.includes('x.com');
    let isGif = false;
    
    if (isTwitter && !isAudio) {
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
    const actualFinalFile = isGif ? path.join(TEMP_DIR, `${downloadId}-final.gif`) : finalFile;

    sendProgress(downloadId, 'processing', isGif ? 'Converting to GIF...' : 'Processing video...', 100);

    const ffmpegArgs = [
      '-y',
      '-i', downloadedPath,
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
        '-i', downloadedPath,
        '-vf', 'fps=15,scale=480:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse',
        '-loop', '0'
      );
    } else {
      ffmpegArgs.push('-codec', 'copy');
      if (container === 'mp4' || container === 'mov') {
        ffmpegArgs.push('-movflags', '+faststart');
      }
    }

    ffmpegArgs.push(actualFinalFile);

    sendProgress(downloadId, 'remuxing', isGif ? 'Creating GIF...' : 'Preparing file...');

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', ffmpegArgs);
      
      ffmpeg.stderr.on('data', (data) => {
        const msg = data.toString();
        if (msg.includes('Error') || msg.includes('error')) {
          console.error(`[${downloadId}] ffmpeg: ${msg.trim()}`);
        }
      });
      
      ffmpeg.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Encoding failed (code ${code})`));
      });

      ffmpeg.on('error', reject);
    });

    try { fs.unlinkSync(downloadedPath); } catch {}

    sendProgress(downloadId, 'sending', 'Sending file to you...');
    
    const stat = fs.statSync(actualFinalFile);
    const safeFilename = sanitizeFilename(filename || 'download');
    const fullFilename = `${safeFilename}.${actualOutputExt}`;
    const asciiFilename = safeFilename.replace(/[^\x20-\x7E]/g, '_') + '.' + actualOutputExt;
    const mimeType = isGif 
      ? 'image/gif'
      : isAudio 
        ? (AUDIO_MIMES[audioFormat] || 'audio/mpeg')
        : (CONTAINER_MIMES[container] || 'video/mp4');

    res.setHeader('Content-Type', mimeType);
    res.setHeader('Content-Length', stat.size);
    res.setHeader('Content-Disposition', `attachment; filename="${asciiFilename}"; filename*=UTF-8''${encodeURIComponent(fullFilename)}`);

    const stream = fs.createReadStream(actualFinalFile);
    let finished = false;
    
    stream.pipe(res);

    stream.on('close', () => {
      if (finished) return;
      finished = true;
      sendProgress(downloadId, 'complete', 'Download complete!');
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      
      try {
        const site = new URL(url).hostname.replace('www.', '');
        trackDownload(actualOutputExt, site, getCountryFromIP(req.headers['x-forwarded-for'] || req.ip));
      } catch (e) {}
      
      console.log(`[Queue] Download finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => {
        try { fs.unlinkSync(actualFinalFile); } catch {}
      }, 5000);
    });

    stream.on('error', (err) => {
      if (finished) return;
      finished = true;
      console.error(`[${downloadId}] Stream error:`, err);
      sendProgress(downloadId, 'error', 'Failed to send file');
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Download failed. Active: ${JSON.stringify(activeJobsByType)}`);
      try { fs.unlinkSync(actualFinalFile); } catch {}
    });

    res.on('finish', () => {
      if (finished) return;
      finished = true;
      sendProgress(downloadId, 'complete', 'Download complete!');
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Download finished (res). Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => {
        try { fs.unlinkSync(actualFinalFile); } catch {}
      }, 5000);
    });

    req.on('close', () => {
      if (finished) return;
      finished = true;
      stream.destroy();
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Download cancelled. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => {
        try { fs.unlinkSync(actualFinalFile); } catch {}
      }, 1000);
    });

  } catch (err) {
    console.error(`[${downloadId}] Error:`, err.message);
    
    activeProcesses.delete(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Download error. Active: ${JSON.stringify(activeJobsByType)}`);
    
    const files = fs.readdirSync(TEMP_DIR);
    files.filter(f => f.startsWith(downloadId)).forEach(f => {
      try { fs.unlinkSync(path.join(TEMP_DIR, f)); } catch {}
    });

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Download failed' });
    }
  }
});

app.get('/api/download-playlist', async (req, res) => {
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
  const playlistDir = path.join(TEMP_DIR, downloadId);
  
  if (!fs.existsSync(playlistDir)) {
    fs.mkdirSync(playlistDir, { recursive: true });
  }

  const processInfo = { cancelled: false, process: null, tempDir: playlistDir };
  activeProcesses.set(downloadId, processInfo);

  sendProgress(downloadId, 'starting', 'Getting playlist info...');

  try {
    const playlistInfo = await new Promise((resolve, reject) => {
      const ytdlp = spawn('yt-dlp', [
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
          reject(new Error('Failed to get playlist info'));
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
        const ytdlpArgs = [
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

        ytdlpArgs.push(videoUrl || `https://www.youtube.com/watch?v=${entry.id}`);

        await new Promise((resolve, reject) => {
          const ytdlp = spawn('yt-dlp', ytdlpArgs);
          
          processInfo.process = ytdlp;
          
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
                  videoProgress,
                  format: isAudio ? audioFormat : `${quality} ${container}`
                });
            }
          });

          ytdlp.stderr.on('data', (data) => {
            const msg = data.toString();
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
                    videoProgress,
                    format: isAudio ? audioFormat : `${quality} ${container}`
                  });
              }
            }
          });

          ytdlp.on('close', (code) => {
            if (code === 0) resolve();
            else reject(new Error(`Failed to download video ${videoNum}`));
          });

          ytdlp.on('error', reject);
        });

        const tempFiles = fs.readdirSync(playlistDir);
        const downloadedTemp = tempFiles.find(f => f.startsWith(`temp_${videoNum}.`));
        
        if (downloadedTemp) {
          const tempPath = path.join(playlistDir, downloadedTemp);
          
          sendProgress(downloadId, 'processing', 
            `Processing ${videoNum}/${totalVideos}: ${videoTitle}`, 
            ((videoNum - 0.5) / totalVideos) * 100, {
              playlistTitle,
              totalVideos,
              currentVideo: videoNum,
              currentVideoTitle: videoTitle,
              format: isAudio ? audioFormat : `${quality} ${container}`
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
            ffmpeg.on('close', (code) => {
              if (code === 0) resolve();
              else reject(new Error(`Processing failed for video ${videoNum}`));
            });
            ffmpeg.on('error', reject);
          });

          try { fs.unlinkSync(tempPath); } catch {}
          
          downloadedFiles.push(videoFile);
        }

      } catch (err) {
        console.error(`[${downloadId}] Error downloading video ${videoNum}:`, err.message);
      }
    }

    if (downloadedFiles.length === 0) {
      throw new Error('No videos were successfully downloaded');
    }

    sendProgress(downloadId, 'zipping', `Creating zip file with ${downloadedFiles.length} videos...`, 95, {
      playlistTitle,
      totalVideos,
      downloadedCount: downloadedFiles.length,
      format: isAudio ? audioFormat : `${quality} ${container}`
    });

    const zipPath = path.join(TEMP_DIR, `${downloadId}.zip`);
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
        downloadedCount: downloadedFiles.length
      });
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Playlist finished. Active: ${JSON.stringify(activeJobsByType)}`);
      
      setTimeout(() => {
        try { fs.unlinkSync(zipPath); } catch {}
        try { fs.rmSync(playlistDir, { recursive: true }); } catch {}
      }, 5000);
    });

    stream.on('error', (err) => {
      console.error(`[${downloadId}] Stream error:`, err);
      sendProgress(downloadId, 'error', 'Failed to send zip file');
      activeProcesses.delete(downloadId);
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Playlist failed. Active: ${JSON.stringify(activeJobsByType)}`);
    });

  } catch (err) {
    console.error(`[${downloadId}] Playlist error:`, err.message);
    
    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Playlist download failed');
    }
    
    activeProcesses.delete(downloadId);
    activeJobsByType.playlist--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Playlist error. Active: ${JSON.stringify(activeJobsByType)}`);
    
    try { fs.rmSync(playlistDir, { recursive: true }); } catch {}
    
    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Playlist download failed' });
    }
  }
});

const upload = multer({ 
  dest: TEMP_DIR,
  limits: { fileSize: FILE_SIZE_LIMIT }
});

app.post('/api/convert', upload.single('file'), async (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const { format = 'mp4', clientId } = req.body;

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => {});
      return res.status(429).json({ 
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.` 
      });
    }
  }

  const convertId = uuidv4();
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIR, `${convertId}-converted.${format}`);

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(convertId, clientId);
  }

  activeJobsByType.convert++;
  console.log(`[Queue] Convert started. Active: ${JSON.stringify(activeJobsByType)}`);

  console.log(`[${convertId}] Converting ${req.file.originalname} to ${format}`);

  try {
    const ffmpegArgs = ['-y', '-i', inputPath];

    if (['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format)) {
      if (format === 'mp3') {
        ffmpegArgs.push('-codec:a', 'libmp3lame', '-b:a', '320k');
      } else if (format === 'm4a') {
        ffmpegArgs.push('-codec:a', 'aac', '-b:a', '256k');
      } else if (format === 'opus') {
        ffmpegArgs.push('-codec:a', 'libopus', '-b:a', '128k');
      } else if (format === 'wav') {
        ffmpegArgs.push('-codec:a', 'pcm_s16le');
      } else if (format === 'flac') {
        ffmpegArgs.push('-codec:a', 'flac');
      }
      ffmpegArgs.push('-vn');
    } else {
      ffmpegArgs.push('-codec', 'copy');
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

    try { fs.unlinkSync(inputPath); } catch {}

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}.${format}`;
    const isAudioFormat = ['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format);
    const mimeType = isAudioFormat 
      ? (AUDIO_MIMES[format] || 'audio/mpeg')
      : (CONTAINER_MIMES[format] || 'video/mp4');

    res.setHeader('Content-Type', mimeType);
    res.setHeader('Content-Length', stat.size);
    res.setHeader('Content-Disposition', `attachment; filename="${outputFilename}"; filename*=UTF-8''${encodeURIComponent(outputFilename)}`);

    const stream = fs.createReadStream(outputPath);
    stream.pipe(res);

    stream.on('close', () => {
      console.log(`[${convertId}] Conversion complete: ${outputFilename}`);
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      
      const fromExt = path.extname(req.file.originalname).replace('.', '') || 'unknown';
      trackConvert(fromExt, format);
      
      console.log(`[Queue] Convert finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => {
        try { fs.unlinkSync(outputPath); } catch {}
      }, FILE_RETENTION_MS);
    });

    stream.on('error', () => {
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);
      console.log(`[Queue] Convert failed. Active: ${JSON.stringify(activeJobsByType)}`);
    });

  } catch (err) {
    console.error(`[${convertId}] Error:`, err);
    try { fs.unlinkSync(inputPath); } catch {}
    try { fs.unlinkSync(outputPath); } catch {}
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    console.log(`[Queue] Convert error. Active: ${JSON.stringify(activeJobsByType)}`);
    
    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Conversion failed' });
    }
  }
});

app.post('/api/compress', upload.single('file'), async (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const { targetSize = '50', duration = '0', progressId, clientId } = req.body;
  const targetMB = parseFloat(targetSize);
  const videoDuration = parseFloat(duration);

  if (videoDuration > SAFETY_LIMITS.maxVideoDuration) {
    fs.unlink(req.file.path, () => {});
    return res.status(400).json({ 
      error: `Video too long. Maximum duration is ${SAFETY_LIMITS.maxVideoDuration / 3600} hours.` 
    });
  }

  if (clientId) {
    const clientJobs = getClientJobCount(clientId);
    if (clientJobs >= SAFETY_LIMITS.maxJobsPerClient) {
      fs.unlink(req.file.path, () => {});
      return res.status(429).json({ 
        error: `Too many active jobs. Maximum ${SAFETY_LIMITS.maxJobsPerClient} concurrent jobs per user.` 
      });
    }
  }
  
  const compressId = progressId || uuidv4();
  const inputPath = req.file.path;
  const outputPath = path.join(TEMP_DIR, `${compressId}-compressed.mp4`);
  const passLogFile = path.join(TEMP_DIR, `${compressId}-pass`);

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(compressId, clientId);
  }

  activeJobsByType.compress++;
  console.log(`[Queue] Compress started. Active: ${JSON.stringify(activeJobsByType)}`);

  console.log(`[${compressId}] Compressing ${req.file.originalname} to ${targetMB}MB`);

  const processInfo = { cancelled: false, process: null, tempFile: outputPath };
  activeProcesses.set(compressId, processInfo);

  try {
    let actualDuration = videoDuration;
    if (!actualDuration || actualDuration <= 0) {
      sendProgress(compressId, 'compressing', 'Analyzing video...', 0);
      
      actualDuration = await new Promise((resolve, reject) => {
        const ffprobe = spawn('ffprobe', [
          '-v', 'error',
          '-show_entries', 'format=duration',
          '-of', 'default=noprint_wrappers=1:nokey=1',
          inputPath
        ]);
        
        let output = '';
        ffprobe.stdout.on('data', (data) => { output += data.toString(); });
        ffprobe.on('close', (code) => {
          const dur = parseFloat(output.trim());
          resolve(isNaN(dur) ? 60 : dur);
        });
        ffprobe.on('error', () => resolve(60));
      });
    }

    const targetBytes = targetMB * 1024 * 1024;
    const audioBitrate = 128000;
    const audioBytes = (audioBitrate / 8) * actualDuration;
    const videoBytes = targetBytes - audioBytes;
    const videoBitrate = Math.floor((videoBytes * 8) / actualDuration);
    
    const safetyMargin = 0.92;
    const adjustedVideoBitrate = Math.floor(videoBitrate * safetyMargin);
    const finalVideoBitrate = Math.max(100000, adjustedVideoBitrate);
    const videoBitrateK = Math.floor(finalVideoBitrate / 1000);
    const maxrateK = Math.floor(videoBitrateK * 1.2);
    const bufsizeK = Math.floor(videoBitrateK * 2);

    console.log(`[${compressId}] Duration: ${actualDuration.toFixed(1)}s, Video bitrate: ${videoBitrateK}k (max: ${maxrateK}k), Audio: 128k`);

    sendProgress(compressId, 'compressing', `Pass 1/2 - Analyzing video...`, 5);

    const pass1Args = [
      '-y',
      '-i', inputPath,
      '-c:v', 'libx264',
      '-preset', 'medium',
      '-b:v', `${videoBitrateK}k`,
      '-maxrate', `${maxrateK}k`,
      '-bufsize', `${bufsizeK}k`,
      '-pass', '1',
      '-passlogfile', passLogFile,
      '-an',
      '-f', 'null',
      process.platform === 'win32' ? 'NUL' : '/dev/null'
    ];

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', pass1Args);
      processInfo.process = ffmpeg;
      
      let lastProgress = 0;
      ffmpeg.stderr.on('data', (data) => {
        const msg = data.toString();
        const timeMatch = msg.match(/time=(\d+):(\d+):(\d+)/);
        if (timeMatch) {
          const hours = parseInt(timeMatch[1]);
          const mins = parseInt(timeMatch[2]);
          const secs = parseInt(timeMatch[3]);
          const currentTime = hours * 3600 + mins * 60 + secs;
          const progress = Math.min(45, (currentTime / actualDuration) * 45);
          if (progress > lastProgress + 2) {
            lastProgress = progress;
            sendProgress(compressId, 'compressing', `Pass 1/2 - ${Math.round(progress / 45 * 100)}%`, progress);
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

    sendProgress(compressId, 'compressing', `Pass 2/2 - Encoding video...`, 50);

    const pass2Args = [
      '-y',
      '-i', inputPath,
      '-c:v', 'libx264',
      '-preset', 'medium',
      '-b:v', `${videoBitrateK}k`,
      '-maxrate', `${maxrateK}k`,
      '-bufsize', `${bufsizeK}k`,
      '-pass', '2',
      '-passlogfile', passLogFile,
      '-c:a', 'aac',
      '-b:a', '128k',
      '-movflags', '+faststart',
      outputPath
    ];

    await new Promise((resolve, reject) => {
      const ffmpeg = spawn('ffmpeg', pass2Args);
      processInfo.process = ffmpeg;
      
      let lastProgress = 50;
      ffmpeg.stderr.on('data', (data) => {
        const msg = data.toString();
        const timeMatch = msg.match(/time=(\d+):(\d+):(\d+)/);
        if (timeMatch) {
          const hours = parseInt(timeMatch[1]);
          const mins = parseInt(timeMatch[2]);
          const secs = parseInt(timeMatch[3]);
          const currentTime = hours * 3600 + mins * 60 + secs;
          const progress = 50 + Math.min(45, (currentTime / actualDuration) * 45);
          if (progress > lastProgress + 2) {
            lastProgress = progress;
            sendProgress(compressId, 'compressing', `Pass 2/2 - ${Math.round((progress - 50) / 45 * 100)}%`, progress);
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

    try { fs.unlinkSync(inputPath); } catch {}
    try { fs.unlinkSync(`${passLogFile}-0.log`); } catch {}
    try { fs.unlinkSync(`${passLogFile}-0.log.mbtree`); } catch {}

    sendProgress(compressId, 'compressing', 'Sending compressed file...', 98);

    const stat = fs.statSync(outputPath);
    const originalName = path.parse(req.file.originalname).name;
    const outputFilename = `${sanitizeFilename(originalName)}_compressed.mp4`;

    console.log(`[${compressId}] Compression complete: ${(stat.size / 1024 / 1024).toFixed(2)}MB (target: ${targetMB}MB)`);

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
      trackCompress();
      console.log(`[Queue] Compress finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => {
        try { fs.unlinkSync(outputPath); } catch {}
      }, FILE_RETENTION_MS);
    });

    stream.on('error', () => {
      activeJobsByType.compress--;
      unlinkJobFromClient(compressId);
      console.log(`[Queue] Compress failed. Active: ${JSON.stringify(activeJobsByType)}`);
    });

  } catch (err) {
    console.error(`[${compressId}] Error:`, err.message);
    activeProcesses.delete(compressId);
    activeJobsByType.compress--;
    unlinkJobFromClient(compressId);
    console.log(`[Queue] Compress error. Active: ${JSON.stringify(activeJobsByType)}`);
    
    try { fs.unlinkSync(inputPath); } catch {}
    try { fs.unlinkSync(outputPath); } catch {}
    try { fs.unlinkSync(`${passLogFile}-0.log`); } catch {}
    try { fs.unlinkSync(`${passLogFile}-0.log.mbtree`); } catch {}
    
    if (!processInfo.cancelled) {
      sendProgress(compressId, 'error', err.message || 'Compression failed');
    }
    
    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Compression failed' });
    }
  }
});

app.post('/api/analytics/track', (req, res) => {
  const { type, page } = req.body;
  const country = getCountryFromIP(getClientIp(req));
  
  if (type === 'pageview' && page) {
    trackPageView(page, country);
  }
  if (type === 'dailyUser') {
    trackDailyUser(getClientIp(req), country);
  }
  
  res.json({ ok: true });
});

app.post('/api/admin/login', (req, res) => {
  const { password } = req.body;
  
  if (!adminConfig.ADMIN_PASSWORD) {
    return res.status(503).json({ error: 'Admin features not configured' });
  }
  
  if (password === adminConfig.ADMIN_PASSWORD) {
    const token = generateAdminToken();
    res.json({ success: true, token });
  } else {
    res.status(401).json({ error: 'Invalid password' });
  }
});

app.get('/api/admin/analytics', (req, res) => {
  const token = req.headers['authorization']?.replace('Bearer ', '');
  
  if (!validateAdminToken(token)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }
  
  analytics = loadAnalytics();
  
  const dailyUsersFormatted = {};
  for (const [date, users] of Object.entries(analytics.dailyUsers)) {
    if (users instanceof Set) {
      dailyUsersFormatted[date] = users.size;
    } else if (Array.isArray(users)) {
      dailyUsersFormatted[date] = users.length;
    } else {
      dailyUsersFormatted[date] = Object.keys(users).length;
    }
  }
  
  res.json({
    totalDownloads: analytics.totalDownloads,
    totalConverts: analytics.totalConverts,
    totalCompresses: analytics.totalCompresses,
    formats: analytics.formats,
    sites: analytics.sites,
    countries: analytics.countries,
    dailyUsers: dailyUsersFormatted,
    pageViews: analytics.pageViews,
    currentUsers: clientSessions.size,
    peakUsers: analytics.peakUsers || { count: 0, timestamp: Date.now() },
    activeJobs: activeJobsByType,
    queueLength: jobQueue.length,
    lastUpdated: analytics.lastUpdated
  });
});

app.listen(PORT, () => {
  console.log(`\n🎯 yoink API server running at http://localhost:${PORT}`);
  console.log(`   Job limits: downloads=${JOB_LIMITS.download}, playlists=${JOB_LIMITS.playlist}, convert=${JOB_LIMITS.convert}, compress=${JOB_LIMITS.compress}`);
  console.log(`   Max queue size: ${MAX_QUEUE_SIZE}`);
  console.log(`   Max file upload: ${(FILE_SIZE_LIMIT / 1024 / 1024 / 1024).toFixed(0)}GB`);
  console.log(`   File retention: ${FILE_RETENTION_MS / 60000} minutes`);
  console.log(`   Heartbeat timeout: ${HEARTBEAT_TIMEOUT_MS / 1000}s`);
  console.log(`\nEndpoints:`);
  console.log(`  GET  /health               - Health check + queue status`);
  console.log(`  POST /api/connect          - Get client ID`);
  console.log(`  POST /api/heartbeat/:id    - Keep session alive`);
  console.log(`  GET  /api/queue-status     - Queue status`);
  console.log(`  GET  /api/metadata         - Get video metadata`);
  console.log(`  GET  /api/download         - Download video/audio`);
  console.log(`  GET  /api/download-playlist - Download playlist as zip`);
  console.log(`  POST /api/convert          - Convert uploaded file`);
  console.log(`  POST /api/compress         - Compress video to target size\n`);
});
