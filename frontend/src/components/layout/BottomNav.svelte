<script>
  import { path } from '../../lib/router.js';
  import { queue } from '../../stores/queue.js';
  import QueueDropdown from '../queue/QueueDropdown.svelte';

  let queueOpen = $state(false);
  let sheetRef;
  let sheetTranslateY = $state(0);
  let swiping = $state(false);
  let touchStartY = 0;
  let touchStartTime = 0;

  let items = $derived($queue);
  let count = $derived(items.length);
  let currentPath = $derived($path);

  let activeCount = $derived(
    items.filter(i => ['starting', 'downloading', 'processing', 'zipping', 'sending', 'transcribing'].includes(i.stage)).length
  );

  function toggleQueue(e) {
    e.preventDefault();
    e.stopPropagation();
    queueOpen = !queueOpen;
    sheetTranslateY = 0;
  }

  function closeQueue() {
    queueOpen = false;
    sheetTranslateY = 0;
  }

  function handleSheetTouchStart(e) {
    const el = sheetRef;
    if (!el) return;
    // Only start swipe if at the top of scroll or touching the handle area
    if (el.scrollTop > 0 && e.target.closest('.queue-sheet') && !e.target.closest('.sheet-handle')) return;
    touchStartY = e.touches[0].clientY;
    touchStartTime = Date.now();
    swiping = true;
  }

  function handleSheetTouchMove(e) {
    if (!swiping) return;
    const deltaY = e.touches[0].clientY - touchStartY;
    if (deltaY > 0) {
      sheetTranslateY = deltaY;
      e.preventDefault();
    } else {
      sheetTranslateY = 0;
    }
  }

  function handleSheetTouchEnd() {
    if (!swiping) return;
    swiping = false;
    const velocity = sheetTranslateY / (Date.now() - touchStartTime);
    // Close if dragged >80px or fast flick (>0.5px/ms)
    if (sheetTranslateY > 80 || velocity > 0.5) {
      closeQueue();
    } else {
      sheetTranslateY = 0;
    }
  }

  const navItems = [
    { href: '/', label: 'download', icon: 'download' },
    { href: '/convert', label: 'convert', icon: 'convert' },
    { href: '/compress', label: 'compress', icon: 'compress' },
    { href: '/trim', label: 'trim', icon: 'trim' },
    { href: '/transcribe', label: 'transcribe', icon: 'transcribe' },
    { href: '/settings', label: 'settings', icon: 'settings' },
  ];
</script>

<nav class="bottom-nav">
  {#each navItems as item}
    <a
      class="bottom-nav-item"
      class:active={currentPath === item.href}
      href={item.href}
    >
      <div class="nav-icon">
        {#if item.icon === 'download'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
        {:else if item.icon === 'convert'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="17 1 21 5 17 9"></polyline>
            <path d="M3 11V9a4 4 0 0 1 4-4h14"></path>
            <polyline points="7 23 3 19 7 15"></polyline>
            <path d="M21 13v2a4 4 0 0 1-4 4H3"></path>
          </svg>
        {:else if item.icon === 'compress'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="4 14 10 14 10 20"></polyline>
            <polyline points="20 10 14 10 14 4"></polyline>
            <line x1="14" y1="10" x2="21" y2="3"></line>
            <line x1="3" y1="21" x2="10" y2="14"></line>
          </svg>
        {:else if item.icon === 'trim'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="6" cy="6" r="3"></circle>
            <circle cx="6" cy="18" r="3"></circle>
            <line x1="20" y1="4" x2="8.12" y2="15.88"></line>
            <line x1="14.47" y1="14.48" x2="20" y2="20"></line>
            <line x1="8.12" y1="8.12" x2="12" y2="12"></line>
          </svg>
        {:else if item.icon === 'transcribe'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
          </svg>
        {:else if item.icon === 'settings'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="3"></circle>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
          </svg>
        {/if}
      </div>
      <span class="nav-label">{item.label}</span>
    </a>
  {/each}

  <button
    class="bottom-nav-item queue-item"
    class:active={queueOpen}
    class:has-active={activeCount > 0}
    onclick={toggleQueue}
  >
    <div class="nav-icon">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <line x1="8" y1="6" x2="21" y2="6"></line>
        <line x1="8" y1="12" x2="21" y2="12"></line>
        <line x1="8" y1="18" x2="21" y2="18"></line>
        <line x1="3" y1="6" x2="3.01" y2="6"></line>
        <line x1="3" y1="12" x2="3.01" y2="12"></line>
        <line x1="3" y1="18" x2="3.01" y2="18"></line>
      </svg>
      {#if count > 0}
        <span class="nav-badge">{count}</span>
      {/if}
    </div>
    <span class="nav-label">queue</span>
  </button>
</nav>

{#if queueOpen}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="queue-sheet-overlay" onclick={closeQueue}></div>
  <div
    class="queue-sheet"
    class:swiping
    bind:this={sheetRef}
    style={sheetTranslateY > 0 ? `transform: translateY(${sheetTranslateY}px)` : ''}
    ontouchstart={handleSheetTouchStart}
    ontouchmove={handleSheetTouchMove}
    ontouchend={handleSheetTouchEnd}
  >
    <div class="sheet-handle"></div>
    <QueueDropdown />
  </div>
{/if}

<svelte:document onkeydown={(e) => { if (e.key === 'Escape' && queueOpen) closeQueue(); }} />

<style>
  .bottom-nav {
    display: none;
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--surface);
    border-top: 1px solid var(--border);
    padding: 6px 8px;
    padding-bottom: calc(6px + env(safe-area-inset-bottom, 0px));
    z-index: 1000;
    justify-content: space-around;
    align-items: center;
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    background: rgba(18, 18, 26, 0.92);
  }

  .bottom-nav-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    padding: 6px 8px;
    color: var(--text-muted);
    text-decoration: none;
    font-size: 0;
    background: none;
    border: none;
    cursor: pointer;
    transition: color 0.15s ease-out;
    position: relative;
    -webkit-tap-highlight-color: transparent;
  }

  .bottom-nav-item:active {
    transform: scale(0.92);
  }

  .bottom-nav-item.active {
    color: var(--purple-400);
  }

  .bottom-nav-item.has-active .nav-icon {
    animation: pulse-glow 2s ease-in-out infinite;
  }

  @keyframes pulse-glow {
    0%, 100% { filter: drop-shadow(0 0 0px transparent); }
    50% { filter: drop-shadow(0 0 6px var(--purple-400)); }
  }

  .nav-icon {
    position: relative;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .nav-icon svg {
    width: 22px;
    height: 22px;
  }

  .nav-badge {
    position: absolute;
    top: -4px;
    right: -8px;
    background: var(--purple-500);
    color: white;
    font-size: 0.6rem;
    font-weight: 700;
    min-width: 16px;
    height: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-full);
    padding: 0 4px;
  }

  .nav-label {
    font-family: var(--font-body);
    font-size: 0.65rem;
    font-weight: 500;
    letter-spacing: 0.01em;
  }

  .queue-sheet-overlay {
    display: none;
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px);
    z-index: 999;
  }

  .queue-sheet {
    display: none;
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: var(--surface);
    border-top-left-radius: 20px;
    border-top-right-radius: 20px;
    border-top: 1px solid var(--border);
    padding: 8px 12px;
    padding-bottom: calc(72px + env(safe-area-inset-bottom, 0px));
    max-height: 70vh;
    overflow-y: auto;
    z-index: 1001;
    animation: sheet-up 0.25s ease-out;
    box-shadow: 0 -8px 32px rgba(0, 0, 0, 0.3);
    transition: transform 0.2s ease-out;
    touch-action: pan-y;
  }

  .queue-sheet.swiping {
    transition: none;
    overflow-y: hidden;
  }

  @keyframes sheet-up {
    from { transform: translateY(100%); }
    to { transform: translateY(0); }
  }

  .sheet-handle {
    width: 36px;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    margin: 4px auto 12px;
  }

  @media (max-width: 600px) {
    .bottom-nav {
      display: flex;
    }

    .queue-sheet-overlay {
      display: block;
    }

    .queue-sheet {
      display: block;
    }
  }
</style>
