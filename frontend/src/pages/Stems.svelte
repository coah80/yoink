<script>
  import { get } from 'svelte/store';
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
  import { settings } from '../stores/settings.js';
  import { queue } from '../stores/queue.js';
  import { getQuery } from '../lib/router.js';

  const saved = get(settings);

  let selectedFile = $state(null);
  let mediaDuration = $state(0);
  let urlInput = $state('');
  let inputMode = $state('file');
  let fetchedFile = $state(null);

  const initialQuery = getQuery();
  if (initialQuery.url) {
    urlInput = initialQuery.url;
    inputMode = 'url';
  }

  let mode = $state(saved.stems && saved.stems !== 'off' ? saved.stems : 'both');
  let format = $state(saved.audioFormat || 'mp3');
  let bitrate = $state(saved.audioBitrate || '320');
  let splitting = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let progressDetail = $state('');
  let showResult = $state(false);
  let dragging = $state(false);
  let inputEl;

  const modes = [
    { id: 'vocals', label: 'vocals', sublabel: 'acapella' },
    { id: 'instrumental', label: 'instrumental', sublabel: 'karaoke' },
    { id: 'both', label: 'both', sublabel: 'zip with the two' },
  ];

  const formats = ['mp3', 'm4a', 'opus', 'flac', 'wav'];
  const bitrates = ['320', '256', '192', '128'];
  let lossless = $derived(format === 'flac' || format === 'wav');
  let displayName = $derived((selectedFile?.name || fetchedFile?.fileName || '').replace(/^fetch-[0-9a-f-]{36}-/, ''));

  // roughly what the server does with its default gpu limits
  const secondsPerMinute = 12;

  let estimateText = $derived.by(() => {
    if (!selectedFile && !fetchedFile) {
      return inputMode === 'url' && urlInput ? 'starts after the link is fetched' : 'select a file to see estimate';
    }
    if (!mediaDuration) return 'unknown';
    const seconds = Math.ceil((mediaDuration / 60) * secondsPerMinute) + 5;
    if (seconds < 60) return `~${seconds} seconds`;
    return `~${Math.ceil(seconds / 60)} minutes`;
  });

  let durationText = $derived.by(() => {
    if (!mediaDuration) return '--:--';
    const mins = Math.floor(mediaDuration / 60);
    const secs = Math.floor(mediaDuration % 60);
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  });

  function getMediaDuration(file) {
    return new Promise((resolve) => {
      const el = file.type.startsWith('video/')
        ? document.createElement('video')
        : document.createElement('audio');
      el.preload = 'metadata';
      el.onloadedmetadata = () => {
        URL.revokeObjectURL(el.src);
        resolve(el.duration || 0);
      };
      el.onerror = () => {
        URL.revokeObjectURL(el.src);
        resolve(0);
      };
      el.src = URL.createObjectURL(file);
    });
  }

  async function handleFile(file) {
    if (!file) return;
    const isMedia = file.type.startsWith('video/') || file.type.startsWith('audio/');
    if (!isMedia) {
      addToast('Please select an audio or video file', 'error');
      return;
    }
    if (file.size > 8 * 1024 * 1024 * 1024) {
      addToast('File too large. Maximum size is 8GB.', 'error');
      return;
    }
    selectedFile = file;
    showResult = false;
    mediaDuration = await getMediaDuration(file);
  }

  function reset() {
    selectedFile = null;
    fetchedFile = null;
    mediaDuration = 0;
    progress = 0;
    progressLabel = '';
    progressDetail = '';
    showResult = false;
    if (inputEl) inputEl.value = '';
  }

  async function readError(res, fallback) {
    try {
      const err = await res.json();
      return err.error || fallback;
    } catch {
      return fallback;
    }
  }

  async function startChunkedJob(filePath, fileName) {
    const res = await fetch(`${apiBase()}/api/stems-chunked`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filePath, fileName, mode, format, bitrate }),
    });
    if (!res.ok) throw new Error(await readError(res, 'Failed to start stem split'));
    return (await res.json()).jobId;
  }

  async function startJob(queueId) {
    if (inputMode === 'url' && !selectedFile) {
      progressLabel = 'downloading media...';
      progressDetail = 'fetching media from link...';
      const res = await fetch(`${apiBase()}/api/fetch-url`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: urlInput.trim() }),
      });
      if (!res.ok) throw new Error(await readError(res, 'Failed to download from link'));
      const data = await res.json();
      fetchedFile = data;
      mediaDuration = data.duration || 0;
      queue.updateItem(queueId, { title: `${data.fileName.replace(/^fetch-[0-9a-f-]{36}-/, '')} → ${mode}` });
      return startChunkedJob(data.filePath, data.fileName);
    }

    progressLabel = 'uploading...';
    progressDetail = 'sending file to server...';
    if (selectedFile.size > 90 * 1024 * 1024) {
      const { filePath, fileName } = await uploadChunked(selectedFile, (p) => {
        progress = p;
        progressDetail = 'uploading chunks...';
        queue.updateItem(queueId, { progress: p, status: 'uploading...' });
      });
      return startChunkedJob(filePath, fileName);
    }

    const formData = new FormData();
    formData.append('file', selectedFile);
    formData.append('mode', mode);
    formData.append('format', format);
    formData.append('bitrate', bitrate);
    const res = await fetch(`${apiBase()}/api/stems`, { method: 'POST', body: formData });
    if (!res.ok) throw new Error(await readError(res, 'Failed to start stem split'));
    return (await res.json()).jobId;
  }

  async function waitForJob(jobId, queueId) {
    const started = Date.now();
    let consecutiveErrors = 0;
    while (Date.now() - started < 60 * 60 * 1000) {
      await new Promise(r => setTimeout(r, 1500));
      try {
        const res = await fetch(`${apiBase()}/api/job/${jobId}/status`);
        if (!res.ok) throw new Error(`Server returned ${res.status}`);
        const status = await res.json();
        consecutiveErrors = 0;
        if (status.status === 'error') return status.error || 'Stem split failed';
        if (status.progress !== undefined) progress = status.progress;
        if (status.message) progressDetail = status.message;
        queue.updateItem(queueId, { progress: status.progress || 0, status: status.message || 'splitting...' });
        if (status.status === 'complete') return null;
      } catch (err) {
        consecutiveErrors++;
        if (consecutiveErrors >= 5) return `Failed to check job status: ${err.message}`;
      }
    }
    return 'Job timed out after 60 minutes.';
  }

  async function split() {
    const isUrlMode = inputMode === 'url' && urlInput.trim();
    if ((!selectedFile && !isUrlMode) || splitting) return;

    splitting = true;
    progress = 0;
    showResult = false;

    const queueId = Date.now().toString() + Math.random().toString(36).slice(2, 11);
    queue.add({
      id: queueId,
      title: `${selectedFile ? selectedFile.name : 'link'} → ${mode}`,
      type: 'stems',
      stage: isUrlMode ? 'downloading' : 'uploading',
      status: isUrlMode ? 'downloading media...' : 'uploading...',
      progress: 0,
      startTime: Date.now(),
    });

    let heartbeatJobId = null;
    try {
      heartbeatJobId = startHeartbeat();
      const jobId = await startJob(queueId);

      progress = 0;
      progressLabel = 'splitting stems...';
      progressDetail = 'waiting for the stem splitter...';
      queue.updateItem(queueId, { stage: 'processing', status: 'splitting...', progress: 0 });

      const failure = await waitForJob(jobId, queueId);
      if (failure) throw new Error(failure);

      const dl = await fetch(`${apiBase()}/api/job/${jobId}/download`);
      if (!dl.ok) throw new Error('Failed to download stems');
      const blob = await dl.blob();
      const match = (dl.headers.get('content-disposition') || '').match(/filename="([^"]+)"/);
      downloadBlob(blob, match ? match[1] : (mode === 'both' ? 'stems.zip' : `${mode}.${format}`));

      showResult = true;
      addToast('Stems ready!', 'success');
      queue.updateItem(queueId, { stage: 'complete', status: 'complete!', progress: 100 });
      setTimeout(() => queue.remove(queueId), 5000);
    } catch (err) {
      addToast(err.message || 'Stem split failed', 'error');
      queue.updateItem(queueId, { stage: 'error', status: err.message || 'stem split failed' });
    } finally {
      splitting = false;
      progress = 0;
      progressLabel = '';
      progressDetail = '';
      if (heartbeatJobId) stopHeartbeat(heartbeatJobId);
    }
  }
</script>

<HeaderSimple>
  {#snippet extraContent()}<QueueToggle />{/snippet}
</HeaderSimple>

<main>
  <div class="page-header">
    <h1>stems</h1>
    <p>split any song into vocals and instrumental</p>
  </div>

  <div class="stems-container">
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
          <p class="drop-zone-text">drop a song or video here</p>
          <p class="drop-zone-hint">supports mp3, wav, flac, m4a, opus, mp4, webm, mkv (up to 60 minutes)</p>
          <input
            bind:this={inputEl}
            type="file"
            accept="audio/*,video/*"
            onchange={(e) => { handleFile(e.target.files?.[0]); e.target.value = ''; }}
            style="display: none;"
          />
        </button>
      {:else}
        <input
          type="url"
          class="url-input"
          placeholder="https://youtube.com/watch?v=..."
          bind:value={urlInput}
        />
      {/if}
    {:else}
      <div class="file-info">
        <div class="file-header">
          <div class="file-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9 18V5l12-2v13"></path>
              <circle cx="6" cy="18" r="3"></circle>
              <circle cx="18" cy="16" r="3"></circle>
            </svg>
          </div>
          <div class="file-details">
            <div class="file-name" title={displayName}>{displayName}</div>
            <div class="file-size">{formatBytes(selectedFile?.size || fetchedFile?.fileSize || 0)}</div>
          </div>
          <button class="file-remove" onclick={reset} disabled={splitting} aria-label="Remove file">
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
          </button>
        </div>
        <div class="file-meta">
          <span>duration: {mediaDuration ? durationText : 'unknown'}</span>
        </div>
      </div>
    {/if}

    <div class="section">
      <div class="section-label">what you get</div>
      <div class="section-description">pick the part of the song you want back</div>
      <div class="mode-grid">
        {#each modes as m}
          <button class="mode-option" class:active={mode === m.id} onclick={() => mode = m.id}>
            <div class="mode-option-value">{m.label}</div>
            <div class="mode-option-label">{m.sublabel}</div>
          </button>
        {/each}
      </div>
    </div>

    <div class="section">
      <div class="section-label">format</div>
      <div class="format-row">
        {#each formats as f}
          <button class="format-btn" class:active={format === f} onclick={() => format = f}>{f}</button>
        {/each}
      </div>
      {#if !lossless}
        <div class="section-label bitrate-label">bitrate</div>
        <div class="format-row">
          {#each bitrates as b}
            <button class="format-btn" class:active={bitrate === b} onclick={() => bitrate = b}>{b}k</button>
          {/each}
        </div>
      {/if}
    </div>

    <div class="estimate-row">
      <span class="estimate-label">estimated time</span>
      <span class="estimate-value">{estimateText}</span>
    </div>

    <button class="split-btn" onclick={split} disabled={(!selectedFile && !(inputMode === 'url' && urlInput.trim())) || splitting}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M9 18V5l12-2v13"></path>
        <circle cx="6" cy="18" r="3"></circle>
        <circle cx="18" cy="16" r="3"></circle>
      </svg>
      {splitting ? 'splitting...' : 'split'}
    </button>

    {#if splitting}
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
          <div class="result-title">{mode === 'both' ? 'stems ready!' : `${mode} ready!`}</div>
        </div>
        <button class="result-btn" onclick={reset}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="1 4 1 10 7 10"></polyline>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
          </svg>
          split another
        </button>
      </div>
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

  .stems-container {
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

  .file-remove:hover:not(:disabled) {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
  }

  .file-remove:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .file-meta {
    display: flex;
    gap: 16px;
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .section {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 20px;
  }

  .section-label {
    font-weight: 600;
    margin-bottom: 4px;
  }

  .bitrate-label {
    margin-top: 16px;
    margin-bottom: 8px;
  }

  .section-description {
    font-size: 0.85rem;
    color: var(--text-secondary);
    margin-bottom: 16px;
  }

  .mode-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }

  .mode-option {
    padding: 16px 8px;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
    font-family: var(--font-body);
  }

  .mode-option:hover {
    border-color: var(--purple-500);
  }

  .mode-option.active {
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .mode-option-value {
    font-family: var(--font-heading);
    font-size: 1.1rem;
    font-weight: 700;
    color: var(--text);
    margin-bottom: 4px;
  }

  .mode-option-label {
    font-size: 0.65rem;
    color: var(--text-muted);
    text-transform: uppercase;
  }

  .mode-option.active .mode-option-label {
    color: var(--purple-400);
  }

  .format-row {
    display: flex;
    gap: 8px;
  }

  .format-btn {
    flex: 1;
    padding: 12px 4px;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-family: var(--font-heading);
    font-size: 1rem;
    font-weight: 700;
    color: var(--text);
    transition: all 0.15s ease-out;
  }

  .format-btn:hover {
    border-color: var(--purple-500);
  }

  .format-btn.active {
    border-color: var(--purple-500);
    background: var(--purple-900);
    color: var(--purple-400);
  }

  .estimate-row {
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    padding: 12px 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .estimate-label {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .estimate-value {
    font-weight: 600;
    color: var(--purple-400);
  }

  .split-btn {
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

  .split-btn:hover:not(:disabled) {
    transform: translateY(-2px);
    box-shadow: 0 8px 24px rgba(139, 92, 246, 0.4);
  }

  .split-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .split-btn svg {
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

  .result-btn {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
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

  @media (max-width: 600px) {
    main {
      padding: 20px 12px;
    }

    .page-header h1 {
      font-size: 1.8rem;
    }

    .mode-option {
      padding: 14px 6px;
    }

    .mode-option-value {
      font-size: 0.95rem;
    }

    .section {
      padding: 16px;
    }

    .drop-zone {
      padding: 32px 16px;
    }

    .drop-zone-icon {
      width: 40px;
      height: 40px;
    }

    .format-btn {
      font-size: 0.9rem;
    }

    .split-btn {
      padding: 18px 24px;
    }

    .estimate-row {
      flex-direction: column;
      gap: 4px;
      text-align: center;
    }
  }
</style>
