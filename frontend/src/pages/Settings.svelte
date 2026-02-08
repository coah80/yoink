<script>
  import { onMount } from 'svelte';
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
  import { settings } from '../stores/settings.js';
  import { addToast } from '../stores/toast.js';
  import { deleteUserAnalyticsData } from '../stores/analytics.js';

  let activeTab = $state('video');
  let s = $derived($settings);

  const tabs = [
    { id: 'video', label: 'video' },
    { id: 'audio', label: 'audio' },
    { id: 'downloads', label: 'downloads' },
    { id: 'advanced', label: 'advanced' },
    { id: 'privacy', label: 'privacy' },
  ];

  const qualityOptions = ['best', '4k', '1440p', '1080p', '720p', '480p', '360p'];
  const qualityLabels = { best: '8k+', '4k': '4k', '1440p': '1440p', '1080p': '1080p', '720p': '720p', '480p': '480p', '360p': '360p' };

  const codecOptions = [
    { value: 'h264', label: 'h264 + aac' },
    { value: 'av1', label: 'av1 + opus' },
    { value: 'vp9', label: 'vp9 + opus' },
  ];

  const containerOptions = [
    { value: 'mp4', label: 'mp4' },
    { value: 'webm', label: 'webm' },
    { value: 'mkv', label: 'mkv' },
  ];

  const audioFormatOptions = [
    { value: 'mp3', label: 'mp3' },
    { value: 'm4a', label: 'm4a' },
    { value: 'opus', label: 'opus' },
    { value: 'flac', label: 'flac' },
    { value: 'wav', label: 'wav' },
  ];

  const audioBitrateOptions = [
    { value: '320', label: '320 kbps' },
    { value: '256', label: '256 kbps' },
    { value: '192', label: '192 kbps' },
    { value: '128', label: '128 kbps' },
  ];

  const filenameOptions = [
    { value: 'classic', label: 'classic' },
    { value: 'basic', label: 'basic' },
    { value: 'pretty', label: 'pretty' },
    { value: 'nerdy', label: 'nerdy' },
  ];

  const playlistOptions = [
    { value: 'ask', label: 'ask me' },
    { value: 'video', label: 'just video' },
    { value: 'playlist', label: 'full playlist' },
  ];

  let filenameExample = $derived.by(() => {
    const ext = s.container || 'mp4';
    const examples = {
      classic: `Video Title (${s.quality}).${ext}`,
      basic: `Video Title.${ext}`,
      pretty: `Video Title - Author (${s.quality}, ${s.codec}).${ext}`,
      nerdy: `Video_Title_${s.quality}_${s.codec}_aac.${ext}`,
    };
    return examples[s.filenameStyle] || examples.basic;
  });

  function set(key, value) {
    settings.setSetting(key, value);
  }

  function resetAll() {
    if (confirm('Reset all settings to defaults?')) {
      settings.reset();
      addToast('Settings reset!', 'success');
    }
  }

  async function toggleAnalytics() {
    const wasEnabled = s.analytics !== false;

    if (wasEnabled) {
      const confirmed = confirm(
        'This will delete ALL your tracking data from the server!\n\n' +
        'When you opt out, all collected data is permanently deleted.\n\n' +
        'Are you sure?'
      );

      if (confirmed) {
        settings.setSetting('analytics', false);
        const result = await deleteUserAnalyticsData();
        if (result.deleted) {
          addToast('Analytics disabled & data deleted', 'success');
        } else {
          addToast('Analytics disabled', 'info');
        }
      }
    } else {
      settings.setSetting('analytics', true);
      addToast('Analytics enabled - thanks!', 'success');
    }
  }
</script>

<HeaderSimple />

<main>
  <div class="page-header">
    <h1>settings</h1>
    <p>customize your download experience</p>
  </div>

  <nav class="settings-nav">
    {#each tabs as tab}
      <button
        class="settings-nav-item"
        class:active={activeTab === tab.id}
        onclick={() => activeTab = tab.id}
      >
        <span>{tab.label}</span>
      </button>
    {/each}
  </nav>

  {#if activeTab === 'video'}
    <section class="settings-section">
      <h2 class="section-title">video</h2>

      <div class="settings-card">
        <div class="setting-label">video quality</div>
        <div class="quality-grid">
          {#each qualityOptions as q}
            <button
              class="quality-option"
              class:active={s.quality === q}
              onclick={() => set('quality', q)}
            >
              {qualityLabels[q]}
            </button>
          {/each}
        </div>
        <p class="setting-description">if preferred quality isn't available, the next best option will be used. 1080p is recommended for most uses.</p>
      </div>

      <div class="settings-card">
        <div class="setting-label">preferred codec</div>
        <div class="segmented-control">
          {#each codecOptions as opt}
            <button
              class="segment"
              class:active={s.codec === opt.value}
              onclick={() => set('codec', opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="setting-description">h264: best compatibility, max 1080p. av1: best quality & efficiency, supports 8k & HDR. vp9: same quality as av1 but ~2x larger.</p>
      </div>

      <div class="settings-card">
        <div class="setting-label">file container</div>
        <div class="segmented-control">
          {#each containerOptions as opt}
            <button
              class="segment"
              class:active={s.container === opt.value}
              onclick={() => set('container', opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="setting-description">mp4 has the best compatibility. webm is great for web. mkv supports more features but has limited playback support.</p>
      </div>
    </section>
  {/if}

  {#if activeTab === 'audio'}
    <section class="settings-section">
      <h2 class="section-title">audio</h2>

      <div class="settings-card">
        <div class="setting-label">audio format</div>
        <div class="segmented-control">
          {#each audioFormatOptions as opt}
            <button
              class="segment"
              class:active={s.audioFormat === opt.value}
              onclick={() => set('audioFormat', opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="setting-description">mp3: universal compatibility. m4a: better quality at same size. opus: best compression. flac/wav: lossless quality.</p>
      </div>

      <div class="settings-card">
        <div class="setting-label">audio bitrate</div>
        <div class="segmented-control">
          {#each audioBitrateOptions as opt}
            <button
              class="segment"
              class:active={s.audioBitrate === opt.value}
              onclick={() => set('audioBitrate', opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="setting-description">higher bitrate = better quality but larger file size. 320kbps is CD-quality for most people.</p>
      </div>
    </section>
  {/if}

  {#if activeTab === 'downloads'}
    <section class="settings-section">
      <h2 class="section-title">downloads</h2>

      <div class="settings-card">
        <div class="setting-label">filename style</div>
        <div class="segmented-control">
          {#each filenameOptions as opt}
            <button
              class="segment"
              class:active={s.filenameStyle === opt.value}
              onclick={() => set('filenameStyle', opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <div class="filename-preview">
          <code>{filenameExample}</code>
        </div>
      </div>

      <div class="settings-card">
        <div class="toggle-row">
          <div class="toggle-info">
            <div class="setting-label">twitter/x gifs as gif</div>
            <p class="setting-description">automatically convert twitter/x "gifs" (which are actually videos) to real gif files. turn off to keep them as mp4.</p>
          </div>
          <button
            class="toggle"
            class:active={s.twitterGifs !== false}
            onclick={() => set('twitterGifs', !s.twitterGifs || s.twitterGifs === undefined ? false : true)}
          ></button>
        </div>
      </div>

      <div class="settings-card">
        <div class="setting-label">playlist behavior</div>
        <div class="segmented-control">
          {#each playlistOptions as opt}
            <button
              class="segment"
              class:active={(s.playlistPreference === null ? 'ask' : s.playlistPreference) === opt.value}
              onclick={() => set('playlistPreference', opt.value === 'ask' ? null : opt.value)}
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="setting-description">when a link contains a playlist, should we download just the video or the entire playlist? "ask me" will show a popup each time.</p>
      </div>
    </section>
  {/if}

  {#if activeTab === 'advanced'}
    <section class="settings-section">
      <h2 class="section-title">advanced</h2>

      <div class="settings-card">
        <div class="toggle-row">
          <div class="toggle-info">
            <div class="setting-label">allow h265 codec</div>
            <p class="setting-description">enables downloading h265/HEVC videos for platforms like TikTok. higher quality but may have compatibility issues with some players.</p>
          </div>
          <button
            class="toggle"
            class:active={s.h265}
            onclick={() => set('h265', !s.h265)}
          ></button>
        </div>
      </div>

      <div class="settings-card">
        <div class="toggle-row">
          <div class="toggle-info">
            <div class="setting-label">embed metadata</div>
            <p class="setting-description">include title, artist, and other metadata in downloaded files.</p>
          </div>
          <button
            class="toggle"
            class:active={s.metadata !== false}
            onclick={() => set('metadata', s.metadata === false ? true : false)}
          ></button>
        </div>
      </div>

      <div class="divider"></div>

      <button class="reset-btn" onclick={resetAll}>
        reset all settings
      </button>
    </section>
  {/if}

  {#if activeTab === 'privacy'}
    <section class="settings-section">
      <h2 class="section-title">privacy</h2>

      <div class="settings-card">
        <div class="toggle-row">
          <div class="toggle-info">
            <div class="setting-label">allow anonymous analytics</div>
            <p class="setting-description">help improve yoink by sharing anonymous usage stats like download format preferences. no personal data or URLs are collected.</p>
          </div>
          <button
            class="toggle"
            class:active={s.analytics !== false}
            onclick={toggleAnalytics}
          ></button>
        </div>
      </div>

      <div class="settings-card highlight">
        <div class="setting-label">your data, your choice</div>
        <p class="setting-description">
          if you turn off analytics above, <strong>all data collected from you will be permanently deleted</strong>. no questions asked. if you don't want to be tracked, you shouldn't be - and that means wiping everything.
        </p>
      </div>

      <div class="settings-card">
        <div class="setting-label">what i collect</div>
        <p class="setting-description">
          when enabled: which file formats are popular, how many downloads happen daily, and general country data (from IP, not stored). i do <strong>not</strong> collect: URLs you download, video titles, your IP address, or any identifying information.
        </p>
      </div>

      <div class="settings-card">
        <div class="setting-label">why?</div>
        <p class="setting-description">
          honestly, just for fun! it's cool to see how people use the tool. if you'd rather not participate, flip the toggle above.
        </p>
        <p class="setting-description">
          <a href="#/privacy">read the full privacy policy</a>
        </p>
      </div>
    </section>
  {/if}
</main>

<FooterSimple />

<style>
  main {
    max-width: 700px;
    margin: 0 auto;
    padding: 32px 24px;
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: flex-start;
  }

  .page-header {
    text-align: center;
    margin-bottom: 32px;
  }

  .page-header h1 {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 2.5rem;
    letter-spacing: -0.03em;
    margin-bottom: 8px;
  }

  .page-header p {
    color: var(--text-secondary);
    font-size: 1rem;
  }

  .settings-nav {
    display: flex;
    gap: 8px;
    margin-bottom: 32px;
    flex-wrap: wrap;
    justify-content: center;
  }

  .settings-nav-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 16px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .settings-nav-item:hover {
    color: var(--text);
    border-color: var(--purple-400);
  }

  .settings-nav-item.active {
    background: var(--purple-500);
    border-color: var(--purple-500);
    color: white;
  }

  .settings-section {
    animation: fadeIn 0.2s ease-out;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .section-title {
    font-family: var(--font-heading);
    font-size: 1.25rem;
    font-weight: 700;
    margin-bottom: 24px;
    color: var(--purple-400);
  }

  .settings-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 20px;
    margin-bottom: 16px;
  }

  .settings-card.highlight {
    border-color: var(--purple-500);
    background: rgba(139, 92, 246, 0.05);
  }

  .setting-label {
    font-size: 0.95rem;
    font-weight: 500;
    color: var(--text);
    margin-bottom: 8px;
  }

  .setting-description {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin-top: 10px;
    line-height: 1.5;
  }

  .setting-description strong {
    color: var(--text);
  }

  .setting-description a {
    color: var(--purple-400);
    text-decoration: none;
  }

  .setting-description a:hover {
    color: var(--purple-300);
  }

  .segmented-control {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    background: var(--surface-elevated);
    border-radius: var(--radius-md);
    padding: 4px;
  }

  .segment {
    flex: 1;
    min-width: fit-content;
    padding: 10px 14px;
    font-family: var(--font-body);
    font-size: 0.85rem;
    font-weight: 500;
    color: var(--text-secondary);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
  }

  .segment:hover:not(.active) {
    color: var(--text);
    background: var(--border);
  }

  .segment.active {
    background: var(--purple-500);
    color: white;
  }

  .quality-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(70px, 1fr));
    gap: 4px;
    background: var(--surface-elevated);
    border-radius: var(--radius-md);
    padding: 4px;
  }

  .quality-option {
    padding: 12px 8px;
    font-family: var(--font-body);
    font-size: 0.85rem;
    font-weight: 500;
    color: var(--text-secondary);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
  }

  .quality-option:hover:not(.active) {
    color: var(--text);
    background: var(--border);
  }

  .quality-option.active {
    background: var(--purple-500);
    color: white;
  }

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }

  .toggle-info {
    flex: 1;
  }

  .toggle {
    position: relative;
    width: 52px;
    height: 28px;
    background: var(--surface-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-full);
    cursor: pointer;
    transition: all 0.2s ease-out;
    flex-shrink: 0;
  }

  .toggle::after {
    content: '';
    position: absolute;
    top: 3px;
    left: 3px;
    width: 20px;
    height: 20px;
    background: var(--text-muted);
    border-radius: 50%;
    transition: all 0.2s ease-out;
  }

  .toggle.active {
    background: var(--purple-500);
    border-color: var(--purple-500);
  }

  .toggle.active::after {
    left: 27px;
    background: white;
  }

  .filename-preview {
    margin-top: 12px;
    padding: 12px 16px;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .filename-preview code {
    font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
    color: var(--text);
  }

  .divider {
    height: 1px;
    background: var(--border);
    margin: 24px 0;
  }

  .reset-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    width: 100%;
    padding: 14px 20px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
    border: 1px solid var(--error);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .reset-btn:hover {
    background: rgba(248, 113, 113, 0.2);
  }

  @media (max-width: 600px) {
    main {
      padding: 24px 16px;
    }

    .page-header {
      margin-bottom: 24px;
    }

    .page-header h1 {
      font-size: 2rem;
    }

    .settings-nav {
      gap: 6px;
      flex-wrap: nowrap;
      overflow-x: auto;
      justify-content: flex-start;
      padding-bottom: 4px;
      -webkit-overflow-scrolling: touch;
      scrollbar-width: none;
    }

    .settings-nav::-webkit-scrollbar {
      display: none;
    }

    .settings-nav-item {
      padding: 10px 16px;
      font-size: 0.85rem;
      flex-shrink: 0;
    }

    .settings-card {
      padding: 16px;
    }

    .quality-grid {
      grid-template-columns: repeat(4, 1fr);
    }

    .segmented-control {
      flex-wrap: nowrap;
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
    }

    .segment {
      flex-shrink: 0;
    }
  }
</style>
