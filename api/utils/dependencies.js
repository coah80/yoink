const { execSync } = require('child_process');
const { hasCookiesFile } = require('./cookies');

let galleryDlAvailable = false;

function checkDependencies() {
  try {
    execSync('which yt-dlp', { stdio: 'ignore' });
    console.log('✓ yt-dlp found');
  } catch {
    console.error('✗ yt-dlp not found. Please install: pip install yt-dlp');
    process.exit(1);
  }

  try {
    execSync('which ffmpeg', { stdio: 'ignore' });
    console.log('✓ ffmpeg found');
  } catch {
    console.error('✗ ffmpeg not found. Please install: apt install ffmpeg');
    process.exit(1);
  }

  try {
    execSync('which gallery-dl', { stdio: 'ignore' });
    galleryDlAvailable = true;
    console.log('✓ gallery-dl found');
  } catch {
    console.log('⚠ gallery-dl not found - image downloads disabled. Install: pip install gallery-dl');
  }

  if (hasCookiesFile()) {
    console.log('✓ cookies.txt found - YouTube authentication enabled');
  } else {
    console.log('⚠ cookies.txt not found - YouTube may block some requests');
  }
}

function isGalleryDlAvailable() {
  return galleryDlAvailable;
}

module.exports = {
  checkDependencies,
  isGalleryDlAvailable
};
