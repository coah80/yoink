<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
  import QueueToggle from '../components/queue/QueueToggle.svelte';
  import ProgressBar from '../components/ui/ProgressBar.svelte';
  import CropOverlay from '../components/editor/CropOverlay.svelte';
  import ThumbnailTimeline from '../components/editor/ThumbnailTimeline.svelte';
  import VideoControls from '../components/editor/VideoControls.svelte';
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
  let processing = $state(false);
  let progress = $state(0);
  let progressLabel = $state('');
  let statusType = $state(null);
  let statusMessage = $state('');
  let dragging = $state(false);
  let inputEl;
  let videoEl = $state(null);
  let videoSrc = $state('');

  // new editor state
  let cropX = $state(0);
  let cropY = $state(0);
  let cropW = $state(0);
  let cropH = $state(0);
  let aspectLock = $state('');
  let isPlaying = $state(false);
  let currentTime = $state(0);
  let segments = $state([]);
  let thumbnails = $state([]);
  let thumbnailsLoading = $state(false);
  let containerWidth = $state(0);
  let containerHeight = $state(0);
  let videoWrapperEl = $state(null);

  let nextSegId = 1;

  const segmentColors = ['var(--purple-400)', '#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#ec4899'];

  const aspectOptions = [
    { value: '', label: 'free' },
    { value: '16:9', label: '16:9' },
    { value: '9:16', label: '9:16' },
    { value: '1:1', label: '1:1' },
    { value: '4:3', label: '4:3' },
    { value: '4:5', label: '4:5' },
  ];

  let hasCrop = $derived(
    videoWidth > 0 && videoHeight > 0 &&
    (cropX > 0 || cropY > 0 || cropW < videoWidth || cropH < videoHeight)
  );

  let hasValidTrim = $derived(
    endTime > startTime && endTime > 0 &&
    (videoDuration === 0 || startTime > 0.1 || endTime < videoDuration - 0.1)
  );
  let hasSegments = $derived(segments.length > 1);

  let buttonLabel = $derived.by(() => {
    if (hasSegments) return 'export';
    if (hasCrop && hasValidTrim) return 'trim & crop';
    if (hasCrop) return 'crop';
    return 'trim';
  });

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
    startTime = 0;
    startTimeInput = '0:00.0';

    // init crop to full video
    cropX = 0;
    cropY = 0;
    cropW = videoWidth;
    cropH = videoHeight;

    // init segments
    segments = [{ id: nextSegId++, start: 0, end: videoEl.duration, color: segmentColors[0] }];

    // generate thumbnails for file uploads
    if (videoSrc && videoSrc.startsWith('blob:')) {
      generateThumbnails();
    }
  }

  // rAF loop for smooth playhead - only runs while playing
  let rafId = null;
  function startRafLoop() {
    if (rafId) return;
    function tick() {
      if (videoEl && !videoEl.paused) {
        currentTime = videoEl.currentTime;
        rafId = requestAnimationFrame(tick);
      } else {
        rafId = null;
      }
    }
    rafId = requestAnimationFrame(tick);
  }

  function handlePlay() { isPlaying = true; startRafLoop(); }
  function handlePause() { isPlaying = false; }
  function handleEnded() { isPlaying = false; }

  function togglePlayPause() {
    if (!videoEl) return;
    if (videoEl.paused) {
      videoEl.play();
    } else {
      videoEl.pause();
    }
  }

  function seekTo(time) {
    if (!videoEl) return;
    const clamped = Math.max(0, Math.min(videoDuration, time));
    videoEl.currentTime = clamped;
    currentTime = clamped;
  }

  function handleStartTimeChange(time) {
    startTime = Math.max(0, time);
    startTimeInput = formatTime(startTime);
  }

  function handleEndTimeChange(time) {
    endTime = Math.min(videoDuration, time);
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

  // crop
  function handleCropChange(x, y, w, h) {
    cropX = x;
    cropY = y;
    cropW = w;
    cropH = h;
  }

  function setAspect(value) {
    aspectLock = value;
    if (!videoWidth || !videoHeight) return;

    if (value === '') {
      // free: reset to full video
      cropX = 0;
      cropY = 0;
      cropW = videoWidth;
      cropH = videoHeight;
      return;
    }

    const [aw, ah] = value.split(':').map(Number);
    const ratio = aw / ah;
    let w, h;

    if (videoWidth / videoHeight > ratio) {
      h = videoHeight;
      w = h * ratio;
    } else {
      w = videoWidth;
      h = w / ratio;
    }

    // center the crop
    cropX = (videoWidth - w) / 2;
    cropY = (videoHeight - h) / 2;
    cropW = w;
    cropH = h;
  }

  // segments
  function splitAtPlayhead() {
    if (segments.length === 0) return;

    const time = currentTime;
    const segIdx = segments.findIndex(s => time > s.start + 0.05 && time < s.end - 0.05);
    if (segIdx === -1) return;

    const seg = segments[segIdx];
    const newSegs = [...segments];
    const colorIdx1 = segIdx % segmentColors.length;
    const colorIdx2 = (segIdx + 1) % segmentColors.length;

    newSegs.splice(segIdx, 1,
      { id: nextSegId++, start: seg.start, end: time, color: segmentColors[colorIdx1] },
      { id: nextSegId++, start: time, end: seg.end, color: segmentColors[colorIdx2] },
    );

    // recolor all
    segments = newSegs.map((s, i) => ({ ...s, color: segmentColors[i % segmentColors.length] }));
  }

  function removeSegment(id) {
    segments = segments.filter(s => s.id !== id);
    if (segments.length === 0) {
      segments = [{ id: nextSegId++, start: startTime, end: endTime, color: segmentColors[0] }];
    }
    // recolor
    segments = segments.map((s, i) => ({ ...s, color: segmentColors[i % segmentColors.length] }));
  }

  // thumbnails
  async function generateThumbnails() {
    if (!videoSrc || thumbnailsLoading) return;
    thumbnailsLoading = true;
    thumbnails = [];

    try {
      const thumbVideo = document.createElement('video');
      thumbVideo.crossOrigin = 'anonymous';
      thumbVideo.muted = true;
      thumbVideo.preload = 'auto';
      thumbVideo.src = videoSrc;

      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('video load timeout')), 10000);
        thumbVideo.onloadeddata = () => { clearTimeout(timer); resolve(); };
        thumbVideo.onerror = () => { clearTimeout(timer); reject(new Error('video load error')); };
      });

      const count = Math.min(Math.ceil(videoDuration / 2), 60);
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');
      const aspect = thumbVideo.videoWidth / thumbVideo.videoHeight;
      canvas.height = 56;
      canvas.width = Math.round(56 * aspect);

      const results = [];

      for (let i = 0; i < count; i++) {
        const time = (i / count) * videoDuration;
        thumbVideo.currentTime = time;

        await new Promise((resolve) => {
          thumbVideo.onseeked = resolve;
          setTimeout(resolve, 500);
        });

        ctx.drawImage(thumbVideo, 0, 0, canvas.width, canvas.height);
        results.push(canvas.toDataURL('image/jpeg', 0.6));
      }

      thumbnails = results;
      thumbVideo.src = '';
      thumbVideo.load();
    } catch (err) {
      console.warn('thumbnail generation failed:', err);
    } finally {
      thumbnailsLoading = false;
    }
  }

  // measure container
  function updateContainerSize() {
    if (videoWrapperEl && videoEl) {
      containerWidth = videoEl.clientWidth;
      containerHeight = videoEl.clientHeight;
    }
  }

  $effect(() => {
    if (videoWrapperEl) {
      const ro = new ResizeObserver(updateContainerSize);
      ro.observe(videoWrapperEl);
      return () => ro.disconnect();
    }
  });

  // cleanup rAF on unmount
  $effect(() => {
    return () => {
      if (rafId) cancelAnimationFrame(rafId);
    };
  });

  // file handling
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
    cropX = 0; cropY = 0; cropW = 0; cropH = 0;
    aspectLock = '';
    isPlaying = false;
    currentTime = 0;
    segments = [];
    thumbnails = [];
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

  async function processTrim() {
    const isUrlMode = inputMode === 'url' && urlInput.trim();
    if ((!selectedFile && !isUrlMode && !fetchedFile) || processing) return;

    if (!hasValidTrim && !hasCrop && !hasSegments) {
      addToast('set start and end times, pick a crop, or split the video', 'error');
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

      if (hasValidTrim && !hasSegments) {
        body.startTime = String(startTime);
        body.endTime = String(endTime);
      }

      if (hasCrop) {
        // round to even numbers for ffmpeg
        body.cropX = Math.round(cropX);
        body.cropY = Math.round(cropY);
        body.cropW = Math.round(cropW) - (Math.round(cropW) % 2);
        body.cropH = Math.round(cropH) - (Math.round(cropH) % 2);
      } else if (aspectLock) {
        // URL mode without video preview - use ratio string
        body.cropRatio = aspectLock;
      }

      if (hasSegments) {
        body.segments = segments.map(s => ({ start: s.start, end: s.end }));
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

  let hasVideo = $derived(!!((selectedFile && videoSrc) || fetchedFile));
</script>

<HeaderSimple>
  {#snippet extraContent()}<QueueToggle />{/snippet}
</HeaderSimple>

<main>
  <div class="page-header">
    <h1>trim & crop</h1>
    <p>cut videos and change aspect ratios</p>
  </div>

  <!-- file input section - always narrow -->
  <div class="input-section">
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
            <div class="file-size">{fetchedFile ? formatBytes(fetchedFile.fileSize) : formatBytes(selectedFile.size)}{videoWidth && videoHeight ? ` \u2022 ${videoWidth}x${videoHeight}` : ''}</div>
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
  </div>

  <!-- editor section - wider when video loaded -->
  {#if selectedFile && videoSrc}
    <div class="editor-section">
      <div class="video-wrapper" bind:this={videoWrapperEl}>
        <!-- svelte-ignore a11y_media_has_caption -->
        <video
          bind:this={videoEl}
          src={videoSrc}
          preload="metadata"
          onloadedmetadata={handleVideoLoaded}
          onplay={handlePlay}
          onpause={handlePause}
          onended={handleEnded}
          onclick={togglePlayPause}
        ></video>
        {#if videoWidth > 0 && containerWidth > 0}
          <CropOverlay
            {videoWidth}
            {videoHeight}
            {containerWidth}
            {containerHeight}
            {cropX}
            {cropY}
            {cropW}
            {cropH}
            {aspectLock}
            onCropChange={handleCropChange}
          />
        {/if}
      </div>

      <!-- aspect ratio bar -->
      <div class="aspect-bar">
        {#each aspectOptions as opt}
          <button
            class="aspect-btn"
            class:active={aspectLock === opt.value}
            onclick={() => setAspect(opt.value)}
          >
            {opt.label}
          </button>
        {/each}
      </div>

      <!-- video controls -->
      <VideoControls
        {isPlaying}
        {currentTime}
        duration={videoDuration}
        onPlayPause={togglePlayPause}
        onSplitAtPlayhead={splitAtPlayhead}
      />

      <!-- thumbnail timeline -->
      <ThumbnailTimeline
        duration={videoDuration}
        {currentTime}
        {startTime}
        {endTime}
        {thumbnails}
        {thumbnailsLoading}
        {segments}
        onSeek={seekTo}
        onStartTimeChange={handleStartTimeChange}
        onEndTimeChange={handleEndTimeChange}
      />

      <!-- segment list -->
      {#if segments.length > 1}
        <div class="segment-list">
          <div class="segment-list-label">segments</div>
          {#each segments as seg, i}
            <div class="segment-row">
              <div class="segment-color" style="background: {seg.color}"></div>
              <span class="segment-range">{formatTime(seg.start)} → {formatTime(seg.end)}</span>
              <button class="segment-remove" onclick={() => removeSegment(seg.id)} title="Remove segment">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="18" y1="6" x2="6" y2="18"></line>
                  <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- url mode: manual time inputs when no video preview -->
  {#if inputMode === 'url' && !selectedFile && !fetchedFile}
    <div class="input-section">
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

      <div class="section">
        <div class="section-label">crop aspect ratio</div>
        <div class="segmented-control">
          {#each aspectOptions.slice(1) as opt}
            <button
              class="segment"
              class:active={aspectLock === opt.value}
              onclick={() => { aspectLock = aspectLock === opt.value ? '' : opt.value; }}
            >
              {opt.label}
            </button>
          {/each}
        </div>
      </div>
    </div>
  {/if}

  <!-- process button & status -->
  <div class="input-section">
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
    gap: 16px;
  }

  .page-header {
    text-align: center;
    margin-bottom: 16px;
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

  .input-section {
    width: 100%;
    max-width: 500px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .editor-section {
    width: 100%;
    max-width: 900px;
    display: flex;
    flex-direction: column;
    gap: 8px;
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

  .video-wrapper {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    border: 3px solid var(--purple-300);
    box-shadow: 0 0 20px rgba(139, 92, 246, 0.12);
  }

  .video-wrapper video {
    width: 100%;
    display: block;
    cursor: pointer;
  }

  .aspect-bar {
    display: flex;
    gap: 3px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 20px;
    padding: 3px;
  }

  .aspect-btn {
    flex: 1;
    padding: 6px 10px;
    font-family: var(--font-body);
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-muted);
    background: transparent;
    border: none;
    border-radius: 16px;
    cursor: pointer;
    transition: all 0.15s ease-out;
    text-align: center;
  }

  .aspect-btn:hover:not(.active) {
    color: var(--text);
    background: var(--surface-elevated);
  }

  .aspect-btn.active {
    background: var(--purple-500);
    color: white;
  }

  .segment-list {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .segment-list-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-bottom: 2px;
  }

  .segment-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    background: var(--surface-elevated);
    border-radius: var(--radius-sm);
  }

  .segment-color {
    width: 10px;
    height: 10px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .segment-range {
    font-family: monospace;
    font-size: 0.8rem;
    color: var(--text);
    flex: 1;
  }

  .segment-remove {
    padding: 4px;
    background: transparent;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 4px;
    transition: all 0.15s ease-out;
    display: flex;
    align-items: center;
  }

  .segment-remove:hover {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
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

  .trim-arrow {
    color: var(--text-muted);
    font-size: 0.9rem;
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

    .aspect-btn {
      padding: 10px 8px;
      font-size: 0.75rem;
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

    .time-field input {
      font-size: 16px;
    }
  }
</style>
