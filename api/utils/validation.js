const { spawnSync } = require('child_process');
const { SAFETY_LIMITS } = require('../config/constants');

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

module.exports = {
  validateVideoFile,
  validateTimeParam,
  validateUrl
};
