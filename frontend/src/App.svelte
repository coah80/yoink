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
    '/': 'yoink.tools - Free Video Downloader, Converter & Compressor',
    '/convert': 'yoink.tools - Video & Audio Converter',
    '/compress': 'yoink.tools - Video Compressor',
    '/trim': 'yoink.tools - Trim & Crop Videos',
    '/transcribe': 'yoink.tools - Transcripts, Subtitles & Captions',
    '/settings': 'yoink.tools - Settings',
    '/privacy': 'yoink.tools - Privacy',
    '/download': 'yoink.tools - Download',
    '/updates': 'yoink.tools - Updates',
  };

  let currentPath = $derived($path);
  let CurrentPage = $derived(routes[currentPath] || (currentPath.startsWith('/download/') ? Download : NotFound));

  $effect(() => {
    document.title = pageTitles[currentPath] || (currentPath.startsWith('/download/') ? pageTitles['/download'] : 'yoink.tools - Page Not Found');
  });

  function handleClick(e) {
    const a = e.target.closest('a');
    if (!a) return;
    const href = a.getAttribute('href');
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (!href || !href.startsWith('/') || href.startsWith('//') || a.target === '_blank' || a.hasAttribute('download')) return;
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
