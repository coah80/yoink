const path = require('path');
const os = require('os');

// Server Configuration
const PORT = process.env.PORT || 3001;

// Job Limits
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

// Safety Limits
const SAFETY_LIMITS = {
  playlistChunkSize: 50,
  maxPlaylistVideos: 1000,
  maxVideoDuration: 4 * 60 * 60,
  maxJobsPerClient: 5,
  rateLimitWindowMs: 60 * 1000,
  rateLimitMaxRequests: 60,
  maxUrlLength: 2048
};

// Quality Settings
const QUALITY_HEIGHT = {
  '2160p': 2160,
  '1440p': 1440,
  '1080p': 1080,
  '720p': 720,
  '480p': 480,
  '360p': 360
};

// File Types
const CONTAINER_MIMES = {
  mp4: 'video/mp4',
  webm: 'video/webm',
  mkv: 'video/x-matroska',
  mov: 'video/quicktime'
};

const AUDIO_MIMES = {
  mp3: 'audio/mpeg',
  m4a: 'audio/mp4',
  opus: 'audio/opus',
  wav: 'audio/wav',
  flac: 'audio/flac'
};

// Temporary Directories
const TEMP_DIR = path.join(os.tmpdir(), 'yoink');
const TEMP_DIRS = {
  download: path.join(TEMP_DIR, 'downloads'),
  convert: path.join(TEMP_DIR, 'converts'),
  compress: path.join(TEMP_DIR, 'compress'),
  bot: path.join(TEMP_DIR, 'bot'),
  gallery: path.join(TEMP_DIR, 'gallery'),
  playlist: path.join(TEMP_DIR, 'playlists'),
  upload: path.join(TEMP_DIR, 'uploads')
};

// Compression Configuration
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

// Bot Configuration
const BOT_SECRET = process.env.BOT_SECRET || 'yoinky-bot-secret';
const BOT_DOWNLOAD_EXPIRY = 5 * 60 * 1000;

// Cobalt Configuration
const COBALT_API_KEY = process.env.COBALT_API_KEY;
const COBALT_APIS = [
  'https://nuko-c.meowing.de',
  'https://subito-c.meowing.de',
  'https://cessi-c.meowing.de'
];

// Heavy Job Types
const HEAVY_JOB_TYPES = ['playlist', 'convert', 'compress'];
const SESSION_IDLE_TIMEOUT_MS = 60 * 1000;

// Upload Configuration
const CHUNK_SIZE = 50 * 1024 * 1024;
const CHUNK_TIMEOUT = 30 * 60 * 1000;

// Async Job Timeout
const ASYNC_JOB_TIMEOUT = 60 * 60 * 1000;

// Allowed Formats
const ALLOWED_FORMATS = ['mp4', 'webm', 'mkv', 'mov', 'mp3', 'm4a', 'opus', 'wav', 'flac'];
const ALLOWED_REENCODES = ['auto', 'always', 'never'];

// Error Messages
const BOT_DETECTION_ERRORS = [
  'Sign in to confirm you',
  'confirm your age',
  'Sign in to confirm your age',
  'This video is unavailable',
  'Private video'
];

module.exports = {
  PORT,
  JOB_LIMITS,
  MAX_QUEUE_SIZE,
  FILE_SIZE_LIMIT,
  FILE_RETENTION_MS,
  HEARTBEAT_TIMEOUT_MS,
  SAFETY_LIMITS,
  QUALITY_HEIGHT,
  CONTAINER_MIMES,
  AUDIO_MIMES,
  TEMP_DIR,
  TEMP_DIRS,
  ALLOWED_MODES,
  ALLOWED_QUALITIES,
  ALLOWED_PRESETS,
  ALLOWED_DENOISE,
  COMPRESSION_CONFIG,
  BOT_SECRET,
  BOT_DOWNLOAD_EXPIRY,
  COBALT_API_KEY,
  COBALT_APIS,
  HEAVY_JOB_TYPES,
  SESSION_IDLE_TIMEOUT_MS,
  CHUNK_SIZE,
  CHUNK_TIMEOUT,
  ASYNC_JOB_TIMEOUT,
  ALLOWED_FORMATS,
  ALLOWED_REENCODES,
  BOT_DETECTION_ERRORS
};
