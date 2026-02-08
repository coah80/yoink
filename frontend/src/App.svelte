<script>
  import { onMount, onDestroy } from 'svelte';
  import { path } from './lib/router.js';
  import ToastContainer from './components/ui/ToastContainer.svelte';
  import BottomNav from './components/layout/BottomNav.svelte';
  import { initSession } from './stores/session.js';
  import { reportDailyUser, reportPageView } from './stores/analytics.js';
  import { banner, startBannerPolling, stopBannerPolling } from './stores/banner.js';

  import Home from './pages/Home.svelte';
  import Settings from './pages/Settings.svelte';
  import Convert from './pages/Convert.svelte';
  import Compress from './pages/Compress.svelte';
  import Privacy from './pages/Privacy.svelte';
  import Download from './pages/Download.svelte';
  import Share from './pages/Share.svelte';
  import Admin from './pages/Admin.svelte';
  import NotFound from './pages/NotFound.svelte';

  const routes = {
    '/': Home,
    '/settings': Settings,
    '/convert': Convert,
    '/compress': Compress,
    '/privacy': Privacy,
    '/download': Download,
    '/share': Share,
    '/admin': Admin,
  };

  let currentPath = $derived($path);
  let CurrentPage = $derived(routes[currentPath] || NotFound);
  let isAdmin = $derived(currentPath === '/admin');
  let currentBanner = $derived($banner);

  $effect(() => {
    reportPageView(currentPath);
  });

  function dismissBanner() {
    banner.set(null);
  }

  onMount(() => {
    initSession();
    reportDailyUser();
    startBannerPolling();

    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch(() => {});
    }
  });

  onDestroy(() => {
    stopBannerPolling();
  });
</script>

{#if currentBanner && !isAdmin}
  <div class="site-banner banner-{currentBanner.type}">
    <span class="banner-msg">{currentBanner.message}</span>
    <button class="banner-dismiss" onclick={dismissBanner}>
      <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
    </button>
  </div>
{/if}

<CurrentPage />
{#if !isAdmin}
  <BottomNav />
{/if}
<ToastContainer />

<style>
  .site-banner {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 10px 16px;
    font-size: 0.85rem;
    font-weight: 500;
    text-align: center;
    position: relative;
    z-index: 200;
  }

  .banner-info {
    background: #1e3a5f;
    color: #93c5fd;
    border-bottom: 1px solid #2563eb;
  }

  .banner-warning {
    background: #3d2e00;
    color: #fcd34d;
    border-bottom: 1px solid #f59e0b;
  }

  .banner-error {
    background: #3d0f0f;
    color: #fca5a5;
    border-bottom: 1px solid #ef4444;
  }

  .banner-maintenance {
    background: #1e1b4b;
    color: #c4b5fd;
    border-bottom: 1px solid #8b5cf6;
  }

  .banner-msg {
    flex: 1;
  }

  .banner-dismiss {
    background: none;
    border: none;
    color: inherit;
    opacity: 0.6;
    cursor: pointer;
    padding: 2px;
    display: flex;
    flex-shrink: 0;
  }

  .banner-dismiss:hover {
    opacity: 1;
  }
</style>
