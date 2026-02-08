export const splashTexts = [
  { text: 'yoink it, go on...' },
  { text: 'not cobalt' },
  { text: 'im bored' },
  { text: 'have you yoinked it today?' },
  { text: 'download helium browser', link: 'https://helium.computer/', classes: ['clickable'] },
  { text: "dont like it? use cliply (dont)" },
  { text: 'the biggest yt-dlp wrapper of them all' },
  { text: 'awesome name, right?' },
  { text: 'get yoinked!' },
  { text: 'funny message here' },
  { text: 'SHOUTOUT TO COAH' },
  { text: 'new feature: yoink!' },
  { text: 'new feature: ai download' },
  { text: 'welcome to yoink.tools' },
  { text: "feelin' yoinky?" },
  { text: 'im parched' },
  { text: 'stay hydrated!' },
  { text: 'computers are cool' },
  { text: 'missing assignments?' },
  { text: 'procrastinate with yoink' },
  { text: 'newgrounds.com', link: 'https://newgrounds.com', classes: ['clickable'] },
  { text: 'bring back flash!' },
  { text: 'FUCK ICE' },
  { text: 'post this one to twitter' },
  { text: 'google i need yoinks! NOW!!!!!' },
  { text: "im feeling fancy today...", classes: ['fancy'] },
  { text: 'powered by yt-dlp' },
  { text: 'hello world!' },
  { text: '1000+ sites!', link: 'https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md', classes: ['clickable'] },
  { text: "yes, that site works. even the one you're thinking of." },
];

export const pornSites = [
  '4tube', 'alphaporno', 'beeg', 'behindkink', 'bongacams', 'cam4', 'cammodels', 'camsoda',
  'chaturbate', 'drtuber', 'empflix', 'eporner', 'erocast', 'eroprofile', 'fux', 'hellporno',
  'hotmovs', 'iceporn', 'lovehomeporn', 'manyvids', 'motherless', 'moviefap', 'noodlemagazine',
  'nuvid', 'peekvideos', 'peekvids', 'playvids', 'porn', 'pornbox', 'pornerbros', 'pornflip',
  'pornhub', 'pornotube', 'pornovoisines', 'pornoxo', 'porntop', 'porntube', 'redtube',
  'rule34video', 'slutload', 'spankbang', 'stripchat', 'sunporno', 'thisvid', 'tnaflix',
  'tube8', 'txxx', 'xhamster', 'xnxx', 'xvideos', 'xxxymovies', 'youjizz', 'youporn',
  'zenporn', 'xtube', 'erome', 'hentai', 'hclips', 'gotporn', 'fuq', 'ashemaletube',
];

export function isPornSite(url) {
  try {
    const hostname = new URL(url).hostname.toLowerCase().replace('www.', '');
    return pornSites.some((site) => hostname.includes(site));
  } catch {
    return false;
  }
}

export const DEFAULT_SETTINGS = {
  quality: '1080p',
  codec: 'h264',
  container: 'mp4',
  audioFormat: 'mp3',
  audioBitrate: '320',
  filenameStyle: 'basic',
  playlistPreference: null,
  twitterGifs: true,
  analytics: true,
};

export const ACTIVE_STAGES = [
  'starting', 'downloading', 'processing', 'remuxing', 'sending', 'zipping', 'playlist-info', 'reconnecting',
];
