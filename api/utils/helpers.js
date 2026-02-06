const fs = require('fs');
const path = require('path');
const net = require('net');
const geoip = require('geoip-lite');
const { SAFETY_LIMITS } = require('../config/constants');

const COOKIES_FILE = path.join(__dirname, '../youtube-cookies.txt');

function hasCookiesFile() {
  return fs.existsSync(COOKIES_FILE);
}

function getCookiesArgs() {
  const cookiesFile = path.join(__dirname, '../youtube-cookies.txt');
  if (fs.existsSync(cookiesFile)) {
    return ['--cookies', cookiesFile];
  }
  return [];
}

function toUserError(message) {
  const text = String(message || '');
  const msg = text.toLowerCase();

  if (msg.includes('cancelled')) return 'Download cancelled';
  if (msg.includes('content.video.unavailable') || msg.includes('video unavailable') || msg.includes('private video') || msg.includes('this content is private')) return 'Video unavailable or private';
  if (msg.includes('content.video.live') || msg.includes('live stream')) return 'Live streams cannot be downloaded';
  if (msg.includes('content.video.age') || msg.includes('age-restricted')) return 'Age-restricted video (sign-in required)';
  if (msg.includes('rate')) return 'Rate limited - please wait and try again';
  if (msg.includes('econnreset') || msg.includes('fetch failed') || msg.includes('connection')) return 'Connection interrupted - try again';
  if (msg.includes('processing failed') || msg.includes('encoding failed')) return 'Processing failed';
  if (msg.includes('download interrupted')) return 'Download interrupted';
  if (msg.includes('no videos were successfully downloaded')) return 'No videos were successfully downloaded';
  if (msg.includes('downloaded file not found')) return 'Download failed';

  return 'Download failed';
}

function sanitizeFilename(filename) {
  return filename
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200);
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

function getClientIp(req) {
  return req.headers['x-forwarded-for']?.split(',')[0]?.trim() ||
    req.headers['x-real-ip'] ||
    req.socket?.remoteAddress ||
    'unknown';
}

function formatETA(seconds) {
  if (!seconds || seconds < 0) return null;
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
}

module.exports = {
  hasCookiesFile,
  getCookiesArgs,
  toUserError,
  sanitizeFilename,
  normalizeIp,
  isPrivateIp,
  getCountryFromIP,
  validateUrl,
  getClientIp,
  formatETA
};
