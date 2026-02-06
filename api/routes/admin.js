const express = require('express');
const router = express.Router();
const path = require('path');
const fs = require('fs');

const {
  generateAdminToken,
  validateAdminToken,
  getAdminConfig
} = require('../services/admin');

const {
  trackPageView,
  trackDailyUser,
  deleteUserData,
  getAnalytics
} = require('../services/analytics');

const {
  getBannerState,
  setBannerState
} = require('../services/banner');

const { getClientIp, getCountryFromIP } = require('../utils/ip');

const {
  clientSessions,
  jobQueue,
  activeJobsByType
} = require('../services/state');

const discordAlerts = require('../discord-alerts');

const adminConfig = getAdminConfig();

const PAGE_ROUTES = {
  '/convert': '/pages/convert.html',
  '/compress': '/pages/compress.html',
  '/settings': '/pages/settings.html',
  '/privacy': '/pages/privacy.html',
  '/share': '/pages/share.html',
  '/admin': '/pages/admin.html'
};

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

router.post('/api/admin/login', (req, res) => {
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

router.post('/api/admin/logout', (req, res) => {
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

router.get('/api/admin/analytics', (req, res) => {
  const token = req.cookies?.yoink_admin_token || req.headers['authorization']?.replace('Bearer ', '');

  if (!validateAdminToken(token)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const analyticsData = getAnalytics();

  const dailyUsersFormatted = {};
  for (const [date, users] of Object.entries(analyticsData.dailyUsers)) {
    if (users instanceof Set) {
      dailyUsersFormatted[date] = users.size;
    } else if (Array.isArray(users)) {
      dailyUsersFormatted[date] = users.length;
    } else {
      dailyUsersFormatted[date] = Object.keys(users).length;
    }
  }

  res.json({
    totalDownloads: analyticsData.totalDownloads,
    totalConverts: analyticsData.totalConverts,
    totalCompresses: analyticsData.totalCompresses,
    formats: analyticsData.formats,
    sites: analyticsData.sites,
    countries: analyticsData.countries,
    dailyUsers: dailyUsersFormatted,
    pageViews: analyticsData.pageViews,
    currentUsers: clientSessions.size,
    peakUsers: analyticsData.peakUsers || { count: 0, timestamp: Date.now() },
    activeJobs: activeJobsByType,
    queueLength: jobQueue.length,
    lastUpdated: analyticsData.lastUpdated
  });
});

router.get('/api/banner', (req, res) => {
  res.json(getBannerState());
});

router.post('/api/admin/banner', (req, res) => {
  const token = req.cookies?.yoink_admin_token || req.headers['authorization']?.replace('Bearer ', '');

  if (!validateAdminToken(token)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }

  const { enabled, message, type } = req.body;
  setBannerState({ enabled, message, type });
  res.json({ success: true, banner: getBannerState() });
});

router.get('/api/alert/test', async (req, res) => {
  if (!discordAlerts.isEnabled()) {
    return res.status(503).json({
      success: false,
      error: 'Discord alerts not configured. Edit discord-config.js to enable.'
    });
  }

  const sent = await discordAlerts.test();
  res.json({ success: sent, message: sent ? 'Test alert sent to Discord!' : 'Failed to send alert' });
});

router.post('/api/github-webhook', async (req, res) => {
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

router.get('*', (req, res) => {
  if (!req.path.startsWith('/api/')) {
    if (PAGE_ROUTES[req.path]) {
      return res.sendFile(path.join(__dirname, '../../public', PAGE_ROUTES[req.path]));
    }

    const publicDir = path.resolve(__dirname, '../../public');
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

module.exports = router;
