const fs = require('fs');
const path = require('path');

const COOKIES_FILE = path.join(__dirname, '..', 'cookies.txt');
const YOUTUBE_COOKIES_FILE = path.join(__dirname, '..', 'youtube-cookies.txt');

const BOT_DETECTION_ERRORS = [
  'Sign in to confirm you',
  'confirm your age',
  'Sign in to confirm your age',
  'This video is unavailable',
  'Private video'
];

function hasCookiesFile() {
  return fs.existsSync(COOKIES_FILE);
}

function needsCookiesRetry(errorOutput) {
  return BOT_DETECTION_ERRORS.some(err => errorOutput.includes(err));
}

function getCookiesArgs() {
  if (fs.existsSync(YOUTUBE_COOKIES_FILE)) {
    return ['--cookies', YOUTUBE_COOKIES_FILE];
  }
  return [];
}

module.exports = {
  COOKIES_FILE,
  YOUTUBE_COOKIES_FILE,
  BOT_DETECTION_ERRORS,
  hasCookiesFile,
  needsCookiesRetry,
  getCookiesArgs
};
