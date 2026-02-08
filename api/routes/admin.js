const express = require('express');
const router = express.Router();

const {
  trackPageView,
  trackDailyUser,
  deleteUserData
} = require('../services/analytics');

const { getClientIp, getCountryFromIP } = require('../utils/ip');

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

module.exports = router;
