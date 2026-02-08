<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
  import ProgressBar from '../components/ui/ProgressBar.svelte';
  import Modal from '../components/ui/Modal.svelte';
  import { apiBase } from '../lib/api.js';
  import { uploadChunked } from '../lib/upload.js';
  import { downloadBlob } from '../lib/download.js';
  import { createSSEConnection } from '../lib/sse.js';
  import { formatBytes } from '../lib/utils.js';
  import { addToast } from '../stores/toast.js';
  import { startHeartbeat, stopHeartbeat } from '../stores/session.js';

  let selectedFile = $state(null);
  let videoDuration = $state(0);
  let videoWidth = $state(0);
  let videoHeight = $state(0);
  let selectedSize = $state(50);
  let actualSize = $state(47);
  let customSize = $state('');
  let isCustomSize = $state(false);
  let selectedPreset = $state('balanced');
  let downscale = $state(false);
  let compressing = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let progressDetail = $state('');
  let showResult = $state(false);
  let resultOriginal = $state('');
  let resultCompressed = $state('');
  let resultSaved = $state('');
  let dragging = $state(false);
  let showQualityWarning = $state(false);
  let inputEl;

  const sizePresets = [
    { size: 8, actual: 7.5, label: 'MB free' },
    { size: 10, actual: 9.5, label: 'MB' },
    { size: 50, actual: 47, label: 'MB basic' },
    { size: 100, actual: 95, label: 'MB' },
    { size: 500, actual: 475, label: 'MB nitro' },
  ];

  const encodingPresets = [
    { id: 'fast', label: 'fast', sublabel: 'quick encode' },
    { id: 'balanced', label: 'balanced', sublabel: 'recommended' },
    { id: 'quality', label: 'quality', sublabel: 'best results' },
  ];

  let estimatedBitrate = $derived.by(() => {
    if (!selectedFile || !videoDuration) return 0;
    const targetBytes = actualSize * 1024 * 1024;
    const audioBitrate = 128 * 1024;
    const availableForVideo = targetBytes - (audioBitrate / 8 * videoDuration);
    return Math.floor((availableForVideo * 8) / videoDuration / 1024);
  });

  let estimateText = $derived.by(() => {
    if (!selectedFile || !videoDuration) return 'select a file to see estimate';
    const fileSizeMB = selectedFile.size / (1024 * 1024);
    if (fileSizeMB <= actualSize) return `already under ${selectedSize}MB!`;

    if (estimatedBitrate < 500) return `~${estimatedBitrate} kbps video - very low quality (may be blocky)`;
    if (estimatedBitrate < 1000) return `~${estimatedBitrate} kbps video - low quality`;
    if (estimatedBitrate < 2500) return `~${estimatedBitrate} kbps video - medium quality`;
    if (estimatedBitrate < 5000) return `~${estimatedBitrate} kbps video - good quality`;
    return `~${estimatedBitrate} kbps video - excellent quality`;
  });

  let estimateWarning = $derived(selectedFile && videoDuration && estimatedBitrate < 500 && (selectedFile.size / (1024 * 1024)) > actualSize);

  let gaugeAngle = $derived.by(() => {
    const maxBitrate = 2000;
    const normalized = Math.min(estimatedBitrate, maxBitrate);
    return -90 + (normalized / maxBitrate) * 180;
  });

  let durationText = $derived.by(() => {
    if (!videoDuration) return '--:--';
    const mins = Math.floor(videoDuration / 60);
    const secs = Math.floor(videoDuration % 60);
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  });

  let resolutionText = $derived.by(() => {
    if (!videoWidth || !videoHeight) return 'unknown';
    return `${videoWidth}x${videoHeight}`;
  });

  function getVideoMetadata(file) {
    return new Promise((resolve) => {
      const video = document.createElement('video');
      video.preload = 'metadata';
      video.onloadedmetadata = () => {
        URL.revokeObjectURL(video.src);
        resolve({ duration: video.duration, width: video.videoWidth, height: video.videoHeight });
      };
      video.onerror = () => {
        resolve({ duration: 0, width: 0, height: 0 });
      };
      video.src = URL.createObjectURL(file);
    });
  }

  async function handleFile(file) {
    if (!file) return;
    if (!file.type.startsWith('video/')) {
      addToast('Please select a video file', 'error');
      return;
    }
    const maxSize = 15 * 1024 * 1024 * 1024;
    if (file.size > maxSize) {
      addToast('File too large. Maximum size is 15GB.', 'error');
      return;
    }
    selectedFile = file;
    showResult = false;
    const meta = await getVideoMetadata(file);
    videoDuration = meta.duration;
    videoWidth = meta.width;
    videoHeight = meta.height;
  }

  function removeFile() {
    selectedFile = null;
    videoDuration = 0;
    videoWidth = 0;
    videoHeight = 0;
    progress = 0;
    progressLabel = '';
    progressDetail = '';
    showResult = false;
    customSize = '';
    isCustomSize = false;
    if (inputEl) inputEl.value = '';
  }

  function selectSize(preset) {
    selectedSize = preset.size;
    actualSize = preset.actual;
    isCustomSize = false;
    customSize = '';
  }

  function handleCustomSize(value) {
    customSize = value;
    const parsed = parseInt(value);
    if (parsed && parsed > 0) {
      selectedSize = parsed;
      actualSize = parsed * 0.95;
      isCustomSize = true;
    }
  }

  function handleCustomBlur() {
    if (!customSize || parseInt(customSize) <= 0) {
      if (isCustomSize) {
        selectedSize = 50;
        actualSize = 47;
        isCustomSize = false;
      }
    }
  }

  function checkQualityAndCompress() {
    if (!selectedFile) return;
    const fileSizeMB = selectedFile.size / (1024 * 1024);
    if (fileSizeMB <= actualSize) {
      addToast(`File is already under ${selectedSize}MB!`, 'success');
      return;
    }
    if (estimatedBitrate < 500) {
      showQualityWarning = true;
    } else {
      compress();
    }
  }

  async function compress() {
    if (!selectedFile || compressing) return;
    showQualityWarning = false;
    
    const fileSizeMB = selectedFile.size / (1024 * 1024);
    if (fileSizeMB <= actualSize) {
      addToast(`File is already under ${selectedSize}MB!`, 'success');
      return;
    }

    compressing = true;
    progress = 0;
    progressLabel = 'Uploading...';
    progressDetail = 'Sending file to server...';
    showResult = false;

    const progressId = Date.now().toString() + Math.random().toString(36).substr(2, 9);
    const useChunked = selectedFile.size > 90 * 1024 * 1024;
    let heartbeatJobId = null;
    let sseConnection = null;

    try {
      heartbeatJobId = startHeartbeat();

      sseConnection = createSSEConnection(progressId, {
        onMessage: (data) => {
          if (data.stage === 'compressing') {
            progressLabel = 'Compressing...';
            if (data.progress !== undefined) progress = data.progress;
            if (data.message) progressDetail = data.message;
          }
        },
        onError: () => {},
      });

      let response;

      if (useChunked) {
        progressLabel = 'Uploading...';
        progressDetail = 'Large file detected, uploading in chunks...';

        const uploadResult = await uploadChunked(selectedFile, (p) => {
          progress = p;
          progressDetail = 'Uploading chunks...';
        });

        const { filePath, fileName } = uploadResult;
        progress = 0;
        progressLabel = 'Compressing...';
        progressDetail = 'Starting compression...';

        const initResponse = await fetch(`${apiBase()}/api/compress-chunked`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            filePath, fileName,
            targetSize: actualSize.toString(),
            duration: videoDuration.toString(),
            progressId,
            preset: selectedPreset,
            downscale,
          }),
        });

        if (!initResponse.ok) {
          const err = await initResponse.json();
          throw new Error(err.error || 'Failed to start compression');
        }

        const { jobId } = await initResponse.json();
        const pollStartTime = Date.now();
        const maxPollDuration = 30 * 60 * 1000;
        let consecutiveErrors = 0;

        while (Date.now() - pollStartTime <= maxPollDuration) {
          await new Promise(r => setTimeout(r, 2000));

          try {
            const controller = new AbortController();
            const timeoutId = setTimeout(() => controller.abort(), 10000);
            let statusRes;
            try {
              statusRes = await fetch(`${apiBase()}/api/job/${jobId}/status`, { signal: controller.signal });
            } finally {
              clearTimeout(timeoutId);
            }

            if (!statusRes.ok) throw new Error(`Server returned ${statusRes.status}`);
            const status = await statusRes.json();
            consecutiveErrors = 0;

            if (status.status === 'error') throw new Error(status.error || 'Compression failed');
            if (status.progress !== undefined) progress = status.progress;
            if (status.message) progressDetail = status.message;
            if (status.status === 'complete') break;
          } catch (fetchErr) {
            consecutiveErrors++;
            if (consecutiveErrors >= 3) throw new Error(`Failed to check job status after 3 attempts: ${fetchErr.message}`);
          }
        }

        if (Date.now() - pollStartTime > maxPollDuration) {
          throw new Error('Job timed out after 30 minutes.');
        }

        response = await fetch(`${apiBase()}/api/job/${jobId}/download`);
      } else {
        const formData = new FormData();
        formData.append('file', selectedFile);
        formData.append('targetSize', actualSize.toString());
        formData.append('duration', videoDuration.toString());
        formData.append('progressId', progressId);
        formData.append('preset', selectedPreset);
        formData.append('downscale', downscale);

        response = await fetch(`${apiBase()}/api/compress`, {
          method: 'POST',
          body: formData,
        });
      }

      if (sseConnection) sseConnection.close();

      if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Compression failed');
      }

      const blob = await response.blob();
      const compressedSize = blob.size;

      const originalMB = (selectedFile.size / (1024 * 1024)).toFixed(1);
      const compressedMB = (compressedSize / (1024 * 1024)).toFixed(1);
      const savedPercent = Math.round((1 - compressedSize / selectedFile.size) * 100);

      resultOriginal = `${originalMB} MB`;
      resultCompressed = `${compressedMB} MB`;
      resultSaved = `${savedPercent}%`;
      showResult = true;

      const lastDot = selectedFile.name.lastIndexOf('.');
      const baseName = lastDot > 0 ? selectedFile.name.slice(0, lastDot) : selectedFile.name;
      downloadBlob(blob, `${baseName}_compressed.mp4`);

      addToast('Compression complete!', 'success');
      if (heartbeatJobId) stopHeartbeat(heartbeatJobId);
    } catch (err) {
      addToast(err.message || 'Compression failed', 'error');
      if (sseConnection) sseConnection.close();
      if (heartbeatJobId) stopHeartbeat(heartbeatJobId);
    } finally {
      compressing = false;
      progress = 0;
      progressLabel = '';
      progressDetail = '';
    }
  }
</script>

<HeaderSimple />

<main>
  <div class="page-header">
    <h1>compress</h1>
    <p>shrink videos for discord, email, or anywhere!</p>
  </div>

  <div class="compress-container">
    {#if !selectedFile}
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
        <p class="drop-zone-hint">supports mp4, webm, mov, mkv, avi (max 15GB)</p>
        <input
          bind:this={inputEl}
          type="file"
          accept="video/*"
          onchange={(e) => { handleFile(e.target.files?.[0]); e.target.value = ''; }}
          style="display: none;"
        />
      </button>
    {:else}
      <div class="file-info">
        <div class="file-header">
          <div class="file-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="2" width="20" height="20" rx="2.18" ry="2.18"></rect>
              <line x1="7" y1="2" x2="7" y2="22"></line>
              <line x1="17" y1="2" x2="17" y2="22"></line>
              <line x1="2" y1="12" x2="22" y2="12"></line>
            </svg>
          </div>
          <div class="file-details">
            <div class="file-name" title={selectedFile.name}>{selectedFile.name}</div>
            <div class="file-size">{formatBytes(selectedFile.size)}</div>
          </div>
          <button class="file-remove" onclick={removeFile}>
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
          </button>
        </div>
        <div class="file-meta">
          <span>Duration: {videoDuration ? durationText : 'unknown'}</span>
          <span>Resolution: {resolutionText}</span>
        </div>
      </div>
    {/if}

    <div class="size-section">
      <div class="size-label">target size</div>
      <div class="size-description">choose a size limit or enter a custom size</div>
      <div class="size-grid">
        {#each sizePresets as preset}
          <button
            class="size-option"
            class:active={!isCustomSize && selectedSize === preset.size}
            onclick={() => selectSize(preset)}
          >
            <div class="size-option-value">{preset.size}</div>
            <div class="size-option-label">{preset.label}</div>
          </button>
        {/each}
      </div>

      <div class="custom-size-row">
        <input
          type="number"
          class="custom-size-input"
          placeholder="custom"
          min="1"
          max="4096"
          value={customSize}
          oninput={(e) => handleCustomSize(e.target.value)}
          onblur={handleCustomBlur}
        />
        <span class="custom-size-suffix">MB</span>
      </div>

      <div class="estimate">
        <span class="estimate-label">estimated quality</span>
        <span class="estimate-value" class:estimate-warning={estimateWarning}>{estimateText}</span>
      </div>
    </div>

    <div class="size-section">
      <div class="size-label">encoding preset</div>
      <div class="size-description">quality = better compression, slower | fast = quicker encoding</div>
      <div class="preset-grid">
        {#each encodingPresets as preset}
          <button
            class="size-option"
            class:active={selectedPreset === preset.id}
            onclick={() => selectedPreset = preset.id}
          >
            <div class="size-option-value">{preset.label}</div>
            <div class="size-option-label">{preset.sublabel}</div>
          </button>
        {/each}
      </div>
      <label class="checkbox-row">
        <input type="checkbox" bind:checked={downscale} />
        <span>downscale for better quality (reduces blockiness)</span>
      </label>
    </div>

    <button class="compress-btn" onclick={checkQualityAndCompress} disabled={!selectedFile || compressing}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="4 14 10 14 10 20"></polyline>
        <polyline points="20 10 14 10 14 4"></polyline>
        <line x1="14" y1="10" x2="21" y2="3"></line>
        <line x1="3" y1="21" x2="10" y2="14"></line>
      </svg>
      {compressing ? 'compressing...' : 'compress video'}
    </button>

    {#if compressing}
      <div class="progress-container">
        <div class="progress-header">
          <span class="progress-status">{progressLabel}</span>
          <span class="progress-percent">{Math.round(progress)}%</span>
        </div>
        <ProgressBar percent={progress} />
        <div class="progress-details">{progressDetail}</div>
      </div>
    {/if}

    {#if showResult}
      <div class="result-container">
        <div class="result-header">
          <div class="result-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
          </div>
          <div class="result-title">compression complete!</div>
        </div>
        <div class="result-stats">
          <div class="result-stat">
            <div class="result-stat-value">{resultOriginal}</div>
            <div class="result-stat-label">original</div>
          </div>
          <div class="result-stat">
            <div class="result-stat-value arrow">-></div>
            <div class="result-stat-label"></div>
          </div>
          <div class="result-stat">
            <div class="result-stat-value success">{resultCompressed}</div>
            <div class="result-stat-label">compressed</div>
          </div>
          <div class="result-stat">
            <div class="result-stat-value success">{resultSaved}</div>
            <div class="result-stat-label">saved</div>
          </div>
        </div>
        <button class="result-btn" onclick={removeFile}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="1 4 1 10 7 10"></polyline>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
          </svg>
          compress another
        </button>
      </div>
    {/if}
  </div>
</main>

{#if showQualityWarning}
  <div class="quality-overlay" onclick={(e) => { if (e.target === e.currentTarget) showQualityWarning = false; }}>
    <div class="quality-modal">
      <div class="quality-modal-title">
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
          <line x1="12" y1="9" x2="12" y2="13"></line>
          <line x1="12" y1="17" x2="12.01" y2="17"></line>
        </svg>
        Quality Warning
      </div>
      <div class="quality-gauge">
        <div class="quality-gauge-bg"></div>
        <div class="quality-gauge-mask"></div>
        <div class="quality-gauge-needle" style="transform: translateX(-50%) rotate({gaugeAngle}deg);"></div>
        <div class="quality-gauge-center"></div>
        <div class="quality-gauge-value">
          <span>{estimatedBitrate}</span>
          <span class="quality-gauge-unit">Kbps</span>
        </div>
      </div>
      <div class="quality-modal-message">
        Your video is <strong>{durationText}</strong> long. At this size, the estimated bitrate is very low and the output <strong>may look blocky or blurry</strong>.
        <br /><br />
        For better results, try a <strong>shorter clip</strong> or a <strong>larger target size</strong>.
      </div>
      <div class="quality-modal-buttons">
        <button class="quality-modal-btn cancel" onclick={() => showQualityWarning = false}>Cancel</button>
        <button class="quality-modal-btn proceed" onclick={compress}>Compress Anyway</button>
      </div>
    </div>
  </div>
{/if}

<FooterSimple />

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    padding: 40px 20px;
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

  .compress-container {
    width: 100%;
    max-width: 500px;
    display: flex;
    flex-direction: column;
    gap: 20px;
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
    color: var(--text-muted);
  }

  .drop-zone.dragover .drop-zone-icon {
    color: var(--purple-400);
  }

  .drop-zone-text {
    font-size: 1rem;
    color: var(--text-secondary);
    margin-bottom: 8px;
  }

  .drop-zone-hint {
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .file-info {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 16px;
  }

  .file-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
  }

  .file-icon {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--purple-900);
    border-radius: var(--radius-sm);
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
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .file-size {
    font-size: 0.85rem;
    color: var(--text-secondary);
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

  .file-meta {
    display: flex;
    gap: 16px;
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .size-section {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 20px;
  }

  .size-label {
    font-weight: 600;
    margin-bottom: 4px;
  }

  .size-description {
    font-size: 0.85rem;
    color: var(--text-secondary);
    margin-bottom: 16px;
  }

  .size-grid {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 8px;
  }

  .preset-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }

  .size-option {
    padding: 16px 8px;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
    font-family: var(--font-body);
  }

  .size-option:hover {
    border-color: var(--purple-500);
  }

  .size-option.active {
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .size-option-value {
    font-family: var(--font-heading);
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--text);
    margin-bottom: 4px;
  }

  .size-option-label {
    font-size: 0.7rem;
    color: var(--text-muted);
    text-transform: uppercase;
  }

  .size-option.active .size-option-label {
    color: var(--purple-400);
  }

  .custom-size-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }

  .custom-size-input {
    flex: 1;
    padding: 12px 16px;
    font-family: var(--font-body);
    font-size: 1rem;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    outline: none;
    transition: border-color 0.15s ease-out;
    -webkit-appearance: none;
    -moz-appearance: textfield;
  }

  .custom-size-input:focus {
    border-color: var(--purple-500);
  }

  .custom-size-input::placeholder {
    color: var(--text-muted);
  }

  .custom-size-suffix {
    font-weight: 600;
    color: var(--text-secondary);
  }

  .estimate {
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    padding: 12px 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-top: 16px;
  }

  .estimate-label {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .estimate-value {
    font-weight: 600;
    color: var(--purple-400);
  }

  .estimate-warning {
    color: var(--error);
  }

  .checkbox-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
    cursor: pointer;
  }

  .checkbox-row input {
    width: 18px;
    height: 18px;
    accent-color: var(--purple-500);
    cursor: pointer;
  }

  .checkbox-row span {
    font-size: 13px;
    color: var(--text-secondary);
  }

  .compress-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    width: 100%;
    padding: 18px 32px;
    font-family: var(--font-body);
    font-size: 1.1rem;
    font-weight: 600;
    color: white;
    background: linear-gradient(135deg, var(--purple-500), var(--purple-600));
    border: none;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease-out;
  }

  .compress-btn:hover:not(:disabled) {
    transform: translateY(-2px);
    box-shadow: 0 8px 24px rgba(139, 92, 246, 0.4);
  }

  .compress-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .compress-btn svg {
    width: 20px;
    height: 20px;
  }

  .progress-container {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 20px;
  }

  .progress-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }

  .progress-status {
    font-weight: 500;
  }

  .progress-percent {
    font-family: var(--font-heading);
    font-weight: 700;
    color: var(--purple-400);
  }

  .progress-details {
    margin-top: 12px;
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .result-container {
    background: var(--surface);
    border: 1px solid var(--success);
    border-radius: var(--radius-md);
    padding: 20px;
  }

  .result-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }

  .result-icon {
    width: 40px;
    height: 40px;
    background: rgba(52, 211, 153, 0.1);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--success);
  }

  .result-icon svg {
    width: 20px;
    height: 20px;
  }

  .result-title {
    font-weight: 600;
  }

  .result-stats {
    display: flex;
    gap: 24px;
    margin-bottom: 16px;
  }

  .result-stat {
    text-align: center;
  }

  .result-stat-value {
    font-family: var(--font-heading);
    font-size: 1.5rem;
    font-weight: 700;
  }

  .result-stat-value.success {
    color: var(--success);
  }

  .result-stat-value.arrow {
    color: var(--text-muted);
  }

  .result-stat-label {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .result-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    width: 100%;
    padding: 12px 20px;
    font-family: var(--font-body);
    font-size: 0.95rem;
    font-weight: 600;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .result-btn:hover {
    background: var(--surface-elevated);
  }

  .result-btn svg {
    width: 18px;
    height: 18px;
  }

  .quality-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.75);
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    backdrop-filter: blur(4px);
  }

  .quality-modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 32px;
    max-width: 400px;
    width: 90%;
    text-align: center;
    animation: modalIn 0.2s ease-out;
  }

  @keyframes modalIn {
    from { opacity: 0; transform: scale(0.95) translateY(-10px); }
    to { opacity: 1; transform: scale(1) translateY(0); }
  }

  .quality-modal-title {
    font-family: var(--font-heading);
    font-size: 1.25rem;
    font-weight: 700;
    margin-bottom: 8px;
    color: var(--warning);
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .quality-gauge {
    position: relative;
    width: 180px;
    height: 100px;
    margin: 24px auto;
  }

  .quality-gauge-bg {
    position: absolute;
    width: 180px;
    height: 90px;
    border-radius: 90px 90px 0 0;
    background: conic-gradient(from 180deg at 50% 100%, var(--error) 0deg, var(--warning) 60deg, var(--success) 120deg, var(--success) 180deg);
    overflow: hidden;
  }

  .quality-gauge-mask {
    position: absolute;
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
    width: 130px;
    height: 65px;
    background: var(--surface);
    border-radius: 65px 65px 0 0;
  }

  .quality-gauge-needle {
    position: absolute;
    bottom: 0;
    left: 50%;
    width: 4px;
    height: 70px;
    background: white;
    border-radius: 2px;
    transform-origin: bottom center;
    transition: transform 0.5s ease-out;
    box-shadow: 0 0 10px rgba(255, 255, 255, 0.5);
  }

  .quality-gauge-center {
    position: absolute;
    bottom: -6px;
    left: 50%;
    transform: translateX(-50%);
    width: 16px;
    height: 16px;
    background: white;
    border-radius: 50%;
    box-shadow: 0 0 10px rgba(255, 255, 255, 0.3);
  }

  .quality-gauge-value {
    position: absolute;
    bottom: -40px;
    left: 50%;
    transform: translateX(-50%);
    font-family: var(--font-heading);
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--text);
  }

  .quality-gauge-unit {
    font-size: 0.875rem;
    font-weight: 500;
    color: var(--text-muted);
    margin-left: 4px;
  }

  .quality-modal-message {
    font-size: 0.95rem;
    color: var(--text-secondary);
    line-height: 1.6;
    margin-top: 48px;
    margin-bottom: 24px;
  }

  .quality-modal-message strong {
    color: var(--text);
  }

  .quality-modal-buttons {
    display: flex;
    gap: 12px;
  }

  .quality-modal-btn {
    flex: 1;
    padding: 14px 20px;
    font-family: var(--font-body);
    font-size: 0.95rem;
    font-weight: 600;
    border: none;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .quality-modal-btn.cancel {
    background: var(--surface-elevated);
    color: var(--text);
    border: 1px solid var(--border);
  }

  .quality-modal-btn.cancel:hover {
    background: var(--border);
  }

  .quality-modal-btn.proceed {
    background: linear-gradient(135deg, var(--purple-500), var(--purple-600));
    color: white;
  }

  .quality-modal-btn.proceed:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgba(139, 92, 246, 0.4);
  }

  @media (max-width: 600px) {
    main {
      padding: 24px 16px;
    }

    .page-header h1 {
      font-size: 2rem;
    }

    .size-grid {
      grid-template-columns: repeat(3, 1fr);
    }

    .size-option {
      padding: 14px 6px;
    }

    .size-option-value {
      font-size: 1.1rem;
    }

    .size-section {
      padding: 16px;
    }

    .drop-zone {
      padding: 32px 16px;
    }

    .drop-zone-icon {
      width: 40px;
      height: 40px;
    }

    .file-remove {
      padding: 10px;
      -webkit-tap-highlight-color: transparent;
    }

    .file-remove:active {
      transform: scale(0.9);
    }

    .compress-btn {
      padding: 18px 24px;
    }

    .result-stats {
      gap: 12px;
      flex-wrap: wrap;
      justify-content: center;
    }

    .result-stat-value {
      font-size: 1.2rem;
    }

    .quality-modal {
      padding: 24px 20px;
      width: 94%;
    }

    .quality-gauge {
      width: 150px;
      height: 85px;
    }

    .quality-gauge-bg {
      width: 150px;
      height: 75px;
      border-radius: 75px 75px 0 0;
    }

    .quality-gauge-mask {
      width: 108px;
      height: 54px;
      border-radius: 54px 54px 0 0;
    }

    .quality-gauge-needle {
      height: 58px;
    }

    .quality-modal-btn {
      padding: 16px 16px;
    }

    .estimate {
      flex-direction: column;
      gap: 4px;
      text-align: center;
    }

    .custom-size-input {
      font-size: 16px;
    }

    .checkbox-row span {
      font-size: 0.85rem;
    }
  }

  @media (max-width: 380px) {
    .size-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }
</style>
