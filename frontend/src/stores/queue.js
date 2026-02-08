import { writable, get } from 'svelte/store';
import { createSSEConnection } from '../lib/sse.js';
import { triggerIframeDownload } from '../lib/download.js';
import { apiBase, post } from '../lib/api.js';
import { addToast } from './toast.js';
import { startHeartbeat, stopHeartbeat, clientId } from './session.js';
import { settings } from './settings.js';
import { ACTIVE_STAGES } from '../lib/constants.js';
import { requestNotificationPermission, sendPlaylistNotification, registerServiceWorker } from '../lib/notifications.js';

function createQueueStore() {
  let initial = [];
  try {
    const saved = localStorage.getItem('yoink_queue');
    if (saved) {
      initial = JSON.parse(saved)
        .map((item) => {
          if (item.isPlaylist && item.serverJobId && item.stage !== 'ready') {
            return { ...item, stage: 'reconnecting', progress: item.progress || 0, status: 'reconnecting...' };
          }
          if (ACTIVE_STAGES.includes(item.stage)) {
            return { ...item, stage: 'queued', progress: 0, status: 'interrupted - tap to restart' };
          }
          return item;
        })
        .filter((item) => item.stage !== 'complete' && item.stage !== 'error');
    }
  } catch {
    initial = [];
  }

  const { subscribe, set, update } = writable(initial);
  const sseConnections = new Map();
  const heartbeatJobs = new Map();
  const pollingIntervals = new Map();

  function save(q) {
    localStorage.setItem('yoink_queue', JSON.stringify(q));
  }

  function getQueue() {
    return get({ subscribe });
  }

  function stopPolling(id) {
    const pollId = pollingIntervals.get(id);
    if (pollId) {
      clearInterval(pollId);
      pollingIntervals.delete(id);
    }
  }

  function cleanupJob(id) {
    const sse = sseConnections.get(id);
    if (sse) {
      sse.close();
      sseConnections.delete(id);
    }
    stopPolling(id);
    const hbId = heartbeatJobs.get(id);
    if (hbId) {
      stopHeartbeat(hbId);
      heartbeatJobs.delete(id);
    }
  }

  const store = {
    subscribe,

    add(item) {
      update((q) => {
        if (q.some((i) => i.id === item.id)) return q;
        const next = [...q, item];
        save(next);
        return next;
      });
    },

    remove(id) {
      const q = getQueue();
      const item = q.find((i) => i.id === id);

      cleanupJob(id);

      if (item && ACTIVE_STAGES.includes(item.stage)) {
        const cancelId = item.serverJobId || id;
        fetch(`${apiBase()}/api/cancel/${cancelId}`, { method: 'POST' }).catch(() => {});
      }

      update((q) => {
        const next = q.filter((i) => i.id !== id);
        save(next);
        return next;
      });
    },

    updateItem(id, patch) {
      update((q) => {
        const next = q.map((item) => (item.id === id ? { ...item, ...patch } : item));
        save(next);
        return next;
      });
    },

    clear() {
      const q = getQueue();
      q.forEach((item) => {
        cleanupJob(item.id);
        if (ACTIVE_STAGES.includes(item.stage)) {
          const cancelId = item.serverJobId || item.id;
          fetch(`${apiBase()}/api/cancel/${cancelId}`, { method: 'POST' }).catch(() => {});
        }
      });
      sseConnections.clear();
      heartbeatJobs.clear();
      pollingIntervals.clear();
      set([]);
      save([]);
    },

    startDownload(item) {
      if (item.isPlaylist) {
        this._startPlaylistDownload(item);
        return;
      }
      this._startDirectDownload(item);
    },

    _startDirectDownload(item) {
      const s = get(settings);
      const heartbeatJobId = startHeartbeat();
      heartbeatJobs.set(item.id, heartbeatJobId);

      this.updateItem(item.id, {
        stage: 'starting',
        status: 'starting download...',
        startTime: Date.now(),
      });

      const sse = createSSEConnection(item.id, {
        onMessage: (data) => {
          const patch = {
            stage: data.stage,
            status: data.message,
          };
          if (data.progress !== undefined) patch.progress = data.progress;
          if (data.speed !== undefined) patch.speed = data.speed;
          if (data.eta !== undefined) patch.eta = data.eta;
          if (data.totalVideos !== undefined) patch.videoCount = data.totalVideos;
          if (data.currentVideo !== undefined) patch.currentVideo = data.currentVideo;
          if (data.currentVideoTitle !== undefined) patch.currentVideoTitle = data.currentVideoTitle;
          if (data.format !== undefined) patch.formatDisplay = data.format;
          if (data.downloadedCount !== undefined) patch.downloadedCount = data.downloadedCount;
          if (data.failedVideos) patch.failedVideos = data.failedVideos;
          if (data.failedCount !== undefined) patch.failedCount = data.failedCount;

          this.updateItem(item.id, patch);

          if (data.stage === 'complete') {
            patch.endTime = Date.now();
            this.updateItem(item.id, patch);
            cleanupJob(item.id);

            const q = getQueue();
            const qItem = q.find((i) => i.id === item.id);
            const elapsed = qItem?.startTime ? Math.floor((Date.now() - qItem.startTime) / 1000) : 0;
            const timeStr = elapsed >= 60 ? `${Math.floor(elapsed / 60)}m ${elapsed % 60}s` : `${elapsed}s`;
            const msg = item.format === 'images'
              ? 'gallery download complete!'
              : `download complete! (${timeStr})`;
            addToast(msg, 'success');

            setTimeout(() => this.remove(item.id), 5000);
          } else if (data.stage === 'error') {
            cleanupJob(item.id);
            addToast(data.message, 'error');
          } else if (data.stage === 'cancelled') {
            cleanupJob(item.id);
          }
        },
        onError: (err) => {
          if (err.type === 'reconnecting') {
            this.updateItem(item.id, { status: `reconnecting... (${err.attempt}/${err.max})` });
          } else {
            this.updateItem(item.id, { stage: 'error', status: err.message });
            cleanupJob(item.id);
          }
        },
      });

      sseConnections.set(item.id, sse);

      const params = new URLSearchParams({
        url: item.url,
        format: item.format,
        filename: item.title,
        quality: s.quality,
        container: s.container,
        audioFormat: s.audioFormat,
        audioBitrate: s.audioBitrate,
        progressId: item.id,
        twitterGifs: s.twitterGifs !== false ? 'true' : 'false',
      });

      let endpoint;
      if (item.format === 'images') {
        endpoint = '/api/gallery/download';
      } else {
        endpoint = '/api/download';
      }

      triggerIframeDownload(`${apiBase()}${endpoint}?${params.toString()}`);
    },

    async _startPlaylistDownload(item) {
      const s = get(settings);

      this.updateItem(item.id, {
        stage: 'starting',
        status: 'starting playlist download...',
        startTime: Date.now(),
      });

      registerServiceWorker();
      requestNotificationPermission();

      try {
        const cid = get(clientId);
        const data = await post('/api/playlist/start', {
          url: item.url,
          format: item.format,
          quality: s.quality,
          container: s.container,
          audioFormat: s.audioFormat,
          audioBitrate: s.audioBitrate,
          clientId: cid || undefined,
        });

        const serverJobId = data.jobId;
        this.updateItem(item.id, { serverJobId });

        this._connectPlaylistSSE(item.id, serverJobId);
        this._startPlaylistPolling(item.id, serverJobId);

      } catch (err) {
        this.updateItem(item.id, { stage: 'error', status: err.message || 'failed to start playlist' });
        addToast(err.message || 'failed to start playlist', 'error');
      }
    },

    _connectPlaylistSSE(itemId, serverJobId) {
      const sse = createSSEConnection(serverJobId, {
        onMessage: (data) => {
          this._handlePlaylistUpdate(itemId, data);
        },
        onError: () => {},
      });
      sseConnections.set(itemId, sse);
    },

    _startPlaylistPolling(itemId, serverJobId) {
      stopPolling(itemId);

      const pollId = setInterval(async () => {
        try {
          const res = await fetch(`${apiBase()}/api/playlist/status/${serverJobId}`);
          if (!res.ok) {
            if (res.status === 404) {
              this.updateItem(itemId, { stage: 'error', status: 'playlist job not found on server' });
              stopPolling(itemId);
            }
            return;
          }
          const data = await res.json();
          this._handlePlaylistUpdate(itemId, {
            stage: data.status === 'complete' ? 'complete' : data.status,
            message: data.message,
            progress: data.progress,
            totalVideos: data.totalVideos,
            currentVideo: data.currentVideo,
            currentVideoTitle: data.currentVideoTitle,
            failedVideos: data.failedVideos,
            failedCount: data.failedCount,
            downloadedCount: data.videosCompleted,
            downloadToken: data.downloadToken,
            fileName: data.fileName,
            fileSize: data.fileSize,
            speed: data.speed,
            eta: data.eta,
          });
        } catch {}
      }, 3000);

      pollingIntervals.set(itemId, pollId);
    },

    _handlePlaylistUpdate(itemId, data) {
      const patch = {
        stage: data.stage,
        status: data.message,
      };
      if (data.progress !== undefined) patch.progress = data.progress;
      if (data.speed !== undefined) patch.speed = data.speed;
      if (data.eta !== undefined) patch.eta = data.eta;
      if (data.totalVideos !== undefined) patch.videoCount = data.totalVideos;
      if (data.currentVideo !== undefined) patch.currentVideo = data.currentVideo;
      if (data.currentVideoTitle !== undefined) patch.currentVideoTitle = data.currentVideoTitle;
      if (data.downloadedCount !== undefined) patch.downloadedCount = data.downloadedCount;
      if (data.failedVideos) patch.failedVideos = data.failedVideos;
      if (data.failedCount !== undefined) patch.failedCount = data.failedCount;

      if (data.stage === 'complete' && data.downloadToken) {
        patch.stage = 'ready';
        patch.downloadToken = data.downloadToken;
        patch.fileName = data.fileName;
        patch.fileSize = data.fileSize;
        patch.endTime = Date.now();
        patch.status = 'playlist ready — tap to download';

        this.updateItem(itemId, patch);
        cleanupJob(itemId);

        const q = getQueue();
        const qItem = q.find((i) => i.id === itemId);
        addToast(`${qItem?.title || 'playlist'} is ready to download!`, 'success', 8000);
        sendPlaylistNotification(qItem?.title || 'playlist', qItem?.serverJobId);
        return;
      }

      if (data.stage === 'error') {
        this.updateItem(itemId, patch);
        cleanupJob(itemId);
        addToast(data.message, 'error');
        return;
      }

      if (data.stage === 'cancelled') {
        this.updateItem(itemId, patch);
        cleanupJob(itemId);
        return;
      }

      this.updateItem(itemId, patch);
    },

    downloadReady(id) {
      const q = getQueue();
      const item = q.find((i) => i.id === id);
      if (!item || !item.downloadToken) return;

      triggerIframeDownload(`${apiBase()}/api/playlist/download/${item.downloadToken}`);
      addToast('downloading playlist zip...', 'success');

      setTimeout(() => this.remove(id), 10000);
    },

    startAllQueued() {
      const q = getQueue();
      const queued = q.filter((item) => item.stage === 'queued');
      queued.forEach((item, i) => {
        setTimeout(() => this.startDownload(item), i * 500);
      });
    },

    retryDownload(id) {
      const q = getQueue();
      const item = q.find((i) => i.id === id);
      if (!item) return;

      const newId = Date.now().toString() + Math.random().toString(36).substr(2, 9);
      this.updateItem(id, {
        id: newId,
        stage: 'queued',
        progress: 0,
        status: 'queued for retry',
        logs: [],
        endTime: null,
        serverJobId: null,
        downloadToken: null,
      });

      const updatedQ = getQueue();
      const updatedItem = updatedQ.find((i) => i.id === newId);
      if (updatedItem) {
        this.startDownload(updatedItem);
      }
    },

    async finishPlaylistEarly(id) {
      const q = getQueue();
      const item = q.find((i) => i.id === id);
      const cancelId = item?.serverJobId || id;
      try {
        await fetch(`${apiBase()}/api/finish-early/${cancelId}`, { method: 'POST' });
      } catch {
        addToast('failed to finish early', 'error');
      }
    },

    resumePlaylistJobs() {
      const q = getQueue();
      q.filter((item) => item.stage === 'reconnecting' && item.serverJobId).forEach((item) => {
        this._connectPlaylistSSE(item.id, item.serverJobId);
        this._startPlaylistPolling(item.id, item.serverJobId);
      });
    },
  };

  if (typeof window !== 'undefined') {
    setTimeout(() => store.resumePlaylistJobs(), 500);
  }

  return store;
}

export const queue = createQueueStore();
