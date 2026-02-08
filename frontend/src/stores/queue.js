import { writable, get } from 'svelte/store';
import { createSSEConnection } from '../lib/sse.js';
import { triggerIframeDownload } from '../lib/download.js';
import { apiBase } from '../lib/api.js';
import { addToast } from './toast.js';
import { startHeartbeat, stopHeartbeat } from './session.js';
import { settings } from './settings.js';
import { ACTIVE_STAGES } from '../lib/constants.js';

function createQueueStore() {
  let initial = [];
  try {
    const saved = localStorage.getItem('yoink_queue');
    if (saved) {
      initial = JSON.parse(saved)
        .map((item) => {
          if (ACTIVE_STAGES.includes(item.stage)) {
            return { ...item, stage: 'queued', progress: 0, status: 'Interrupted - tap to restart' };
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

  function save(q) {
    localStorage.setItem('yoink_queue', JSON.stringify(q));
  }

  function getQueue() {
    return get({ subscribe });
  }

  return {
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
      const sse = sseConnections.get(id);
      if (sse) {
        sse.close();
        sseConnections.delete(id);
      }

      const hbId = heartbeatJobs.get(id);
      if (hbId) {
        stopHeartbeat(hbId);
        heartbeatJobs.delete(id);
      }

      const q = getQueue();
      const item = q.find((i) => i.id === id);
      if (item && ACTIVE_STAGES.includes(item.stage)) {
        fetch(`${apiBase()}/api/cancel/${id}`, { method: 'POST' }).catch(() => {});
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

    addLog(id, msg) {
      update((q) => {
        const next = q.map((item) => {
          if (item.id !== id) return item;
          const logs = [...(item.logs || []), { time: new Date().toLocaleTimeString(), msg }];
          if (logs.length > 50) logs.shift();
          return { ...item, logs };
        });
        save(next);
        return next;
      });
    },

    clear() {
      const q = getQueue();
      q.forEach((item) => {
        const sse = sseConnections.get(item.id);
        if (sse) sse.close();
        const hbId = heartbeatJobs.get(item.id);
        if (hbId) stopHeartbeat(hbId);
        if (ACTIVE_STAGES.includes(item.stage)) {
          fetch(`${apiBase()}/api/cancel/${item.id}`, { method: 'POST' }).catch(() => {});
        }
      });
      sseConnections.clear();
      heartbeatJobs.clear();
      set([]);
      save([]);
    },

    startDownload(item) {
      const s = get(settings);
      const heartbeatJobId = startHeartbeat();
      heartbeatJobs.set(item.id, heartbeatJobId);

      this.updateItem(item.id, {
        stage: 'starting',
        status: 'Starting download...',
        startTime: Date.now(),
        logs: [{ time: new Date().toLocaleTimeString(), msg: 'Starting download...' }],
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

          if (data.log) this.addLog(item.id, data.log);
          if (data.message) this.addLog(item.id, data.message);

          if (data.stage === 'complete') {
            patch.endTime = Date.now();
            this.updateItem(item.id, patch);
            stopHeartbeat(heartbeatJobId);
            heartbeatJobs.delete(item.id);
            sseConnections.delete(item.id);

            const q = getQueue();
            const qItem = q.find((i) => i.id === item.id);
            const elapsed = qItem?.startTime ? Math.floor((Date.now() - qItem.startTime) / 1000) : 0;
            const timeStr = elapsed >= 60 ? `${Math.floor(elapsed / 60)}m ${elapsed % 60}s` : `${elapsed}s`;
            const msg = item.format === 'images'
              ? 'Gallery download complete!'
              : item.isPlaylist
                ? 'Playlist download complete!'
                : `Download complete! (${timeStr})`;
            addToast(msg, 'success');

            setTimeout(() => this.remove(item.id), 5000);
          } else if (data.stage === 'error') {
            stopHeartbeat(heartbeatJobId);
            heartbeatJobs.delete(item.id);
            sseConnections.delete(item.id);
            addToast(data.message, 'error');
          } else if (data.stage === 'cancelled') {
            stopHeartbeat(heartbeatJobId);
            heartbeatJobs.delete(item.id);
            sseConnections.delete(item.id);
          }
        },
        onError: (err) => {
          if (err.type === 'reconnecting') {
            this.updateItem(item.id, { status: `Reconnecting... (${err.attempt}/${err.max})` });
          } else {
            this.updateItem(item.id, { stage: 'error', status: err.message });
            stopHeartbeat(heartbeatJobId);
            heartbeatJobs.delete(item.id);
            sseConnections.delete(item.id);
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
      } else if (item.isPlaylist) {
        endpoint = '/api/download-playlist';
      } else {
        endpoint = '/api/download';
      }

      triggerIframeDownload(`${apiBase()}${endpoint}?${params.toString()}`);
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
        status: 'Queued for retry',
        logs: [],
        endTime: null,
      });

      const updatedQ = getQueue();
      const updatedItem = updatedQ.find((i) => i.id === newId);
      if (updatedItem) {
        this.startDownload(updatedItem);
      }
    },

    async finishPlaylistEarly(id) {
      try {
        await fetch(`${apiBase()}/api/finish-early/${id}`, { method: 'POST' });
      } catch {
        addToast('Failed to finish early', 'error');
      }
    },

    toggleLogs(id) {
      update((q) => {
        const next = q.map((item) => (item.id === id ? { ...item, logsExpanded: !item.logsExpanded } : item));
        return next;
      });
    },
  };
}

export const queue = createQueueStore();
