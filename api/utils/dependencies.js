const { execSync } = require('child_process');
const { hasCookiesFile } = require('./cookies');

let galleryDlAvailable = false;
let whisperAvailable = false;

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

  try {
    execSync('python3 -c "from faster_whisper import WhisperModel"', { stdio: 'ignore' });
    whisperAvailable = true;
    console.log('✓ faster-whisper found');
  } catch {
    console.log('⚠ faster-whisper not found - transcription disabled. Install: pip3 install faster-whisper');
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

function isWhisperAvailable() {
  return whisperAvailable;
}

module.exports = {
  checkDependencies,
  isGalleryDlAvailable,
  isWhisperAvailable
};
