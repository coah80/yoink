<script>
  import { onMount } from 'svelte';
  import Header from '../components/layout/Header.svelte';
  import Footer from '../components/layout/Footer.svelte';
  import QueueToggle from '../components/queue/QueueToggle.svelte';
  import Spinner from '../components/ui/Spinner.svelte';
  import { queue } from '../stores/queue.js';
  import { settings } from '../stores/settings.js';
  import { addToast } from '../stores/toast.js';
  import { apiBase, fetchJson } from '../lib/api.js';
  import { normalizeUrl, hasPlaylistParam, generateProgressId, isYouTubeUrl } from '../lib/utils.js';
  import { splashTexts, isPornSite } from '../lib/constants.js';
  import { triggerIframeDownload } from '../lib/download.js';
  import { query, navigate } from '../lib/router.js';
  import { changelog } from '../lib/changelog.js';

  let urlValue = $state('');
  let currentFormat = $state('auto');
  let loading = $state(false);
  let statusType = $state(null);
  let statusMessage = $state('');
  let showPlaylistModal = $state(false);
  let showBatchModal = $state(false);
  let yoinkDropdownOpen = $state(false);
  let batchText = $state('');
  let playlistResolve = $state(null);
  let showCarouselModal = $state(false);
  let carouselResolve = $state(null);

  let splash = $state(null);
  let splashEl;
  let urlInput;

  let batchUrlCount = $derived.by(() => {
    return batchText.split('\n').filter(line => {
      const trimmed = line.trim();
      if (!trimmed) return false;
      try { new URL(trimmed); return true; } catch { return false; }
    }).length;
  });

  const formats = [
    { id: 'auto', label: 'auto', icon: 'bolt' },
    { id: 'video', label: 'video', icon: 'video' },
    { id: 'audio', label: 'audio', icon: 'audio' },
    { id: 'images', label: 'images', icon: 'image' },
  ];

  function initSplash() {
    splash = splashTexts[Math.floor(Math.random() * splashTexts.length)];
  }

  function handleSplashClick() {
    if (splash?.link) window.open(splash.link, '_blank');
  }

  function checkSharedUrl() {
    let sharedUrl = null;

    // read from localStorage (set by Share.svelte)
    try {
      const raw = localStorage.getItem('yoink_shared_url');
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Date.now() - parsed.timestamp < 5 * 60 * 1000) {
          sharedUrl = parsed.url;
        }
        localStorage.removeItem('yoink_shared_url');
      }
    } catch {
      localStorage.removeItem('yoink_shared_url');
    }

    // Detect PWA share target: browser loads /share?url=...
    if (!sharedUrl && window.location.pathname === '/share') {
      const params = new URLSearchParams(window.location.search);
      const raw = params.get('url') || params.get('text') || '';
      const urlMatch = raw.match(/https?:\/\/[^\s]+/);
      if (urlMatch) {
        sharedUrl = urlMatch[0].replace(/[.,!?;:)\]}>]+$/, '');
      }
      history.replaceState(null, '', '/');
    }

    if (sharedUrl) {
      urlValue = sharedUrl;
      setTimeout(() => handleYoink(), 150);
    }
  }

  onMount(() => {
    initSplash();
    checkSharedUrl();
  });

  async function handlePaste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        urlValue = text.trim();
        urlInput?.focus();
      }
    } catch {
      addToast('Could not read clipboard', 'error');
    }
  }

  function setStatus(type, msg) {
    statusType = type;
    statusMessage = msg;
  }

  function clearStatus() {
    statusType = null;
    statusMessage = '';
  }

  function askPlaylistChoice() {
    return new Promise((resolve) => {
      rememberChoice = false;
      playlistResolve = resolve;
      showPlaylistModal = true;
    });
  }

  function handlePlaylistChoice(choice, remember) {
    if (remember) {
      settings.setSetting('playlistPreference', choice);
    }
    if (playlistResolve) playlistResolve(choice);
    playlistResolve = null;
    showPlaylistModal = false;
  }

  function dismissPlaylistModal() {
    if (playlistResolve) playlistResolve(null);
    playlistResolve = null;
    showPlaylistModal = false;
  }

  function askCarouselChoice() {
    return new Promise((resolve) => {
      rememberChoice = false;
      carouselResolve = resolve;
      showCarouselModal = true;
    });
  }

  function handleCarouselChoice(choice, remember) {
    if (remember) {
      settings.setSetting('tiktokCarouselPreference', choice);
    }
    if (carouselResolve) carouselResolve(choice);
    carouselResolve = null;
    showCarouselModal = false;
  }

  function dismissCarouselModal() {
    if (carouselResolve) carouselResolve(null);
    carouselResolve = null;
    showCarouselModal = false;
  }

  async function resolveCarouselFormat(metadata, s) {
    if (!metadata.isTikTokCarousel || !metadata.hasAudio) return 'skip';
    if (s.tiktokCarouselPreference === 'photos') return 'photos';
    if (s.tiktokCarouselPreference === 'video') return 'video';
    return await askCarouselChoice();
  }

  async function addUrlToQueue(rawUrl, startImmediately = false) {
    const url = normalizeUrl(rawUrl);
    const format = currentFormat === 'auto' ? 'video' : currentFormat;
    let queueFormat = format;

    if (format === 'images' && isYouTubeUrl(url)) {
      addToast('YouTube doesn\'t support image downloads', 'error');
      return null;
    }

    const progressId = generateProgressId();
    const s = $settings;

    let downloadPlaylist = false;
    if (format !== 'images' && hasPlaylistParam(url)) {
      if (s.playlistPreference === 'video') {
        downloadPlaylist = false;
      } else if (s.playlistPreference === 'playlist') {
        downloadPlaylist = true;
      } else {
        const choice = await askPlaylistChoice();
        if (choice === null) return null;
        downloadPlaylist = choice === 'playlist';
      }
    }

    let title = 'Loading...';
    try {
      if (format === 'images') {
        const metaRes = await fetch(`${apiBase()}/api/gallery/metadata?url=${encodeURIComponent(url)}`);
        const ct = metaRes.headers.get('content-type');
        if (metaRes.ok && ct?.includes('application/json')) {
          const metadata = await metaRes.json();
          title = metadata.title || 'Image';
          const carouselChoice = await resolveCarouselFormat(metadata, s);
          if (carouselChoice === null) return null;
          if (carouselChoice === 'video') queueFormat = 'slideshow';
        } else {
          title = url.substring(0, 50);
        }
      } else {
        const metaRes = await fetch(`${apiBase()}/api/metadata?url=${encodeURIComponent(url)}&playlist=${downloadPlaylist}`);
        const ct = metaRes.headers.get('content-type');
        if (metaRes.ok && ct?.includes('application/json')) {
          const metadata = await metaRes.json();
          title = metadata.title || 'Unknown';
          if (metadata.isGallery === true) {
            queueFormat = 'images';
          }
          const carouselChoice = await resolveCarouselFormat(metadata, s);
          if (carouselChoice === null) return null;
          if (carouselChoice === 'video') queueFormat = 'slideshow';
          if (metadata.usingCookies) {
            addToast('Using intervaled requests to stay under the radar (this may be slower)', 'warning', 5000);
          }
          if (metadata.clipNote) {
            const isWarning = metadata.clipNote.toLowerCase().includes('warning') || metadata.originalDuration > 600;
            addToast(metadata.clipNote, isWarning ? 'warning' : 'info', 6000);
          }
        } else if (!metaRes.ok && ct?.includes('application/json')) {
          const err = await metaRes.json();
          if (err.clipUnsupported) {
            addToast('Could not parse YouTube clip. Try using the full video URL instead.', 'error', 8000);
            return null;
          }
          throw new Error(err.error || 'Metadata fetch failed');
        } else {
          title = url.substring(0, 50);
        }
      }
    } catch (e) {
      title = url.substring(0, 50);
    }

    const queueItem = {
      id: progressId,
      title,
      url,
      format: queueFormat,
      stage: startImmediately ? 'starting' : 'queued',
      status: startImmediately ? 'initializing...' : 'in queue',
      progress: 0,
      isPlaylist: downloadPlaylist,
      formatDisplay: queueFormat === 'slideshow' ? 'slideshow' : (queueFormat === 'images' ? 'images' : (queueFormat === 'audio' ? s.audioFormat : `${s.quality} ${s.container}`)),
      startTime: startImmediately ? Date.now() : null,
      failedVideos: [],
    };

    queue.add(queueItem);

    if (startImmediately) {
      queue.startDownload(queueItem);
    }

    return queueItem;
  }

  async function handleYoink() {
    const rawUrl = urlValue.trim();
    if (!rawUrl) {
      addToast('Please enter a URL', 'error');
      return;
    }

    const url = normalizeUrl(rawUrl);
    try { new URL(url); } catch {
      addToast('Please enter a valid URL', 'error');
      return;
    }

    if (isPornSite(url)) {
      splash = { text: 'okay dude are you just gonna download porn' };
    }

    if (currentFormat === 'images' && isYouTubeUrl(url)) {
      addToast('YouTube doesn\'t support image downloads, use video or audio', 'error');
      return;
    }

    loading = true;
    const format = currentFormat === 'auto' ? 'video' : currentFormat;
    const s = $settings;

    let downloadPlaylist = false;
    if (format !== 'images' && hasPlaylistParam(url)) {
      if (s.playlistPreference === 'video') {
        downloadPlaylist = false;
      } else if (s.playlistPreference === 'playlist') {
        downloadPlaylist = true;
      } else {
        const choice = await askPlaylistChoice();
        if (choice === null) {
          loading = false;
          return;
        }
        downloadPlaylist = choice === 'playlist';
      }
    }

    const progressId = generateProgressId();

    try {
      setStatus('loading', format === 'images' ? 'fetching gallery info...' : 'fetching video info...');

      let metadata;
      if (format === 'images') {
        const metaRes = await fetch(`${apiBase()}/api/gallery/metadata?url=${encodeURIComponent(url)}`);
        const ct = metaRes.headers.get('content-type');
        if (!metaRes.ok) {
          let errorMsg = 'Failed to fetch gallery info';
          if (ct?.includes('application/json')) {
            const err = await metaRes.json();
            errorMsg = err.error || errorMsg;
          } else {
            const text = await metaRes.text();
            errorMsg = text.includes('Cloudflare') || text.includes('403')
              ? 'Access denied (VPN blocked?)' : `Server error: ${metaRes.status}`;
          }
          throw new Error(errorMsg);
        }
        metadata = await metaRes.json();
        metadata.title = metadata.title || 'Image';
      } else {
        const metaRes = await fetch(`${apiBase()}/api/metadata?url=${encodeURIComponent(url)}&playlist=${downloadPlaylist}`);
        const ct = metaRes.headers.get('content-type');
        if (!metaRes.ok) {
          let errorMsg = 'Failed to fetch metadata';
          if (ct?.includes('application/json')) {
            const err = await metaRes.json();
            if (err.clipUnsupported) {
              setStatus('error', 'Could not parse YouTube clip');
              addToast('Could not parse YouTube clip. Try using the full video URL instead.', 'error', 8000);
              loading = false;
              return;
            }
            errorMsg = err.error || errorMsg;
          } else {
            const text = await metaRes.text();
            errorMsg = `Server error ${metaRes.status} (likely VPN blocked)`;
          }
          throw new Error(errorMsg);
        }
        metadata = await metaRes.json();
      }

      const title = metadata.title;
      const isPlaylist = metadata.isPlaylist && downloadPlaylist;
      const isGallery = format === 'images' || metadata.isGallery === true;
      let actualFormat = metadata.isGallery === true ? 'images' : format;
      const videoCount = metadata.videoCount || metadata.imageCount || 0;

      // tiktok carousel detection
      const carouselChoice = await resolveCarouselFormat(metadata, s);
      if (carouselChoice === null) { loading = false; return; }
      if (carouselChoice === 'video') actualFormat = 'slideshow';

      if (metadata.usingCookies) {
        addToast('Using intervaled requests to stay under the radar (this may be slower)', 'warning', 5000);
      }

      const queueItem = {
        id: progressId,
        title,
        url,
        format: actualFormat,
        stage: 'starting',
        status: 'initializing...',
        progress: 0,
        isPlaylist,
        isGallery,
        videoCount,
        currentVideo: 0,
        currentVideoTitle: '',
        formatDisplay: actualFormat === 'slideshow' ? 'slideshow' : (isGallery ? 'images' : (actualFormat === 'audio' ? s.audioFormat : `${s.quality} ${s.container}`)),
        failedVideos: [],
        startTime: Date.now(),
      };

      queue.add(queueItem);
      queue.startDownload(queueItem);

      urlValue = '';
      clearStatus();
    } catch (err) {
      setStatus('error', err.message || 'Download failed');
      addToast(err.message || 'Download failed', 'error');
    } finally {
      loading = false;
    }
  }

  function handleKeydown(e) {
    if (e.key === 'Enter') handleYoink();
  }

  async function handleAddToQueue() {
    yoinkDropdownOpen = false;
    const url = urlValue.trim();
    if (!url) {
      addToast('Please enter a URL', 'error');
      return;
    }
    try { new URL(url); } catch {
      addToast('Please enter a valid URL', 'error');
      return;
    }
    await addUrlToQueue(url);
    urlValue = '';
  }

  function openBatchModal() {
    yoinkDropdownOpen = false;
    batchText = '';
    showBatchModal = true;
  }

  async function handleBatchAdd() {
    const urls = batchText.split('\n')
      .map(l => l.trim())
      .filter(l => { try { new URL(l); return true; } catch { return false; } });

    if (urls.length === 0) {
      addToast('No valid URLs found', 'error');
      return;
    }

    showBatchModal = false;
    addToast(`Adding ${urls.length} URLs to queue...`, 'info');
    for (const url of urls) {
      await addUrlToQueue(url, false);
    }
    addToast(`${urls.length} items added to queue`, 'success');
  }

  let rememberChoice = $state(false);

  let showUpdates = $derived('updates' in $query);
  let updatesVisible = $state(false);
  let updatesClosing = $state(false);
  let closeTimer = null;

  $effect(() => {
    if (showUpdates) {
      updatesVisible = true;
      updatesClosing = false;
      if (closeTimer) { clearTimeout(closeTimer); closeTimer = null; }
    }
  });

  function closeUpdates() {
    if (updatesClosing) return;
    updatesClosing = true;
    navigate('/');
    closeTimer = setTimeout(() => {
      updatesVisible = false;
      updatesClosing = false;
      closeTimer = null;
    }, 350);
  }
</script>

<Header>
  {#snippet extraContent()}
    <QueueToggle />
  {/snippet}
</Header>

<main>
  <div class="branding">
    <h1>yoink<span class="brand-tld">.tools</span></h1>
    {#if splash}
      <p
        class="splash"
        class:fancy={splash.classes?.includes('fancy')}
        class:clickable={splash.classes?.includes('clickable')}
        class:rainbow={splash.classes?.includes('rainbow')}
        onclick={handleSplashClick}
        style={splash.link ? 'cursor: pointer;' : ''}
      >
        {splash.text}
      </p>
    {/if}
  </div>

  <div class="input-container">
    <div class="url-input-wrapper">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
        <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
      </svg>
      <input
        bind:this={urlInput}
        type="text"
        class="url-input"
        placeholder="paste the link here..."
        autocomplete="off"
        spellcheck="false"
        bind:value={urlValue}
        onkeydown={handleKeydown}
        onfocus={clearStatus}
      />
      {#if urlValue}
        <button class="clear-btn" type="button" title="Clear" onclick={() => { urlValue = ''; urlInput?.focus(); }}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      {:else}
        <button class="paste-btn" type="button" title="Paste from clipboard" onclick={handlePaste}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
          </svg>
        </button>
      {/if}
    </div>

    <div class="format-section">
      <div class="segmented-control">
        {#each formats as fmt}
          <button
            class="segment"
            class:active={currentFormat === fmt.id}
            onclick={() => currentFormat = fmt.id}
          >
            {#if fmt.icon === 'bolt'}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
              </svg>
            {:else if fmt.icon === 'video'}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="2" y="2" width="20" height="20" rx="2.18" ry="2.18"></rect>
                <line x1="7" y1="2" x2="7" y2="22"></line>
                <line x1="17" y1="2" x2="17" y2="22"></line>
                <line x1="2" y1="12" x2="22" y2="12"></line>
                <line x1="2" y1="7" x2="7" y2="7"></line>
                <line x1="2" y1="17" x2="7" y2="17"></line>
                <line x1="17" y1="17" x2="22" y2="17"></line>
                <line x1="17" y1="7" x2="22" y2="7"></line>
              </svg>
            {:else if fmt.icon === 'audio'}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M9 18V5l12-2v13"></path>
                <circle cx="6" cy="18" r="3"></circle>
                <circle cx="18" cy="16" r="3"></circle>
              </svg>
            {:else if fmt.icon === 'image'}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <circle cx="8.5" cy="8.5" r="1.5"></circle>
                <polyline points="21 15 16 10 5 21"></polyline>
              </svg>
            {/if}
            <span>{fmt.label}</span>
          </button>
        {/each}
      </div>

      <div class="yoink-btn-wrapper" class:open={yoinkDropdownOpen}>
        <button class="yoink-btn" type="button" onclick={handleYoink} disabled={loading}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
          {loading ? 'loading...' : 'yoink'}
        </button>
        <button class="yoink-dropdown-toggle" type="button" onclick={(e) => { e.stopPropagation(); yoinkDropdownOpen = !yoinkDropdownOpen; }}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 9 12 15 18 9"></polyline>
          </svg>
        </button>
        {#if yoinkDropdownOpen}
          <div class="yoink-dropdown">
            <button class="yoink-dropdown-item" type="button" onclick={handleAddToQueue}>
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="12" y1="5" x2="12" y2="19"></line>
                <line x1="5" y1="12" x2="19" y2="12"></line>
              </svg>
              add to queue
            </button>
            <button class="yoink-dropdown-item" type="button" onclick={openBatchModal}>
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="8" y1="6" x2="21" y2="6"></line>
                <line x1="8" y1="12" x2="21" y2="12"></line>
                <line x1="8" y1="18" x2="21" y2="18"></line>
                <line x1="3" y1="6" x2="3.01" y2="6"></line>
                <line x1="3" y1="12" x2="3.01" y2="12"></line>
                <line x1="3" y1="18" x2="3.01" y2="18"></line>
              </svg>
              batch add urls
            </button>
          </div>
        {/if}
      </div>
    </div>

    {#if statusType}
      <div class="status {statusType}">
        {#if statusType === 'loading'}
          <Spinner size={16} />
        {/if}
        {statusMessage}
      </div>
    {/if}
  </div>

</main>

<Footer />

{#if showPlaylistModal}
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) dismissPlaylistModal(); }}>
    <div class="modal">
      <div class="modal-title">playlist detected!</div>
      <div class="modal-subtitle">do you want to download just this video, or the whole playlist?</div>
      <div class="modal-buttons">
        <button class="modal-btn" onclick={() => handlePlaylistChoice('video', rememberChoice)}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="5 3 19 12 5 21 5 3"></polygon>
          </svg>
          video
        </button>
        <button class="modal-btn primary" onclick={() => handlePlaylistChoice('playlist', rememberChoice)}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="8" y1="6" x2="21" y2="6"></line>
            <line x1="8" y1="12" x2="21" y2="12"></line>
            <line x1="8" y1="18" x2="21" y2="18"></line>
            <line x1="3" y1="6" x2="3.01" y2="6"></line>
            <line x1="3" y1="12" x2="3.01" y2="12"></line>
            <line x1="3" y1="18" x2="3.01" y2="18"></line>
          </svg>
          playlist
        </button>
      </div>
      <label class="modal-toggle">
        <input type="checkbox" bind:checked={rememberChoice} />
        <span class="modal-toggle-switch"></span>
        <span class="modal-toggle-label">remember this choice</span>
      </label>
      <div class="modal-hint">this can be changed in <a href="/settings">settings</a>!</div>
    </div>
  </div>
{/if}

{#if showCarouselModal}
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) dismissCarouselModal(); }}>
    <div class="modal">
      <div class="modal-title">tiktok photo post detected!</div>
      <div class="modal-subtitle">download just the photos, or as a video with the audio?</div>
      <div class="modal-buttons">
        <button class="modal-btn" onclick={() => handleCarouselChoice('photos', rememberChoice)}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
            <circle cx="8.5" cy="8.5" r="1.5"></circle>
            <polyline points="21 15 16 10 5 21"></polyline>
          </svg>
          photos
        </button>
        <button class="modal-btn primary" onclick={() => handleCarouselChoice('video', rememberChoice)}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="5 3 19 12 5 21 5 3"></polygon>
          </svg>
          video (includes audio)
        </button>
      </div>
      <label class="modal-toggle">
        <input type="checkbox" bind:checked={rememberChoice} />
        <span class="modal-toggle-switch"></span>
        <span class="modal-toggle-label">remember this choice</span>
      </label>
      <div class="modal-hint">this can be changed in <a href="/settings">settings</a>!</div>
    </div>
  </div>
{/if}

{#if showBatchModal}
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) showBatchModal = false; }}>
    <div class="modal batch-modal">
      <div class="modal-title">batch add urls</div>
      <div class="modal-subtitle">paste multiple URLs, one per line</div>
      <textarea
        class="batch-textarea"
        bind:value={batchText}
        placeholder={"https://youtube.com/watch?v=...\nhttps://youtube.com/watch?v=...\nhttps://youtube.com/watch?v=..."}
      ></textarea>
      <div class="batch-count">
        <span>{batchUrlCount}</span> urls detected
      </div>
      <div class="modal-buttons">
        <button class="modal-btn" onclick={() => showBatchModal = false}>cancel</button>
        <button class="modal-btn primary" onclick={handleBatchAdd}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          add to queue
        </button>
      </div>
    </div>
  </div>
{/if}

{#if updatesVisible}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="updates-overlay" class:closing={updatesClosing} role="dialog" aria-modal="true" aria-label="Updates" onclick={(e) => { if (e.target === e.currentTarget) closeUpdates(); }} onkeydown={(e) => { if (e.key === 'Escape') closeUpdates(); }}>
    <div class="updates-scroll">
      <div class="updates-header">
        <span class="updates-pill">updates</span>
        <button class="updates-close" onclick={closeUpdates} aria-label="Close">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      </div>

      {#each changelog as entry}
        <article class="update-card">
          <div class="update-meta">
            <span class="update-version">{entry.version}</span>
            <span class="update-date">{entry.date}</span>
          </div>
          <h2 class="update-title">{entry.title}</h2>
          <img class="update-image" src={entry.image} alt={entry.title} />
          <div class="update-content">
            {#each entry.content.split('\n\n') as block}
              {#if block.startsWith('## ')}
                <h3>{block.replace('## ', '')}</h3>
              {:else if block.startsWith('- ')}
                <ul>
                  {#each block.split('\n') as line}
                    {#if line.startsWith('- ')}
                      <li>{line.replace('- ', '')}</li>
                    {/if}
                  {/each}
                </ul>
              {:else}
                <p>{block}</p>
              {/if}
            {/each}
          </div>
        </article>
      {/each}
    </div>
  </div>
{/if}

<svelte:document onclick={(e) => {
  if (!e.target.closest('.yoink-btn-wrapper')) {
    yoinkDropdownOpen = false;
  }
}} />

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 20px 20px 40px;
    width: 100%;
  }

  .branding {
    text-align: center;
    margin-bottom: 40px;
    max-width: 100%;
    overflow-wrap: break-word;
  }

  h1 {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 3.5rem;
    letter-spacing: -0.03em;
    margin-bottom: 8px;
    color: var(--text);
  }

  .brand-tld {
    color: var(--purple-400);
  }

  .splash {
    font-size: 1rem;
    color: var(--purple-400);
    font-weight: 500;
    transition: all 0.15s ease;
  }

  .splash.fancy {
    font-family: 'Pacifico', cursive;
    font-size: 1.1rem;
  }

  .splash.clickable {
    cursor: pointer;
    text-decoration: underline;
    text-decoration-style: dotted;
    text-underline-offset: 3px;
  }

  .splash.clickable:hover {
    color: var(--purple-300);
  }

  .splash.rainbow {
    background: linear-gradient(90deg, #f87171, #fbbf24, #4ade80, #38bdf8, #a78bfa, #f472b6);
    background-size: 200% auto;
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
    animation: rainbow-shift 3s linear infinite;
  }

  @keyframes rainbow-shift {
    0% { background-position: 0% center; }
    100% { background-position: 200% center; }
  }

  .input-container {
    width: 100%;
    max-width: 600px;
  }

  .url-input-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--surface);
    border: 2px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 6px 6px 6px 16px;
    transition: border-color 0.15s ease-out;
  }

  .url-input-wrapper:focus-within {
    border-color: var(--purple-500);
  }

  .url-input-wrapper > svg {
    width: 20px;
    height: 20px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .url-input {
    flex: 1;
    min-width: 0;
    padding: 12px 8px;
    font-family: var(--font-body);
    font-size: 1rem;
    font-weight: 400;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text);
  }

  .url-input::placeholder {
    color: var(--text-muted);
  }

  .paste-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    background: var(--surface-elevated);
    color: var(--text-secondary);
    border: none;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease-out;
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
  }

  .paste-btn:hover {
    background: var(--purple-500);
    color: white;
  }

  .paste-btn:active {
    transform: scale(0.95);
  }

  .paste-btn svg {
    width: 18px;
    height: 18px;
  }

  .clear-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    background: transparent;
    color: var(--text-muted);
    border: none;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease-out;
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
  }

  .clear-btn:hover {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
  }

  .clear-btn svg {
    width: 18px;
    height: 18px;
  }

  .format-section {
    margin-top: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
  }

  .segmented-control {
    display: flex;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    padding: 4px;
  }

  .segment {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 20px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    background: transparent;
    border: none;
    border-radius: var(--radius-full);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .segment:hover:not(.active) {
    color: var(--text);
  }

  .segment.active {
    background: var(--purple-500);
    color: white;
  }

  .segment svg {
    width: 16px;
    height: 16px;
  }

  .yoink-btn-wrapper {
    position: relative;
    display: inline-flex;
  }

  .yoink-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 16px 40px;
    font-family: var(--font-heading);
    font-size: 1.1rem;
    font-weight: 700;
    letter-spacing: 0.02em;
    background: var(--purple-500);
    color: white;
    border: none;
    border-radius: var(--radius-full) 0 0 var(--radius-full);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .yoink-btn:hover:not(:disabled) {
    background: var(--purple-400);
  }

  .yoink-btn:active:not(:disabled) {
    transform: scale(0.98);
  }

  .yoink-btn:disabled {
    background: var(--surface-elevated);
    color: var(--text-muted);
    cursor: not-allowed;
  }

  .yoink-btn svg {
    width: 20px;
    height: 20px;
  }

  .yoink-dropdown-toggle {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px 14px;
    background: var(--purple-600);
    color: white;
    border: none;
    border-left: 1px solid var(--purple-400);
    border-radius: 0 var(--radius-full) var(--radius-full) 0;
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .yoink-dropdown-toggle:hover {
    background: var(--purple-500);
  }

  .yoink-dropdown-toggle svg {
    width: 16px;
    height: 16px;
    transition: transform 0.15s ease-out;
  }

  .yoink-btn-wrapper.open .yoink-dropdown-toggle svg {
    transform: rotate(180deg);
  }

  .yoink-dropdown {
    position: absolute;
    top: calc(100% + 8px);
    left: 50%;
    transform: translateX(-50%);
    min-width: 180px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 6px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    z-index: 100;
    animation: dropdown-in 0.15s ease-out;
  }

  @keyframes dropdown-in {
    from { opacity: 0; transform: translateX(-50%) translateY(-8px); }
    to { opacity: 1; transform: translateX(-50%) translateY(0); }
  }

  .yoink-dropdown-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 12px 16px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text);
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: left;
  }

  .yoink-dropdown-item:hover {
    background: var(--purple-900);
    color: var(--purple-300);
  }

  .yoink-dropdown-item svg {
    width: 18px;
    height: 18px;
    color: var(--text-secondary);
  }

  .yoink-dropdown-item:hover svg {
    color: var(--purple-400);
  }

  .status {
    margin-top: 24px;
    padding: 14px 20px;
    border-radius: var(--radius-md);
    font-size: 0.9rem;
    font-weight: 500;
    max-width: 600px;
    width: 100%;
    text-align: center;
  }

  .status.loading {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    background: var(--purple-900);
    color: var(--purple-300);
    border: 1px solid var(--purple-500);
  }

  .status.error {
    background: rgba(248, 113, 113, 0.1);
    color: var(--error);
    border: 1px solid var(--error);
  }

  .status.success {
    background: rgba(74, 222, 128, 0.1);
    color: var(--success);
    border: 1px solid var(--success);
  }

  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.7);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2000;
  }

  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 24px;
    max-width: 400px;
    width: 90%;
    animation: modalIn 0.2s ease-out;
  }

  @keyframes modalIn {
    from { opacity: 0; transform: scale(0.95); }
    to { opacity: 1; transform: scale(1); }
  }

  .modal-title {
    font-family: var(--font-heading);
    font-size: 1.25rem;
    font-weight: 700;
    margin-bottom: 8px;
    color: var(--text);
  }

  .modal-subtitle {
    font-size: 0.9rem;
    color: var(--text-secondary);
    margin-bottom: 20px;
  }

  .modal-buttons {
    display: flex;
    gap: 10px;
    margin-bottom: 16px;
  }

  .modal-btn {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 14px 20px;
    font-family: var(--font-body);
    font-size: 0.95rem;
    font-weight: 600;
    border: 2px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--surface-elevated);
    color: var(--text);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .modal-btn:hover {
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .modal-btn.primary {
    background: var(--purple-500);
    border-color: var(--purple-500);
    color: white;
  }

  .modal-btn.primary:hover {
    background: var(--purple-600);
    border-color: var(--purple-600);
  }

  .modal-btn svg {
    width: 18px;
    height: 18px;
  }

  .modal-toggle {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    cursor: pointer;
    margin-bottom: 12px;
  }

  .modal-toggle input {
    display: none;
  }

  .modal-toggle-switch {
    width: 40px;
    height: 22px;
    background: var(--border);
    border-radius: 11px;
    position: relative;
    transition: background 0.2s ease-out;
    flex-shrink: 0;
  }

  .modal-toggle-switch::after {
    content: '';
    position: absolute;
    width: 18px;
    height: 18px;
    background: white;
    border-radius: 50%;
    top: 2px;
    left: 2px;
    transition: transform 0.2s ease-out;
  }

  .modal-toggle input:checked + .modal-toggle-switch {
    background: var(--purple-500);
  }

  .modal-toggle input:checked + .modal-toggle-switch::after {
    transform: translateX(18px);
  }

  .modal-toggle-label {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .modal-hint {
    font-size: 0.8rem;
    color: var(--text-muted);
    text-align: center;
  }

  .modal-hint a {
    color: var(--purple-400);
    text-decoration: none;
  }

  .batch-modal {
    max-width: 500px;
  }

  .batch-textarea {
    width: 100%;
    height: 180px;
    padding: 16px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    resize: vertical;
    outline: none;
    transition: border-color 0.15s ease-out;
    margin-bottom: 12px;
  }

  .batch-textarea:focus {
    border-color: var(--purple-500);
  }

  .batch-textarea::placeholder {
    color: var(--text-muted);
  }

  .batch-count {
    font-size: 0.85rem;
    color: var(--text-secondary);
    margin-bottom: 16px;
    text-align: center;
  }

  .batch-count span {
    font-weight: 700;
    color: var(--purple-400);
  }

  @media (max-width: 600px) {
    main {
      padding: 16px 12px 32px;
    }

    .branding {
      margin-bottom: 24px;
    }

    h1 {
      font-size: 2.5rem;
    }

    .splash {
      font-size: 0.9rem;
    }

    .input-container {
      max-width: 100%;
    }

    .format-section {
      flex-direction: column;
      gap: 14px;
    }

    .segmented-control {
      padding: 3px;
      width: 100%;
    }

    .segment {
      flex: 1;
      padding: 12px 10px;
      font-size: 0.85rem;
      gap: 5px;
      justify-content: center;
    }

    .segment svg {
      width: 15px;
      height: 15px;
    }

    .yoink-btn-wrapper {
      width: 100%;
    }

    .yoink-btn {
      flex: 1;
      padding: 16px 24px;
      font-size: 1rem;
    }

    .yoink-dropdown-toggle {
      padding: 16px 16px;
    }

    .url-input-wrapper {
      padding: 4px 4px 4px 12px;
    }

    .url-input {
      padding: 10px 6px;
      font-size: 16px;
    }

    .yoink-dropdown {
      left: 0;
      right: 0;
      transform: none;
      min-width: auto;
    }

    @keyframes dropdown-in {
      from { opacity: 0; transform: translateY(-8px); }
      to { opacity: 1; transform: translateY(0); }
    }

    .modal {
      width: 92%;
      padding: 20px;
    }

    .batch-modal {
      max-width: none;
      width: 92%;
    }

    .batch-textarea {
      height: 200px;
      font-size: 16px;
    }

    .modal-btn {
      padding: 16px 16px;
    }

    .yoink-dropdown-item {
      padding: 14px 16px;
    }
  }

  @media (max-width: 400px) {
    h1 {
      font-size: 2rem;
    }

    .segment span {
      display: none;
    }

    .segment {
      padding: 12px 14px;
      gap: 0;
    }

    .yoink-btn {
      padding: 14px 20px;
      font-size: 0.9rem;
    }

    .yoink-dropdown-toggle {
      padding: 14px 12px;
    }
  }

  .updates-overlay {
    position: fixed;
    inset: 0;
    background: rgba(10, 10, 15, 0.8);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    z-index: 2500;
    animation: updates-slide-in 0.4s cubic-bezier(0.22, 1, 0.36, 1) forwards;
  }

  .updates-overlay.closing {
    animation: updates-slide-out 0.3s ease-in forwards;
  }

  @keyframes updates-slide-in {
    0% { transform: translateY(100%); opacity: 0; }
    60% { opacity: 1; }
    85% { transform: translateY(-1.5%); }
    100% { transform: translateY(0); }
  }

  @keyframes updates-slide-out {
    from { transform: translateY(0); opacity: 1; }
    to { transform: translateY(100%); opacity: 0; }
  }

  .updates-scroll {
    width: 100%;
    height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 40px 20px 80px;
  }

  .updates-header {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-bottom: 24px;
  }

  .updates-pill {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 1.8rem;
    color: var(--purple-400);
    background: var(--surface);
    border: 2px solid var(--border);
    border-radius: var(--radius-full);
    padding: 10px 36px;
  }

  .updates-close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    background: var(--surface);
    border: 2px solid var(--border);
    border-radius: 50%;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .updates-close:hover {
    color: var(--text);
    border-color: var(--purple-500);
  }

  .updates-close svg {
    width: 20px;
    height: 20px;
  }

  .update-card {
    width: 100%;
    max-width: 560px;
    background: var(--surface);
    border: 2px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 32px;
    margin-bottom: 32px;
  }

  .update-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 8px;
  }

  .update-version {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 0.85rem;
    color: var(--text);
    background: var(--surface-elevated);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 3px 8px;
  }

  .update-date {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .update-title {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 1.5rem;
    color: var(--text);
    margin: 0 0 16px 0;
    line-height: 1.3;
  }

  .update-image {
    width: 100%;
    border-radius: var(--radius-md);
    margin-bottom: 20px;
    border: 1px solid var(--border);
  }

  .update-content {
    color: var(--text-secondary);
    font-size: 0.9rem;
    line-height: 1.7;
  }

  .update-content h3 {
    font-family: var(--font-heading);
    font-weight: 700;
    font-size: 1.1rem;
    color: var(--text);
    margin: 24px 0 8px 0;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }

  .update-content p {
    margin: 0 0 12px 0;
  }

  .update-content ul {
    margin: 0 0 12px 0;
    padding-left: 20px;
  }

  .update-content li {
    margin-bottom: 4px;
  }

  @media (max-width: 600px) {
    .updates-scroll {
      padding: 24px 12px 100px;
    }

    .updates-pill {
      font-size: 1.3rem;
      padding: 8px 24px;
    }

    .update-card {
      padding: 20px;
    }

    .update-title {
      font-size: 1.2rem;
    }
  }
</style>
