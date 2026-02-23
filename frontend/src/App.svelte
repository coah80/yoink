<script>
  import { onMount } from 'svelte';
  import { path } from './lib/router.js';
  import ToastContainer from './components/ui/ToastContainer.svelte';
  import BottomNav from './components/layout/BottomNav.svelte';
  import { initSession } from './stores/session.js';

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

  onMount(() => {
    initSession();

    // One-time cleanup of old analytics localStorage
    localStorage.removeItem('yoink_trackingId');
    localStorage.removeItem('yoink_last_daily_report');

    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch(() => {});
    }
  });
</script>

<CurrentPage />
<BottomNav />
<ToastContainer />
