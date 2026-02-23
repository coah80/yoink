const fs = require('fs');
const path = require('path');
const { FILE_RETENTION_MS, DISK_SPACE_MIN_GB } = require('../config/constants');

const TEMP_DIR = '/var/tmp/yoink';
const TEMP_DIRS = {
  download: path.join(TEMP_DIR, 'downloads'),
  convert: path.join(TEMP_DIR, 'convert'),
  compress: path.join(TEMP_DIR, 'compress'),
  playlist: path.join(TEMP_DIR, 'playlists'),
  gallery: path.join(TEMP_DIR, 'galleries'),
  upload: path.join(TEMP_DIR, 'uploads'),
  bot: path.join(TEMP_DIR, 'bot'),
  transcribe: path.join(TEMP_DIR, 'transcribe')
};

function clearTempDir() {
  Object.values(TEMP_DIRS).forEach(dir => {
    try {
      if (fs.existsSync(dir)) {
        const items = fs.readdirSync(dir);
        items.forEach(item => {
          const itemPath = path.join(dir, item);
          try {
            const stat = fs.statSync(itemPath);
            if (stat.isDirectory()) fs.rmSync(itemPath, { recursive: true });
            else fs.unlinkSync(itemPath);
          } catch {}
        });
      } else {
        fs.mkdirSync(dir, { recursive: true });
      }
    } catch {
      fs.mkdirSync(dir, { recursive: true });
    }
  });
  console.log('✓ Cleared temp directories');
}

function cleanupTempFiles() {
  try {
    const now = Date.now();
    Object.values(TEMP_DIRS).forEach(dir => {
      if (!fs.existsSync(dir)) return;
      const items = fs.readdirSync(dir);
      items.forEach(item => {
        const itemPath = path.join(dir, item);
        try {
          const stat = fs.statSync(itemPath);
          if (now - stat.mtimeMs > FILE_RETENTION_MS) {
            if (stat.isDirectory()) {
              fs.rmSync(itemPath, { recursive: true });
            } else {
              fs.unlinkSync(itemPath);
            }
            console.log(`Cleaned up old temp: ${item}`);
          }
        } catch { }
      });
    });
    try {
      const stats = fs.statfsSync(TEMP_DIR);
      const availableGB = (stats.bavail * stats.bsize) / (1024 * 1024 * 1024);
      const totalGB = (stats.blocks * stats.bsize) / (1024 * 1024 * 1024);
      const usedGB = totalGB - availableGB;
      console.log(`[DiskSpace] ${availableGB.toFixed(1)}GB free / ${totalGB.toFixed(1)}GB total (${usedGB.toFixed(1)}GB used)`);
      if (availableGB < DISK_SPACE_MIN_GB) {
        console.warn(`[DiskSpace] WARNING: Only ${availableGB.toFixed(1)}GB free, below ${DISK_SPACE_MIN_GB}GB threshold!`);
      }
    } catch {}
  } catch (err) {
    console.error('Cleanup error:', err);
  }
}

function cleanupJobFiles(jobId) {
  try {
    let cleaned = 0;
    Object.values(TEMP_DIRS).forEach(dir => {
      if (!fs.existsSync(dir)) return;
      const items = fs.readdirSync(dir);
      items.forEach(item => {
        if (item.includes(jobId)) {
          const itemPath = path.join(dir, item);
          try {
            const stat = fs.statSync(itemPath);
            if (stat.isDirectory()) {
              fs.rmSync(itemPath, { recursive: true });
            } else {
              fs.unlinkSync(itemPath);
            }
            console.log(`[Cleanup] Removed: ${item}`);
            cleaned++;
          } catch (innerErr) {
            console.debug(`[Cleanup] Failed to remove ${item} for job ${jobId}: ${innerErr.message}`);
          }
        }
      });
    });
    if (cleaned === 0) {
      console.debug(`[Cleanup] No files found for job ${jobId.slice(0, 12)}`);
    }
  } catch (outerErr) {
    console.debug(`[Cleanup] Failed to cleanup job ${jobId}: ${outerErr.message}`);
  }
}

function sanitizeFilename(filename) {
  return filename
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200);
}

function startCleanupInterval() {
  setInterval(cleanupTempFiles, 5 * 60 * 1000);
}

module.exports = {
  TEMP_DIR,
  TEMP_DIRS,
  clearTempDir,
  cleanupTempFiles,
  cleanupJobFiles,
  sanitizeFilename,
  startCleanupInterval
};
