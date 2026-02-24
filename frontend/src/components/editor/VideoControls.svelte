<script>
  let {
    isPlaying = false,
    currentTime = 0,
    duration = 0,
    onPlayPause = () => {},
    onSplitAtPlayhead = () => {},
  } = $props();

  function formatTimeDisplay(seconds) {
    if (!seconds || !isFinite(seconds)) return '0:00.0';
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    const tenths = Math.floor((seconds % 1) * 10);
    return `${mins}:${secs.toString().padStart(2, '0')}.${tenths}`;
  }
</script>

<div class="controls">
  <button class="play-btn" onclick={onPlayPause} title={isPlaying ? 'Pause' : 'Play'}>
    {#if isPlaying}
      <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
        <rect x="6" y="4" width="4" height="16" rx="1"></rect>
        <rect x="14" y="4" width="4" height="16" rx="1"></rect>
      </svg>
    {:else}
      <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
        <polygon points="6 3 20 12 6 21 6 3"></polygon>
      </svg>
    {/if}
  </button>

  <div class="time-display">
    <span class="time-current">{formatTimeDisplay(currentTime)}</span>
    <span class="time-sep">/</span>
    <span class="time-total">{formatTimeDisplay(duration)}</span>
  </div>

  <button class="split-btn" onclick={onSplitAtPlayhead} title="Split at playhead">
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="6" cy="6" r="3"></circle>
      <circle cx="6" cy="18" r="3"></circle>
      <line x1="20" y1="4" x2="8.12" y2="15.88"></line>
      <line x1="14.47" y1="14.48" x2="20" y2="20"></line>
      <line x1="8.12" y1="8.12" x2="12" y2="12"></line>
    </svg>
    split
  </button>
</div>

<style>
  .controls {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .play-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    background: var(--purple-500);
    color: white;
    border: none;
    border-radius: 50%;
    cursor: pointer;
    transition: all 0.15s ease-out;
    flex-shrink: 0;
  }

  .play-btn:hover {
    background: var(--purple-400);
    transform: scale(1.05);
  }

  .time-display {
    font-family: monospace;
    font-size: 0.85rem;
    color: var(--text);
    flex: 1;
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .time-sep {
    color: var(--text-muted);
  }

  .time-total {
    color: var(--text-secondary);
  }

  .split-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    font-family: var(--font-body);
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--purple-400);
    background: var(--purple-900);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    flex-shrink: 0;
  }

  .split-btn:hover {
    background: var(--purple-500);
    color: white;
  }

  @media (max-width: 600px) {
    .controls {
      padding: 8px;
      gap: 8px;
    }

    .split-btn {
      padding: 7px 10px;
      font-size: 0.75rem;
    }
  }
</style>
