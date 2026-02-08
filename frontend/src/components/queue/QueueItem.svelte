<script>
  import { queue } from '../../stores/queue.js';
  import { ACTIVE_STAGES } from '../../lib/constants.js';

  let { item } = $props();

  let isActive = $derived(ACTIVE_STAGES.includes(item.stage));
  let isComplete = $derived(item.stage === 'complete');
  let isError = $derived(item.stage === 'error');
  let isQueued = $derived(item.stage === 'queued');
  let isReady = $derived(item.stage === 'ready');
  let showProgress = $derived(isActive && item.progress !== undefined);

  let statusClass = $derived(
    isReady ? 'success' : isActive ? 'active' : isComplete ? 'success' : isError ? 'error' : ''
  );

  let fileSizeText = $derived.by(() => {
    if (!item.fileSize) return '';
    const mb = item.fileSize / (1024 * 1024);
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${mb.toFixed(1)} MB`;
  });

  let timerText = $derived.by(() => {
    if (!item.startTime || (!isActive && !isComplete)) return '';
    const end = isComplete && item.endTime ? item.endTime : Date.now();
    const secs = Math.floor((end - item.startTime) / 1000);
    const mins = Math.floor(secs / 60);
    return mins > 0 ? `${mins}m ${secs % 60}s` : `${secs}s`;
  });

  let statusText = $derived.by(() => {
    if (item.isPlaylist && item.currentVideo && item.videoCount) {
      return `${item.currentVideo}/${item.videoCount} - ${item.currentVideoTitle || item.status}`;
    }
    let text = item.status || 'waiting...';
    if (isActive && (item.speed || item.eta)) {
      const parts = [];
      if (item.speed) parts.push(item.speed);
      if (item.eta) parts.push(`~${item.eta} left`);
      if (parts.length) text += ` · ${parts.join(' · ')}`;
    }
    return text;
  });
</script>

<div class="queue-item" class:clickable={isReady || isQueued} onclick={() => { if (isReady) queue.downloadReady(item.id); else if (isQueued) queue.startDownload(item); }}>
  <div class="queue-item-header">
    <div class="queue-item-icon" class:spinning={isActive} class:ready={isReady}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        {#if isReady}
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
          <polyline points="7 10 12 15 17 10"></polyline>
          <line x1="12" y1="15" x2="12" y2="3"></line>
        {:else if isComplete}
          <polyline points="20 6 9 17 4 12"></polyline>
        {:else if isError}
          <line x1="18" y1="6" x2="6" y2="18"></line>
          <line x1="6" y1="6" x2="18" y2="18"></line>
        {:else if item.isPlaylist}
          <line x1="8" y1="6" x2="21" y2="6"></line>
          <line x1="8" y1="12" x2="21" y2="12"></line>
          <line x1="8" y1="18" x2="21" y2="18"></line>
          <line x1="3" y1="6" x2="3.01" y2="6"></line>
          <line x1="3" y1="12" x2="3.01" y2="12"></line>
          <line x1="3" y1="18" x2="3.01" y2="18"></line>
        {:else if item.format === 'audio'}
          <path d="M9 18V5l12-2v13"></path>
          <circle cx="6" cy="18" r="3"></circle>
          <circle cx="18" cy="16" r="3"></circle>
        {:else if isActive}
          <path d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
          <path d="M9 12l2 2 4-4"></path>
        {:else}
          <polygon points="5 3 19 12 5 21 5 3"></polygon>
        {/if}
      </svg>
    </div>
    <div class="queue-item-info">
      <div class="queue-item-title">
        {item.title}
        {#if item.isPlaylist && item.formatDisplay}
          <span class="queue-item-format">{item.formatDisplay}</span>
        {/if}
        {#if isReady && fileSizeText}
          <span class="queue-item-timer">{fileSizeText}</span>
        {:else if timerText}
          <span class="queue-item-timer">{timerText}</span>
        {/if}
      </div>
      <div class="queue-item-status {statusClass}">{statusText}</div>
    </div>
    <div class="queue-item-actions">
      {#if isReady}
        <button class="queue-item-download" onclick={(e) => { e.stopPropagation(); queue.downloadReady(item.id); }} title="Download">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
        </button>
      {:else if isError && item.url}
        <button class="queue-item-retry" onclick={(e) => { e.stopPropagation(); queue.retryDownload(item.id); }} title="Retry">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="1 4 1 10 7 10"></polyline>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
          </svg>
        </button>
      {/if}
      {#if isQueued}
        <button class="queue-item-retry" onclick={(e) => { e.stopPropagation(); queue.startDownload(item); }} title="Start now">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="5 3 19 12 5 21 5 3"></polygon>
          </svg>
        </button>
      {/if}
      {#if item.isPlaylist && isActive && item.currentVideo > 0}
        <button class="queue-item-retry" onclick={(e) => { e.stopPropagation(); queue.finishPlaylistEarly(item.id); }} title="Download now (skip remaining)">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
        </button>
      {/if}
      <button class="queue-item-remove" onclick={(e) => { e.stopPropagation(); queue.remove(item.id); }} title="Remove">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="18" y1="6" x2="6" y2="18"></line>
          <line x1="6" y1="6" x2="18" y2="18"></line>
        </svg>
      </button>
    </div>
  </div>

  {#if showProgress}
    <div class="queue-item-progress">
      <div class="queue-item-progress-fill" style="width: {item.progress || 0}%"></div>
    </div>
  {/if}

  {#if item.isPlaylist && item.failedVideos?.length > 0}
    <div class="queue-item-failed">
      <div class="queue-item-failed-header">failed videos ({item.failedCount || item.failedVideos.length})</div>
      {#each item.failedVideos as v}
        <div class="queue-item-failed-item">
          <span class="queue-item-failed-num">#{v.num}</span>
          <span>{v.title || 'Video'} - {v.reason || 'Download failed'}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .queue-item {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    margin-bottom: 4px;
  }

  .queue-item.clickable {
    cursor: pointer;
  }

  .queue-item.clickable:hover {
    background: var(--surface-hover, var(--surface-elevated));
  }

  .queue-item:last-child {
    margin-bottom: 0;
  }

  .queue-item-header {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .queue-item-icon {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--purple-900);
    border-radius: var(--radius-sm);
    color: var(--purple-400);
    flex-shrink: 0;
  }

  .queue-item-icon svg {
    width: 14px;
    height: 14px;
  }

  .queue-item-icon.spinning svg {
    animation: spin 1s linear infinite;
  }

  .queue-item-icon.ready {
    background: rgba(74, 222, 128, 0.1);
    color: var(--success);
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .queue-item-info {
    flex: 1;
    min-width: 0;
  }

  .queue-item-title {
    font-size: 0.85rem;
    font-weight: 500;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .queue-item-format {
    display: inline-block;
    margin-left: 8px;
    padding: 2px 6px;
    background: var(--purple-900);
    color: var(--purple-400);
    font-size: 0.7rem;
    font-weight: 600;
    border-radius: var(--radius-sm);
    text-transform: uppercase;
  }

  .queue-item-timer {
    display: inline-block;
    margin-left: 8px;
    padding: 2px 6px;
    background: var(--surface-elevated);
    color: var(--text-secondary);
    font-size: 0.7rem;
    font-weight: 500;
    border-radius: var(--radius-sm);
    font-family: monospace;
  }

  .queue-item-status {
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .queue-item-status.active { color: var(--purple-400); }
  .queue-item-status.success { color: var(--success); }
  .queue-item-status.error { color: var(--error); }

  .queue-item-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .queue-item-progress {
    width: 100%;
    height: 4px;
    background: var(--surface);
    border-radius: 2px;
    overflow: hidden;
  }

  .queue-item-progress-fill {
    height: 100%;
    background: var(--purple-500);
    border-radius: 2px;
    transition: width 0.3s ease-out;
  }

  .queue-item-remove {
    padding: 8px;
    background: transparent;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
  }

  .queue-item-remove:hover {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
  }

  .queue-item-remove:active {
    transform: scale(0.9);
  }

  .queue-item-retry {
    padding: 8px;
    background: transparent;
    border: none;
    color: var(--purple-400);
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
  }

  .queue-item-retry:hover {
    color: var(--purple-300);
    background: var(--purple-900);
  }

  .queue-item-retry:active {
    transform: scale(0.9);
  }

  .queue-item-download {
    padding: 8px;
    background: transparent;
    border: none;
    color: var(--success);
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
  }

  .queue-item-download:hover {
    color: #4ade80;
    background: rgba(74, 222, 128, 0.1);
  }

  .queue-item-download:active {
    transform: scale(0.9);
  }

  .queue-item-failed {
    padding: 8px;
    background: rgba(248, 113, 113, 0.05);
    border: 1px solid rgba(248, 113, 113, 0.2);
    border-radius: var(--radius-sm);
    font-size: 0.75rem;
  }

  .queue-item-failed-header {
    color: var(--error);
    font-weight: 600;
    margin-bottom: 4px;
  }

  .queue-item-failed-item {
    color: var(--text-muted);
    padding: 2px 0;
  }

  .queue-item-failed-num {
    color: var(--text-secondary);
    margin-right: 6px;
  }
</style>
