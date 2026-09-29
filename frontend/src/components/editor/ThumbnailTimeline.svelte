<script>
  let {
    duration = 0,
    currentTime = 0,
    startTime = 0,
    endTime = 0,
    thumbnails = [],
    thumbnailsLoading = false,
    segments = [],
    onSeek = () => {},
    onStartTimeChange = () => {},
    onEndTimeChange = () => {},
  } = $props();

  let timelineEl = $state(null);
  let draggingHandle = $state(null); // 'start' | 'end' | 'playhead' | null

  let trimStartPct = $derived(duration > 0 ? (startTime / duration) * 100 : 0);
  let trimEndPct = $derived(duration > 0 ? (endTime / duration) * 100 : 100);
  let playheadPct = $derived(duration > 0 ? (currentTime / duration) * 100 : 0);

  function clientXToTime(clientX) {
    if (!timelineEl || duration <= 0) return 0;
    const rect = timelineEl.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return pct * duration;
  }

  function onTimelineClick(e) {
    if (draggingHandle) return;
    const time = clientXToTime(e.clientX);
    onSeek(time);
  }

  function startDrag(e, handle) {
    e.preventDefault();
    e.stopPropagation();
    draggingHandle = handle;
    window.addEventListener('pointermove', onDragMove);
    window.addEventListener('pointerup', onDragEnd);
  }

  function onDragMove(e) {
    if (!draggingHandle) return;
    e.preventDefault();
    const time = clientXToTime(e.clientX);

    if (draggingHandle === 'start') {
      onStartTimeChange(Math.min(time, endTime - 0.1));
    } else if (draggingHandle === 'end') {
      onEndTimeChange(Math.max(time, startTime + 0.1));
    } else if (draggingHandle === 'playhead') {
      onSeek(Math.max(0, Math.min(duration, time)));
    }
  }

  function onDragEnd() {
    draggingHandle = null;
    window.removeEventListener('pointermove', onDragMove);
    window.removeEventListener('pointerup', onDragEnd);
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="timeline" bind:this={timelineEl} onclick={onTimelineClick}>
  <!-- thumbnail strip -->
  <div class="thumbnail-strip">
    {#if thumbnails.length > 0}
      {#each thumbnails as thumb}
        <img src={thumb} alt="" class="thumb" draggable="false" />
      {/each}
    {:else}
      <div class="thumb-placeholder" class:loading={thumbnailsLoading}>
        {#if thumbnailsLoading}
          <span class="thumb-loading-text">generating thumbnails...</span>
        {/if}
      </div>
    {/if}
  </div>

  <!-- dim overlays for trimmed regions -->
  <div class="dim-overlay dim-left" style="width: {trimStartPct}%"></div>
  <div class="dim-overlay dim-right" style="width: {100 - trimEndPct}%"></div>

  <!-- segment markers -->
  {#each segments as seg, i}
    {#if i > 0}
      {@const pct = duration > 0 ? (seg.start / duration) * 100 : 0}
      <div class="segment-marker" style="left: {pct}%"></div>
    {/if}
  {/each}

  <!-- trim handles -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="trim-handle trim-handle-left"
    style="left: {trimStartPct}%"
    onpointerdown={(e) => startDrag(e, 'start')}
  >
    <div class="trim-handle-bar"></div>
  </div>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="trim-handle trim-handle-right"
    style="left: {trimEndPct}%"
    onpointerdown={(e) => startDrag(e, 'end')}
  >
    <div class="trim-handle-bar"></div>
  </div>

  <!-- playhead -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="playhead"
    style="left: {playheadPct}%"
    onpointerdown={(e) => startDrag(e, 'playhead')}
  >
    <div class="playhead-head"></div>
    <div class="playhead-line"></div>
  </div>
</div>

<style>
  .timeline {
    position: relative;
    height: 64px;
    border-radius: 20px;
    overflow: hidden;
    cursor: pointer;
    background: var(--surface-elevated);
    user-select: none;
    touch-action: none;
    border: 3px solid var(--purple-300);
    box-shadow: 0 0 12px rgba(139, 92, 246, 0.15);
  }

  .thumbnail-strip {
    display: flex;
    width: 100%;
    height: 100%;
    position: absolute;
    top: 0;
    left: 0;
  }

  .thumb {
    flex: 1;
    height: 100%;
    object-fit: cover;
    min-width: 0;
    pointer-events: none;
  }

  .thumb-placeholder {
    flex: 1;
    height: 100%;
    background: var(--surface);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .thumb-placeholder.loading {
    background: linear-gradient(90deg, var(--surface) 0%, var(--surface-elevated) 50%, var(--surface) 100%);
    background-size: 200% 100%;
    animation: shimmer 1.5s infinite;
  }

  @keyframes shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  .thumb-loading-text {
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .dim-overlay {
    position: absolute;
    top: 0;
    height: 100%;
    background: rgba(0, 0, 0, 0.6);
    pointer-events: none;
    z-index: 1;
  }

  .dim-left {
    left: 0;
  }

  .dim-right {
    right: 0;
  }

  .segment-marker {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
    background: var(--warning);
    opacity: 0.8;
    z-index: 2;
    transform: translateX(-1px);
    pointer-events: none;
  }

  .trim-handle {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 16px;
    transform: translateX(-50%);
    cursor: ew-resize;
    z-index: 4;
    display: flex;
    align-items: center;
    justify-content: center;
    touch-action: none;
  }

  .trim-handle-bar {
    width: 4px;
    height: 28px;
    background: var(--purple-400);
    border-radius: 2px;
    box-shadow: 0 0 4px rgba(0, 0, 0, 0.4);
  }

  .trim-handle:hover .trim-handle-bar,
  .trim-handle:active .trim-handle-bar {
    background: var(--purple-300);
    width: 5px;
  }

  .playhead {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 20px;
    transform: translateX(-50%);
    cursor: ew-resize;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: center;
    touch-action: none;
  }

  .playhead-head {
    width: 10px;
    height: 10px;
    background: white;
    border-radius: 50%;
    flex-shrink: 0;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.5);
    margin-top: -2px;
  }

  .playhead-line {
    width: 2px;
    flex: 1;
    background: white;
    box-shadow: 0 0 4px rgba(0, 0, 0, 0.4);
  }

  @media (max-width: 600px) {
    .trim-handle {
      width: 24px;
    }

    .playhead {
      width: 28px;
    }

    .playhead-head {
      width: 14px;
      height: 14px;
    }
  }
</style>
