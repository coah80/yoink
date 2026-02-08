const fs = require('fs');
const path = require('path');

const BANNER_FILE = path.join(__dirname, '../banner.json');

let currentBanner = null;

try {
  if (fs.existsSync(BANNER_FILE)) {
    const loaded = JSON.parse(fs.readFileSync(BANNER_FILE, 'utf8'));
    if (loaded.expiresAt && Date.now() > loaded.expiresAt) {
      currentBanner = null;
    } else {
      currentBanner = loaded;
    }
  }
} catch (e) {}

function setBanner({ message, type, expiresAt, auto }) {
  currentBanner = {
    message,
    type: type || 'info',
    createdAt: Date.now(),
    expiresAt: expiresAt || null,
    auto: auto || false
  };
  saveBanner();
  return currentBanner;
}

function clearBanner() {
  currentBanner = null;
  saveBanner();
}

function getBanner() {
  if (currentBanner && currentBanner.expiresAt && Date.now() > currentBanner.expiresAt) {
    currentBanner = null;
    saveBanner();
  }
  return currentBanner;
}

function saveBanner() {
  try {
    if (currentBanner) {
      fs.writeFileSync(BANNER_FILE, JSON.stringify(currentBanner, null, 2));
    } else if (fs.existsSync(BANNER_FILE)) {
      fs.unlinkSync(BANNER_FILE);
    }
  } catch (e) {}
}

let trafficWarningActive = false;

function checkTrafficLoad(activeJobs, jobLimits) {
  const totalActive = Object.values(activeJobs).reduce((a, b) => a + b, 0);
  const totalLimit = Object.values(jobLimits).reduce((a, b) => a + b, 0);
  const loadPercent = (totalActive / totalLimit) * 100;

  if (loadPercent >= 80 && !trafficWarningActive) {
    trafficWarningActive = true;
    if (!currentBanner || currentBanner.auto) {
      setBanner({
        message: 'High traffic — downloads may be slower than usual.',
        type: 'warning',
        auto: true,
        expiresAt: Date.now() + 10 * 60 * 1000
      });
    }
  } else if (loadPercent < 50 && trafficWarningActive) {
    trafficWarningActive = false;
    if (currentBanner && currentBanner.auto) {
      clearBanner();
    }
  }
}

module.exports = { setBanner, clearBanner, getBanner, checkTrafficLoad };
