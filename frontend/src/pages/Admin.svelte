<script>
  import { onMount, onDestroy } from 'svelte';
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
  import { apiBase, fetchJson } from '../lib/api.js';

  let token = $state(localStorage.getItem('yoink_admin_token') || '');
  let loggedIn = $state(false);
  let password = $state('');
  let loginError = $state('');
  let loading = $state(false);

  let analytics = $state(null);
  let status = $state(null);
  let currentBanner = $state(null);

  let bannerMessage = $state('');
  let bannerType = $state('info');
  let bannerExpiry = $state('');

  let refreshInterval;
  let tab = $state('overview');

  function adminFetch(path, opts = {}) {
    return fetchJson(`${apiBase()}${path}`, {
      ...opts,
      headers: { ...opts.headers, 'x-admin-token': token }
    });
  }

  async function adminPost(path, body = {}) {
    return adminFetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'x-admin-token': token },
      body: JSON.stringify(body)
    });
  }

  async function login() {
    loginError = '';
    loading = true;
    try {
      const res = await fetchJson(`${apiBase()}/api/admin/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      });
      token = res.token;
      localStorage.setItem('yoink_admin_token', token);
      loggedIn = true;
      password = '';
      loadData();
    } catch (e) {
      loginError = e.message || 'login failed';
    }
    loading = false;
  }

  async function logout() {
    try {
      await adminPost('/api/admin/logout');
    } catch {}
    token = '';
    loggedIn = false;
    localStorage.removeItem('yoink_admin_token');
  }

  async function verifyToken() {
    if (!token) return;
    try {
      await adminFetch('/api/admin/verify');
      loggedIn = true;
      loadData();
    } catch {
      token = '';
      localStorage.removeItem('yoink_admin_token');
    }
  }

  async function loadData() {
    if (!loggedIn) return;
    try {
      const [a, s, b] = await Promise.all([
        adminFetch('/api/admin/analytics'),
        adminFetch('/api/admin/status'),
        fetchJson(`${apiBase()}/api/banner`)
      ]);
      analytics = a;
      status = s;
      currentBanner = b.banner;
    } catch (e) {
      if (e.message?.includes('401') || e.message?.includes('Unauthorized')) {
        logout();
      }
    }
  }

  async function setBannerAction() {
    if (!bannerMessage.trim()) return;
    try {
      const res = await adminPost('/api/admin/banner', {
        message: bannerMessage.trim(),
        type: bannerType,
        expiresIn: bannerExpiry ? parseInt(bannerExpiry) : null
      });
      currentBanner = res.banner;
      bannerMessage = '';
      bannerExpiry = '';
    } catch {}
  }

  async function clearBannerAction() {
    try {
      await adminFetch('/api/admin/banner', { method: 'DELETE', headers: { 'x-admin-token': token } });
      currentBanner = null;
    } catch {}
  }

  function formatUptime(seconds) {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  function formatNumber(n) {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
    return String(n);
  }

  function getTopEntries(obj, limit = 8) {
    if (!obj) return [];
    return Object.entries(obj)
      .sort((a, b) => b[1] - a[1])
      .slice(0, limit);
  }

  function getRecentDays(dailyUsers, days = 14) {
    if (!dailyUsers) return [];
    const entries = Object.entries(dailyUsers).sort((a, b) => a[0].localeCompare(b[0]));
    return entries.slice(-days);
  }

  onMount(() => {
    verifyToken();
    refreshInterval = setInterval(loadData, 15000);
  });

  onDestroy(() => {
    clearInterval(refreshInterval);
  });

  let todayUsers = $derived(() => {
    if (!analytics?.dailyUsers) return 0;
    const today = new Date().toISOString().split('T')[0];
    return analytics.dailyUsers[today] || 0;
  });

  let topSites = $derived(getTopEntries(analytics?.sites));
  let topFormats = $derived(getTopEntries(analytics?.formats));
  let topCountries = $derived(getTopEntries(analytics?.countries));
  let recentDays = $derived(getRecentDays(analytics?.dailyUsers));
  let maxDayUsers = $derived(recentDays.length ? Math.max(...recentDays.map(d => d[1]), 1) : 1);
</script>

<HeaderSimple />

<main>
  {#if !loggedIn}
    <div class="login-container">
      <div class="login-card">
        <h1>admin</h1>
        <p class="login-sub">yoink.tools dashboard</p>
        <form onsubmit={(e) => { e.preventDefault(); login(); }}>
          <input
            type="password"
            placeholder="password"
            bind:value={password}
            disabled={loading}
            autocomplete="current-password"
          />
          <button type="submit" disabled={loading || !password}>
            {loading ? 'logging in...' : 'login'}
          </button>
          {#if loginError}
            <p class="error">{loginError}</p>
          {/if}
        </form>
      </div>
    </div>
  {:else}
    <div class="dashboard">
      <div class="dash-header">
        <h1>dashboard</h1>
        <button class="logout-btn" onclick={logout}>logout</button>
      </div>

      <div class="tabs">
        <button class:active={tab === 'overview'} onclick={() => tab = 'overview'}>overview</button>
        <button class:active={tab === 'analytics'} onclick={() => tab = 'analytics'}>analytics</button>
        <button class:active={tab === 'banner'} onclick={() => tab = 'banner'}>banner</button>
        <button class:active={tab === 'server'} onclick={() => tab = 'server'}>server</button>
      </div>

      {#if tab === 'overview'}
        <div class="stats-grid">
          <div class="stat-card">
            <span class="stat-value">{analytics ? formatNumber(analytics.totalDownloads) : '—'}</span>
            <span class="stat-label">downloads</span>
          </div>
          <div class="stat-card">
            <span class="stat-value">{analytics ? formatNumber(analytics.totalConverts) : '—'}</span>
            <span class="stat-label">converts</span>
          </div>
          <div class="stat-card">
            <span class="stat-value">{analytics ? formatNumber(analytics.totalCompresses) : '—'}</span>
            <span class="stat-label">compresses</span>
          </div>
          <div class="stat-card">
            <span class="stat-value">{todayUsers()}</span>
            <span class="stat-label">users today</span>
          </div>
          <div class="stat-card">
            <span class="stat-value">{status?.connectedClients ?? '—'}</span>
            <span class="stat-label">connected</span>
          </div>
          <div class="stat-card">
            <span class="stat-value">{analytics?.peakUsers?.count ?? '—'}</span>
            <span class="stat-label">peak users (24h)</span>
          </div>
        </div>

        {#if status}
          <div class="jobs-bar">
            <h3>active jobs</h3>
            <div class="jobs-row">
              {#each Object.entries(status.activeJobs) as [type, count]}
                <div class="job-pill">
                  <span class="job-type">{type}</span>
                  <span class="job-count">{count}/{status.jobLimits[type]}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}

        {#if recentDays.length > 0}
          <div class="chart-section">
            <h3>daily users (last {recentDays.length} days)</h3>
            <div class="bar-chart">
              {#each recentDays as [date, count]}
                <div class="bar-col">
                  <span class="bar-value">{count}</span>
                  <div class="bar" style="height: {Math.max((count / maxDayUsers) * 100, 4)}%"></div>
                  <span class="bar-label">{date.slice(5)}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}

      {:else if tab === 'analytics'}
        <div class="analytics-grid">
          <div class="list-card">
            <h3>top sites</h3>
            {#if topSites.length === 0}
              <p class="empty">no data yet</p>
            {/if}
            {#each topSites as [site, count]}
              <div class="list-row">
                <span class="list-name">{site}</span>
                <span class="list-count">{formatNumber(count)}</span>
              </div>
            {/each}
          </div>

          <div class="list-card">
            <h3>top formats</h3>
            {#if topFormats.length === 0}
              <p class="empty">no data yet</p>
            {/if}
            {#each topFormats as [format, count]}
              <div class="list-row">
                <span class="list-name">{format}</span>
                <span class="list-count">{formatNumber(count)}</span>
              </div>
            {/each}
          </div>

          <div class="list-card">
            <h3>top countries</h3>
            {#if topCountries.length === 0}
              <p class="empty">no data yet</p>
            {/if}
            {#each topCountries as [country, count]}
              <div class="list-row">
                <span class="list-name">{country}</span>
                <span class="list-count">{formatNumber(count)}</span>
              </div>
            {/each}
          </div>
        </div>

      {:else if tab === 'banner'}
        <div class="banner-section">
          {#if currentBanner}
            <div class="current-banner banner-{currentBanner.type}">
              <div class="banner-preview">
                <strong>active banner ({currentBanner.type}):</strong>
                <p>{currentBanner.message}</p>
                {#if currentBanner.expiresAt}
                  <small>expires: {new Date(currentBanner.expiresAt).toLocaleString()}</small>
                {/if}
                {#if currentBanner.auto}
                  <small class="auto-tag">auto-generated</small>
                {/if}
              </div>
              <button class="clear-btn" onclick={clearBannerAction}>clear</button>
            </div>
          {:else}
            <p class="no-banner">no active banner</p>
          {/if}

          <div class="banner-form">
            <h3>set banner</h3>
            <input
              type="text"
              placeholder="banner message..."
              bind:value={bannerMessage}
            />
            <div class="banner-options">
              <select bind:value={bannerType}>
                <option value="info">info</option>
                <option value="warning">warning</option>
                <option value="error">error</option>
                <option value="maintenance">maintenance</option>
              </select>
              <input
                type="number"
                placeholder="expires in (min)"
                bind:value={bannerExpiry}
                min="1"
              />
              <button onclick={setBannerAction} disabled={!bannerMessage.trim()}>set banner</button>
            </div>
            <div class="quick-banners">
              <button onclick={() => { bannerMessage = 'scheduled maintenance — downloads may be interrupted.'; bannerType = 'maintenance'; }}>maintenance</button>
              <button onclick={() => { bannerMessage = 'high traffic — downloads may be slower than usual.'; bannerType = 'warning'; }}>high traffic</button>
              <button onclick={() => { bannerMessage = 'youtube is currently blocking some requests — try again later.'; bannerType = 'error'; }}>yt blocked</button>
            </div>
          </div>
        </div>

      {:else if tab === 'server'}
        {#if status}
          <div class="server-grid">
            <div class="server-card">
              <h3>uptime</h3>
              <span class="big-value">{formatUptime(status.uptime)}</span>
            </div>
            <div class="server-card">
              <h3>memory</h3>
              <div class="mem-row">
                <span>rss: {status.memory.rss} MB</span>
                <span>heap: {status.memory.heapUsed}/{status.memory.heapTotal} MB</span>
              </div>
            </div>
            <div class="server-card">
              <h3>system</h3>
              <div class="mem-row">
                <span>{status.system.platform}</span>
                <span>ram: {status.system.freeMem}/{status.system.totalMem} MB free</span>
                <span>load: {status.system.loadAvg.map(l => l.toFixed(2)).join(', ')}</span>
              </div>
            </div>
            <div class="server-card">
              <h3>connections</h3>
              <div class="mem-row">
                <span>clients: {status.connectedClients}</span>
                <span>streams: {status.activeStreams}</span>
                <span>processes: {status.activeProcesses}</span>
                <span>bot downloads: {status.botDownloads}</span>
              </div>
            </div>
          </div>
        {:else}
          <p class="empty">loading server status...</p>
        {/if}
      {/if}
    </div>
  {/if}
</main>

<FooterSimple />

<style>
  main {
    flex: 1;
    width: 100%;
    max-width: 900px;
    margin: 0 auto;
    padding: 20px 24px 60px;
  }

  .login-container {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 60vh;
  }

  .login-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 40px;
    width: 100%;
    max-width: 360px;
    text-align: center;
  }

  .login-card h1 {
    font-family: var(--font-heading);
    font-size: 1.8rem;
    font-weight: 800;
    margin-bottom: 4px;
  }

  .login-sub {
    color: var(--text-secondary);
    font-size: 0.85rem;
    margin-bottom: 24px;
  }

  .login-card form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .login-card input[type="password"] {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px 16px;
    color: var(--text);
    font-size: 0.95rem;
    font-family: var(--font-body);
    outline: none;
    transition: border-color 0.2s;
  }

  .login-card input:focus {
    border-color: var(--purple-500);
  }

  .login-card button[type="submit"] {
    background: var(--purple-600);
    color: white;
    border: none;
    border-radius: var(--radius-sm);
    padding: 12px;
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    font-family: var(--font-body);
    transition: background 0.2s;
  }

  .login-card button[type="submit"]:hover:not(:disabled) {
    background: var(--purple-500);
  }

  .login-card button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .error {
    color: var(--error);
    font-size: 0.85rem;
  }

  /* Dashboard */
  .dashboard {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .dash-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .dash-header h1 {
    font-family: var(--font-heading);
    font-size: 1.6rem;
    font-weight: 800;
  }

  .logout-btn {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    padding: 6px 16px;
    font-size: 0.8rem;
    cursor: pointer;
    font-family: var(--font-body);
    transition: all 0.2s;
  }

  .logout-btn:hover {
    border-color: var(--error);
    color: var(--error);
  }

  .tabs {
    display: flex;
    gap: 4px;
    background: var(--surface);
    border-radius: var(--radius-sm);
    padding: 4px;
    border: 1px solid var(--border);
  }

  .tabs button {
    flex: 1;
    background: transparent;
    border: none;
    border-radius: 6px;
    padding: 8px 12px;
    color: var(--text-secondary);
    font-size: 0.85rem;
    font-weight: 500;
    cursor: pointer;
    font-family: var(--font-body);
    transition: all 0.2s;
  }

  .tabs button.active {
    background: var(--purple-600);
    color: white;
  }

  .tabs button:hover:not(.active) {
    color: var(--text);
  }

  /* Stats */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
  }

  .stat-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .stat-value {
    font-family: var(--font-heading);
    font-size: 1.6rem;
    font-weight: 800;
    color: var(--purple-400);
  }

  .stat-label {
    font-size: 0.75rem;
    color: var(--text-secondary);
    text-transform: lowercase;
  }

  /* Jobs */
  .jobs-bar {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .jobs-bar h3 {
    font-size: 0.85rem;
    font-weight: 600;
    margin-bottom: 10px;
    color: var(--text-secondary);
  }

  .jobs-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .job-pill {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    padding: 6px 14px;
    display: flex;
    gap: 8px;
    align-items: center;
    font-size: 0.8rem;
  }

  .job-type {
    color: var(--text-secondary);
  }

  .job-count {
    color: var(--purple-400);
    font-weight: 600;
  }

  /* Chart */
  .chart-section {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .chart-section h3 {
    font-size: 0.85rem;
    font-weight: 600;
    margin-bottom: 12px;
    color: var(--text-secondary);
  }

  .bar-chart {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    height: 140px;
  }

  .bar-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    height: 100%;
    justify-content: flex-end;
  }

  .bar-value {
    font-size: 0.65rem;
    color: var(--text-muted);
  }

  .bar {
    width: 100%;
    max-width: 36px;
    background: var(--purple-600);
    border-radius: 4px 4px 0 0;
    min-height: 4px;
    transition: height 0.3s ease;
  }

  .bar-label {
    font-size: 0.6rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  /* Analytics lists */
  .analytics-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 12px;
  }

  .list-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .list-card h3 {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-secondary);
    margin-bottom: 10px;
  }

  .list-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.8rem;
  }

  .list-row:last-child {
    border-bottom: none;
  }

  .list-name {
    color: var(--text);
  }

  .list-count {
    color: var(--purple-400);
    font-weight: 600;
  }

  .empty {
    color: var(--text-muted);
    font-size: 0.8rem;
    text-align: center;
    padding: 16px;
  }

  /* Banner management */
  .banner-section {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .current-banner {
    border-radius: var(--radius-md);
    padding: 16px;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }

  .banner-info { background: #1e3a5f; border: 1px solid #2563eb; }
  .banner-warning { background: #3d2e00; border: 1px solid #f59e0b; }
  .banner-error { background: #3d0f0f; border: 1px solid #ef4444; }
  .banner-maintenance { background: #1e1b4b; border: 1px solid #8b5cf6; }

  .banner-preview p {
    margin-top: 4px;
    font-size: 0.9rem;
  }

  .banner-preview strong {
    font-size: 0.75rem;
    text-transform: uppercase;
    opacity: 0.7;
  }

  .banner-preview small {
    display: block;
    margin-top: 4px;
    opacity: 0.6;
    font-size: 0.7rem;
  }

  .auto-tag {
    color: var(--purple-400);
  }

  .clear-btn {
    background: transparent;
    border: 1px solid rgba(255,255,255,0.2);
    border-radius: var(--radius-sm);
    color: var(--text);
    padding: 6px 16px;
    font-size: 0.8rem;
    cursor: pointer;
    font-family: var(--font-body);
    white-space: nowrap;
  }

  .clear-btn:hover {
    border-color: var(--error);
    color: var(--error);
  }

  .no-banner {
    color: var(--text-muted);
    font-size: 0.85rem;
    text-align: center;
    padding: 20px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .banner-form {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .banner-form h3 {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-secondary);
  }

  .banner-form input[type="text"],
  .banner-form input[type="number"],
  .banner-form select {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    color: var(--text);
    font-size: 0.85rem;
    font-family: var(--font-body);
    outline: none;
  }

  .banner-form input:focus,
  .banner-form select:focus {
    border-color: var(--purple-500);
  }

  .banner-options {
    display: flex;
    gap: 8px;
  }

  .banner-options select {
    flex: 1;
  }

  .banner-options input {
    width: 120px;
  }

  .banner-options button {
    background: var(--purple-600);
    color: white;
    border: none;
    border-radius: var(--radius-sm);
    padding: 10px 20px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    font-family: var(--font-body);
    white-space: nowrap;
  }

  .banner-options button:disabled {
    opacity: 0.5;
  }

  .quick-banners {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .quick-banners button {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    padding: 6px 14px;
    color: var(--text-secondary);
    font-size: 0.75rem;
    cursor: pointer;
    font-family: var(--font-body);
    transition: all 0.2s;
  }

  .quick-banners button:hover {
    border-color: var(--purple-500);
    color: var(--text);
  }

  /* Server */
  .server-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 12px;
  }

  .server-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .server-card h3 {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: lowercase;
    margin-bottom: 8px;
  }

  .big-value {
    font-family: var(--font-heading);
    font-size: 1.4rem;
    font-weight: 800;
    color: var(--purple-400);
  }

  .mem-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 0.8rem;
    color: var(--text-secondary);
  }

  @media (max-width: 600px) {
    main {
      padding: 16px 12px 80px;
    }

    .stats-grid {
      grid-template-columns: repeat(2, 1fr);
    }

    .stat-value {
      font-size: 1.3rem;
    }

    .banner-options {
      flex-direction: column;
    }

    .banner-options input {
      width: 100%;
    }

    .tabs button {
      font-size: 0.75rem;
      padding: 6px 8px;
    }

    .bar-chart {
      height: 100px;
    }
  }
</style>
