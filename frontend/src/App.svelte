<script>
  import { onMount } from 'svelte';
  import { path, navigate } from './lib/router.js';
  import ToastContainer from './components/ui/ToastContainer.svelte';
  import BottomNav from './components/layout/BottomNav.svelte';
  import { initSession, resumeHeartbeat } from './stores/session.js';
  import { queue } from './stores/queue.js';

  import Home from './pages/Home.svelte';
  import Settings from './pages/Settings.svelte';
  import Convert from './pages/Convert.svelte';
  import Compress from './pages/Compress.svelte';
  import Trim from './pages/Trim.svelte';
  import Transcribe from './pages/Transcribe.svelte';
  import Privacy from './pages/Privacy.svelte';
  import Download from './pages/Download.svelte';
  import Share from './pages/Share.svelte';
  import Updates from './pages/Updates.svelte';
  import NotFound from './pages/NotFound.svelte';
  import UpdateBanner from './components/ui/UpdateBanner.svelte';

  const routes = {
    '/': Home,
    '/settings': Settings,
    '/convert': Convert,
    '/compress': Compress,
    '/trim': Trim,
    '/transcribe': Transcribe,
    '/privacy': Privacy,
    '/download': Download,
    '/share': Share,
    '/updates': Updates,
  };

  const pageTitles = {
    '/': [
      'yoink - download youtube videos, audio, and clips for free',
      'yoink - free video downloader for youtube, twitter, tiktok',
      'yoink - save videos from any site in seconds',
      'yoink - fast youtube downloader, no ads, no signup',
      'yoink - download videos and audio from 1000+ sites',
    ],
    '/convert': [
      'yoink - convert videos to mp4, webm, mp3, and more',
      'yoink - free online video and audio converter',
      'yoink - convert any video format instantly',
      'yoink - turn videos into mp3, mp4, wav, flac, and more',
      'yoink - fast video converter, no file size limit',
    ],
    '/compress': [
      'yoink - compress videos to any file size for free',
      'yoink - shrink video files without losing quality',
      'yoink - free video compressor for discord, email, and more',
      'yoink - make videos smaller in seconds',
      'yoink - reduce video file size online, fast and free',
    ],
    '/trim': [
      'yoink - trim and crop videos online for free',
      'yoink - cut videos to any length and aspect ratio',
      'yoink - clip videos for reels, shorts, and tiktok',
      'yoink - free video trimmer with crop and preview',
      'yoink - trim videos to 9:16, 16:9, 1:1, and more',
    ],
    '/transcribe': [
      'yoink - transcribe videos and audio to text for free',
      'yoink - free video transcription with subtitles and captions',
      'yoink - generate subtitles from any video or audio file',
      'yoink - turn speech into text, srt, or burned-in captions',
      'yoink - free audio transcription, no account needed',
    ],
    '/settings': [
      'yoink - settings',
    ],
    '/privacy': [
      'yoink - privacy',
    ],
    '/updates': [
      'yoink - updates and changelog',
    ],
  };

  function pickTitle(route) {
    const titles = pageTitles[route];
    if (!titles) return 'yoink.tools';
    return titles[Math.floor(Math.random() * titles.length)];
  }

  let currentPath = $derived($path);
  let CurrentPage = $derived(routes[currentPath] || NotFound);

  $effect(() => {
    document.title = pickTitle(currentPath);
  });

  function handleClick(e) {
    const a = e.target.closest('a');
    if (!a) return;
    const href = a.getAttribute('href');
    if (!href || !href.startsWith('/') || a.target === '_blank') return;
    e.preventDefault();
    navigate(href);
  }

  onMount(() => {
    initSession();

    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch(() => {});
    }

    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        queue.reconnectOnResume();
        resumeHeartbeat();
      }
    });
  });
</script>

<svelte:body onclick={handleClick} />

<CurrentPage />
<BottomNav />
<ToastContainer />
<UpdateBanner />
