<script>
  import { path } from '../../lib/router.js';

  let { extraContent } = $props();

  let navOpen = $state(false);

  const navItems = [
    { href: '/', label: 'download', icon: 'download' },
    { href: '/convert', label: 'convert', icon: 'convert' },
    { href: '/compress', label: 'compress', icon: 'compress' },
    { href: '/trim', label: 'trim', icon: 'trim' },
    { href: '/transcribe', label: 'transcribe', icon: 'transcribe' },
  ];

  let currentItem = $derived(navItems.find(i => i.href === $path) || navItems[0]);
  let settingsActive = $derived($path === '/settings');
</script>

<header>
  <div class="header-center">
    <a href="/" class="logo-link">
      <span class="logo-text">yoink</span>
      <span class="logo-tld">.tools</span>
    </a>
  </div>
  <div class="header-right">
    {#if extraContent}
      {@render extraContent()}
    {/if}
    <div class="nav-dropdown-wrapper">
      <button class="nav-trigger" onclick={() => navOpen = !navOpen}>
        {#if currentItem.icon === 'download'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
        {:else if currentItem.icon === 'convert'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="16 3 21 3 21 8"></polyline>
            <line x1="4" y1="20" x2="21" y2="3"></line>
            <polyline points="21 16 21 21 16 21"></polyline>
            <line x1="15" y1="15" x2="21" y2="21"></line>
            <line x1="4" y1="4" x2="9" y2="9"></line>
          </svg>
        {:else if currentItem.icon === 'compress'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="12" y1="1" x2="12" y2="23"></line>
            <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path>
          </svg>
        {:else if currentItem.icon === 'trim'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="6" cy="6" r="3"></circle>
            <circle cx="6" cy="18" r="3"></circle>
            <line x1="20" y1="4" x2="8.12" y2="15.88"></line>
            <line x1="14.47" y1="14.48" x2="20" y2="20"></line>
            <line x1="8.12" y1="8.12" x2="12" y2="12"></line>
          </svg>
        {:else if currentItem.icon === 'transcribe'}
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
          </svg>
        {/if}
        <span>{currentItem.label}</span>
        <svg class="chevron" class:open={navOpen} xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </button>
      {#if navOpen}
        <div class="nav-dropdown">
          {#each navItems as item}
            <a class="nav-dropdown-item" class:active={item.href === currentItem.href} href={item.href} onclick={() => navOpen = false}>
              {#if item.icon === 'download'}
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                  <polyline points="7 10 12 15 17 10"></polyline>
                  <line x1="12" y1="15" x2="12" y2="3"></line>
                </svg>
              {:else if item.icon === 'convert'}
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="16 3 21 3 21 8"></polyline>
                  <line x1="4" y1="20" x2="21" y2="3"></line>
                  <polyline points="21 16 21 21 16 21"></polyline>
                  <line x1="15" y1="15" x2="21" y2="21"></line>
                  <line x1="4" y1="4" x2="9" y2="9"></line>
                </svg>
              {:else if item.icon === 'compress'}
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="12" y1="1" x2="12" y2="23"></line>
                  <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path>
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
              {/if}
              <span>{item.label}</span>
            </a>
          {/each}
        </div>
      {/if}
    </div>
    <a class="settings-btn" class:active={settingsActive} href="/settings" aria-label="Settings">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="3"></circle>
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
      </svg>
    </a>
  </div>
</header>

<svelte:document onclick={(e) => {
  if (!e.target.closest('.nav-dropdown-wrapper')) {
    navOpen = false;
  }
}} />

<style>
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 20px 24px;
    position: relative;
    z-index: 100;
  }

  .header-center {
    display: flex;
    align-items: center;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .logo-link {
    text-decoration: none;
    display: flex;
    align-items: center;
    gap: 0;
  }

  .logo-text {
    font-family: var(--font-heading);
    font-size: 1.1rem;
    font-weight: 800;
    color: var(--text);
  }

  .logo-tld {
    font-family: var(--font-heading);
    font-size: 1.1rem;
    font-weight: 800;
    color: var(--purple-400);
  }

  .nav-dropdown-wrapper {
    position: relative;
  }

  .nav-trigger {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    padding: 10px 16px;
    border-radius: var(--radius-full);
    background: var(--surface);
    border: 1px solid var(--border);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .nav-trigger:hover {
    color: var(--text);
    border-color: var(--purple-500);
  }

  .nav-trigger > svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }

  .nav-trigger .chevron {
    width: 14px;
    height: 14px;
    color: var(--text-muted);
    transition: transform 0.15s ease-out;
  }

  .nav-trigger .chevron.open {
    transform: rotate(180deg);
  }

  .nav-dropdown {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    min-width: 180px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 6px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    z-index: 200;
    animation: nav-dropdown-in 0.15s ease-out;
  }

  @keyframes nav-dropdown-in {
    from { opacity: 0; transform: translateY(-8px) scale(0.95); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  .nav-dropdown-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 10px 14px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    text-decoration: none;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
  }

  .nav-dropdown-item:hover {
    background: var(--purple-900);
    color: var(--purple-300);
  }

  .nav-dropdown-item.active {
    color: var(--purple-400);
    background: var(--purple-900);
  }

  .nav-dropdown-item svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }

  .settings-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    border-radius: var(--radius-full);
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    text-decoration: none;
    transition: all 0.15s ease-out;
  }

  .settings-btn:hover {
    color: var(--text);
    border-color: var(--purple-500);
  }

  .settings-btn.active {
    color: var(--purple-400);
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .settings-btn svg {
    width: 16px;
    height: 16px;
  }

  @media (max-width: 600px) {
    header {
      padding: 12px 12px;
    }
  }
</style>
