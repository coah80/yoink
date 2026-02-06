const fs = require('fs');
const path = require('path');

const ANALYTICS_FILE = path.join(__dirname, '../analytics.json');

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

const seenCountryUsers = new Map();

setInterval(() => {
  const today = new Date().toISOString().split('T')[0];
  for (const key of seenCountryUsers.keys()) {
    if (!key.startsWith(today)) {
      seenCountryUsers.delete(key);
    }
  }
}, 60 * 60 * 1000);

function trackDailyUser(clientId, country, trackingId) {
  const today = new Date().toISOString().split('T')[0];
  if (!analytics.dailyUsers[today]) {
    analytics.dailyUsers[today] = new Set();
  }
  if (typeof analytics.dailyUsers[today] === 'object' && !(analytics.dailyUsers[today] instanceof Set)) {
    if (Array.isArray(analytics.dailyUsers[today])) {
      analytics.dailyUsers[today] = new Set(analytics.dailyUsers[today]);
    } else {
      analytics.dailyUsers[today] = new Set();
    }
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

function getAnalytics() {
  return analytics;
}

function updatePeakUsers(currentCount) {
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

module.exports = {
  trackDownload,
  trackConvert,
  trackCompress,
  trackPageView,
  trackDailyUser,
  deleteUserData,
  getAnalytics,
  saveAnalytics,
  updatePeakUsers
};
