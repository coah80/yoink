const { spawn } = require('child_process');
const { getCookiesArgs } = require('../utils/cookies');

const MULLVAD_SERVERS = [
  'us-qas-wg-002', 'us-qas-wg-003', 'us-qas-wg-004',
  'us-qas-wg-101', 'us-qas-wg-102', 'us-qas-wg-103',
  'us-qas-wg-201', 'us-qas-wg-203', 'us-qas-wg-204',
  'us-qas-wg-303', 'us-qas-wg-304', 'us-qas-wg-305', 'us-qas-wg-306', 'us-qas-wg-307', 'us-qas-wg-308',
  'us-atl-wg-001', 'us-atl-wg-002',
  'us-atl-wg-301', 'us-atl-wg-302', 'us-atl-wg-303', 'us-atl-wg-304', 'us-atl-wg-305', 'us-atl-wg-306',
  'us-atl-wg-401', 'us-atl-wg-402', 'us-atl-wg-403', 'us-atl-wg-404', 'us-atl-wg-405', 'us-atl-wg-406', 'us-atl-wg-407', 'us-atl-wg-408',
  'us-bos-wg-001', 'us-bos-wg-101', 'us-bos-wg-102',
  'us-chi-wg-201', 'us-chi-wg-203',
  'us-chi-wg-301', 'us-chi-wg-302', 'us-chi-wg-303', 'us-chi-wg-304', 'us-chi-wg-305', 'us-chi-wg-306'
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

async function parseClipViaInnerTube(clipUrl) {
  console.log('[InnerTube] Attempting to parse clip via InnerTube API');
  
  const clipResponse = await fetch(clipUrl, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    }
  });
  
  const clipHtml = await clipResponse.text();
  
  const videoIdMatch = clipHtml.match(/"videoId":"([^"]+)"/);
  if (!videoIdMatch) {
    throw new Error('Could not extract video ID from clip page');
  }
  
  const videoId = videoIdMatch[1];
  console.log(`[InnerTube] Extracted video ID: ${videoId}`);
  
  let startTimeMs = 0;
  let endTimeMs = 0;
  
  const clipConfigMatch = clipHtml.match(/"clipConfig":\{"startTimeMs":"(\d+)","endTimeMs":"(\d+)"/);
  if (clipConfigMatch) {
    startTimeMs = parseInt(clipConfigMatch[1]);
    endTimeMs = parseInt(clipConfigMatch[2]);
    console.log(`[InnerTube] Extracted clip times from page: ${startTimeMs}-${endTimeMs}ms`);
  }

  if (startTimeMs && endTimeMs) {
    return {
      videoId,
      startTimeMs,
      endTimeMs,
      fullVideoUrl: `https://www.youtube.com/watch?v=${videoId}`
    };
  }

  throw new Error('Could not extract clip times from InnerTube');
}

async function tryParseYouTubeClip(clipUrl, proxy = null) {
  return new Promise((resolve, reject) => {
    const ytdlpArgs = [
      ...getCookiesArgs(),
      '--dump-json',
      '--no-download',
      '-t', 'sleep',
      clipUrl
    ];

    if (proxy) {
      ytdlpArgs.push('--proxy', proxy.url);
      console.log(`[Clip] Using Mullvad proxy: ${proxy.server}`);
    }

    const ytdlp = spawn('yt-dlp', ytdlpArgs);
    let output = '';
    let errorOutput = '';

    ytdlp.stdout.on('data', (data) => {
      output += data.toString();
    });

    ytdlp.stderr.on('data', (data) => {
      errorOutput += data.toString();
    });

    ytdlp.on('close', (code) => {
      if (code !== 0) {
        console.error('[Clip] yt-dlp failed:', errorOutput);
        reject(new Error(`Failed to parse clip: ${errorOutput.slice(0, 200)}`));
        return;
      }

      try {
        const json = JSON.parse(output);
        
        let videoId = json.id;
        let startTimeMs = 0;
        let endTimeMs = 0;

        if (json.section_start !== undefined && json.section_end !== undefined) {
          startTimeMs = json.section_start * 1000;
          endTimeMs = json.section_end * 1000;
        } else if (json.start_time !== undefined && json.end_time !== undefined) {
          startTimeMs = json.start_time * 1000;
          endTimeMs = json.end_time * 1000;
        }

        if (!startTimeMs || !endTimeMs) {
          const clipIdMatch = clipUrl.match(/\/clip\/([^/?#]+)/);
          if (clipIdMatch && json.clip_start !== undefined && json.clip_end !== undefined) {
            startTimeMs = json.clip_start * 1000;
            endTimeMs = json.clip_end * 1000;
          }
        }

        if (!startTimeMs || !endTimeMs) {
          reject(new Error('Could not extract clip timestamps'));
          return;
        }

        resolve({
          videoId,
          startTimeMs,
          endTimeMs,
          fullVideoUrl: `https://www.youtube.com/watch?v=${videoId}`
        });
      } catch (parseErr) {
        console.error('[Clip] Parse error:', parseErr.message);
        reject(new Error(parseErr.message));
      }
    });

    ytdlp.on('error', (err) => {
      console.error('[Clip] yt-dlp spawn error:', err.message);
      reject(new Error(`Failed to spawn yt-dlp: ${err.message}`));
    });
  });
}

async function parseYouTubeClip(clipUrl) {
  console.log(`[Clip] Parsing YouTube clip: ${clipUrl}`);

  try {
    console.log('[Clip] Trying InnerTube API first...');
    const innerTubeResult = await parseClipViaInnerTube(clipUrl);
    return innerTubeResult;
  } catch (innerTubeError) {
    console.error(`[Clip] InnerTube failed: ${innerTubeError.message}, falling back to yt-dlp`);
  }

  const maxRetries = 3;
  const triedServers = [];
  let lastError = null;

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      const proxy = getRandomMullvadProxy(triedServers);
      if (proxy) {
        triedServers.push(proxy.server);
      }

      console.log(`[Clip] Attempt ${attempt}/${maxRetries}`);
      const result = await tryParseYouTubeClip(clipUrl, proxy);
      return result;
    } catch (error) {
      lastError = error;
      console.error(`[Clip] Attempt ${attempt} failed: ${error.message}`);

      if (attempt < maxRetries) {
        console.log(`[Clip] Retrying with different server...`);
      }
    }
  }

  throw lastError;
}

module.exports = {
  MULLVAD_SERVERS,
  getRandomMullvadProxy,
  parseClipViaInnerTube,
  tryParseYouTubeClip,
  parseYouTubeClip
};
