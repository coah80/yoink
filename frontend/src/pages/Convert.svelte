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
  let targetFormat = $state('mp4');
  let urlInput = $state('');
  let inputMode = $state('file');
  let fetchedFile = $state(null);

  const initialQuery = getQuery();
  if (initialQuery.url) {
    urlInput = initialQuery.url;
    inputMode = 'url';
  }
  let converting = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let statusType = $state(null);
  let statusMessage = $state('');
  let dragging = $state(false);
  let inputEl;

  const formats = [
    { value: 'mp4', label: 'mp4' },
    { value: 'webm', label: 'webm' },
    { value: 'mp3', label: 'mp3' },
    { value: 'm4a', label: 'm4a' },
    { value: 'wav', label: 'wav' },
  ];

  function isAudioFile(file) {
    return file.type.startsWith('audio/') ||
      ['mp3', 'm4a', 'wav', 'flac', 'ogg', 'opus'].some(ext => file.name.toLowerCase().endsWith('.' + ext));
  }

  function handleFile(file) {
    if (!file) return;
    const maxSize = 8 * 1024 * 1024 * 1024;
    if (file.size > maxSize) {
      statusType = 'error';
      statusMessage = 'File too large. Maximum size is 8GB.';
      return;
    }
    selectedFile = file;
    statusType = null;
    statusMessage = '';
  }

  function removeFile() {
    selectedFile = null;
    progress = 0;
    progressLabel = '';
    statusType = null;
    statusMessage = '';
    if (inputEl) inputEl.value = '';
  }

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

  async function convertFile() {
    const isUrlMode = inputMode === 'url' && urlInput.trim();
    if ((!selectedFile && !isUrlMode) || converting) return;

    converting = true;
    progress = 0;
    statusType = null;
    statusMessage = '';

    const queueId = Date.now().toString() + Math.random().toString(36).substr(2, 9);
    const queueTitle = isUrlMode ? `URL → .${targetFormat}` : `${selectedFile.name} → .${targetFormat}`;
    queue.add({
      id: queueId,
      title: queueTitle,
      type: 'convert',
      stage: isUrlMode ? 'downloading' : 'uploading',
      status: isUrlMode ? 'downloading media...' : 'uploading...',
      progress: 0,
      startTime: Date.now(),
    });

    let heartbeatJobId = null;

    try {
      heartbeatJobId = startHeartbeat();
      let response;

      if (isUrlMode) {
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

        queue.updateItem(queueId, {
          title: `${fetchData.fileName} → .${targetFormat}`,
          stage: 'converting',
          status: 'converting...',
          progress: 0
        });

        progress = 0;
        progressLabel = 'Converting...';

        const initResponse = await fetch(`${apiBase()}/api/convert-chunked`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ filePath: fetchData.filePath, fileName: fetchData.fileName, format: targetFormat }),
        });

        if (!initResponse.ok) {
          throw new Error(await safeParseError(initResponse));
        }

        const { jobId } = await initResponse.json();
        let pollAttempts = 0;

        while (true) {
          if (pollAttempts >= 300) throw new Error('Conversion timed out after 10 minutes');
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
          if (jobStatus.status === 'error') throw new Error(jobStatus.error || 'Conversion failed');
          progress = jobStatus.progress || 0;
          progressLabel = jobStatus.message || 'processing...';
          queue.updateItem(queueId, { progress: jobStatus.progress || 0, status: jobStatus.message || 'converting...' });
          if (jobStatus.status === 'complete') break;
        }

        response = await fetch(`${apiBase()}/api/job/${jobId}/download`);

        if (!response.ok) {
          throw new Error(await safeParseError(response));
        }

        progress = 100;
        progressLabel = 'complete!';

        const contentDisposition = response.headers.get('Content-Disposition');
        let downloadName = fetchData.fileName.replace(/\.[^.]+$/, '') + '.' + targetFormat;
        if (contentDisposition) {
          const match = contentDisposition.match(/filename="?([^"]+)"?/);
          if (match) downloadName = match[1];
        }

        const blob = await response.blob();
        downloadBlob(blob, downloadName);

        statusType = 'success';
        statusMessage = 'conversion complete! download started.';
        queue.updateItem(queueId, { stage: 'complete', status: 'complete!', progress: 100 });
        setTimeout(() => queue.remove(queueId), 5000);
      } else {
      const useChunked = selectedFile.size > 90 * 1024 * 1024;
      progressLabel = 'Uploading...';

      if (useChunked) {
        const { filePath, fileName } = await uploadChunked(selectedFile, (p) => {
          progress = p;
          progressLabel = 'Uploading...';
          queue.updateItem(queueId, { progress: p, status: 'uploading...' });
        });

        progress = 0;
        progressLabel = 'Converting...';
        queue.updateItem(queueId, { stage: 'converting', status: 'converting...', progress: 0 });

        const initResponse = await fetch(`${apiBase()}/api/convert-chunked`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ filePath, fileName, format: targetFormat }),
        });

        if (!initResponse.ok) {
          throw new Error(await safeParseError(initResponse));
        }

        const { jobId } = await initResponse.json();
        let pollAttempts = 0;

        while (true) {
          if (pollAttempts >= 300) throw new Error('Conversion timed out after 10 minutes');
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
          if (jobStatus.status === 'error') throw new Error(jobStatus.error || 'Conversion failed');
          progress = jobStatus.progress || 0;
          progressLabel = jobStatus.message || 'processing...';
          queue.updateItem(queueId, { progress: jobStatus.progress || 0, status: jobStatus.message || 'converting...' });
          if (jobStatus.status === 'complete') break;
        }

        response = await fetch(`${apiBase()}/api/job/${jobId}/download`);
      } else {
        response = await new Promise((resolve, reject) => {
          const xhr = new XMLHttpRequest();
          xhr.open('POST', `${apiBase()}/api/convert`);

          xhr.upload.onprogress = (e) => {
            if (e.lengthComputable) {
              const percent = Math.round((e.loaded / e.total) * 90);
              progress = percent;
              progressLabel = `uploading... ${percent}%`;
              queue.updateItem(queueId, { progress: percent, status: `uploading... ${percent}%` });
            }
          };

          xhr.onload = () => {
            progress = 95;
            progressLabel = 'processing...';
            queue.updateItem(queueId, { stage: 'converting', status: 'converting...', progress: 95 });
            resolve(new Response(xhr.response, {
              status: xhr.status,
              statusText: xhr.statusText,
              headers: { 'Content-Disposition': xhr.getResponseHeader('Content-Disposition') || '' },
            }));
          };

          xhr.onerror = () => reject(new Error('Network error during upload'));
          xhr.responseType = 'blob';

          const formData = new FormData();
          formData.append('file', selectedFile);
          formData.append('format', targetFormat);
          xhr.send(formData);
        });
      }

      if (!response.ok) {
        throw new Error(await safeParseError(response));
      }

      progress = 100;
      progressLabel = 'complete!';

      const contentDisposition = response.headers.get('Content-Disposition');
      let downloadName = selectedFile.name.replace(/\.[^.]+$/, '') + '.' + targetFormat;
      if (contentDisposition) {
        const match = contentDisposition.match(/filename="?([^"]+)"?/);
        if (match) downloadName = match[1];
      }

      const blob = await response.blob();
      downloadBlob(blob, downloadName);

      statusType = 'success';
      statusMessage = 'conversion complete! download started.';
      queue.updateItem(queueId, { stage: 'complete', status: 'complete!', progress: 100 });
      setTimeout(() => queue.remove(queueId), 5000);
      }
    } catch (err) {
      statusType = 'error';
      statusMessage = err.message || 'conversion failed';
      progress = 0;
      progressLabel = '';
      queue.updateItem(queueId, { stage: 'error', status: err.message || 'conversion failed' });
    } finally {
      converting = false;
      if (heartbeatJobId) stopHeartbeat(heartbeatJobId);
    }
  }
</script>

<HeaderSimple>
  {#snippet extraContent()}<QueueToggle />{/snippet}
</HeaderSimple>

<main>
  <div class="page-header">
    <h1>convert</h1>
    <p>change video and audio file formats</p>
  </div>

  <div class="converter">
    {#if !selectedFile && !(inputMode === 'url')}
      <div class="input-tabs">
        <button class="input-tab" class:active={inputMode === 'file'} onclick={() => inputMode = 'file'}>upload file</button>
        <button class="input-tab" class:active={inputMode === 'url'} onclick={() => inputMode = 'url'}>paste link</button>
      </div>

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
        <p class="drop-zone-text">drop a file here or click to browse</p>
        <p class="drop-zone-hint">supports video and audio files (max 8GB)</p>
        <input
          bind:this={inputEl}
          type="file"
          accept="video/*,audio/*"
          onchange={(e) => { handleFile(e.target.files?.[0]); e.target.value = ''; }}
          style="display: none;"
        />
      </button>
    {:else if inputMode === 'url' && !selectedFile}
      <div class="input-tabs">
        <button class="input-tab" class:active={inputMode === 'file'} onclick={() => inputMode = 'file'}>upload file</button>
        <button class="input-tab" class:active={inputMode === 'url'} onclick={() => inputMode = 'url'}>paste link</button>
      </div>

      <div class="url-input-section">
        <input
          type="url"
          class="url-input"
          placeholder="https://youtube.com/watch?v=..."
          bind:value={urlInput}
        />
      </div>

      <div class="convert-options">
        <div class="option-label">convert to</div>
        <div class="segmented-control">
          {#each formats as fmt}
            <button
              class="segment"
              class:active={targetFormat === fmt.value}
              onclick={() => targetFormat = fmt.value}
            >
              {fmt.label}
            </button>
          {/each}
        </div>
      </div>

      <button class="convert-btn" onclick={convertFile} disabled={!urlInput.trim() || converting}>
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="16 3 21 3 21 8"></polyline>
          <line x1="4" y1="20" x2="21" y2="3"></line>
          <polyline points="21 16 21 21 16 21"></polyline>
          <line x1="15" y1="15" x2="21" y2="21"></line>
          <line x1="4" y1="4" x2="9" y2="9"></line>
        </svg>
        {converting ? 'converting...' : 'convert'}
      </button>
    {:else}
      <div class="file-info">
        <div class="file-header">
          <div class="file-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              {#if selectedFile && isAudioFile(selectedFile)}
                <path d="M9 18V5l12-2v13"></path>
                <circle cx="6" cy="18" r="3"></circle>
                <circle cx="18" cy="16" r="3"></circle>
              {:else}
                <polygon points="5 3 19 12 5 21 5 3"></polygon>
              {/if}
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

        <div class="convert-options">
          <div class="option-label">convert to</div>
          <div class="segmented-control">
            {#each formats as fmt}
              <button
                class="segment"
                class:active={targetFormat === fmt.value}
                onclick={() => targetFormat = fmt.value}
              >
                {fmt.label}
              </button>
            {/each}
          </div>
        </div>

        <button class="convert-btn" onclick={convertFile} disabled={converting}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="16 3 21 3 21 8"></polyline>
            <line x1="4" y1="20" x2="21" y2="3"></line>
            <polyline points="21 16 21 21 16 21"></polyline>
            <line x1="15" y1="15" x2="21" y2="21"></line>
            <line x1="4" y1="4" x2="9" y2="9"></line>
          </svg>
          {converting ? 'converting...' : 'convert'}
        </button>
      </div>
    {/if}

    {#if converting}
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

  .converter {
    width: 100%;
    max-width: 500px;
  }

  .input-tabs {
    display: flex;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
    padding: 4px;
    gap: 4px;
    margin-bottom: 12px;
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
    margin-bottom: 20px;
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

  .convert-options {
    margin-top: 20px;
  }

  .option-label {
    font-size: 0.9rem;
    font-weight: 500;
    color: var(--text-secondary);
    margin-bottom: 10px;
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

  .convert-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    width: 100%;
    padding: 16px 24px;
    margin-top: 24px;
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

  .convert-btn:hover:not(:disabled) {
    background: var(--purple-400);
    transform: translateY(-1px);
  }

  .convert-btn:disabled {
    background: var(--surface-elevated);
    color: var(--text-muted);
    cursor: not-allowed;
  }

  .convert-btn svg {
    width: 20px;
    height: 20px;
  }

  .progress-container {
    margin-top: 20px;
  }

  .status {
    margin-top: 20px;
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

    .convert-btn {
      padding: 18px 24px;
    }

    .file-remove {
      padding: 10px;
      -webkit-tap-highlight-color: transparent;
    }

    .file-remove:active {
      transform: scale(0.9);
    }
  }
</style>
