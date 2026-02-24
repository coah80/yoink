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
  let mediaDuration = $state(0);
  let urlInput = $state('');
  let inputMode = $state('file');
  let fetchedFile = $state(null);

  // Check for ?url= query param on mount
  const initialQuery = getQuery();
  if (initialQuery.url) {
    urlInput = initialQuery.url;
    inputMode = 'url';
  }
  let outputMode = $state('text');
  let selectedModel = $state('base');
  let subtitleFormat = $state('srt');
  let language = $state('');
  let captionSize = $state(72);
  let maxWordsPerCaption = $state(0);
  let maxCharsPerLine = $state(0);
  let minDuration = $state(0);
  let captionGap = $state(0);
  let showAdvanced = $state(false);
  let transcribing = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let progressDetail = $state('');
  let showResult = $state(false);
  let resultText = $state('');
  let copied = $state(false);
  let dragging = $state(false);
  let inputEl;

  const outputModes = [
    { id: 'text', label: 'text', sublabel: 'plain transcript' },
    { id: 'subtitles', label: 'subtitles', sublabel: 'SRT / ASS file' },
    { id: 'captions', label: 'captions', sublabel: 'burned into video' },
  ];

  const models = [
    { id: 'tiny', label: 'tiny', sublabel: 'fastest, less accurate' },
    { id: 'base', label: 'base', sublabel: 'recommended' },
    { id: 'small', label: 'small', sublabel: 'slower, more accurate' },
    { id: 'medium', label: 'medium', sublabel: 'slowest local model' },
    { id: 'large', label: 'large', sublabel: 'cloud, best quality' },
  ];

  let showCaptionSettings = $derived(outputMode === 'subtitles' || outputMode === 'captions');

  const loremWords = 'lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua'.split(' ');

  let previewText = $derived.by(() => {
    const count = maxWordsPerCaption > 0 ? maxWordsPerCaption : 7;
    let text = loremWords.slice(0, count).join(' ');
    if (maxCharsPerLine > 0) {
      const words = text.split(' ');
      const lines = [];
      let current = words[0] || '';
      for (let i = 1; i < words.length; i++) {
        if (current.length + 1 + words[i].length <= maxCharsPerLine) {
          current += ' ' + words[i];
        } else {
          lines.push(current);
          current = words[i];
        }
      }
      lines.push(current);
      text = lines.join('\n');
    }
    return text;
  });

  const modelMultipliers = { tiny: 0.17, base: 0.33, small: 1.0, medium: 3.3, large: 1.5 };

  let estimateText = $derived.by(() => {
    if (inputMode === 'url' && !fetchedFile && !selectedFile) {
      return urlInput ? 'paste a link and transcribe to see estimate' : 'paste a link to see estimate';
    }
    if (!selectedFile && !fetchedFile) return 'select a file to see estimate';
    if (!mediaDuration) return 'select a file to see estimate';
    const minutes = mediaDuration / 60;
    const mult = modelMultipliers[selectedModel];
    const est = Math.ceil(minutes * mult);
    if (est < 1) return 'less than a minute';
    if (est === 1) return '~1 minute';
    return `~${est} minutes`;
  });

  let estimateWarning = $derived(
    selectedFile && mediaDuration && Math.ceil((mediaDuration / 60) * modelMultipliers[selectedModel]) > 30
  );

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
      addToast('Please select a video or audio file', 'error');
      return;
    }
    const maxSize = 8 * 1024 * 1024 * 1024;
    if (file.size > maxSize) {
      addToast('File too large. Maximum size is 8GB.', 'error');
      return;
    }
    selectedFile = file;
    showResult = false;
    resultText = '';
    mediaDuration = await getMediaDuration(file);
  }

  function removeFile() {
    selectedFile = null;
    mediaDuration = 0;
    progress = 0;
    progressLabel = '';
    progressDetail = '';
    showResult = false;
    resultText = '';
    copied = false;
    if (inputEl) inputEl.value = '';
  }

  async function copyText() {
    try {
      await navigator.clipboard.writeText(resultText);
      copied = true;
      addToast('Copied to clipboard!', 'success');
      setTimeout(() => { copied = false; }, 2000);
    } catch {
      addToast('Failed to copy', 'error');
    }
  }

  async function transcribe() {
    const isUrlMode = inputMode === 'url' && urlInput.trim();
    if ((!selectedFile && !isUrlMode) || transcribing) return;

    if (estimateWarning) {
      addToast('This may take over 30 minutes. Consider using a faster model.', 'warning');
    }

    transcribing = true;
    progress = 0;
    showResult = false;
    resultText = '';

    const queueTitle = isUrlMode ? `URL → ${outputMode}` : `${selectedFile.name} → ${outputMode}`;
    const queueId = Date.now().toString() + Math.random().toString(36).slice(2, 11);
    queue.add({
      id: queueId,
      title: queueTitle,
      type: 'transcribe',
      stage: isUrlMode ? 'downloading' : 'uploading',
      status: isUrlMode ? 'downloading media...' : 'uploading...',
      progress: 0,
      startTime: Date.now(),
    });

    let heartbeatJobId = null;

    try {
      heartbeatJobId = startHeartbeat();
      let jobId;

      if (isUrlMode) {
        progressLabel = 'downloading media...';
        progressDetail = 'fetching media from URL...';
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
        mediaDuration = fetchData.duration || 0;

        queue.updateItem(queueId, {
          title: `${fetchData.fileName} → ${outputMode}`,
          stage: 'transcribing',
          status: 'transcribing...',
          progress: 0
        });

        progress = 0;
        progressLabel = 'transcribing...';
        progressDetail = 'starting transcription...';

        const initResponse = await fetch(`${apiBase()}/api/transcribe-chunked`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            filePath: fetchData.filePath,
            fileName: fetchData.fileName,
            outputMode,
            model: selectedModel,
            subtitleFormat,
            language: language || undefined,
            ...(captionSize !== 72 && { captionSize }),
            ...(maxWordsPerCaption > 0 && { maxWordsPerCaption }),
            ...(maxCharsPerLine > 0 && { maxCharsPerLine }),
            ...(minDuration > 0 && { minDuration }),
            ...(captionGap > 0 && { captionGap }),
          }),
        });

        if (!initResponse.ok) {
          let errMsg = 'Failed to start transcription';
          try { const err = await initResponse.json(); errMsg = err.error || errMsg; } catch {}
          throw new Error(errMsg);
        }

        const result = await initResponse.json();
        jobId = result.jobId;
      } else {
      const useChunked = selectedFile.size > 90 * 1024 * 1024;
      progressLabel = 'uploading...';
      progressDetail = 'sending file to server...';

      if (useChunked) {
        progressLabel = 'uploading...';
        progressDetail = 'large file detected, uploading in chunks...';

        const uploadResult = await uploadChunked(selectedFile, (p) => {
          progress = p;
          progressDetail = 'uploading chunks...';
          queue.updateItem(queueId, { progress: p, status: 'uploading...' });
        });

        const { filePath, fileName } = uploadResult;
        progress = 0;
        progressLabel = 'transcribing...';
        progressDetail = 'starting transcription...';
        queue.updateItem(queueId, { stage: 'transcribing', status: 'transcribing...', progress: 0 });

        const initResponse = await fetch(`${apiBase()}/api/transcribe-chunked`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            filePath, fileName,
            outputMode,
            model: selectedModel,
            subtitleFormat,
            language: language || undefined,
            ...(captionSize !== 72 && { captionSize }),
            ...(maxWordsPerCaption > 0 && { maxWordsPerCaption }),
            ...(maxCharsPerLine > 0 && { maxCharsPerLine }),
            ...(minDuration > 0 && { minDuration }),
            ...(captionGap > 0 && { captionGap }),
          }),
        });

        if (!initResponse.ok) {
          let errMsg = 'Failed to start transcription';
          try { const err = await initResponse.json(); errMsg = err.error || errMsg; } catch {}
          throw new Error(errMsg);
        }

        const result = await initResponse.json();
        jobId = result.jobId;
      } else {
        const formData = new FormData();
        formData.append('file', selectedFile);
        formData.append('outputMode', outputMode);
        formData.append('model', selectedModel);
        formData.append('subtitleFormat', subtitleFormat);
        if (language) formData.append('language', language);
        if (captionSize !== 72) formData.append('captionSize', captionSize);
        if (maxWordsPerCaption > 0) formData.append('maxWordsPerCaption', maxWordsPerCaption);
        if (maxCharsPerLine > 0) formData.append('maxCharsPerLine', maxCharsPerLine);
        if (minDuration > 0) formData.append('minDuration', minDuration);
        if (captionGap > 0) formData.append('captionGap', captionGap);

        const response = await fetch(`${apiBase()}/api/transcribe`, {
          method: 'POST',
          body: formData,
        });

        if (!response.ok) {
          let errMsg = 'Failed to start transcription';
          try { const err = await response.json(); errMsg = err.error || errMsg; } catch {}
          throw new Error(errMsg);
        }

        const result = await response.json();
        jobId = result.jobId;
      }
      }

      // Poll for job completion
      progressLabel = 'transcribing...';
      progressDetail = 'starting transcription...';
      queue.updateItem(queueId, { stage: 'transcribing', status: 'transcribing...', progress: 0 });

      const pollStartTime = Date.now();
      const maxPollDuration = 60 * 60 * 1000;
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

          if (status.status === 'error') throw new Error(status.error || 'Transcription failed');
          if (status.progress !== undefined) progress = status.progress;
          if (status.message) {
            progressDetail = status.message;
            progressLabel = 'transcribing...';
          }
          queue.updateItem(queueId, { progress: status.progress || 0, status: status.message || 'transcribing...' });

          if (status.status === 'complete') {
            // Check if text content is included in status
            if (status.textContent) {
              resultText = status.textContent;
            }
            break;
          }
        } catch (fetchErr) {
          consecutiveErrors++;
          if (consecutiveErrors >= 5) throw new Error(`Failed to check job status after 5 attempts: ${fetchErr.message}`);
        }
      }

      if (Date.now() - pollStartTime > maxPollDuration) {
        throw new Error('Job timed out after 60 minutes.');
      }

      // Handle result based on output mode
      if (outputMode === 'text') {
        // If text wasn't in status response, fetch from download endpoint
        if (!resultText) {
          const dlRes = await fetch(`${apiBase()}/api/job/${jobId}/download`);
          if (!dlRes.ok) throw new Error('Failed to download transcript');
          resultText = await dlRes.text();
        }

        showResult = true;
        addToast('Transcription complete!', 'success');
      } else {
        // subtitles or captions: download the file
        const dlRes = await fetch(`${apiBase()}/api/job/${jobId}/download`);
        if (!dlRes.ok) throw new Error('Failed to download result');

        const blob = await dlRes.blob();
        const contentDisp = dlRes.headers.get('content-disposition') || '';
        const filenameMatch = contentDisp.match(/filename="([^"]+)"/);
        const filename = filenameMatch ? filenameMatch[1] : (
          outputMode === 'captions' ? 'captioned.mp4' : `subtitles.${subtitleFormat}`
        );

        downloadBlob(blob, filename);
        showResult = true;
        addToast('Transcription complete!', 'success');
      }

      queue.updateItem(queueId, { stage: 'complete', status: 'complete!', progress: 100 });
      setTimeout(() => queue.remove(queueId), 5000);

    } catch (err) {
      addToast(err.message || 'Transcription failed', 'error');
      queue.updateItem(queueId, { stage: 'error', status: err.message || 'transcription failed' });
    } finally {
      transcribing = false;
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
    <h1>transcribe</h1>
    <p>get subtitles, captions, or a text transcript from any video or audio</p>
  </div>

  <div class="transcribe-container">
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
          <p class="drop-zone-text">drop a video or audio file here</p>
          <p class="drop-zone-hint">supports mp4, webm, mov, mkv, mp3, wav, flac, m4a (max 8GB)</p>
          <input
            bind:this={inputEl}
            type="file"
            accept="video/*,audio/*"
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
          <p class="url-hint">paste a link to any video or audio — youtube, twitter, tiktok, etc.</p>
        </div>
      {/if}
    {:else if fetchedFile}
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
            <div class="file-name" title={fetchedFile.fileName}>{fetchedFile.fileName}</div>
            <div class="file-size">{formatBytes(fetchedFile.fileSize)}</div>
          </div>
          <button class="file-remove" onclick={() => { fetchedFile = null; mediaDuration = 0; }} aria-label="Remove file">
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
    {:else}
      <div class="file-info">
        <div class="file-header">
          <div class="file-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              {#if selectedFile.type.startsWith('audio/')}
                <path d="M9 18V5l12-2v13"></path>
                <circle cx="6" cy="18" r="3"></circle>
                <circle cx="18" cy="16" r="3"></circle>
              {:else}
                <rect x="2" y="2" width="20" height="20" rx="2.18" ry="2.18"></rect>
                <line x1="7" y1="2" x2="7" y2="22"></line>
                <line x1="17" y1="2" x2="17" y2="22"></line>
                <line x1="2" y1="12" x2="22" y2="12"></line>
              {/if}
            </svg>
          </div>
          <div class="file-details">
            <div class="file-name" title={selectedFile.name}>{selectedFile.name}</div>
            <div class="file-size">{formatBytes(selectedFile.size)}</div>
          </div>
          <button class="file-remove" onclick={removeFile} aria-label="Remove file">
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
      <div class="section-label">output mode</div>
      <div class="section-description">choose what you want back</div>
      <div class="mode-grid">
        {#each outputModes as mode}
          <button
            class="mode-option"
            class:active={outputMode === mode.id}
            onclick={() => outputMode = mode.id}
          >
            <div class="mode-option-value">{mode.label}</div>
            <div class="mode-option-label">{mode.sublabel}</div>
          </button>
        {/each}
      </div>
    </div>

    {#if outputMode === 'subtitles'}
      <div class="section">
        <div class="section-label">subtitle format</div>
        <div class="format-row">
          <button
            class="format-btn"
            class:active={subtitleFormat === 'srt'}
            onclick={() => subtitleFormat = 'srt'}
          >SRT</button>
          <button
            class="format-btn"
            class:active={subtitleFormat === 'ass'}
            onclick={() => subtitleFormat = 'ass'}
          >ASS</button>
        </div>
      </div>
    {/if}

    <button class="advanced-toggle" onclick={() => showAdvanced = !showAdvanced}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class:rotated={showAdvanced}>
        <polyline points="6 9 12 15 18 9"></polyline>
      </svg>
      advanced options
    </button>

    {#if showAdvanced}
      <div class="section">
        <div class="section-label">model</div>
        <div class="section-description">larger models are more accurate but slower</div>
        <div class="model-grid">
          {#each models as m}
            <button
              class="mode-option"
              class:active={selectedModel === m.id}
              onclick={() => selectedModel = m.id}
            >
              <div class="mode-option-value">{m.label}</div>
              <div class="mode-option-label">{m.sublabel}</div>
            </button>
          {/each}
        </div>
      </div>

      <div class="section">
        <div class="section-label">language hint</div>
        <div class="section-description">optional, auto-detected if empty (e.g. en, es, ja, fr)</div>
        <input
          type="text"
          class="language-input"
          placeholder="auto-detect"
          maxlength="5"
          value={language}
          oninput={(e) => language = e.target.value.trim()}
        />
      </div>

      {#if showCaptionSettings}
        <div class="section">
          <div class="section-label">caption formatting</div>
          <div class="section-description">control how captions are split and timed</div>

          {#if outputMode === 'captions' || subtitleFormat === 'ass'}
            <div class="caption-preview">
              <!-- font size maps to ASS PlayResX (1920), so /19.2 gives container-width % -->
              <div class="caption-preview-text" style="font-size: calc({captionSize} / 19.2 * 1cqw); white-space: pre-line;">
                {previewText}
              </div>
            </div>
          {/if}

          {#if outputMode === 'captions' || subtitleFormat === 'ass'}
            <div class="slider-group">
              <div class="slider-header">
                <span class="slider-label">caption size</span>
                <span class="slider-value">{captionSize}</span>
              </div>
              <input type="range" class="caption-slider" min="40" max="120" step="2" bind:value={captionSize} />
              <div class="slider-hint">size of caption text</div>
            </div>
          {/if}

          <div class="slider-group">
            <div class="slider-header">
              <span class="slider-label">max words per caption</span>
              <span class="slider-value">{maxWordsPerCaption === 0 ? 'off' : maxWordsPerCaption}</span>
            </div>
            <input type="range" class="caption-slider" min="0" max="20" step="1" bind:value={maxWordsPerCaption} />
            <div class="slider-hint">split long captions into smaller chunks</div>
          </div>

          <div class="slider-group">
            <div class="slider-header">
              <span class="slider-label">max characters per line</span>
              <span class="slider-value">{maxCharsPerLine === 0 ? 'off' : maxCharsPerLine}</span>
            </div>
            <input type="range" class="caption-slider" min="0" max="80" step="1" bind:value={maxCharsPerLine} />
            <div class="slider-hint">wrap long lines to fit on screen</div>
          </div>

          <div class="slider-group">
            <div class="slider-header">
              <span class="slider-label">minimum duration</span>
              <span class="slider-value">{minDuration === 0 ? 'off' : minDuration.toFixed(1) + 's'}</span>
            </div>
            <input type="range" class="caption-slider" min="0" max="5" step="0.1" bind:value={minDuration} />
            <div class="slider-hint">keep captions on screen longer</div>
          </div>

          <div class="slider-group">
            <div class="slider-header">
              <span class="slider-label">gap between captions</span>
              <span class="slider-value">{captionGap === 0 ? 'off' : captionGap.toFixed(2) + 's'}</span>
            </div>
            <input type="range" class="caption-slider" min="0" max="1" step="0.05" bind:value={captionGap} />
            <div class="slider-hint">add breathing room between captions</div>
          </div>
        </div>
      {/if}
    {/if}

    <div class="estimate-row">
      <span class="estimate-label">estimated time</span>
      <span class="estimate-value" class:estimate-warning={estimateWarning}>{estimateText}</span>
    </div>

    <button class="transcribe-btn" onclick={transcribe} disabled={(!selectedFile && !(inputMode === 'url' && urlInput.trim())) || transcribing}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
      </svg>
      {transcribing ? 'transcribing...' : 'transcribe'}
    </button>

    {#if transcribing}
      <div class="progress-container">
        <div class="progress-header">
          <span class="progress-status">{progressLabel}</span>
          <span class="progress-percent">{Math.round(progress)}%</span>
        </div>
        <ProgressBar percent={progress} />
        <div class="progress-details">{progressDetail}</div>
      </div>
    {/if}

    {#if showResult && outputMode === 'text'}
      <div class="result-container">
        <div class="result-header">
          <div class="result-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
          </div>
          <div class="result-title">transcription complete!</div>
        </div>
        <div class="transcript-box">
          <pre class="transcript-text">{resultText}</pre>
        </div>
        <div class="result-actions">
          <button class="result-btn copy-btn" onclick={copyText}>
            {#if copied}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
              </svg>
              copied!
            {:else}
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
              </svg>
              copy text
            {/if}
          </button>
          <button class="result-btn" onclick={removeFile}>
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="1 4 1 10 7 10"></polyline>
              <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
            </svg>
            transcribe another
          </button>
        </div>
      </div>
    {:else if showResult}
      <div class="result-container">
        <div class="result-header">
          <div class="result-icon">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
          </div>
          <div class="result-title">
            {outputMode === 'captions' ? 'captions burned in!' : 'subtitles ready!'}
          </div>
        </div>
        <button class="result-btn" onclick={removeFile}>
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="1 4 1 10 7 10"></polyline>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
          </svg>
          transcribe another
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

  .transcribe-container {
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

  .model-grid {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
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
    padding: 12px;
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

  .advanced-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    cursor: pointer;
    font-family: var(--font-body);
    font-size: 0.9rem;
    transition: all 0.15s ease-out;
  }

  .advanced-toggle:hover {
    background: var(--surface);
    color: var(--text);
  }

  .advanced-toggle svg {
    width: 16px;
    height: 16px;
    transition: transform 0.2s ease-out;
  }

  .advanced-toggle svg.rotated {
    transform: rotate(180deg);
  }

  .language-input {
    width: 100%;
    padding: 12px 16px;
    font-family: var(--font-body);
    font-size: 1rem;
    background: var(--surface-elevated);
    border: 2px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    outline: none;
    transition: border-color 0.15s ease-out;
    box-sizing: border-box;
  }

  .language-input:focus {
    border-color: var(--purple-500);
  }

  .language-input::placeholder {
    color: var(--text-muted);
  }

  .caption-preview {
    aspect-ratio: 16 / 9;
    background: #111;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    display: flex;
    align-items: flex-end;
    justify-content: center;
    margin-bottom: 20px;
    overflow: hidden;
    container-type: inline-size;
  }

  .caption-preview-text {
    font-family: Arial, sans-serif;
    font-weight: bold;
    color: #FFFF00;
    text-align: center;
    padding-bottom: 6%;
    line-height: 1.3;
    text-shadow:
      -2px -2px 0 #000, 2px -2px 0 #000, -2px 2px 0 #000, 2px 2px 0 #000,
      -1px -2px 0 #000, 1px -2px 0 #000, -1px 2px 0 #000, 1px 2px 0 #000,
      -2px -1px 0 #000, 2px -1px 0 #000, -2px 1px 0 #000, 2px 1px 0 #000,
      3px 3px 4px rgba(0, 0, 0, 0.6);
  }

  .slider-group {
    margin-bottom: 20px;
  }

  .slider-group:last-child {
    margin-bottom: 0;
  }

  .slider-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .slider-label {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .slider-value {
    font-family: var(--font-heading);
    font-weight: 700;
    font-size: 0.85rem;
    color: var(--purple-400);
    min-width: 40px;
    text-align: right;
  }

  .caption-slider {
    width: 100%;
    height: 6px;
    -webkit-appearance: none;
    appearance: none;
    background: var(--surface-elevated);
    border-radius: 3px;
    outline: none;
    cursor: pointer;
  }

  .caption-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--purple-500);
    cursor: pointer;
    border: 2px solid var(--surface);
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3);
    transition: transform 0.1s ease-out;
  }

  .caption-slider::-webkit-slider-thumb:hover {
    transform: scale(1.15);
  }

  .caption-slider::-moz-range-thumb {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--purple-500);
    cursor: pointer;
    border: 2px solid var(--surface);
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3);
  }

  .caption-slider::-moz-range-track {
    height: 6px;
    background: var(--surface-elevated);
    border-radius: 3px;
    border: none;
  }

  .slider-hint {
    font-size: 0.75rem;
    color: var(--text-muted);
    margin-top: 6px;
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

  .estimate-warning {
    color: var(--error);
  }

  .transcribe-btn {
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

  .transcribe-btn:hover:not(:disabled) {
    transform: translateY(-2px);
    box-shadow: 0 8px 24px rgba(139, 92, 246, 0.4);
  }

  .transcribe-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .transcribe-btn svg {
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

  .transcript-box {
    background: var(--surface-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 16px;
    max-height: 300px;
    overflow-y: auto;
    margin-bottom: 16px;
  }

  .transcript-text {
    font-family: var(--font-body);
    font-size: 0.9rem;
    line-height: 1.6;
    color: var(--text);
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
  }

  .result-actions {
    display: flex;
    gap: 8px;
  }

  .result-btn {
    flex: 1;
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

  .copy-btn {
    border-color: var(--purple-500);
    color: var(--purple-400);
  }

  .copy-btn:hover {
    background: var(--purple-900);
  }

  @media (max-width: 600px) {
    main {
      padding: 20px 12px;
    }

    .page-header h1 {
      font-size: 1.8rem;
    }

    .mode-grid {
      grid-template-columns: repeat(3, 1fr);
    }

    .model-grid {
      grid-template-columns: repeat(2, 1fr);
    }

    .mode-option {
      padding: 14px 6px;
    }

    .mode-option-value {
      font-size: 1rem;
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

    .file-remove {
      padding: 10px;
      -webkit-tap-highlight-color: transparent;
    }

    .file-remove:active {
      transform: scale(0.9);
    }

    .transcribe-btn {
      padding: 18px 24px;
    }

    .result-actions {
      flex-direction: column;
    }

    .estimate-row {
      flex-direction: column;
      gap: 4px;
      text-align: center;
    }

    .language-input {
      font-size: 16px;
    }
  }
</style>
