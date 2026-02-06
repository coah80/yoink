const { SAFETY_LIMITS } = require('../config/constants');
const { getClientIp } = require('../utils/ip');

const rateLimitStore = new Map();

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

function startRateLimitCleanup() {
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
}

module.exports = {
  checkRateLimit,
  rateLimitMiddleware,
  startRateLimitCleanup
};
