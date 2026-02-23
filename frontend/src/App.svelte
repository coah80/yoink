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
  import Privacy from './pages/Privacy.svelte';
  import Download from './pages/Download.svelte';
  import Share from './pages/Share.svelte';
  import NotFound from './pages/NotFound.svelte';

  const routes = {
    '/': Home,
    '/settings': Settings,
    '/convert': Convert,
    '/compress': Compress,
    '/privacy': Privacy,
    '/download': Download,
    '/share': Share,
  };

  let currentPath = $derived($path);
  let CurrentPage = $derived(routes[currentPath] || NotFound);

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
