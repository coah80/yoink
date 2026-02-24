<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
  import QueueToggle from '../components/queue/QueueToggle.svelte';
  import ProgressBar from '../components/ui/ProgressBar.svelte';
  import { apiBase } from '../lib/api.js';
  import { uploadChunked } from '../lib/upload.js';
  import { downloadBlob } from '../lib/download.js';
  import { formatBytes } from '../lib/utils.js';
  import { addToast } from '../stores/toast.js';
  import { startHeartbeat, stopHeartbeat } from '../stores/session.js';
  import { queue } from '../stores/queue.js';
  import { getQuery } from '../lib/router.js';

  let selectedFile = $state(null);
  let urlInput = $state('');
  let inputMode = $state('file');
  let fetchedFile = $state(null);

  const initialQuery = getQuery();
  if (initialQuery.url) {
    urlInput = initialQuery.url;
    inputMode = 'url';
  }

  let videoDuration = $state(0);
  let videoWidth = $state(0);
  let videoHeight = $state(0);
  let startTime = $state(0);
  let endTime = $state(0);
  let startTimeInput = $state('');
  let endTimeInput = $state('');
  let cropRatio = $state('');
  let processing = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let statusType = $state(null);
  let statusMessage = $state('');
  let dragging = $state(false);
  let inputEl;
  let videoEl = $state(null);
  let videoSrc = $state('');

  const cropOptions = [
    { value: '', label: 'original' },
    { value: '16:9', label: '16:9' },
    { value: '9:16', label: '9:16' },
    { value: '1:1', label: '1:1' },
    { value: '4:3', label: '4:3' },
    { value: '4:5', label: '4:5' },
  ];

  let trimDuration = $derived.by(() => {
    if (endTime > startTime) return endTime - startTime;
    return videoDuration;
  });

  let trimDurationText = $derived.by(() => {
    const d = trimDuration;
    if (!d) return '--:--';
    const mins = Math.floor(d / 60);
    const secs = Math.floor(d % 60);
    const tenths = Math.floor((d % 1) * 10);
    return `${mins}:${secs.toString().padStart(2, '0')}.${tenths}`;
  });

  let trimBarStart = $derived(videoDuration > 0 ? (startTime / videoDuration) * 100 : 0);
  let trimBarEnd = $derived(videoDuration > 0 ? (endTime / videoDuration) * 100 : 100);

  let buttonLabel = $derived(cropRatio ? 'trim & crop' : 'trim');
  let hasValidTrim = $derived(endTime > startTime && endTime > 0);

  function formatTime(seconds) {
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    const tenths = Math.floor((seconds % 1) * 10);
    return `${mins}:${secs.toString().padStart(2, '0')}.${tenths}`;
  }

  function parseTimeInput(str) {
    if (!str) return null;
    str = str.trim();
    if (/^\d+(\.\d+)?$/.test(str)) return parseFloat(str);
    const match = str.match(/^(\d+):(\d{1,2})(?:\.(\d+))?$/);
    if (match) {
      return parseInt(match[1]) * 60 + parseInt(match[2]) + (match[3] ? parseFloat('0.' + match[3]) : 0);
    }
    return null;
  }

  function handleVideoLoaded() {
    if (!videoEl) return;
    videoDuration = videoEl.duration;
    videoWidth = videoEl.videoWidth;
    videoHeight = videoEl.videoHeight;
    endTime = videoEl.duration;
    endTimeInput = formatTime(videoEl.duration);
  }

  async function handleFile(file) {
    if (!file) return;
    if (!file.type.startsWith('video/')) {
      addToast('please select a video file', 'error');
      return;
    }
    const maxSize = 8 * 1024 * 1024 * 1024;
    if (file.size > maxSize) {
      addToast('file too large. maximum size is 8GB.', 'error');
      return;
    }
    selectedFile = file;
    statusType = null;
    statusMessage = '';
    if (videoSrc) URL.revokeObjectURL(videoSrc);
    videoSrc = URL.createObjectURL(file);
  }

  function removeFile() {
    selectedFile = null;
    fetchedFile = null;
    if (videoSrc) URL.revokeObjectURL(videoSrc);
    videoSrc = '';
    videoDuration = 0;
    videoWidth = 0;
    videoHeight = 0;
    startTime = 0;
    endTime = 0;
    startTimeInput = '';
    endTimeInput = '';
    cropRatio = '';
    progress = 0;
    progressLabel = '';
    statusType = null;
    statusMessage = '';
    if (inputEl) inputEl.value = '';
  }

  function setStart() {
    if (!videoEl) return;
    startTime = videoEl.currentTime;
    startTimeInput = formatTime(startTime);
  }

  function setEnd() {
    if (!videoEl) return;
    endTime = videoEl.currentTime;
    endTimeInput = formatTime(endTime);
  }

  function handleStartInput(value) {
    startTimeInput = value;
    const parsed = parseTimeInput(value);
    if (parsed !== null && parsed >= 0) startTime = parsed;
  }

  function handleEndInput(value) {
    endTimeInput = value;
    const parsed = parseTimeInput(value);
    if (parsed !== null && parsed >= 0) endTime = parsed;
  }

  // crop overlay dimensions
  let cropOverlay = $derived.by(() => {
    if (!cropRatio || !videoWidth || !videoHeight) return null;
    const [rw, rh] = cropRatio.split(':').map(Number);
    const vidAspect = videoWidth / videoHeight;
    const cropAspect = rw / rh;
    let left = 0, top = 0, right = 0, bottom = 0;
    if (vidAspect > cropAspect) {
      // wider than target, crop sides
      const visibleWidth = cropAspect / vidAspect;
      const margin = (1 - visibleWidth) / 2;
      left = margin * 100;
      right = margin * 100;
    } else {
      // taller than target, crop top/bottom
      const visibleHeight = vidAspect / cropAspect;
      const margin = (1 - visibleHeight) / 2;
      top = margin * 100;
      bottom = margin * 100;
    }
    return { left, top, right, bottom };
  });

  async function safeParseError(res) {
    let text = '';
    try {
      const body = await res.blob();
      text = await body.text();
    } catch {
      text = await res.text().catch(() => '');
    }
    try {
      const data = JSON.parse(text);
      return data.error || `Request failed with status ${res.status}`;
    } catch {
      return text || `Request failed with status ${res.status}`;
    }
  }

  async function processTrim() {
    const isUrlMode = inputMode === 'url' && urlInput.trim();
    if ((!selectedFile && !isUrlMode && !fetchedFile) || processing) return;

    if (!hasValidTrim && !cropRatio) {
      addToast('set start and end times, or pick a crop ratio', 'error');
      return;
    }

    processing = true;
    progress = 0;
    statusType = null;
    statusMessage = '';

    const queueId = Date.now().toString() + Math.random().toString(36).substr(2, 9);
    const fileName = fetchedFile ? fetchedFile.fileName : (selectedFile ? selectedFile.name : 'video');
    const queueTitle = `${fileName} → ${buttonLabel}`;
    queue.add({
      id: queueId,
      title: queueTitle,
      type: 'convert',
      stage: isUrlMode && !fetchedFile ? 'downloading' : 'uploading',
      status: isUrlMode && !fetchedFile ? 'downloading media...' : 'uploading...',
      progress: 0,
      startTime: Date.now(),
    });

    let heartbeatJobId = null;

    try {
      heartbeatJobId = startHeartbeat();

      let filePath, uploadedFileName;

      if (isUrlMode && !fetchedFile) {
        // fetch URL first
        progressLabel = 'Downloading media...';
        queue.updateItem(queueId, { status: 'downloading media...' });

        const fetchRes = await fetch(`${apiBase()}/api/fetch-url`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: urlInput.trim() }),
        });

        if (!fetchRes.ok) {
          let errMsg = 'Failed to download from URL';
          try { const err = await fetchRes.json(); errMsg = err.error || errMsg; } catch {}
          throw new Error(errMsg);
        }

        const fetchData = await fetchRes.json();
        fetchedFile = fetchData;
        filePath = fetchData.filePath;
        uploadedFileName = fetchData.fileName;

        if (!videoDuration && fetchData.duration) videoDuration = fetchData.duration;
        if (!videoWidth && fetchData.width) videoWidth = fetchData.width;
        if (!videoHeight && fetchData.height) videoHeight = fetchData.height;
        if (!endTime && fetchData.duration) {
          endTime = fetchData.duration;
          endTimeInput = formatTime(fetchData.duration);
        }

        queue.updateItem(queueId, {
          title: `${fetchData.fileName} → ${buttonLabel}`,
          stage: 'processing',
          status: 'processing...',
          progress: 0
        });
      } else if (fetchedFile) {
        filePath = fetchedFile.filePath;
        uploadedFileName = fetchedFile.fileName;
        queue.updateItem(queueId, { stage: 'processing', status: 'processing...', progress: 0 });
      } else {
        // file upload
        progressLabel = 'Uploading...';
        const result = await uploadChunked(selectedFile, (p) => {
          progress = p;
          progressLabel = 'Uploading...';
          queue.updateItem(queueId, { progress: p, status: 'uploading...' });
        });
        filePath = result.filePath;
        uploadedFileName = result.fileName;

        queue.updateItem(queueId, { stage: 'processing', status: 'processing...', progress: 0 });
      }

      progress = 0;
      progressLabel = 'Processing...';

      const body = {
        filePath,
        fileName: uploadedFileName,
        format: 'mp4',
        reencode: 'always',
      };
      if (hasValidTrim) {
        body.startTime = String(startTime);
        body.endTime = String(endTime);
      }
      if (cropRatio) {
        body.cropRatio = cropRatio;
      }

      const initResponse = await fetch(`${apiBase()}/api/convert-chunked`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });

      if (!initResponse.ok) {
        throw new Error(await safeParseError(initResponse));
      }

      const { jobId } = await initResponse.json();
      let pollAttempts = 0;

      while (true) {
        if (pollAttempts >= 600) throw new Error('Processing timed out after 20 minutes');
        pollAttempts++;
        await new Promise(r => setTimeout(r, 2000));

        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 10000);
        let statusRes;
        try {
          statusRes = await fetch(`${apiBase()}/api/job/${jobId}/status`, { signal: controller.signal });
        } finally {
          clearTimeout(timeoutId);
        }

        if (!statusRes.ok) {
          if (statusRes.status === 404) throw new Error('Job not found or expired');
          throw new Error('Failed to check job status');
        }

        const jobStatus = await statusRes.json();
        if (jobStatus.status === 'error') throw new Error(jobStatus.error || 'Processing failed');
        progress = jobStatus.progress || 0;
        progressLabel = jobStatus.message || 'processing...';
        queue.updateItem(queueId, { progress: jobStatus.progress || 0, status: jobStatus.message || 'processing...' });
        if (jobStatus.status === 'complete') break;
      }

      const response = await fetch(`${apiBase()}/api/job/${jobId}/download`);

      if (!response.ok) {
        throw new Error(await safeParseError(response));
      }

      progress = 100;
      progressLabel = 'complete!';

      const baseName = (uploadedFileName || 'video').replace(/\.[^.]+$/, '');
      const downloadName = `${baseName}_trimmed.mp4`;

      const blob = await response.blob();
      downloadBlob(blob, downloadName);

      statusType = 'success';
      statusMessage = `${buttonLabel} complete! download started.`;
      queue.updateItem(queueId, { stage: 'complete', status: 'complete!', progress: 100 });
      setTimeout(() => queue.remove(queueId), 5000);

    } catch (err) {
      statusType = 'error';
      statusMessage = err.message || 'processing failed';
      progress = 0;
      progressLabel = '';
      queue.updateItem(queueId, { stage: 'error', status: err.message || 'processing failed' });
    } finally {
      processing = false;
      if (heartbeatJobId) stopHeartbeat(heartbeatJobId);
    }
  }
</script>

<HeaderSimple>
  {#snippet extraContent()}<QueueToggle />{/snippet}
</HeaderSimple>

<main>
  <div class="page-header">
    <h1>trim & crop</h1>
    <p>cut videos and change aspect ratios</p>
  </div>

  <div class="trim-container">
    {#if !selectedFile && !fetchedFile}
      <div class="input-tabs">
        <button class="input-tab" class:active={inputMode === 'file'} onclick={() => inputMode = 'file'}>upload file</button>
        <button class="input-tab" class:active={inputMode === 'url'} onclick={() => inputMode = 'url'}>paste link</button>
      </div>

      {#if inputMode === 'file'}
        <button
          type="button"
          class="drop-zone"
          class:dragover={dragging}
          ondragover={(e) => { e.preventDefault(); dragging = true; }}
          ondragleave={(e) => { e.preventDefault(); dragging = false; }}
          ondrop={(e) => { e.preventDefault(); dragging = false; handleFile(e.dataTransfer?.files?.[0]); }}
          onclick={() => inputEl?.click()}
        >
          <svg class="drop-zone-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="17 8 12 3 7 8"></polyline>
            <line x1="12" y1="3" x2="12" y2="15"></line>
          </svg>
          <p class="drop-zone-text">drop a video here or click to browse</p>
          <p class="drop-zone-hint">supports mp4, webm, mov, mkv (max 8GB)</p>
          <input
            bind:this={inputEl}
            type="file"
            accept="video/*"
            onchange={(e) => { handleFile(e.target.files?.[0]); e.target.value = ''; }}
            style="display: none;"
          />
        </button>
      {:else}
        <div class="url-input-section">
          <input
            type="url"
            class="url-input"
            placeholder="https://youtube.com/watch?v=..."
            bind:value={urlInput}
          />
        </div>
      {/if}
    {:else}
      <div class="file-info">
        <div class="file-header">
          <div class="file-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="5 3 19 12 5 21 5 3"></polygon>
            </svg>
          </div>
          <div class="file-details">
            <div class="file-name" title={fetchedFile ? fetchedFile.fileName : selectedFile.name}>{fetchedFile ? fetchedFile.fileName : selectedFile.name}</div>
            <div class="file-size">{fetchedFile ? formatBytes(fetchedFile.fileSize) : formatBytes(selectedFile.size)}</div>
          </div>
          <button class="file-remove" onclick={removeFile}>
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
          </button>
        </div>
      </div>
    {/if}

    {#if selectedFile && videoSrc}
      <div class="video-preview">
        <div class="video-wrapper">
          <!-- svelte-ignore a11y_media_has_caption -->
          <video
            bind:this={videoEl}
            src={videoSrc}
            controls
            preload="metadata"
            onloadedmetadata={handleVideoLoaded}
          ></video>
          {#if cropOverlay}
            <div class="crop-overlay crop-overlay-top" style="height: {cropOverlay.top}%"></div>
            <div class="crop-overlay crop-overlay-bottom" style="height: {cropOverlay.bottom}%"></div>
            <div class="crop-overlay crop-overlay-left" style="width: {cropOverlay.left}%; top: {cropOverlay.top}%; bottom: {cropOverlay.bottom}%"></div>
            <div class="crop-overlay crop-overlay-right" style="width: {cropOverlay.right}%; top: {cropOverlay.top}%; bottom: {cropOverlay.bottom}%"></div>
          {/if}
        </div>

        <div class="trim-controls">
          <div class="trim-buttons">
            <button class="trim-btn" onclick={setStart}>
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="17" y1="10" x2="3" y2="10"></line>
                <line x1="21" y1="6" x2="3" y2="6"></line>
                <line x1="21" y1="14" x2="3" y2="14"></line>
                <line x1="17" y1="18" x2="3" y2="18"></line>
              </svg>
              set start
            </button>
            <div class="trim-time">{formatTime(startTime)}</div>
            <span class="trim-arrow">→</span>
            <div class="trim-time">{formatTime(endTime)}</div>
            <button class="trim-btn" onclick={setEnd}>
              set end
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="17" y1="10" x2="3" y2="10"></line>
                <line x1="21" y1="6" x2="3" y2="6"></line>
                <line x1="21" y1="14" x2="3" y2="14"></line>
                <line x1="17" y1="18" x2="3" y2="18"></line>
              </svg>
            </button>
          </div>

          <div class="trim-bar">
            <div
              class="trim-bar-selected"
              style="left: {trimBarStart}%; right: {100 - trimBarEnd}%"
            ></div>
          </div>

          <div class="trim-duration">
            trimmed duration: <strong>{trimDurationText}</strong>
          </div>
        </div>
      </div>
    {/if}

    {#if inputMode === 'url' && !selectedFile && !fetchedFile}
      <div class="section">
        <div class="section-label">trim times (seconds or MM:SS)</div>
        <div class="manual-time-inputs">
          <div class="time-field">
            <label>start</label>
            <input
              type="text"
              placeholder="0:00"
              value={startTimeInput}
              oninput={(e) => handleStartInput(e.target.value)}
            />
          </div>
          <span class="trim-arrow">→</span>
          <div class="time-field">
            <label>end</label>
            <input
              type="text"
              placeholder="0:30"
              value={endTimeInput}
              oninput={(e) => handleEndInput(e.target.value)}
            />
          </div>
        </div>
      </div>
    {/if}

    <div class="section">
      <div class="section-label">crop aspect ratio</div>
      <div class="segmented-control">
        {#each cropOptions as opt}
          <button
            class="segment"
            class:active={cropRatio === opt.value}
            onclick={() => cropRatio = opt.value}
          >
            {opt.label}
          </button>
        {/each}
      </div>
    </div>

    <button
      class="process-btn"
      onclick={processTrim}
      disabled={processing || (!selectedFile && !fetchedFile && !(inputMode === 'url' && urlInput.trim()))}
    >
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="6" cy="6" r="3"></circle>
        <circle cx="6" cy="18" r="3"></circle>
        <line x1="20" y1="4" x2="8.12" y2="15.88"></line>
        <line x1="14.47" y1="14.48" x2="20" y2="20"></line>
        <line x1="8.12" y1="8.12" x2="12" y2="12"></line>
      </svg>
      {processing ? 'processing...' : buttonLabel}
    </button>

    {#if processing}
      <div class="progress-container">
        <ProgressBar percent={progress} label={progressLabel} />
      </div>
    {/if}

    {#if statusType}
      <div class="status {statusType}">{statusMessage}</div>
    {/if}
  </div>
</main>

<FooterSimple />

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    padding: 40px 20px;
    width: 100%;
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

  .trim-container {
    width: 100%;
    max-width: 500px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .input-tabs {
    display: flex;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    padding: 4px;
    gap: 4px;
  }

  .input-tab {
    flex: 1;
    padding: 10px 16px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
  }

  .input-tab:hover:not(.active) {
    color: var(--text);
    background: var(--border);
  }

  .input-tab.active {
    background: var(--purple-500);
    color: white;
  }

  .url-input-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .url-input {
    width: 100%;
    padding: 16px;
    font-family: var(--font-body);
    font-size: 1rem;
    background: var(--surface);
    border: 2px dashed var(--border);
    border-radius: var(--radius-lg);
    color: var(--text);
    outline: none;
    transition: border-color 0.15s ease-out;
    box-sizing: border-box;
  }

  .url-input:focus {
    border-color: var(--purple-500);
    border-style: solid;
  }

  .url-input::placeholder {
    color: var(--text-muted);
  }

  .url-hint {
    font-size: 0.85rem;
    color: var(--text-muted);
    text-align: center;
  }

  .drop-zone {
    width: 100%;
    border: 2px dashed var(--border);
    border-radius: var(--radius-lg);
    padding: 48px 24px;
    text-align: center;
    cursor: pointer;
    transition: all 0.2s ease-out;
    background: var(--surface);
    font-family: var(--font-body);
    outline: none;
  }

  .drop-zone:hover,
  .drop-zone.dragover {
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .drop-zone-icon {
    width: 48px;
    height: 48px;
    margin: 0 auto 16px;
    color: var(--purple-400);
  }

  .drop-zone-text {
    font-size: 1rem;
    font-weight: 500;
    color: var(--text);
    margin-bottom: 8px;
  }

  .drop-zone-hint {
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .file-info {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 20px;
  }

  .file-header {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .file-icon {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--purple-900);
    border-radius: var(--radius-md);
    color: var(--purple-400);
  }

  .file-icon svg {
    width: 20px;
    height: 20px;
  }

  .file-details {
    flex: 1;
    min-width: 0;
  }

  .file-name {
    font-size: 0.95rem;
    font-weight: 500;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .file-size {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .file-remove {
    padding: 8px;
    background: transparent;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
  }

  .file-remove:hover {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
  }

  .video-preview {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .video-wrapper {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
  }

  .video-wrapper video {
    width: 100%;
    display: block;
    border-radius: var(--radius-md);
  }

  .crop-overlay {
    position: absolute;
    background: rgba(0, 0, 0, 0.6);
    pointer-events: none;
    z-index: 2;
  }

  .crop-overlay-top {
    top: 0;
    left: 0;
    right: 0;
  }

  .crop-overlay-bottom {
    bottom: 0;
    left: 0;
    right: 0;
  }

  .crop-overlay-left {
    left: 0;
    position: absolute;
  }

  .crop-overlay-right {
    right: 0;
    position: absolute;
  }

  .trim-controls {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .trim-buttons {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .trim-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    font-family: var(--font-body);
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--purple-400);
    background: var(--purple-900);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .trim-btn:hover {
    background: var(--purple-500);
    color: white;
  }

  .trim-time {
    font-family: monospace;
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text);
    padding: 6px 10px;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
  }

  .trim-arrow {
    color: var(--text-muted);
    font-size: 0.9rem;
  }

  .trim-bar {
    position: relative;
    height: 8px;
    background: var(--surface-elevated);
    border-radius: 4px;
    overflow: hidden;
  }

  .trim-bar-selected {
    position: absolute;
    top: 0;
    bottom: 0;
    background: var(--purple-500);
    border-radius: 4px;
    transition: left 0.15s ease-out, right 0.15s ease-out;
  }

  .trim-duration {
    font-size: 0.85rem;
    color: var(--text-secondary);
    text-align: center;
  }

  .trim-duration strong {
    color: var(--text);
    font-family: monospace;
  }

  .section {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .section-label {
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    margin-bottom: 10px;
  }

  .manual-time-inputs {
    display: flex;
    align-items: flex-end;
    gap: 8px;
    justify-content: center;
  }

  .time-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .time-field label {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-weight: 500;
  }

  .time-field input {
    width: 100px;
    padding: 10px 12px;
    font-family: monospace;
    font-size: 0.95rem;
    background: var(--surface-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    outline: none;
    text-align: center;
  }

  .time-field input:focus {
    border-color: var(--purple-500);
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

  .process-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    width: 100%;
    padding: 16px 24px;
    font-family: var(--font-heading);
    font-size: 1rem;
    font-weight: 700;
    background: var(--purple-500);
    color: white;
    border: none;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .process-btn:hover:not(:disabled) {
    background: var(--purple-400);
    transform: translateY(-1px);
  }

  .process-btn:disabled {
    background: var(--surface-elevated);
    color: var(--text-muted);
    cursor: not-allowed;
  }

  .process-btn svg {
    width: 20px;
    height: 20px;
  }

  .progress-container {
    margin-top: 4px;
  }

  .status {
    padding: 14px 20px;
    border-radius: var(--radius-md);
    font-size: 0.9rem;
    font-weight: 500;
    text-align: center;
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

  @media (max-width: 600px) {
    main {
      padding: 20px 12px;
    }

    .page-header h1 {
      font-size: 1.8rem;
    }

    .drop-zone {
      padding: 32px 16px;
    }

    .drop-zone-icon {
      width: 40px;
      height: 40px;
    }

    .file-info {
      padding: 16px;
    }

    .segment {
      padding: 12px 10px;
      font-size: 0.85rem;
    }

    .process-btn {
      padding: 18px 24px;
    }

    .file-remove {
      padding: 10px;
      -webkit-tap-highlight-color: transparent;
    }

    .file-remove:active {
      transform: scale(0.9);
    }

    .trim-buttons {
      gap: 6px;
    }

    .trim-btn {
      padding: 8px 10px;
      font-size: 0.75rem;
    }

    .time-field input {
      font-size: 16px;
    }
  }
</style>
