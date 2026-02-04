require('dotenv').config();
const express = require('express');
const cors = require('cors');
const cookieParser = require('cookie-parser');
const { spawn, execSync, spawnSync } = require('child_process');
const net = require('net');
const path = require('path');
const fs = require('fs');
const os = require('os');
const { v4: uuidv4 } = require('uuid');
const multer = require('multer');
const archiver = require('archiver');
const crypto = require('crypto');
const geoip = require('geoip-lite');
const discordAlerts = require('./discord-alerts.js');

const COOKIES_FILE = path.join(__dirname, 'cookies.txt');
const BOT_DETECTION_ERRORS = [
  'Sign in to confirm you',
  'confirm your age',
  'Sign in to confirm your age',
  'This video is unavailable',
  'Private video'
];

function hasCookiesFile() {
  return fs.existsSync(COOKIES_FILE);
}

function needsCookiesRetry(errorOutput) {
  return BOT_DETECTION_ERRORS.some(err => errorOutput.includes(err));
}

function getCookiesArgs() {
  if (hasCookiesFile()) {
    return ['--cookies', COOKIES_FILE];
  }
  return [];
}

let adminConfig = { ADMIN_PASSWORD: null, ADMIN_TOKEN_SECRET: 'default-secret' };
try {
  adminConfig = require('./admin-config.js');
} catch (e) {
  console.log('[Admin] No admin-config.js found, admin features disabled');
}

const ANALYTICS_FILE = path.join(__dirname, 'analytics.json');
const adminTokens = new Map();

function getDefaultAnalytics() {
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
    lastUpdated: Date.now(),
    userData: {}
  };
}

const seenCountryUsers = new Map();

setInterval(() => {
  const today = new Date().toISOString().split('T')[0];
  for (const key of seenCountryUsers.keys()) {
    if (!key.startsWith(today)) {
      seenCountryUsers.delete(key);
    }
  }
}, 60 * 60 * 1000);

function loadAnalytics() {
  try {
    if (fs.existsSync(ANALYTICS_FILE)) {
      const loaded = JSON.parse(fs.readFileSync(ANALYTICS_FILE, 'utf8'));
      if (!loaded.userData) {
        loaded.userData = {};
      }
      return loaded;
    }
  } catch (e) {
    console.error('[Analytics] Failed to load:', e.message);
  }
  return getDefaultAnalytics();
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

function trackDownload(format, site, country, trackingId) {
  analytics.totalDownloads++;
  analytics.formats[format] = (analytics.formats[format] || 0) + 1;
  if (site) {
    analytics.sites[site] = (analytics.sites[site] || 0) + 1;
  }
  if (trackingId) {
    if (!analytics.userData[trackingId]) {
      analytics.userData[trackingId] = { downloads: 0, converts: 0, compresses: 0, countries: {}, formats: {}, sites: {} };
    }
    analytics.userData[trackingId].downloads++;
    analytics.userData[trackingId].formats[format] = (analytics.userData[trackingId].formats[format] || 0) + 1;
    if (site) analytics.userData[trackingId].sites[site] = (analytics.userData[trackingId].sites[site] || 0) + 1;
    if (country) analytics.userData[trackingId].countries[country] = (analytics.userData[trackingId].countries[country] || 0) + 1;
  }
  saveAnalytics(analytics);
}

function trackConvert(fromFormat, toFormat, trackingId) {
  analytics.totalConverts++;
  const key = `${fromFormat}->${toFormat}`;
  analytics.formats[key] = (analytics.formats[key] || 0) + 1;
  if (trackingId) {
    if (!analytics.userData[trackingId]) {
      analytics.userData[trackingId] = { downloads: 0, converts: 0, compresses: 0, countries: {}, formats: {}, sites: {} };
    }
    analytics.userData[trackingId].converts++;
    analytics.userData[trackingId].formats[key] = (analytics.userData[trackingId].formats[key] || 0) + 1;
  }
  saveAnalytics(analytics);
}

function trackCompress(trackingId) {
  analytics.totalCompresses++;
  if (trackingId) {
    if (!analytics.userData[trackingId]) {
      analytics.userData[trackingId] = { downloads: 0, converts: 0, compresses: 0, countries: {}, formats: {}, sites: {} };
    }
    analytics.userData[trackingId].compresses++;
  }
  saveAnalytics(analytics);
}

function trackPageView(page, country, trackingId) {
  const today = new Date().toISOString().split('T')[0];
  if (!analytics.pageViews[today]) {
    analytics.pageViews[today] = {};
  }
  analytics.pageViews[today][page] = (analytics.pageViews[today][page] || 0) + 1;
  if (trackingId) {
    if (!analytics.userData[trackingId]) {
      analytics.userData[trackingId] = { downloads: 0, converts: 0, compresses: 0, countries: {}, formats: {}, sites: {}, pageViews: 0 };
    }
    analytics.userData[trackingId].pageViews = (analytics.userData[trackingId].pageViews || 0) + 1;
    if (country) analytics.userData[trackingId].countries[country] = (analytics.userData[trackingId].countries[country] || 0) + 1;
  }
  saveAnalytics(analytics);
}

function trackDailyUser(clientId, country, trackingId) {
  const today = new Date().toISOString().split('T')[0];
  if (!analytics.dailyUsers[today]) {
    analytics.dailyUsers[today] = new Set();
  }
  if (typeof analytics.dailyUsers[today] === 'object' && !(analytics.dailyUsers[today] instanceof Set)) {
    analytics.dailyUsers[today] = new Set(analytics.dailyUsers[today]);
  }
  analytics.dailyUsers[today].add(clientId);

  if (country && clientId) {
    const countryKey = `${today}:${clientId}`;
    if (!seenCountryUsers.has(countryKey)) {
      seenCountryUsers.set(countryKey, true);
      analytics.countries[country] = (analytics.countries[country] || 0) + 1;
    }
  }

  if (trackingId) {
    if (!analytics.userData[trackingId]) {
      analytics.userData[trackingId] = { downloads: 0, converts: 0, compresses: 0, countries: {}, formats: {}, sites: {}, pageViews: 0 };
    }
  }
  saveAnalytics(analytics);
}

function deleteUserData(trackingId) {
  if (!trackingId || !analytics.userData[trackingId]) {
    return { deleted: false, reason: 'No data found for this tracking ID' };
  }

  const userData = analytics.userData[trackingId];

  analytics.totalDownloads = Math.max(0, analytics.totalDownloads - (userData.downloads || 0));
  analytics.totalConverts = Math.max(0, analytics.totalConverts - (userData.converts || 0));
  analytics.totalCompresses = Math.max(0, analytics.totalCompresses - (userData.compresses || 0));

  for (const [format, count] of Object.entries(userData.formats || {})) {
    if (analytics.formats[format]) {
      analytics.formats[format] = Math.max(0, analytics.formats[format] - count);
      if (analytics.formats[format] === 0) delete analytics.formats[format];
    }
  }

  for (const [site, count] of Object.entries(userData.sites || {})) {
    if (analytics.sites[site]) {
      analytics.sites[site] = Math.max(0, analytics.sites[site] - count);
      if (analytics.sites[site] === 0) delete analytics.sites[site];
    }
  }

  for (const [country, count] of Object.entries(userData.countries || {})) {
    if (analytics.countries[country]) {
      analytics.countries[country] = Math.max(0, analytics.countries[country] - count);
      if (analytics.countries[country] === 0) delete analytics.countries[country];
    }
  }

  delete analytics.userData[trackingId];

  saveAnalytics(analytics);
  console.log(`[Analytics] Deleted all data for tracking ID: ${trackingId.slice(0, 8)}...`);
  return { deleted: true };
}

function normalizeIp(rawIp) {
  if (!rawIp || typeof rawIp !== 'string') return null;
  let ip = rawIp.split(',')[0].trim();
  if (!ip) return null;
  if (ip.startsWith('::ffff:')) {
    ip = ip.replace('::ffff:', '');
  }
  if (ip.startsWith('[') && ip.includes(']')) {
    ip = ip.slice(1, ip.indexOf(']'));
  }
  if (net.isIP(ip) === 0 && ip.includes(':') && ip.split(':').length === 2) {
    ip = ip.split(':')[0];
  }
  return net.isIP(ip) ? ip : null;
}

function isPrivateIp(ip) {
  if (net.isIPv4(ip)) {
    const parts = ip.split('.').map(Number);
    const [a, b] = parts;
    if (a === 10 || a === 127 || a === 0) return true;
    if (a === 169 && b === 254) return true;
    if (a === 192 && b === 168) return true;
    if (a === 172 && b >= 16 && b <= 31) return true;
    if (a === 100 && b >= 64 && b <= 127) return true;
    return false;
  }
  if (net.isIPv6(ip)) {
    const lower = ip.toLowerCase();
    if (lower === '::1') return true;
    if (lower.startsWith('fe80:')) return true;
    if (lower.startsWith('fc') || lower.startsWith('fd')) return true;
  }
  return false;
}

function getCountryFromIP(ip) {
  const normalized = normalizeIp(ip);
  if (!normalized || isPrivateIp(normalized)) return null;
  try {
    const result = geoip.lookup(normalized);
    return result?.country || null;
  } catch (e) {
    return null;
  }
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
app.set('trust proxy', true);
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
const HEARTBEAT_TIMEOUT_MS = 30 * 1000;

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

function validateVideoFile(filePath) {
  try {
    const result = spawnSync('ffprobe', [
      '-v', 'error',
      '-select_streams', 'v',
      '-show_entries', 'stream=codec_type',
      '-of', 'csv=p=0',
      filePath
    ], { encoding: 'utf8', timeout: 10000 });
    if (result.status !== 0 || result.error) return false;
    return (result.stdout || '').trim().includes('video');
  } catch {
    return false;
  }
}

function validateTimeParam(value) {
  if (!value) return null;
  const str = String(value).trim();
  if (!str) return null;

  if (/^\d+(\.\d+)?$/.test(str)) {
    const num = parseFloat(str);
    return isFinite(num) && num >= 0 ? str : null;
  }

  const hhmmss = /^(?:(\d{1,2}):)?(\d{1,2}):(\d{1,2}(?:\.\d+)?)$/.exec(str);
  if (hhmmss) {
    const h = hhmmss[1] ? parseInt(hhmmss[1], 10) : 0;
    const m = parseInt(hhmmss[2], 10);
    const s = parseFloat(hhmmss[3]);
    if (m < 60 && s < 60 && isFinite(h) && isFinite(m) && isFinite(s)) {
      return str;
    }
  }
  return null;
}

const ALLOWED_MODES = ['size', 'quality'];
const ALLOWED_QUALITIES = ['high', 'medium', 'low'];
const ALLOWED_PRESETS = ['fast', 'balanced', 'quality'];
const ALLOWED_DENOISE = ['auto', 'none', 'light', 'moderate', 'heavy'];

const COMPRESSION_CONFIG = {
  presets: {
    fast: { ffmpegPreset: 'ultrafast', crf: { high: 26, medium: 28, low: 30 }, denoise: 'none', x264Params: 'aq-mode=1' },
    balanced: { ffmpegPreset: 'medium', crf: { high: 22, medium: 24, low: 26 }, denoise: 'auto', x264Params: 'aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0' },
    quality: { ffmpegPreset: 'slow', crf: { high: 20, medium: 22, low: 24 }, denoise: 'auto', x264Params: 'aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0' }
  },
  denoise: {
    none: null,
    light: 'hqdn3d=2:1.5:3:2.25',
    moderate: 'hqdn3d=4:3:6:4.5',
    heavy: 'hqdn3d=6:4:9:6'
  },
  bitrateThresholds: {
    1080: 2500,
    720: 1500,
    480: 800,
    360: 400
  }
};

function selectResolution(width, height, availableBitrateK) {
  const resolutions = [
    { w: 1920, h: 1080, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[1080] },
    { w: 1280, h: 720, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[720] },
    { w: 854, h: 480, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[480] },
    { w: 640, h: 360, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[360] }
  ];

  for (const res of resolutions) {
    if (width < res.w && height < res.h) continue;
    if (availableBitrateK >= res.minBitrate) {
      return { width: res.w, height: res.h, needsScale: width > res.w };
    }
  }

  for (const res of resolutions) {
    if (availableBitrateK >= res.minBitrate) {
      return { width: res.w, height: res.h, needsScale: width > res.w };
    }
  }

  return { width: 640, height: 360, needsScale: width > 640 };
}

function getDenoiseFilter(denoise, sourceHeight, sourceBitrateMbps, presetDenoise = 'auto') {
  if (denoise === 'none' || presetDenoise === 'none') return null;
  if (denoise !== 'auto') return COMPRESSION_CONFIG.denoise[denoise];
  if (presetDenoise !== 'auto') return COMPRESSION_CONFIG.denoise[presetDenoise];

  const expectedBitrate = { 360: 1, 480: 1.5, 720: 3, 1080: 6, 1440: 12, 2160: 25 };
  const heights = Object.keys(expectedBitrate).map(Number);
  const closest = heights.reduce((a, b) => Math.abs(b - sourceHeight) < Math.abs(a - sourceHeight) ? b : a);

  if (sourceBitrateMbps > expectedBitrate[closest] * 2.5) {
    return COMPRESSION_CONFIG.denoise.heavy;
  } else if (sourceBitrateMbps > expectedBitrate[closest] * 1.5) {
    return COMPRESSION_CONFIG.denoise.moderate;
  }
  return COMPRESSION_CONFIG.denoise.light;
}

function getDownscaleResolution(sourceWidth, sourceHeight) {
  if (sourceWidth > 1920 || sourceHeight > 1080) {
    return 1920;
  } else if (sourceWidth >= 1920 || sourceHeight >= 1080) {
    return 1280;
  } else if (sourceWidth >= 1280 || sourceHeight >= 720) {
    return 854;
  }
  return null;
}

function buildVideoFilters(denoiseFilter, scaleWidth, sourceWidth) {
  const filters = [];
  if (scaleWidth && scaleWidth < sourceWidth) {
    filters.push(`scale=${scaleWidth}:-2:flags=lanczos`);
  }
  if (denoiseFilter) filters.push(denoiseFilter);
  return filters.length > 0 ? filters.join(',') : null;
}

function calculateTargetBitrate(targetMB, durationSec, audioBitrateK = 96) {
  const targetBytes = targetMB * 1024 * 1024 * 0.95;
  const audioBytes = (audioBitrateK * 1000 / 8) * durationSec;
  const videoBytes = targetBytes - audioBytes;
  return Math.floor((videoBytes * 8) / durationSec / 1000);
}

function formatETA(seconds) {
  if (!seconds || seconds < 0) return null;
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
}

const activeJobsByType = {
  download: 0,
  playlist: 0,
  convert: 0,
  compress: 0
};

const jobQueue = [];
const HEAVY_JOB_TYPES = ['playlist', 'convert', 'compress'];
const SESSION_IDLE_TIMEOUT_MS = 60 * 1000;

const clientSessions = new Map();
const jobToClient = new Map();

function registerClient(clientId) {
  if (!clientSessions.has(clientId)) {
    clientSessions.set(clientId, {
      lastHeartbeat: Date.now(),
      lastActivity: Date.now(),
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
  } else {
    clientSessions.get(clientId).lastActivity = Date.now();
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
    session.lastActivity = Date.now();
    jobToClient.set(jobId, clientId);
  }
}

function unlinkJobFromClient(jobId) {
  const clientId = jobToClient.get(jobId);
  if (clientId) {
    const session = clientSessions.get(clientId);
    if (session) {
      session.activeJobs.delete(jobId);
      session.lastActivity = Date.now();
    }
    jobToClient.delete(jobId);
  }
}

setInterval(() => {
  const now = Date.now();
  for (const [clientId, session] of clientSessions.entries()) {
    const hasActiveJobs = session.activeJobs.size > 0;

    if (hasActiveJobs && now - session.lastHeartbeat > HEARTBEAT_TIMEOUT_MS) {
      console.log(`[Session] Client ${clientId.slice(0, 8)}... heartbeat timeout, cancelling ${session.activeJobs.size} jobs`);

      for (const jobId of session.activeJobs) {
        const processInfo = activeProcesses.get(jobId);
        if (processInfo) {
          processInfo.cancelled = true;
          if (processInfo.process) {
            processInfo.process.kill('SIGTERM');
          }
          sendProgress(jobId, 'cancelled', 'Connection lost - task cancelled');
          activeProcesses.delete(jobId);
        }
        cleanupJobFiles(jobId);
      }

      clientSessions.delete(clientId);
    } else if (!hasActiveJobs && now - session.lastActivity > SESSION_IDLE_TIMEOUT_MS) {
      console.log(`[Session] Client ${clientId.slice(0, 8)}... idle timeout`);
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

let corsOrigins = null;
try {
  corsOrigins = require('./cors-origins.js');
  console.log(`✓ Loaded ${corsOrigins.length} CORS origins from cors-origins.js`);
} catch (e) {
  console.log('[CORS] No cors-origins.js found, allowing all origins (credentials disabled)');
}

const corsConfig = corsOrigins && corsOrigins.length > 0
  ? { origin: corsOrigins, credentials: true }
  : { origin: true, credentials: false };

app.use(cors(corsConfig));
app.use(cookieParser());

app.use(express.json({ limit: '500mb' }));

app.use(express.static(path.join(__dirname, '../public')));

app.use('/api/download', rateLimitMiddleware);
app.use('/api/download-playlist', rateLimitMiddleware);
app.use('/api/convert', rateLimitMiddleware);
app.use('/api/compress', rateLimitMiddleware);

const TEMP_DIR = path.join(os.tmpdir(), 'yoink-downloads');

function clearTempDir() {
  try {
    if (fs.existsSync(TEMP_DIR)) {
      fs.rmSync(TEMP_DIR, { recursive: true });
      console.log('✓ Cleared temp directory');
    }
    fs.mkdirSync(TEMP_DIR, { recursive: true });
  } catch (err) {
    console.error('Failed to clear temp directory:', err);
    fs.mkdirSync(TEMP_DIR, { recursive: true });
  }
}

clearTempDir();

function cleanupTempFiles() {
  try {
    const items = fs.readdirSync(TEMP_DIR);
    const now = Date.now();
    items.forEach(item => {
      const itemPath = path.join(TEMP_DIR, item);
      try {
        const stat = fs.statSync(itemPath);
        if (now - stat.mtimeMs > FILE_RETENTION_MS) {
          if (stat.isDirectory()) {
            fs.rmSync(itemPath, { recursive: true });
          } else {
            fs.unlinkSync(itemPath);
          }
          console.log(`Cleaned up old temp: ${item}`);
        }
      } catch { }
    });
  } catch (err) {
    console.error('Cleanup error:', err);
  }
}

function cleanupJobFiles(jobId) {
  try {
    const items = fs.readdirSync(TEMP_DIR);
    items.forEach(item => {
      if (item.includes(jobId)) {
        const itemPath = path.join(TEMP_DIR, item);
        try {
          const stat = fs.statSync(itemPath);
          if (stat.isDirectory()) {
            fs.rmSync(itemPath, { recursive: true });
          } else {
            fs.unlinkSync(itemPath);
          }
          console.log(`Cleaned up job files: ${item}`);
        } catch (innerErr) {
          console.debug(`[Cleanup] Failed to remove ${item} for job ${jobId}: ${innerErr.message}`);
        }
      }
    });
  } catch (outerErr) {
    console.debug(`[Cleanup] Failed to read ${TEMP_DIR} for job ${jobId}: ${outerErr.message}`);
  }
}

setInterval(cleanupTempFiles, 5 * 60 * 1000);

let galleryDlAvailable = false;

function checkDependencies() {
  try {
    execSync('which yt-dlp', { stdio: 'ignore' });
    console.log('✓ yt-dlp found');
  } catch {
    console.error('✗ yt-dlp not found. Please install: pip install yt-dlp');
    process.exit(1);
  }

  try {
    execSync('which ffmpeg', { stdio: 'ignore' });
    console.log('✓ ffmpeg found');
  } catch {
    console.error('✗ ffmpeg not found. Please install: apt install ffmpeg');
    process.exit(1);
  }

  try {
    execSync('which gallery-dl', { stdio: 'ignore' });
    galleryDlAvailable = true;
    console.log('✓ gallery-dl found');
  } catch {
    console.log('⚠ gallery-dl not found - image downloads disabled. Install: pip install gallery-dl');
  }

  if (hasCookiesFile()) {
    console.log('✓ cookies.txt found - YouTube authentication enabled');
  } else {
    console.log('⚠ cookies.txt not found - YouTube may block some requests');
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

app.get('/api/metadata', async (req, res) => {
  const { url, playlist } = req.query;
  const downloadPlaylist = playlist === 'true';

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');
  
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

    activeProcesses.delete(id);
    sendProgress(id, 'cancelled', 'Download cancelled');

    setTimeout(() => cleanupJobFiles(id), 1000);

    res.json({ success: true, message: 'Download cancelled' });
  } else {
    res.json({ success: false, message: 'Download not found or already completed' });
  }
});

app.post('/api/finish-early/:id', (req, res) => {
  const { id } = req.params;

  const processInfo = activeProcesses.get(id);
  if (processInfo) {
    console.log(`[${id}] Finishing playlist early...`);
    processInfo.finishEarly = true;

    if (processInfo.process) {
      try {
        processInfo.process.kill('SIGTERM');
      } catch (e) {
        console.error(`[${id}] Error stopping current download:`, e);
      }
    }

    sendProgress(id, 'finishing-early', 'Finishing early, packaging downloaded videos...');
    res.json({ success: true, message: 'Finishing early' });
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
    clientId,
    twitterGifs = 'true'
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
  const tempFile = path.join(TEMP_DIR, `${downloadId}.%(ext)s`);
  const finalFile = path.join(TEMP_DIR, `${downloadId}-final.${outputExt}`);

  const processInfo = { cancelled: false, process: null, tempFile: finalFile };
  activeProcesses.set(downloadId, processInfo);

  activeJobsByType.download++;
  console.log(`[Queue] Download started. Active: ${JSON.stringify(activeJobsByType)}`);

  sendProgress(downloadId, 'starting', 'Initializing download...');

  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');

  try {
    sendProgress(downloadId, 'downloading', 'Downloading from source...', 0);

    let downloadedPath = null;
    let downloadedExt = null;

    if (isYouTube && !downloadPlaylist) {
      console.log(`[${downloadId}] YouTube detected, using Cobalt...`);
      sendProgress(downloadId, 'downloading', 'Downloading via Cobalt...', 10);
      
      try {
        const cobaltResult = await downloadViaCobalt(url, downloadId, isAudio);
        downloadedPath = cobaltResult.filePath;
        downloadedExt = cobaltResult.ext;
        sendProgress(downloadId, 'downloading', 'Download complete', 100);
      } catch (cobaltErr) {
        console.error(`[${downloadId}] Cobalt failed:`, cobaltErr.message);
        throw new Error('YouTube download failed - try again later');
      }
    } else {
      const ytdlpArgs = [
        ...getCookiesArgs(),
        '-t', 'sleep',
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
          }
        }
      });

      ytdlp.stderr.on('data', (data) => {
        const msg = data.toString();
        stderrOutput += msg;
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
        else {
          const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
          const errorMessage = errorMatch ? errorMatch[1].trim() : 'Download failed';
          reject(new Error(errorMessage));
        }
      });

      ytdlp.on('error', reject);

      req.on('close', () => {
        ytdlp.kill('SIGTERM');
      });
    });

    const files = fs.readdirSync(TEMP_DIR);
    const downloadedFile = files.find(f => f.startsWith(downloadId) && !f.includes('-final') && !f.includes('-cobalt'));

    if (!downloadedFile) {
      throw new Error('Downloaded file not found');
    }

    downloadedPath = path.join(TEMP_DIR, downloadedFile);
    downloadedExt = path.extname(downloadedFile).slice(1);
    }

    if (!downloadedPath || !fs.existsSync(downloadedPath)) {
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

    try { fs.unlinkSync(downloadedPath); } catch { }

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
        trackDownload(actualOutputExt, site, getCountryFromIP(getClientIp(req)));
      } catch (e) { }

      console.log(`[Queue] Download finished. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    });

    stream.on('error', (err) => {
      if (finished) return;
      finished = true;
      console.error(`[${downloadId}] Stream error:`, err);
      discordAlerts.fileSendFailed('File Stream Error', 'Failed to send file to client.', { jobId: downloadId, url, error: err.message });
      sendProgress(downloadId, 'error', 'Failed to send file');
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Download failed. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
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
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
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
        try { fs.unlinkSync(actualFinalFile); } catch { }
      }, 1000);
    });

  } catch (err) {
    console.error(`[${downloadId}] Error:`, err.message);
    discordAlerts.downloadFailed('Download Error', 'Video download failed.', { jobId: downloadId, url, format: outputExt, error: err.message });

    sendProgress(downloadId, 'error', err.message || 'Download failed');

    activeProcesses.delete(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Download error. Active: ${JSON.stringify(activeJobsByType)}`);

    const files = fs.readdirSync(TEMP_DIR);
    files.filter(f => f.startsWith(downloadId)).forEach(f => {
      try { fs.unlinkSync(path.join(TEMP_DIR, f)); } catch { }
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

        ytdlpArgs.push(videoUrl || `https://www.youtube.com/watch?v=${entry.id}`);

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
                videoProgress,
                format: isAudio ? audioFormat : `${quality} ${container}`
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
                  videoProgress,
                  format: isAudio ? audioFormat : `${quality} ${container}`
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

          try { fs.unlinkSync(tempPath); } catch { }

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

      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    });

    stream.on('error', (err) => {
      console.error(`[${downloadId}] Stream error:`, err);
      discordAlerts.fileSendFailed('Playlist Stream Error', 'Failed to send zip file to client.', { jobId: downloadId, url, error: err.message });
      sendProgress(downloadId, 'error', 'Failed to send zip file');
      activeProcesses.delete(downloadId);
      activeJobsByType.playlist--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Playlist failed. Active: ${JSON.stringify(activeJobsByType)}`);
      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    });

  } catch (err) {
    console.error(`[${downloadId}] Playlist error:`, err.message);
    discordAlerts.downloadFailed('Playlist Download Error', 'Playlist download failed.', { jobId: downloadId, url, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Playlist download failed');
    }

    activeProcesses.delete(downloadId);
    activeJobsByType.playlist--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Playlist error. Active: ${JSON.stringify(activeJobsByType)}`);

    setTimeout(() => cleanupJobFiles(downloadId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Playlist download failed' });
    }
  }
});


app.use('/api/gallery', rateLimitMiddleware);

app.get('/api/gallery/status', (req, res) => {
  res.json({ available: galleryDlAvailable });
});

app.get('/api/gallery/metadata', async (req, res) => {
  const { url } = req.query;

  if (!galleryDlAvailable) {
    return res.status(503).json({ error: 'gallery-dl not installed on server' });
  }

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  try {
    const { stdout, stderr, exitCode } = await new Promise((resolve, reject) => {
      const proc = spawn('gallery-dl', ['--dump-json', '--range', '1-10', url]);
      let stdoutData = '';
      let stderrData = '';

      const timeout = setTimeout(() => {
        proc.kill('SIGTERM');
        reject(new Error('gallery-dl metadata timeout (30s)'));
      }, 30000);

      proc.stdout.on('data', (data) => {
        stdoutData += data.toString();
        if (stdoutData.length > 10 * 1024 * 1024) {
          proc.kill('SIGTERM');
          reject(new Error('Output too large'));
        }
      });

      proc.stderr.on('data', (data) => {
        stderrData += data.toString();
      });

      proc.on('close', (code) => {
        clearTimeout(timeout);
        resolve({ stdout: stdoutData, stderr: stderrData, exitCode: code });
      });

      proc.on('error', (err) => {
        clearTimeout(timeout);
        reject(err);
      });
    });

    if (exitCode !== 0 && !stdout.trim()) {
      console.error('[gallery-dl] metadata error:', stderr);
      return res.status(500).json({
        error: 'Could not fetch gallery info',
        details: stderr.substring(0, 200)
      });
    }

    const lines = stdout.trim().split('\n').filter(l => l.trim());
    let imageCount = 0;
    let title = 'Gallery';
    let images = [];

    for (const line of lines) {
      try {
        const item = JSON.parse(line);
        imageCount++;
        if (item.filename) {
          images.push({
            filename: item.filename,
            extension: item.extension || 'jpg',
            url: item.url
          });
        }
        if (!title || title === 'Gallery') {
          title = item.subcategory || item.category || item.gallery || 'Gallery';
        }
      } catch { }
    }

    const hostname = new URL(url).hostname.replace('www.', '');

    res.json({
      title,
      imageCount: imageCount || images.length,
      images: images.slice(0, 10),
      site: hostname,
      isGallery: true
    });
  } catch (err) {
    console.error('[gallery-dl] metadata error:', err);
    res.status(500).json({ error: 'Failed to get gallery info' });
  }
});

app.get('/api/gallery/download', async (req, res) => {
  const {
    url,
    progressId,
    clientId,
    filename
  } = req.query;

  if (!galleryDlAvailable) {
    return res.status(503).json({ error: 'gallery-dl not installed on server' });
  }

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
  const galleryDir = path.join(TEMP_DIR, `gallery-${downloadId}`);

  if (!fs.existsSync(galleryDir)) {
    fs.mkdirSync(galleryDir, { recursive: true });
  }

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(downloadId, clientId);
  }

  const processInfo = { cancelled: false, process: null, tempDir: galleryDir };
  activeProcesses.set(downloadId, processInfo);

  activeJobsByType.download++;
  console.log(`[Queue] Gallery download started. Active: ${JSON.stringify(activeJobsByType)}`);

  sendProgress(downloadId, 'starting', 'Starting gallery download...');

  try {
    const galleryArgs = [
      '-d', galleryDir,
      '--filename', '{num:03d}_{filename}.{extension}',
      '--write-metadata',
      url
    ];

    if (hasCookiesFile()) {
      galleryArgs.unshift('--cookies', COOKIES_FILE);
    }

    console.log(`[${downloadId}] gallery-dl command: gallery-dl ${galleryArgs.join(' ')}`);

    await new Promise((resolve, reject) => {
      const galleryDl = spawn('gallery-dl', galleryArgs);
      processInfo.process = galleryDl;

      let downloadedCount = 0;
      let lastUpdate = Date.now();

      galleryDl.stdout.on('data', (data) => {
        const msg = data.toString();
        if (msg.includes('/') || msg.includes('.jpg') || msg.includes('.png') || msg.includes('.gif') || msg.includes('.webp')) {
          downloadedCount++;
          const now = Date.now();
          if (now - lastUpdate > 500) {
            lastUpdate = now;
            sendProgress(downloadId, 'downloading', `Downloaded ${downloadedCount} images...`, null, { downloadedCount });
          }
        }
      });

      let galleryStderr = '';
      galleryDl.stderr.on('data', (data) => {
        const msg = data.toString();
        galleryStderr += msg;
        if (msg.includes('ERROR')) {
          console.error(`[${downloadId}] gallery-dl error: ${msg.trim()}`);
        }
      });

      galleryDl.on('close', (code) => {
        if (processInfo.cancelled) {
          reject(new Error('Download cancelled'));
        } else if (code === 0) {
          resolve();
        } else {
          console.error(`[${downloadId}] gallery-dl exited with code ${code}: ${galleryStderr.trim()}`);
          reject(new Error(`gallery-dl failed with exit code ${code}: ${galleryStderr.trim().slice(0, 200)}`));
        }
      });

      galleryDl.on('error', reject);

      req.on('close', () => {
        processInfo.cancelled = true;
        galleryDl.kill('SIGTERM');
      });
    });

    const allFiles = [];
    function walkDir(dir) {
      const items = fs.readdirSync(dir);
      for (const item of items) {
        const fullPath = path.join(dir, item);
        const stat = fs.statSync(fullPath);
        if (stat.isDirectory()) {
          walkDir(fullPath);
        } else if (!item.endsWith('.json')) {
          allFiles.push(fullPath);
        }
      }
    }
    walkDir(galleryDir);

    if (allFiles.length === 0) {
      throw new Error('No images were downloaded');
    }

    if (allFiles.length === 1) {
      const singleFile = allFiles[0];
      const ext = path.extname(singleFile).toLowerCase();
      const mimeTypes = {
        '.jpg': 'image/jpeg',
        '.jpeg': 'image/jpeg',
        '.png': 'image/png',
        '.gif': 'image/gif',
        '.webp': 'image/webp',
        '.mp4': 'video/mp4',
        '.webm': 'video/webm'
      };
      const mimeType = mimeTypes[ext] || 'application/octet-stream';
      const stat = fs.statSync(singleFile);
      const safeFilename = sanitizeFilename(filename || path.basename(singleFile, ext)) + ext;

      sendProgress(downloadId, 'sending', 'Sending file...');

      res.setHeader('Content-Type', mimeType);
      res.setHeader('Content-Length', stat.size);
      res.setHeader('Content-Disposition', `attachment; filename="${safeFilename}"; filename*=UTF-8''${encodeURIComponent(safeFilename)}`);

      const stream = fs.createReadStream(singleFile);
      stream.pipe(res);

      stream.on('close', () => {
        sendProgress(downloadId, 'complete', 'Download complete!');
        cleanup();
      });

      stream.on('error', () => {
        sendProgress(downloadId, 'error', 'Failed to send file');
        cleanup();
      });

    } else {
      sendProgress(downloadId, 'zipping', `Creating zip with ${allFiles.length} images...`, 90);

      const zipPath = path.join(TEMP_DIR, `${downloadId}.zip`);
      const hostname = new URL(url).hostname.replace('www.', '');
      const safeZipName = sanitizeFilename(filename || hostname || 'gallery');

      await new Promise((resolve, reject) => {
        const output = fs.createWriteStream(zipPath);
        const archive = archiver('zip', { zlib: { level: 5 } });

        output.on('close', resolve);
        archive.on('error', reject);

        archive.pipe(output);

        allFiles.forEach((filePath) => {
          const baseName = path.basename(filePath);
          archive.file(filePath, { name: baseName });
        });

        archive.finalize();
      });

      sendProgress(downloadId, 'sending', 'Sending zip file...');

      const stat = fs.statSync(zipPath);
      const zipFilename = `${safeZipName}.zip`;

      res.setHeader('Content-Type', 'application/zip');
      res.setHeader('Content-Length', stat.size);
      res.setHeader('Content-Disposition', `attachment; filename="${zipFilename}"; filename*=UTF-8''${encodeURIComponent(zipFilename)}`);

      const stream = fs.createReadStream(zipPath);
      stream.pipe(res);

      stream.on('close', () => {
        sendProgress(downloadId, 'complete', `Downloaded ${allFiles.length} images!`);
        cleanup();
      });

      stream.on('error', () => {
        sendProgress(downloadId, 'error', 'Failed to send zip');
        cleanup();
      });
    }

    function cleanup() {
      activeDownloads.delete(downloadId);
      activeProcesses.delete(downloadId);
      activeJobsByType.download--;
      unlinkJobFromClient(downloadId);
      console.log(`[Queue] Gallery finished. Active: ${JSON.stringify(activeJobsByType)}`);

      try {
        const site = new URL(url).hostname.replace('www.', '');
        trackDownload('images', site, getCountryFromIP(getClientIp(req)));
      } catch { }

      setTimeout(() => cleanupJobFiles(downloadId), 2000);
    }

  } catch (err) {
    console.error(`[${downloadId}] Gallery error:`, err.message);
    discordAlerts.galleryError('Gallery Download Error', 'Gallery download failed.', { jobId: downloadId, url, error: err.message });

    if (!processInfo.cancelled) {
      sendProgress(downloadId, 'error', err.message || 'Gallery download failed');
    }

    activeProcesses.delete(downloadId);
    activeJobsByType.download--;
    unlinkJobFromClient(downloadId);
    console.log(`[Queue] Gallery error. Active: ${JSON.stringify(activeJobsByType)}`);

    setTimeout(() => cleanupJobFiles(downloadId), 2000);

    if (!res.headersSent) {
      res.status(500).json({ error: err.message || 'Gallery download failed' });
    }
  }
});


const upload = multer({
  dest: TEMP_DIR,
  limits: { fileSize: FILE_SIZE_LIMIT }
});

app.post('/api/convert', upload.single('file'), (req, res) => handleConvert(req, res));

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

    if (validStartTime) {
      ffmpegArgs.push('-ss', validStartTime);
    }

    if (validEndTime) {
      ffmpegArgs.push('-to', validEndTime);
    }

    ffmpegArgs.push('-i', inputPath);

    ffmpegArgs.push('-threads', '0');

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
      console.log(`[${convertId}] Conversion complete: ${outputFilename}`);
      activeJobsByType.convert--;
      unlinkJobFromClient(convertId);

      const fromExt = path.extname(req.file.originalname).replace('.', '') || 'unknown';
      trackConvert(fromExt, format);

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
  const outputPath = path.join(TEMP_DIR, `${convertId}-converted.${format}`);

  if (clientId) {
    registerClient(clientId);
    linkJobToClient(convertId, clientId);
  }

  activeJobsByType.convert++;
  console.log(`[Queue] Async convert started. Active: ${JSON.stringify(activeJobsByType)}`);
  console.log(`[${convertId}] Converting ${req.file.originalname} to ${format} (async)`);

  if (startTime && !validateTimeParam(startTime)) {
    try { fs.unlinkSync(inputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    job.status = 'error';
    job.error = 'Invalid startTime format';
    job.progress = 0;
    console.log(`[${convertId}] Invalid startTime, aborting`);
    return;
  }
  if (endTime && !validateTimeParam(endTime)) {
    try { fs.unlinkSync(inputPath); } catch { }
    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);
    job.status = 'error';
    job.error = 'Invalid endTime format';
    job.progress = 0;
    console.log(`[${convertId}] Invalid endTime, aborting`);
    return;
  }

  try {
    const isAudioFormat = ['mp3', 'm4a', 'opus', 'wav', 'flac'].includes(format);

    const validStartTime = validateTimeParam(startTime);
    const validEndTime = validateTimeParam(endTime);

    job.message = 'Analyzing file...';
    job.progress = 5;

    const ffmpegArgs = ['-y'];

    if (validStartTime) {
      ffmpegArgs.push('-ss', validStartTime);
    }
    if (validEndTime) {
      ffmpegArgs.push('-to', validEndTime);
    }

    ffmpegArgs.push('-i', inputPath);
    ffmpegArgs.push('-threads', '0');

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

    console.log(`[${convertId}] Async conversion complete: ${outputFilename}`);

    job.status = 'complete';
    job.progress = 100;
    job.message = 'Conversion complete!';
    job.outputPath = outputPath;
    job.outputFilename = outputFilename;
    job.mimeType = mimeType;

    activeJobsByType.convert--;
    unlinkJobFromClient(convertId);

    const fromExt = path.extname(req.file.originalname).replace('.', '') || 'unknown';
    trackConvert(fromExt, format);

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

const chunkedUploads = new Map();
const CHUNK_SIZE = 50 * 1024 * 1024;
const CHUNK_TIMEOUT = 30 * 60 * 1000;

setInterval(() => {
  const now = Date.now();
  for (const [uploadId, data] of chunkedUploads.entries()) {
    if (now - data.lastActivity > CHUNK_TIMEOUT) {
      console.log(`[Chunk] Upload ${uploadId} timed out, cleaning up`);
      try {
        const files = fs.readdirSync(TEMP_DIR).filter(f => f.startsWith(`chunk-${uploadId}-`));
        for (const f of files) {
          fs.unlinkSync(path.join(TEMP_DIR, f));
        }
      } catch { }
      chunkedUploads.delete(uploadId);
    }
  }
}, 60000);

app.post('/api/upload/init', express.json(), (req, res) => {
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

  console.log(`[Chunk] Initialized upload ${uploadId}: ${fileName} (${(fileSize / 1024 / 1024).toFixed(1)}MB, ${totalChunks} chunks)`);
  res.json({ uploadId });
});

app.post('/api/upload/chunk/:uploadId/:chunkIndex', upload.single('chunk'), (req, res) => {
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

  const chunkPath = path.join(TEMP_DIR, `chunk-${uploadId}-${String(index).padStart(4, '0')}`);

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

  res.json({
    received,
    total,
    complete: received === total
  });
});

app.post('/api/upload/complete/:uploadId', express.json(), async (req, res) => {
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

  const assembledPath = path.join(TEMP_DIR, `assembled-${uploadId}-${sanitizeFilename(uploadData.fileName)}`);

  try {
    const writeStream = fs.createWriteStream(assembledPath);

    for (let i = 0; i < uploadData.totalChunks; i++) {
      const chunkPath = path.join(TEMP_DIR, `chunk-${uploadId}-${String(i).padStart(4, '0')}`);

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
    res.json({
      success: true,
      filePath: assembledPath,
      fileName: uploadData.fileName
    });

  } catch (err) {
    console.error(`[Chunk] Assembly failed for ${uploadId}:`, err);
    chunkedUploads.delete(uploadId);
    res.status(500).json({ error: 'Failed to assemble file' });
  }
});

const asyncJobs = new Map();
const ASYNC_JOB_TIMEOUT = 60 * 60 * 1000;

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

app.get('/api/job/:jobId/status', (req, res) => {
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

app.get('/api/job/:jobId/download', (req, res) => {
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

const botDownloads = new Map();
const BOT_DOWNLOAD_EXPIRY = 5 * 60 * 1000;
const BOT_SECRET = process.env.BOT_SECRET || 'yoinky-bot-secret';
const COBALT_API_KEY = process.env.COBALT_API_KEY;
const COBALT_APIS = [
  'https://cessi-c.meowing.de',
  'https://subito-c.meowing.de',
  'https://nuko-c.meowing.de'
];

async function fetchMetadataViaCobalt(videoUrl) {
  let lastError = null;

  for (const apiUrl of COBALT_APIS) {
    try {
      console.log(`[Metadata] Trying Cobalt: ${apiUrl}`);
      
      const headers = {
        'Accept': 'application/json',
        'Content-Type': 'application/json'
      };
      if (COBALT_API_KEY) {
        headers['Authorization'] = `Api-Key ${COBALT_API_KEY}`;
      }
      
      const response = await fetch(apiUrl, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          url: videoUrl,
          downloadMode: 'auto',
          filenameStyle: 'basic',
          videoQuality: '1080'
        })
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const data = await response.json();
      
      if (data.status === 'error') {
        throw new Error(data.error?.code || 'Cobalt error');
      }

      const filename = data.filename || 'download';
      const title = filename.replace(/\.[^.]+$/, '') || 'download';
      const ext = filename.match(/\.([^.]+)$/)?.[1] || 'mp4';

      console.log(`[Metadata] Cobalt success via ${apiUrl}`);
      return {
        title,
        ext,
        id: '',
        uploader: '',
        duration: '',
        thumbnail: '',
        isPlaylist: false,
        viaCobalt: true
      };

    } catch (err) {
      console.log(`[Metadata] Cobalt ${apiUrl} failed: ${err.message}`);
      lastError = err;
    }
  }

  throw lastError || new Error('All Cobalt instances failed');
}

async function downloadViaCobalt(videoUrl, jobId, isAudio = false) {
  let lastError = null;

  for (const apiUrl of COBALT_APIS) {
    try {
      console.log(`[Bot] Trying Cobalt: ${apiUrl}`);
      
      const headers = {
        'Accept': 'application/json',
        'Content-Type': 'application/json'
      };
      if (COBALT_API_KEY) {
        headers['Authorization'] = `Api-Key ${COBALT_API_KEY}`;
      }
      
      const response = await fetch(apiUrl, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          url: videoUrl,
          downloadMode: isAudio ? 'audio' : 'auto',
          filenameStyle: 'basic',
          videoQuality: '1080'
        })
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const data = await response.json();
      
      if (data.status === 'error') {
        throw new Error(data.error?.code || 'Cobalt error');
      }

      let downloadUrl = data.url;
      if (data.status === 'tunnel' || data.status === 'redirect') {
        downloadUrl = data.url;
      } else if (data.status === 'picker' && data.picker?.length > 0) {
        downloadUrl = data.picker[0].url;
      }

      if (!downloadUrl) {
        throw new Error('No download URL from Cobalt');
      }

      const ext = isAudio ? 'mp3' : 'mp4';
      const outputPath = path.join(TEMP_DIR, `bot-${jobId}-cobalt.${ext}`);

      const fileResponse = await fetch(downloadUrl);
      if (!fileResponse.ok) {
        throw new Error('Failed to download from Cobalt');
      }

      const fileBuffer = await fileResponse.arrayBuffer();
      fs.writeFileSync(outputPath, Buffer.from(fileBuffer));

      console.log(`[Bot] Cobalt success via ${apiUrl}`);
      return { filePath: outputPath, ext };

    } catch (err) {
      console.log(`[Bot] Cobalt ${apiUrl} failed: ${err.message}`);
      lastError = err;
    }
  }

  throw lastError || new Error('All Cobalt instances failed');
}

setInterval(() => {
  const now = Date.now();
  for (const [token, data] of botDownloads.entries()) {
    if (now - data.createdAt > BOT_DOWNLOAD_EXPIRY) {
      console.log(`[Bot] Download token ${token.slice(0, 8)}... expired`);
      if (data.filePath && fs.existsSync(data.filePath)) {
        fs.unlink(data.filePath, () => {});
      }
      botDownloads.delete(token);
    }
  }
}, 30000);

app.post('/api/bot/download', express.json(), async (req, res) => {
  const authHeader = req.headers.authorization;
  if (!authHeader || authHeader !== `Bearer ${BOT_SECRET}`) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const { url, format = 'video', quality = '1080p', container = 'mp4', audioFormat = 'mp3' } = req.body;

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
  const tempFile = path.join(TEMP_DIR, `bot-${jobId}.%(ext)s`);
  const finalFile = path.join(TEMP_DIR, `bot-${jobId}-final.${outputExt}`);

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

  (async () => {
    try {
      job.status = 'downloading';
      job.message = 'Downloading from source...';

      const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');
      let downloadedPath = null;
      let downloadedExt = null;
      let usedCobalt = false;

      if (isYouTube) {
        console.log(`[Bot] YouTube detected, using Cobalt directly for ${jobId}`);
        job.message = 'Downloading via Cobalt...';
        
        try {
          const cobaltResult = await downloadViaCobalt(url, jobId, isAudio);
          downloadedPath = cobaltResult.filePath;
          downloadedExt = cobaltResult.ext;
          usedCobalt = true;
          job.progress = 100;
        } catch (cobaltErr) {
          console.error(`[Bot] Cobalt failed for YouTube:`, cobaltErr.message);
          throw new Error('YouTube download failed via Cobalt');
        }
      } else {
        const ytdlpArgs = [
          ...getCookiesArgs(),
          '--no-playlist',
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
            if (code === 0) {
              resolve();
            } else {
              reject(new Error('Download failed'));
            }
          });
          ytdlp.on('error', reject);
        });

        const files = fs.readdirSync(TEMP_DIR);
        const downloadedFile = files.find(f => f.startsWith(`bot-${jobId}`) && !f.includes('-final') && !f.includes('-cobalt'));

        if (!downloadedFile) {
          throw new Error('Downloaded file not found');
        }

        downloadedPath = path.join(TEMP_DIR, downloadedFile);
        downloadedExt = path.extname(downloadedFile).slice(1);
      }

      if (!downloadedPath || !fs.existsSync(downloadedPath)) {
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
        try { fs.unlinkSync(downloadedPath); } catch {}
      } else {
        actualFinalFile = downloadedPath;
        actualOutputExt = downloadedExt;
      }

      const stat = fs.statSync(actualFinalFile);
      const downloadToken = crypto.randomBytes(32).toString('hex');

      let title = 'download';
      try {
        const infoResult = spawnSync('yt-dlp', ['--print', 'title', '--no-playlist', url], { timeout: 10000 });
        if (infoResult.status === 0) {
          title = infoResult.stdout.toString().trim().slice(0, 100);
        }
      } catch {}

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
      job.message = err.message || 'Download failed';

      const files = fs.readdirSync(TEMP_DIR);
      files.filter(f => f.includes(jobId)).forEach(f => {
        try { fs.unlinkSync(path.join(TEMP_DIR, f)); } catch {}
      });
    }
  })();
});

app.get('/api/bot/download/:token', (req, res) => {
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
        fs.unlink(data.filePath, () => {});
        botDownloads.delete(token);
        console.log(`[Bot] Token ${token.slice(0, 8)}... cleaned up after download`);
      }
    }, 30000);
  });
});

app.get('/api/bot/status/:jobId', (req, res) => {
  const authHeader = req.headers.authorization;
  if (!authHeader || authHeader !== `Bearer ${BOT_SECRET}`) {
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
    fileName: job.fileName,
    fileSize: job.fileSize,
    downloadToken: job.downloadToken
  });
});

function validateChunkedFilePath(filePath) {
  if (!filePath) return null;
  const resolved = path.resolve(filePath);
  const relative = path.relative(TEMP_DIR, resolved);
  if (relative.startsWith('..') || path.isAbsolute(relative)) {
    return null;
  }
  return resolved;
}

app.post('/api/compress-chunked', express.json(), async (req, res) => {
  const {
    filePath,
    fileName,
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

  const validPath = validateChunkedFilePath(filePath);
  if (!validPath) {
    return res.status(400).json({ error: 'Invalid file path' });
  }

  if (!fs.existsSync(validPath)) {
    return res.status(400).json({ error: 'File not found. Complete chunked upload first.' });
  }

  if (!ALLOWED_MODES.includes(mode)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid mode. Allowed: ${ALLOWED_MODES.join(', ')}` });
  }
  if (!ALLOWED_QUALITIES.includes(quality)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid quality. Allowed: ${ALLOWED_QUALITIES.join(', ')}` });
  }
  if (!ALLOWED_PRESETS.includes(preset)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid preset. Allowed: ${ALLOWED_PRESETS.join(', ')}` });
  }
  if (!ALLOWED_DENOISE.includes(denoise)) {
    fs.unlink(validPath, () => { });
    return res.status(400).json({ error: `Invalid denoise. Allowed: ${ALLOWED_DENOISE.join(', ')}` });
  }

  req.file = { path: validPath, originalname: fileName || 'video.mp4' };
  req.body.targetSize = targetSize;
  req.body.duration = duration;
  req.body.progressId = progressId;
  req.body.clientId = clientId;
  req.body.mode = mode;
  req.body.quality = quality;
  req.body.preset = preset;
  req.body.denoise = denoise;
  req.body.downscale = downscale;

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
    } else {
      console.error(`[AsyncJob] Job entry not found for ${jobId}, original error:`, err.message || err);
    }
  });
});

app.post('/api/compress', upload.single('file'), (req, res) => handleCompress(req, res));

const ALLOWED_FORMATS = ['mp4', 'webm', 'mkv', 'mov', 'mp3', 'm4a', 'opus', 'wav', 'flac'];
const ALLOWED_REENCODES = ['auto', 'always', 'never'];

app.post('/api/convert-chunked', express.json(), async (req, res) => {
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
  console.log(`[${compressId}] Compressing ${req.file.originalname} | Mode: ${mode} | Preset: ${preset}`);

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
        actualDuration,
        sendProgress
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
          actualDuration,
          sendProgress
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
      trackCompress();
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

async function runCrfEncode({ inputPath, outputPath, crf, ffmpegPreset, vfArg, x264Params, processInfo, compressId, actualDuration, sendProgress }) {
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

async function runTwoPassEncode({ inputPath, outputPath, passLogFile, videoBitrateK, ffmpegPreset, vfArg, x264Params, processInfo, compressId, actualDuration, sendProgress }) {
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
  const outputPath = path.join(TEMP_DIR, `${compressId}-compressed.mp4`);
  const passLogFile = path.join(TEMP_DIR, `${compressId}-pass`);

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


app.post('/api/analytics/track', (req, res) => {
  const { type, page, trackingId } = req.body;
  const country = getCountryFromIP(getClientIp(req));

  if (type === 'pageview' && page) {
    trackPageView(page, country, trackingId);
  }
  if (type === 'dailyUser') {
    trackDailyUser(getClientIp(req), country, trackingId);
  }

  res.json({ ok: true });
});

app.post('/api/analytics/delete', (req, res) => {
  const { trackingId } = req.body;

  if (!trackingId) {
    return res.status(400).json({ error: 'trackingId is required' });
  }

  const result = deleteUserData(trackingId);
  res.json(result);
});

app.post('/api/admin/login', (req, res) => {
  const { password } = req.body;

  if (!adminConfig.ADMIN_PASSWORD) {
    return res.status(503).json({ error: 'Admin features not configured' });
  }

  if (password === adminConfig.ADMIN_PASSWORD) {
    const token = generateAdminToken();
    const isSecure = req.secure || req.headers['x-forwarded-proto'] === 'https';
    res.cookie('yoink_admin_token', token, {
      httpOnly: true,
      secure: isSecure,
      sameSite: 'strict',
      maxAge: 7 * 24 * 60 * 60 * 1000,
      path: '/'
    });
    res.json({ success: true });
  } else {
    res.status(401).json({ error: 'Invalid password' });
  }
});

app.post('/api/admin/logout', (req, res) => {
  const isSecure = req.secure || req.headers['x-forwarded-proto'] === 'https';
  res.cookie('yoink_admin_token', '', {
    httpOnly: true,
    secure: isSecure,
    sameSite: 'strict',
    expires: new Date(0),
    path: '/'
  });
  res.json({ success: true });
});

app.get('/api/admin/analytics', (req, res) => {
  const token = req.cookies?.yoink_admin_token || req.headers['authorization']?.replace('Bearer ', '');

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

const PAGE_ROUTES = {
  '/convert': '/pages/convert.html',
  '/compress': '/pages/compress.html',
  '/settings': '/pages/settings.html',
  '/privacy': '/pages/privacy.html',
  '/share': '/pages/share.html',
  '/admin': '/pages/admin.html'
};

app.get('*', (req, res) => {
  if (!req.path.startsWith('/api/')) {
    if (PAGE_ROUTES[req.path]) {
      return res.sendFile(path.join(__dirname, '../public', PAGE_ROUTES[req.path]));
    }

    const publicDir = path.resolve(__dirname, '../public');
    let decodedPath = '';
    try {
      decodedPath = decodeURIComponent(req.path || '');
    } catch (e) {
      console.debug('[Route] Malformed URI:', req.path);
      return res.sendFile(path.join(publicDir, 'index.html'));
    }

    if (decodedPath.includes('\0') || decodedPath.includes('..')) {
      return res.sendFile(path.join(publicDir, 'index.html'));
    }

    const resolvedPath = path.resolve(publicDir, '.' + decodedPath);
    const relativePath = path.relative(publicDir, resolvedPath);

    if (relativePath.startsWith('..') || path.isAbsolute(relativePath)) {
      return res.sendFile(path.join(publicDir, 'index.html'));
    }

    if (fs.existsSync(resolvedPath) && fs.statSync(resolvedPath).isFile()) {
      return res.sendFile(resolvedPath);
    }
    return res.sendFile(path.join(publicDir, 'index.html'));
  }
  res.status(404).json({ error: 'Not found' });
});

app.get('/api/alert/test', async (req, res) => {
  if (!discordAlerts.isEnabled()) {
    return res.status(503).json({
      success: false,
      error: 'Discord alerts not configured. Edit discord-config.js to enable.'
    });
  }

  const sent = await discordAlerts.test();
  res.json({ success: sent, message: sent ? 'Test alert sent to Discord!' : 'Failed to send alert' });
});

app.post('/api/github-webhook', async (req, res) => {
  try {
    const response = await fetch('http://localhost:3002/webhook/github', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-GitHub-Event': req.headers['x-github-event'] || '',
        'X-GitHub-Delivery': req.headers['x-github-delivery'] || '',
        'X-Hub-Signature-256': req.headers['x-hub-signature-256'] || ''
      },
      body: JSON.stringify(req.body)
    });
    res.status(response.status).json({ forwarded: true });
  } catch (error) {
    console.error('[GitHub Webhook] Forward failed:', error.message);
    res.status(502).json({ error: 'Failed to forward webhook' });
  }
});

app.listen(PORT, '0.0.0.0', () => {
  console.log(`\n🎯 yoink server running at http://localhost:${PORT}`);
  console.log(`   Job limits: downloads=${JOB_LIMITS.download}, playlists=${JOB_LIMITS.playlist}, convert=${JOB_LIMITS.convert}, compress=${JOB_LIMITS.compress}`);
  console.log(`   Max queue size: ${MAX_QUEUE_SIZE}`);
  console.log(`   Max file upload: ${(FILE_SIZE_LIMIT / 1024 / 1024 / 1024).toFixed(0)}GB`);
  console.log(`   File retention: ${FILE_RETENTION_MS / 60000} minutes`);
  console.log(`   Heartbeat timeout: ${HEARTBEAT_TIMEOUT_MS / 1000}s`);
  console.log(`   gallery-dl: ${galleryDlAvailable ? 'enabled' : 'disabled'}`);
  console.log(`\nEndpoints:`);
  console.log(`  GET  /health               - Health check + queue status`);
  console.log(`  POST /api/connect          - Get client ID`);
  console.log(`  POST /api/heartbeat/:id    - Keep session alive`);
  console.log(`  GET  /api/queue-status     - Queue status`);
  console.log(`  GET  /api/metadata         - Get video metadata`);
  console.log(`  GET  /api/download         - Download video/audio`);
  console.log(`  GET  /api/download-playlist - Download playlist as zip`);
  console.log(`  GET  /api/gallery/download - Download images from galleries`);
  console.log(`  POST /api/convert          - Convert uploaded file`);
  console.log(`  POST /api/compress         - Compress video to target size\n`);
});
