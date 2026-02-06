const MULLVAD_SERVERS = [
  'us-qas-wg-002', 'us-qas-wg-003', 'us-qas-wg-004',
  'us-qas-wg-101', 'us-qas-wg-102', 'us-qas-wg-103',
  'us-qas-wg-201', 'us-qas-wg-203', 'us-qas-wg-204',
  'us-atl-wg-001', 'us-atl-wg-002',
  'us-atl-wg-301', 'us-atl-wg-302', 'us-atl-wg-303', 'us-atl-wg-304',
  'us-bos-wg-001', 'us-bos-wg-101', 'us-bos-wg-102',
  'us-chi-wg-201', 'us-chi-wg-203',
  'us-chi-wg-301', 'us-chi-wg-302', 'us-chi-wg-303'
];

function getRandomMullvadProxy(excludeServers = []) {
  const account = process.env.MULLVAD_ACCOUNT;
  if (!account) return null;

  const availableServers = MULLVAD_SERVERS.filter(s => !excludeServers.includes(s));
  if (availableServers.length === 0) return null;

  const randomServer = availableServers[Math.floor(Math.random() * availableServers.length)];
  return {
    url: `socks5h://${account}:m@${randomServer}.relays.mullvad.net:1080`,
    server: randomServer
  };
}

async function parseYouTubeClip(clipUrl) {
  console.log(`[Clip] Parsing YouTube clip: ${clipUrl}`);
  
  const clipResponse = await fetch(clipUrl, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
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
  
  const clipConfigMatch = clipHtml.match(/"clipConfig":\{[^}]*"startTimeMs":"(\d+)","endTimeMs":"(\d+)"/);
  if (!clipConfigMatch) {
    throw new Error('Could not extract clip timestamps from page');
  }
  
  const startTimeMs = parseInt(clipConfigMatch[1]);
  const endTimeMs = parseInt(clipConfigMatch[2]);
  console.log(`[Clip] Extracted clip times: ${startTimeMs}-${endTimeMs}ms (${(endTimeMs - startTimeMs) / 1000}s)`);
  
  return {
    videoId,
    startTimeMs,
    endTimeMs,
    fullVideoUrl: `https://www.youtube.com/watch?v=${videoId}`
  };
}

module.exports = {
  parseYouTubeClip,
  getRandomMullvadProxy
};
