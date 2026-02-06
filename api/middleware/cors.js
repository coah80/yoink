function loadCorsConfig() {
  let corsOrigins = null;
  try {
    corsOrigins = require('../cors-origins.js');
    console.log(`✓ Loaded ${corsOrigins.length} CORS origins from cors-origins.js`);
  } catch (e) {
    console.log('[CORS] No cors-origins.js found, allowing all origins (credentials disabled)');
  }

  return corsOrigins && corsOrigins.length > 0
    ? { origin: corsOrigins, credentials: true }
    : { origin: true, credentials: false };
}

module.exports = { loadCorsConfig };
