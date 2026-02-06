const net = require('net');
const geoip = require('geoip-lite');

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

function getClientIp(req) {
  return req.headers['x-forwarded-for']?.split(',')[0]?.trim() ||
    req.headers['x-real-ip'] ||
    req.connection?.remoteAddress ||
    req.socket?.remoteAddress ||
    'unknown';
}

module.exports = {
  normalizeIp,
  isPrivateIp,
  getCountryFromIP,
  getClientIp
};
