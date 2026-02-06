const crypto = require('crypto');

let adminConfig = { ADMIN_PASSWORD: null, ADMIN_TOKEN_SECRET: 'default-secret' };
try {
  adminConfig = require('../admin-config.js');
} catch (e) {
  console.log('[Admin] No admin-config.js found, admin features disabled');
}

const adminTokens = new Map();

function generateAdminToken() {
  const token = crypto.randomBytes(32).toString('hex');
  adminTokens.set(token, { createdAt: Date.now() });
  return token;
}

function validateAdminToken(token) {
  return adminTokens.has(token);
}

function getAdminConfig() {
  return adminConfig;
}

module.exports = {
  generateAdminToken,
  validateAdminToken,
  getAdminConfig,
  adminTokens
};
