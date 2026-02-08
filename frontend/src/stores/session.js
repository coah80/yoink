import { writable, get } from 'svelte/store';
import { apiBase } from '../lib/api.js';

export const clientId = writable(sessionStorage.getItem('yoink_clientId') || null);

const activeJobIds = new Set();
let jobIdCounter = 0;
let heartbeatInterval = null;
let heartbeatFailures = 0;
const MAX_HEARTBEAT_FAILURES = 3;

export async function initSession() {
  let id = get(clientId);
  if (!id) {
    try {
      const res = await fetch(`${apiBase()}/api/connect`, { method: 'POST' });
      const data = await res.json();
      id = data.clientId;
    } catch {
      id = 'local-' + Date.now() + '-' + Math.random().toString(36).slice(2, 11);
    }
    sessionStorage.setItem('yoink_clientId', id);
    clientId.set(id);
  }
  return id;
}

export function startHeartbeat() {
  const jobId = `job-${++jobIdCounter}-${Date.now()}`;
  const wasEmpty = activeJobIds.size === 0;
  activeJobIds.add(jobId);

  if (wasEmpty && !heartbeatInterval) {
    heartbeatFailures = 0;
    const id = get(clientId);

    if (id && !id.startsWith('local-')) {
      fetch(`${apiBase()}/api/heartbeat/${id}`, { method: 'POST' }).catch(() => {});
    }

    heartbeatInterval = setInterval(async () => {
      const cid = get(clientId);
      if (!cid || cid.startsWith('local-')) return;

      try {
        await fetch(`${apiBase()}/api/heartbeat/${cid}`, { method: 'POST' });
        heartbeatFailures = 0;
      } catch {
        heartbeatFailures++;
        if (heartbeatFailures >= MAX_HEARTBEAT_FAILURES) {
          clearInterval(heartbeatInterval);
          heartbeatInterval = null;
          heartbeatFailures = 0;
        }
      }
    }, 15000);
  }

  return jobId;
}

export function stopHeartbeat(jobId) {
  if (jobId) {
    activeJobIds.delete(jobId);
  } else {
    activeJobIds.clear();
  }

  if (activeJobIds.size === 0 && heartbeatInterval) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', () => {
    if (heartbeatInterval) {
      clearInterval(heartbeatInterval);
      heartbeatInterval = null;
    }
    activeJobIds.clear();
  });
}
