const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

// This module can be DELETED if you don't want to use yt-dlp for downloads
// The server will automatically use only Cobalt if this module is missing

const COOKIES_FILE = path.join(__dirname, '../youtube-cookies.txt');

/**
 * Get cookies arguments for yt-dlp if cookies file exists
 * @returns {Array<string>} Cookies arguments
 */
function getCookiesArgs() {
  if (fs.existsSync(COOKIES_FILE)) {
    return ['--cookies', COOKIES_FILE];
  }
  return [];
}

/**
 * Convert error messages to user-friendly format
 * @param {string} message - Error message to convert
 * @returns {string} User-friendly error message
 */
function toUserError(message) {
  const text = String(message || '');
  const msg = text.toLowerCase();

  if (msg.includes('cancelled')) return 'Download cancelled';
  if (msg.includes('content.video.unavailable') || msg.includes('video unavailable') || msg.includes('private video') || msg.includes('this content is private')) return 'Video unavailable or private';
  if (msg.includes('content.video.live') || msg.includes('live stream')) return 'Live streams cannot be downloaded';
  if (msg.includes('content.video.age') || msg.includes('age-restricted')) return 'Age-restricted video (sign-in required)';
  if (msg.includes('rate')) return 'Rate limited - please wait and try again';
  if (msg.includes('econnreset') || msg.includes('fetch failed') || msg.includes('connection')) return 'Connection interrupted - try again';
  if (msg.includes('processing failed') || msg.includes('encoding failed')) return 'Processing failed';
  if (msg.includes('download interrupted')) return 'Download interrupted';
  if (msg.includes('no videos were successfully downloaded')) return 'No videos were successfully downloaded';
  if (msg.includes('downloaded file not found')) return 'Download failed';

  return 'Download failed';
}

/**
 * Fetch metadata for a video using yt-dlp
 * @param {string} url - The video URL
 * @param {boolean} isPlaylist - Whether to fetch playlist metadata
 * @returns {Promise<Object>} Metadata object
 */
async function fetchMetadataViaYtDlp(url, isPlaylist = false) {
  const ytdlpArgs = [...getCookiesArgs(), '-t', 'sleep'];

  if (!isPlaylist) {
    ytdlpArgs.push('--no-playlist');
    ytdlpArgs.push(
      '--print', '%(title)s',
      '--print', '%(ext)s',
      '--print', '%(id)s',
      '--print', '%(uploader)s',
      '--print', '%(duration)s',
      '--print', '%(thumbnail)s',
      url
    );
  } else {
    ytdlpArgs.push(
      '--yes-playlist',
      '--flat-playlist',
      '--print', '%(playlist_title)s',
      '--print', '%(playlist_count)s',
      '--print', '%(title)s',
      url
    );
  }

  return new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);

    const timeoutMs = 30000;
    const timeoutId = setTimeout(() => {
      if (ytdlp.exitCode === null) {
        console.log(`[Metadata] Timeout reached (${timeoutMs}ms), killing yt-dlp process`);
        ytdlp.kill('SIGKILL');
      }
    }, timeoutMs);

    let stdout = '';
    let stderr = '';

    ytdlp.stdout.on('data', (data) => {
      stdout += data.toString();
    });

    ytdlp.stderr.on('data', (data) => {
      stderr += data.toString();
    });

    ytdlp.on('close', (code) => {
      clearTimeout(timeoutId);

      if (code !== 0) {
        console.error('[Metadata] yt-dlp failed:', stderr);
        return reject(new Error('Failed to fetch metadata'));
      }

      const lines = stdout.trim().split('\n');

      if (isPlaylist) {
        const [playlistTitle, playlistCount, firstVideoTitle] = lines;
        resolve({
          title: playlistTitle || 'Unknown Playlist',
          videoCount: parseInt(playlistCount) || 0,
          firstVideoTitle: firstVideoTitle || '',
          isPlaylist: true
        });
      } else {
        const [title, ext, id, uploader, duration, thumbnail] = lines;
        resolve({
          title: title || 'download',
          ext: ext || 'mp4',
          id: id || '',
          uploader: uploader || '',
          duration: duration || '',
          thumbnail: thumbnail || '',
          isPlaylist: false,
          usingCookies: fs.existsSync(COOKIES_FILE)
        });
      }
    });

    ytdlp.on('error', (err) => {
      clearTimeout(timeoutId);
      reject(err);
    });
  });
}

/**
 * Download a video using yt-dlp
 * @param {string} url - The video URL
 * @param {string} jobId - Job identifier for logging
 * @param {Object} options - Download options
 * @param {string} options.outputPath - Full output path for the downloaded file
 * @param {boolean} options.isAudio - Whether to download audio only
 * @param {string} options.quality - Video quality (e.g., '1080p', '720p')
 * @param {string} options.container - Container format (e.g., 'mp4', 'mkv')
 * @param {boolean} options.isPlaylist - Whether downloading a playlist
 * @param {Object} options.qualityHeight - Map of quality names to heights
 * @param {Function} options.onProgress - Progress callback (progress)
 * @param {Object} options.processInfo - Process info object to store the spawned process
 * @param {Object} options.request - Express request object for cancellation detection
 * @returns {Promise<string>} Path to the downloaded file
 */
async function downloadViaYtDlp(url, jobId, options) {
  const {
    outputPath,
    isAudio = false,
    quality = '1080p',
    container = 'mp4',
    isPlaylist = false,
    qualityHeight = {},
    onProgress = null,
    processInfo = null,
    request = null
  } = options;

  const ytdlpArgs = [
    ...getCookiesArgs(),
    '--continue',
    '-t', 'sleep',
    isPlaylist ? '--yes-playlist' : '--no-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s',
    '-o', outputPath,
    '--ffmpeg-location', '/usr/bin/ffmpeg',
  ];

  if (isAudio) {
    ytdlpArgs.push('-f', 'bestaudio/best');
  } else {
    const maxHeight = qualityHeight[quality];
    if (maxHeight) {
      ytdlpArgs.push('-f', `bv[vcodec^=avc][height<=${maxHeight}]+ba[acodec^=mp4a]/bv[height<=${maxHeight}]+ba/b`);
    } else {
      ytdlpArgs.push('-f', 'bv[vcodec^=avc]+ba[acodec^=mp4a]/bv+ba/b');
    }
    ytdlpArgs.push('--merge-output-format', container);
  }

  ytdlpArgs.push(url);

  console.log(`[${jobId}] yt-dlp command: yt-dlp ${ytdlpArgs.join(' ')}`);

  return new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);

    if (processInfo) {
      processInfo.process = ytdlp;
    }

    let lastProgress = 0;
    let stderrOutput = '';

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString().trim();
      const match = msg.match(/([\d.]+)%/);
      if (match) {
        const progress = parseFloat(match[1]);
        if (progress > lastProgress + 5 || progress >= 100) {
          lastProgress = progress;
          if (onProgress) {
            onProgress(progress);
          }
        }
      }
    });

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (msg.includes('[download]') && msg.includes('%')) {
        const match = msg.match(/([\d.]+)%/);
        if (match) {
          const progress = parseFloat(match[1]);
          if (onProgress) {
            onProgress(progress);
          }
        }
      }
    });

    ytdlp.on('close', (code) => {
      if (processInfo?.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code !== 0) {
        const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        const errorMessage = errorMatch ? errorMatch[1].trim() : 'Download failed';
        reject(new Error(errorMessage));
      } else {
        resolve(outputPath);
      }
    });

    ytdlp.on('error', (err) => {
      reject(err);
    });

    if (request) {
      request.on('close', () => {
        if (processInfo && !processInfo.cancelled) {
          ytdlp.kill('SIGTERM');
        }
      });
    }
  });
}

/**
 * Download a playlist using yt-dlp
 * @param {string} url - The playlist URL
 * @param {string} jobId - Job identifier for logging
 * @param {Object} options - Download options
 * @param {string} options.outputDir - Output directory for playlist files
 * @param {boolean} options.isAudio - Whether to download audio only
 * @param {string} options.quality - Video quality
 * @param {string} options.container - Container format
 * @param {Object} options.qualityHeight - Map of quality names to heights
 * @param {Function} options.onProgress - Progress callback
 * @param {Object} options.processInfo - Process info object
 * @param {Object} options.request - Express request object
 * @returns {Promise<Array<string>>} Array of downloaded file paths
 */
async function downloadPlaylistViaYtDlp(url, jobId, options) {
  const {
    outputDir,
    isAudio = false,
    quality = '1080p',
    container = 'mp4',
    qualityHeight = {},
    onProgress = null,
    processInfo = null,
    request = null
  } = options;

  const outputTemplate = path.join(outputDir, `${jobId}-%(playlist_index)s-%(title).100s.%(ext)s`);

  const ytdlpArgs = [
    ...getCookiesArgs(),
    '--continue',
    '-t', 'sleep',
    '--yes-playlist',
    '--newline',
    '--progress-template', '%(progress._percent_str)s %(info.playlist_index)s/%(info.playlist_count)s',
    '-o', outputTemplate,
    '--ffmpeg-location', '/usr/bin/ffmpeg',
  ];

  if (isAudio) {
    ytdlpArgs.push('-f', 'bestaudio/best');
  } else {
    const maxHeight = qualityHeight[quality];
    if (maxHeight) {
      ytdlpArgs.push('-f', `bv[vcodec^=avc][height<=${maxHeight}]+ba[acodec^=mp4a]/bv[height<=${maxHeight}]+ba/b`);
    } else {
      ytdlpArgs.push('-f', 'bv[vcodec^=avc]+ba[acodec^=mp4a]/bv+ba/b');
    }
    ytdlpArgs.push('--merge-output-format', container);
  }

  ytdlpArgs.push(url);

  console.log(`[${jobId}] Playlist yt-dlp command: yt-dlp ${ytdlpArgs.join(' ')}`);

  return new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', ytdlpArgs);

    if (processInfo) {
      processInfo.process = ytdlp;
    }

    let stderrOutput = '';

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString().trim();
      if (onProgress) {
        onProgress(msg);
      }
    });

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (onProgress) {
        onProgress(msg);
      }
    });

    ytdlp.on('close', (code) => {
      if (processInfo?.cancelled) {
        reject(new Error('Download cancelled'));
      } else if (code !== 0) {
        const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        const errorMessage = errorMatch ? errorMatch[1].trim() : 'Playlist download failed';
        reject(new Error(errorMessage));
      } else {
        // Find all downloaded files
        const files = fs.readdirSync(outputDir);
        const downloadedFiles = files
          .filter(f => f.startsWith(jobId))
          .map(f => path.join(outputDir, f));
        resolve(downloadedFiles);
      }
    });

    ytdlp.on('error', (err) => {
      reject(err);
    });

    if (request) {
      request.on('close', () => {
        if (processInfo && !processInfo.cancelled) {
          ytdlp.kill('SIGTERM');
        }
      });
    }
  });
}

/**
 * Check if yt-dlp should be used for a URL (non-YouTube URLs)
 * @param {string} url - The URL to check
 * @returns {boolean} True if yt-dlp should be used
 */
function shouldUseYtDlp(url) {
  try {
    const urlObj = new URL(url);
    const hostname = urlObj.hostname.toLowerCase();

    // Use yt-dlp for non-YouTube URLs
    return !hostname.includes('youtube.com') && !hostname.includes('youtu.be');
  } catch {
    return true; // Default to yt-dlp for invalid URLs
  }
}

module.exports = {
  fetchMetadataViaYtDlp,
  downloadViaYtDlp,
  downloadPlaylistViaYtDlp,
  shouldUseYtDlp,
  toUserError
};
