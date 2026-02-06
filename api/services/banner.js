const fs = require('fs');
const path = require('path');

const BANNER_FILE = path.join(__dirname, '../banner.json');

let bannerState = { enabled: false, message: '', type: 'info' };

function loadBanner() {
  try {
    if (fs.existsSync(BANNER_FILE)) {
      bannerState = JSON.parse(fs.readFileSync(BANNER_FILE, 'utf8'));
      console.log('[Banner] Loaded banner state');
    }
  } catch (e) {
    console.log('[Banner] Failed to load banner state:', e.message);
  }
}

function saveBanner() {
  try {
    fs.writeFileSync(BANNER_FILE, JSON.stringify(bannerState, null, 2));
  } catch (e) {
    console.error('[Banner] Failed to save:', e.message);
  }
}

function getBannerState() {
  return bannerState;
}

function setBannerState(newState) {
  bannerState = { ...bannerState, ...newState };
  saveBanner();
}

loadBanner();

module.exports = {
  getBannerState,
  setBannerState,
  saveBanner,
  loadBanner
};
