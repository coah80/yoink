async function parseYouTubeClip(clipUrl) {
  console.log(`[Clip] Parsing YouTube clip`);

  const clipResponse = await fetch(clipUrl, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
      'Accept-Language': 'en-US,en;q=0.9',
      'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
    }
  });

  if (!clipResponse.ok) {
    throw new Error(`Failed to fetch clip page: ${clipResponse.status}`);
  }

  const clipHtml = await clipResponse.text();

  const canonicalMatch = clipHtml.match(/<link[^>]*rel="canonical"[^>]*href="([^"]+)"/);
  if (canonicalMatch && canonicalMatch[1] === 'https://www.youtube.com/') {
    if (clipHtml.includes('Clip not available') || clipHtml.includes('clip can be unavailable')) {
      throw new Error('This clip is no longer available,it may have been deleted');
    }
    throw new Error('This clip is unavailable');
  }

  let videoId = null;
  let startTimeMs = null;
  let endTimeMs = null;

  const initialDataMatch = clipHtml.match(/ytInitialData\s*=\s*(\{.*?\});\s*/);
  if (initialDataMatch) {
    try {
      const dataStr = initialDataMatch[1];
      const videoIdMatches = [...dataStr.matchAll(/"videoId"\s*:\s*"([A-Za-z0-9_-]{11})"/g)].map(m => m[1]);
      if (videoIdMatches.length > 0) {
        const idCounts = {};
        for (const id of videoIdMatches) {
          idCounts[id] = (idCounts[id] || 0) + 1;
        }
        videoId = Object.entries(idCounts).sort((a, b) => b[1] - a[1])[0][0];
        console.log(`[Clip] Found videoId in ytInitialData: ${videoId}`);
      }

      const clipConfigMatch = dataStr.match(/"clipConfig"\s*:\s*\{[^}]*"startTimeMs"\s*:\s*"?(\d+)"?\s*,\s*"endTimeMs"\s*:\s*"?(\d+)"?/);
      if (clipConfigMatch) {
        startTimeMs = parseInt(clipConfigMatch[1]);
        endTimeMs = parseInt(clipConfigMatch[2]);
        console.log(`[Clip] Found timestamps in ytInitialData: ${startTimeMs}-${endTimeMs}ms`);
      }
    } catch (e) {
      console.log(`[Clip] Failed to parse ytInitialData: ${e.message}`);
    }
  }

  const playerMatch = clipHtml.match(/ytInitialPlayerResponse\s*=\s*(\{.*?\});\s*/);
  if (playerMatch) {
    try {
      const playerStr = playerMatch[1];
      if (!videoId) {
        const pidMatch = playerStr.match(/"videoId"\s*:\s*"([A-Za-z0-9_-]{11})"/);
        if (pidMatch) {
          videoId = pidMatch[1];
          console.log(`[Clip] Found videoId in ytInitialPlayerResponse: ${videoId}`);
        }
      }
      if (startTimeMs === null) {
        const clipMatch = playerStr.match(/"clipConfig"\s*:\s*\{[^}]*"startTimeMs"\s*:\s*"?(\d+)"?\s*,\s*"endTimeMs"\s*:\s*"?(\d+)"?/);
        if (clipMatch) {
          startTimeMs = parseInt(clipMatch[1]);
          endTimeMs = parseInt(clipMatch[2]);
          console.log(`[Clip] Found timestamps in playerResponse: ${startTimeMs}-${endTimeMs}ms`);
        }
      }
    } catch (e) {
      console.log(`[Clip] Failed to parse playerResponse: ${e.message}`);
    }
  }

  if (!videoId) {
    const videoIds = [...clipHtml.matchAll(/"videoId"\s*:\s*"([A-Za-z0-9_-]{11})"/g)].map(m => m[1]);
    if (videoIds.length > 0) {
      const idCounts = {};
      for (const id of videoIds) {
        idCounts[id] = (idCounts[id] || 0) + 1;
      }
      videoId = Object.entries(idCounts).sort((a, b) => b[1] - a[1])[0][0];
      console.log(`[Clip] Found videoId via raw HTML: ${videoId} (appeared ${idCounts[videoId]} times)`);
    }
  }

  if (!videoId) {
    const watchMatch = clipHtml.match(/\/watch\?v=([A-Za-z0-9_-]{11})/);
    if (watchMatch) {
      videoId = watchMatch[1];
      console.log(`[Clip] Found videoId via watch URL: ${videoId}`);
    }
  }

  if (!videoId) {
    const embedMatch = clipHtml.match(/\/embed\/([A-Za-z0-9_-]{11})/);
    if (embedMatch) {
      videoId = embedMatch[1];
      console.log(`[Clip] Found videoId via embed URL: ${videoId}`);
    }
  }

  if (!videoId) {
    throw new Error('This clip is unavailable or the video has been removed');
  }

  if (startTimeMs === null || endTimeMs === null) {
    const clipPatterns = [
      /"clipConfig"\s*:\s*\{[^}]*"startTimeMs"\s*:\s*"?(\d+)"?\s*,\s*"endTimeMs"\s*:\s*"?(\d+)"?/,
      /"startTimeMs"\s*:\s*"(\d+)"[^}]*"endTimeMs"\s*:\s*"(\d+)"/,
      /startTimeMs.*?(\d{4,}).*?endTimeMs.*?(\d{4,})/,
      /"clipConfig"\s*:\s*\{.*?"startTimeMs"\s*:\s*"?(\d+)"?.*?"endTimeMs"\s*:\s*"?(\d+)"?/s
    ];

    for (const pattern of clipPatterns) {
      const match = clipHtml.match(pattern);
      if (match) {
        startTimeMs = parseInt(match[1]);
        endTimeMs = parseInt(match[2]);
        if (startTimeMs >= 0 && endTimeMs > startTimeMs && (endTimeMs - startTimeMs) < 600000) {
          console.log(`[Clip] Found timestamps via raw HTML pattern: ${startTimeMs}-${endTimeMs}ms`);
          break;
        }
        startTimeMs = null;
        endTimeMs = null;
      }
    }
  }

  if (startTimeMs === null || endTimeMs === null) {
    throw new Error('Could not extract clip timestamps,the clip data may have changed');
  }

  if (startTimeMs < 0 || endTimeMs <= startTimeMs || (endTimeMs - startTimeMs) >= 600000) {
    throw new Error('Invalid clip timestamps extracted');
  }

  console.log(`[Clip] Extracted clip: videoId=${videoId}, ${startTimeMs}-${endTimeMs}ms (${(endTimeMs - startTimeMs) / 1000}s)`);

  return {
    videoId,
    startTimeMs,
    endTimeMs,
    fullVideoUrl: `https://www.youtube.com/watch?v=${videoId}`
  };
}

module.exports = {
  parseYouTubeClip
};
