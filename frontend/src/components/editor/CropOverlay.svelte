<script>
  let {
    videoWidth,
    videoHeight,
    containerWidth,
    containerHeight,
    cropX = 0,
    cropY = 0,
    cropW = 0,
    cropH = 0,
    aspectLock = '',
    onCropChange = () => {},
  } = $props();

  let dragging = $state(null); // null | 'move' | 'nw' | 'ne' | 'sw' | 'se' | 'n' | 's' | 'e' | 'w'
  let dragStart = $state(null);

  const MIN_SIZE = 20;

  let scaleX = $derived(containerWidth && videoWidth ? containerWidth / videoWidth : 1);
  let scaleY = $derived(containerHeight && videoHeight ? containerHeight / videoHeight : 1);

  // convert video coords to render coords
  let renderX = $derived(cropX * scaleX);
  let renderY = $derived(cropY * scaleY);
  let renderW = $derived(cropW * scaleX);
  let renderH = $derived(cropH * scaleY);

  // mask percentages
  let maskTop = $derived(containerHeight > 0 ? (renderY / containerHeight) * 100 : 0);
  let maskBottom = $derived(containerHeight > 0 ? ((containerHeight - renderY - renderH) / containerHeight) * 100 : 0);
  let maskLeft = $derived(containerWidth > 0 ? (renderX / containerWidth) * 100 : 0);
  let maskRight = $derived(containerWidth > 0 ? ((containerWidth - renderX - renderW) / containerWidth) * 100 : 0);

  let dimLabel = $derived(`${Math.round(cropW)} x ${Math.round(cropH)}`);

  function clampRect(x, y, w, h) {
    w = Math.max(MIN_SIZE, Math.min(w, videoWidth));
    h = Math.max(MIN_SIZE, Math.min(h, videoHeight));
    x = Math.max(0, Math.min(x, videoWidth - w));
    y = Math.max(0, Math.min(y, videoHeight - h));
    return { x, y, w, h };
  }

  function applyAspectLock(w, h, handle) {
    if (!aspectLock) return { w, h };
    const [aw, ah] = aspectLock.split(':').map(Number);
    if (!aw || !ah) return { w, h };
    const ratio = aw / ah;

    // for corner handles, use the larger dimension to determine the other
    if (handle === 'nw' || handle === 'ne' || handle === 'sw' || handle === 'se') {
      if (w / h > ratio) {
        w = h * ratio;
      } else {
        h = w / ratio;
      }
    } else if (handle === 'e' || handle === 'w') {
      h = w / ratio;
    } else if (handle === 'n' || handle === 's') {
      w = h * ratio;
    }
    return { w: Math.max(MIN_SIZE, w), h: Math.max(MIN_SIZE, h) };
  }

  function onPointerDown(e, handle) {
    e.preventDefault();
    e.stopPropagation();
    dragging = handle;
    dragStart = {
      clientX: e.clientX,
      clientY: e.clientY,
      cropX, cropY, cropW, cropH,
    };
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
  }

  function onPointerMove(e) {
    if (!dragging || !dragStart) return;
    e.preventDefault();

    const dx = (e.clientX - dragStart.clientX) / scaleX;
    const dy = (e.clientY - dragStart.clientY) / scaleY;
    const { cropX: sx, cropY: sy, cropW: sw, cropH: sh } = dragStart;

    let nx = sx, ny = sy, nw = sw, nh = sh;

    if (dragging === 'move') {
      nx = sx + dx;
      ny = sy + dy;
      const clamped = clampRect(nx, ny, nw, nh);
      onCropChange(clamped.x, clamped.y, clamped.w, clamped.h);
      return;
    }

    // resize handles
    if (dragging.includes('w')) {
      nw = sw - dx;
      nx = sx + dx;
    }
    if (dragging.includes('e')) {
      nw = sw + dx;
    }
    if (dragging.includes('n')) {
      nh = sh - dy;
      ny = sy + dy;
    }
    if (dragging.includes('s')) {
      nh = sh + dy;
    }

    // edge-only handles shouldn't change the other dimension (before aspect lock)
    if (dragging === 'n' || dragging === 's') nw = sw;
    if (dragging === 'e' || dragging === 'w') nh = sh;

    const locked = applyAspectLock(nw, nh, dragging);
    nw = locked.w;
    nh = locked.h;

    // re-anchor after aspect correction
    if (dragging.includes('w')) nx = sx + sw - nw;
    if (dragging.includes('n')) ny = sy + sh - nh;

    // prevent negative dimensions
    if (nw < MIN_SIZE) { nw = MIN_SIZE; if (dragging.includes('w')) nx = sx + sw - MIN_SIZE; }
    if (nh < MIN_SIZE) { nh = MIN_SIZE; if (dragging.includes('n')) ny = sy + sh - MIN_SIZE; }

    const clamped = clampRect(nx, ny, nw, nh);
    onCropChange(clamped.x, clamped.y, clamped.w, clamped.h);
  }

  function onPointerUp() {
    dragging = null;
    dragStart = null;
    window.removeEventListener('pointermove', onPointerMove);
    window.removeEventListener('pointerup', onPointerUp);
  }

  const handles = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

  function handlePosition(h) {
    const pos = {};
    if (h.includes('n')) pos.top = '0%';
    if (h.includes('s')) pos.top = '100%';
    if (h === 'e' || h === 'w' || h === 'n' || h === 's') {
      if (h === 'n' || h === 's') pos.left = '50%';
      if (h === 'e') { pos.top = '50%'; pos.left = '100%'; }
      if (h === 'w') { pos.top = '50%'; pos.left = '0%'; }
    }
    if (h === 'nw') pos.left = '0%';
    if (h === 'ne') pos.left = '100%';
    if (h === 'sw') pos.left = '0%';
    if (h === 'se') pos.left = '100%';
    return `top: ${pos.top || '0%'}; left: ${pos.left || '0%'}; transform: translate(-50%, -50%);`;
  }

  function handleCursor(h) {
    const cursors = { nw: 'nwse-resize', ne: 'nesw-resize', sw: 'nesw-resize', se: 'nwse-resize', n: 'ns-resize', s: 'ns-resize', e: 'ew-resize', w: 'ew-resize' };
    return cursors[h] || 'move';
  }
</script>

<div class="crop-overlay-container" style="width: {containerWidth}px; height: {containerHeight}px;">
  <!-- dark masks -->
  <div class="mask mask-top" style="height: {maskTop}%"></div>
  <div class="mask mask-bottom" style="height: {maskBottom}%"></div>
  <div class="mask mask-left" style="width: {maskLeft}%; top: {maskTop}%; bottom: {maskBottom}%"></div>
  <div class="mask mask-right" style="width: {maskRight}%; top: {maskTop}%; bottom: {maskBottom}%"></div>

  <!-- crop rect -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="crop-rect"
    style="left: {renderX}px; top: {renderY}px; width: {renderW}px; height: {renderH}px;"
    onpointerdown={(e) => onPointerDown(e, 'move')}
    role="application"
  >
    <!-- handles -->
    {#each handles as h}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="handle"
        style="{handlePosition(h)} cursor: {handleCursor(h)};"
        onpointerdown={(e) => onPointerDown(e, h)}
      ></div>
    {/each}
  </div>

  <!-- dimension label -->
  {#if renderW > 60 && renderH > 20}
    <div class="dim-label" style="left: {renderX + renderW / 2}px; top: {renderY + renderH + 6}px;">
      {dimLabel}
    </div>
  {/if}
</div>

<style>
  .crop-overlay-container {
    position: absolute;
    top: 0;
    left: 0;
    pointer-events: none;
    z-index: 5;
  }

  .mask {
    position: absolute;
    background: rgba(0, 0, 0, 0.55);
    pointer-events: none;
  }

  .mask-top {
    top: 0;
    left: 0;
    right: 0;
  }

  .mask-bottom {
    bottom: 0;
    left: 0;
    right: 0;
  }

  .mask-left {
    left: 0;
  }

  .mask-right {
    right: 0;
  }

  .crop-rect {
    position: absolute;
    border: 3px solid var(--purple-300);
    cursor: move;
    pointer-events: auto;
    box-sizing: border-box;
    touch-action: none;
    box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.3);
  }

  .handle {
    position: absolute;
    width: 16px;
    height: 16px;
    background: var(--purple-300);
    border: 2px solid white;
    border-radius: 2px;
    pointer-events: auto;
    touch-action: none;
    z-index: 2;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
  }

  .dim-label {
    position: absolute;
    transform: translateX(-50%);
    font-size: 0.7rem;
    font-family: monospace;
    color: var(--text-secondary);
    background: rgba(0, 0, 0, 0.7);
    padding: 2px 6px;
    border-radius: 4px;
    pointer-events: none;
    white-space: nowrap;
  }

  @media (max-width: 600px) {
    .handle {
      width: 24px;
      height: 24px;
    }
  }
</style>
