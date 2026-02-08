const express = require('express');
const router = express.Router();
const os = require('os');

const {
  trackPageView,
  trackDailyUser,
  deleteUserData,
  getAnalytics
} = require('../services/analytics');

const { getClientIp, getCountryFromIP } = require('../utils/ip');
const {
  generateAdminToken,
  revokeAdminToken,
  validatePassword,
  requireAdmin,
  isAdminConfigured
} = require('../middleware/auth');
const { setBanner, clearBanner, getBanner, checkTrafficLoad } = require('../services/banner');
const {
  activeJobsByType,
  clientSessions,
  activeDownloads,
  activeProcesses,
  botDownloads,
  asyncJobs
} = require('../services/state');
const { JOB_LIMITS } = require('../config/constants');

// --- Public endpoints ---

router.post('/api/analytics/track', (req, res) => {
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

router.post('/api/analytics/delete', (req, res) => {
  const { trackingId } = req.body;
  if (!trackingId) {
    return res.status(400).json({ error: 'trackingId is required' });
  }
  const result = deleteUserData(trackingId);
  res.json(result);
});

router.get('/api/banner', (req, res) => {
  res.json({ banner: getBanner() });
});

// --- Admin endpoints ---

router.post('/api/admin/login', (req, res) => {
  if (!isAdminConfigured()) {
    return res.status(503).json({ error: 'Admin not configured' });
  }
  const { password } = req.body;
  if (!validatePassword(password)) {
    return res.status(401).json({ error: 'Wrong password' });
  }
  const token = generateAdminToken();
  res.json({ token });
});

router.post('/api/admin/logout', requireAdmin, (req, res) => {
  const token = req.headers['x-admin-token'];
  revokeAdminToken(token);
  res.json({ ok: true });
});

router.get('/api/admin/verify', requireAdmin, (req, res) => {
  res.json({ ok: true });
});

router.get('/api/admin/analytics', requireAdmin, (req, res) => {
  const analytics = getAnalytics();
  const serialized = { ...analytics };

  if (serialized.dailyUsers) {
    const dailyCounts = {};
    for (const [date, users] of Object.entries(serialized.dailyUsers)) {
      dailyCounts[date] = users instanceof Set ? users.size : (Array.isArray(users) ? users.length : 0);
    }
    serialized.dailyUsers = dailyCounts;
  }

  res.json(serialized);
});

router.get('/api/admin/status', requireAdmin, (req, res) => {
  const uptime = process.uptime();
  const mem = process.memoryUsage();

  checkTrafficLoad(activeJobsByType, JOB_LIMITS);

  res.json({
    uptime,
    memory: {
      rss: Math.round(mem.rss / 1048576),
      heapUsed: Math.round(mem.heapUsed / 1048576),
      heapTotal: Math.round(mem.heapTotal / 1048576)
    },
    system: {
      platform: os.platform(),
      loadAvg: os.loadavg(),
      freeMem: Math.round(os.freemem() / 1048576),
      totalMem: Math.round(os.totalmem() / 1048576)
    },
    activeJobs: { ...activeJobsByType },
    jobLimits: JOB_LIMITS,
    connectedClients: clientSessions.size,
    activeStreams: activeDownloads.size,
    activeProcesses: activeProcesses.size,
    botDownloads: botDownloads.size,
    asyncJobs: asyncJobs.size
  });
});

router.post('/api/admin/banner', requireAdmin, (req, res) => {
  const { message, type, expiresIn } = req.body;
  if (!message) {
    return res.status(400).json({ error: 'Message required' });
  }
  const banner = setBanner({
    message,
    type: type || 'info',
    expiresAt: expiresIn ? Date.now() + expiresIn * 60 * 1000 : null
  });
  res.json({ banner });
});

router.delete('/api/admin/banner', requireAdmin, (req, res) => {
  clearBanner();
  res.json({ ok: true });
});

module.exports = router;
