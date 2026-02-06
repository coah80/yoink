const COMPRESSION_CONFIG = {
  presets: {
    fast: { ffmpegPreset: 'ultrafast', crf: { high: 26, medium: 28, low: 30 }, denoise: 'none', x264Params: 'aq-mode=1' },
    balanced: { ffmpegPreset: 'medium', crf: { high: 22, medium: 24, low: 26 }, denoise: 'auto', x264Params: 'aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0' },
    quality: { ffmpegPreset: 'slow', crf: { high: 20, medium: 22, low: 24 }, denoise: 'auto', x264Params: 'aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0' }
  },
  denoise: {
    none: null,
    light: 'hqdn3d=2:1.5:3:2.25',
    moderate: 'hqdn3d=4:3:6:4.5',
    heavy: 'hqdn3d=6:4:9:6'
  },
  bitrateThresholds: {
    1080: 2500,
    720: 1500,
    480: 800,
    360: 400
  }
};

function selectResolution(width, height, availableBitrateK) {
  const resolutions = [
    { w: 1920, h: 1080, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[1080] },
    { w: 1280, h: 720, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[720] },
    { w: 854, h: 480, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[480] },
    { w: 640, h: 360, minBitrate: COMPRESSION_CONFIG.bitrateThresholds[360] }
  ];

  for (const res of resolutions) {
    if (width < res.w && height < res.h) continue;
    if (availableBitrateK >= res.minBitrate) {
      return { width: res.w, height: res.h, needsScale: width > res.w };
    }
  }

  for (const res of resolutions) {
    if (availableBitrateK >= res.minBitrate) {
      return { width: res.w, height: res.h, needsScale: width > res.w };
    }
  }

  return { width: 640, height: 360, needsScale: width > 640 };
}

function getDenoiseFilter(denoise, sourceHeight, sourceBitrateMbps, presetDenoise = 'auto') {
  if (denoise === 'none' || presetDenoise === 'none') return null;
  if (denoise !== 'auto') return COMPRESSION_CONFIG.denoise[denoise];
  if (presetDenoise !== 'auto') return COMPRESSION_CONFIG.denoise[presetDenoise];

  const expectedBitrate = { 360: 1, 480: 1.5, 720: 3, 1080: 6, 1440: 12, 2160: 25 };
  const heights = Object.keys(expectedBitrate).map(Number);
  const closest = heights.reduce((a, b) => Math.abs(b - sourceHeight) < Math.abs(a - sourceHeight) ? b : a);

  if (sourceBitrateMbps > expectedBitrate[closest] * 2.5) {
    return COMPRESSION_CONFIG.denoise.heavy;
  } else if (sourceBitrateMbps > expectedBitrate[closest] * 1.5) {
    return COMPRESSION_CONFIG.denoise.moderate;
  }
  return COMPRESSION_CONFIG.denoise.light;
}

function getDownscaleResolution(sourceWidth, sourceHeight) {
  if (sourceWidth > 1920 || sourceHeight > 1080) {
    return 1920;
  } else if (sourceWidth >= 1920 || sourceHeight >= 1080) {
    return 1280;
  } else if (sourceWidth >= 1280 || sourceHeight >= 720) {
    return 854;
  }
  return null;
}

function buildVideoFilters(denoiseFilter, scaleWidth, sourceWidth) {
  const filters = [];
  if (scaleWidth && scaleWidth < sourceWidth) {
    filters.push(`scale=${scaleWidth}:-2:flags=lanczos`);
  }
  if (denoiseFilter) filters.push(denoiseFilter);
  return filters.length > 0 ? filters.join(',') : null;
}

function calculateTargetBitrate(targetMB, durationSec, audioBitrateK = 96) {
  const targetBytes = targetMB * 1024 * 1024 * 0.95;
  const audioBytes = (audioBitrateK * 1000 / 8) * durationSec;
  const videoBytes = targetBytes - audioBytes;
  return Math.floor((videoBytes * 8) / durationSec / 1000);
}

function formatETA(seconds) {
  if (!seconds || seconds < 0) return null;
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
}

module.exports = {
  COMPRESSION_CONFIG,
  selectResolution,
  getDenoiseFilter,
  getDownscaleResolution,
  buildVideoFilters,
  calculateTargetBitrate,
  formatETA
};
