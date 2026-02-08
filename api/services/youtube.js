const { spawn } = require('child_process');
const { getCookiesArgs } = require('../utils/cookies');

async function parseYouTubeClip(clipUrl) {
  console.log(`[Clip] Parsing YouTube clip: ${clipUrl}`);

  const clipResponse = await fetch(clipUrl, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
    }
  });

  if (!clipResponse.ok) {
    throw new Error(`Failed to fetch clip page: ${clipResponse.status}`);
  }

  const clipHtml = await clipResponse.text();

  const videoIds = [...clipHtml.matchAll(/"videoId":"([^"]+)"/g)].map(m => m[1]);

  const idCounts = {};
  for (const id of videoIds) {
    idCounts[id] = (idCounts[id] || 0) + 1;
  }

  const videoId = Object.entries(idCounts).sort((a, b) => b[1] - a[1])[0]?.[0];

  if (!videoId) {
    throw new Error('Could not extract video ID from clip page');
  }

  console.log(`[Clip] Extracted video ID: ${videoId} (appeared ${idCounts[videoId]} times)`);

  const clipPatterns = [
    /"clipConfig":\{[^}]*"startTimeMs":"(\d+)","endTimeMs":"(\d+)"/,
    /"startTimeMs":"(\d+)"[^}]*"endTimeMs":"(\d+)"/,
    /startTimeMs.*?(\d{4,}).*?endTimeMs.*?(\d{4,})/,
    /"clipConfig":\{.*?"startTimeMs"\s*:\s*"?(\d+)"?.*?"endTimeMs"\s*:\s*"?(\d+)"?/s
  ];

  let startTimeMs = null;
  let endTimeMs = null;

  for (const pattern of clipPatterns) {
    const match = clipHtml.match(pattern);
    if (match) {
      startTimeMs = parseInt(match[1]);
      endTimeMs = parseInt(match[2]);
      if (startTimeMs >= 0 && endTimeMs > startTimeMs && (endTimeMs - startTimeMs) < 600000) {
        break;
      }
      startTimeMs = null;
      endTimeMs = null;
    }
  }

  if (startTimeMs === null || endTimeMs === null) {
    throw new Error('Could not extract clip timestamps from page');
  }

  console.log(`[Clip] Extracted clip times: ${startTimeMs}-${endTimeMs}ms (${(endTimeMs - startTimeMs) / 1000}s)`);

  return {
    videoId,
    startTimeMs,
    endTimeMs,
    fullVideoUrl: `https://www.youtube.com/watch?v=${videoId}`
  };
}

async function downloadClipViaYtdlp(clipUrl, jobId, tempDir, onProgress) {
  console.log(`[Clip] Falling back to yt-dlp for clip: ${clipUrl}`);

  const outputTemplate = require('path').join(tempDir, `${jobId}-clip.%(ext)s`);
  const args = [
    ...getCookiesArgs(),
    '--no-playlist',
    '--newline',
    '-o', outputTemplate,
    clipUrl
  ];

  return new Promise((resolve, reject) => {
    const ytdlp = spawn('yt-dlp', args);
    let stderrOutput = '';

    ytdlp.stderr.on('data', (data) => {
      const msg = data.toString();
      stderrOutput += msg;
      if (msg.includes('%') && onProgress) {
        const percentMatch = msg.match(/([\d.]+)%/);
        if (percentMatch) onProgress(parseFloat(percentMatch[1]));
      }
    });

    ytdlp.stdout.on('data', (data) => {
      const msg = data.toString();
      if (msg.includes('%') && onProgress) {
        const percentMatch = msg.match(/([\d.]+)%/);
        if (percentMatch) onProgress(parseFloat(percentMatch[1]));
      }
    });

    ytdlp.on('close', (code) => {
      if (code !== 0) {
        const errorMatch = stderrOutput.match(/ERROR[:\s]+(.+?)(?:\n|$)/i);
        reject(new Error(errorMatch ? errorMatch[1].trim() : 'yt-dlp clip download failed'));
        return;
      }

      const fs = require('fs');
      const path = require('path');
      const files = fs.readdirSync(tempDir);
      const clipFile = files.find(f =>
        f.startsWith(`${jobId}-clip`) &&
        !f.endsWith('.part') &&
        !f.includes('.part-Frag')
      );

      if (!clipFile) {
        reject(new Error('Clip file not found after download'));
        return;
      }

      resolve({
        path: path.join(tempDir, clipFile),
        ext: path.extname(clipFile).slice(1)
      });
    });

    ytdlp.on('error', reject);
  });
}

module.exports = {
  parseYouTubeClip,
  downloadClipViaYtdlp
};
