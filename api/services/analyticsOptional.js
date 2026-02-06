// Optional Analytics Wrapper
// This module provides a safe interface to analytics that works even if analytics.js is deleted
// If you want to disable analytics, simply delete analytics.js and the system will continue working

let analytics = null;
let analyticsEnabled = false;

// Try to load analytics module
try {
  analytics = require('./analytics.js');
  analyticsEnabled = true;
  console.log('[Analytics] Module loaded - tracking enabled');
} catch (e) {
  console.log('[Analytics] Module not found - tracking disabled');
  // Provide no-op functions when analytics is not available
  analytics = {
    trackDownload: () => {},
    trackConvert: () => {},
    trackCompress: () => {},
    trackPageView: () => {},
    trackDailyUser: () => {},
    deleteUserData: () => ({ deleted: false, reason: 'Analytics module not installed' }),
    getAnalytics: () => ({
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
    }),
    saveAnalytics: () => {},
    updatePeakUsers: () => {}
  };
}

/**
 * Check if analytics is enabled
 * @returns {boolean} True if analytics module is loaded
 */
function isAnalyticsEnabled() {
  return analyticsEnabled;
}

// Export analytics functions with optional support
module.exports = {
  ...analytics,
  isAnalyticsEnabled
};
