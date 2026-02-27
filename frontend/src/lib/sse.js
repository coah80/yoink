import { apiBase } from './api.js';

export function createSSEConnection(progressId, { onMessage, onError, maxReconnectAttempts = 10 }) {
  let reconnectAttempts = 0;
  let eventSource = null;
  let closed = false;

  function connect() {
    if (closed) return;

    eventSource = new EventSource(`${apiBase()}/api/progress/${progressId}`);

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        reconnectAttempts = 0;
        onMessage(data);

        if (data.stage === 'complete' || data.stage === 'error' || data.stage === 'cancelled') {
          close();
        }
      } catch (e) {
        console.error('SSE parse error:', e);
      }
    };

    eventSource.onerror = () => {
      eventSource.close();

      if (closed) return;

      if (reconnectAttempts < maxReconnectAttempts) {
        reconnectAttempts++;
        const delay = Math.min(1000 * reconnectAttempts, 5000);

        if (onError) {
          onError({ type: 'reconnecting', attempt: reconnectAttempts, max: maxReconnectAttempts });
        }

        setTimeout(connect, delay);
      } else {
        if (onError) {
          onError({ type: 'failed', message: 'Connection lost - refresh page to retry' });
        }
      }
    };
  }

  function close() {
    closed = true;
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }
  }

  connect();

  return { close };
}
