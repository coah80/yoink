const crypto = require('crypto');

let adminConfig = { ADMIN_PASSWORD: null, TOKEN_EXPIRY_MS: 86400000 };
try {
  adminConfig = require('../admin-config.js');
} catch (e) {
  console.log('[Admin] No admin-config.js found, admin features disabled');
}

const adminTokens = new Map();

function generateAdminToken() {
  const token = crypto.randomBytes(32).toString('hex');
  adminTokens.set(token, {
    createdAt: Date.now(),
    expiresAt: Date.now() + (adminConfig.TOKEN_EXPIRY_MS || 86400000)
  });
  return token;
}

function validateAdminToken(token) {
  if (!token) return false;
  const session = adminTokens.get(token);
  if (!session) return false;
  if (Date.now() > session.expiresAt) {
    adminTokens.delete(token);
    return false;
  }
  return true;
}

function revokeAdminToken(token) {
  adminTokens.delete(token);
}

function validatePassword(password) {
  return adminConfig.ADMIN_PASSWORD && password === adminConfig.ADMIN_PASSWORD;
}

function requireAdmin(req, res, next) {
  const token = req.headers['x-admin-token'];
  if (!validateAdminToken(token)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }
  next();
}

function isAdminConfigured() {
  return !!adminConfig.ADMIN_PASSWORD;
}

module.exports = {
  generateAdminToken,
  validateAdminToken,
  revokeAdminToken,
  validatePassword,
  requireAdmin,
  isAdminConfigured,
  adminTokens
};
