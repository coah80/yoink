const API_BASE = (() => {
  if (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1') {
    return 'http://localhost:3001';
  }
  return 'https://yoink.coah80.com';
})();

let clientId = sessionStorage.getItem('yoink_clientId');
let heartbeatInterval = null;

async function initSession() {
  if (!clientId) {
    try {
      const res = await fetch(`${API_BASE}/api/connect`, { method: 'POST' });
      const data = await res.json();
      clientId = data.clientId;
      sessionStorage.setItem('yoink_clientId', clientId);
    } catch (err) {
      console.error('Failed to connect to server:', err);
      clientId = 'local-' + Date.now() + '-' + Math.random().toString(36).slice(2, 11);
      sessionStorage.setItem('yoink_clientId', clientId);
    }
  }
  
  return clientId;
}

const activeJobIds = new Set();
let jobIdCounter = 0;
let heartbeatFailures = 0;
const MAX_HEARTBEAT_FAILURES = 3;

function startHeartbeat() {
  const jobId = `job-${++jobIdCounter}-${Date.now()}`;
  const wasEmpty = activeJobIds.size === 0;
  activeJobIds.add(jobId);
  
  if (wasEmpty && !heartbeatInterval) {
    heartbeatFailures = 0;
    
    if (clientId && !clientId.startsWith('local-')) {
      fetch(`${API_BASE}/api/heartbeat/${clientId}`, { method: 'POST' }).catch(() => {});
    }
    
    heartbeatInterval = setInterval(async () => {
      if (!clientId || clientId.startsWith('local-')) return;
      
      try {
        await fetch(`${API_BASE}/api/heartbeat/${clientId}`, { method: 'POST' });
        heartbeatFailures = 0;
      } catch (err) {
        heartbeatFailures++;
        console.debug(`[Heartbeat] Failed for ${clientId.slice(0, 12)}...: ${err.message} (${heartbeatFailures}/${MAX_HEARTBEAT_FAILURES})`);
        
        if (heartbeatFailures >= MAX_HEARTBEAT_FAILURES) {
          console.debug(`[Heartbeat] Stopped after ${MAX_HEARTBEAT_FAILURES} consecutive failures`);
          clearInterval(heartbeatInterval);
          heartbeatInterval = null;
          heartbeatFailures = 0;
        }
      }
    }, 15000);
  }
  
  return jobId;
}

function stopHeartbeat(jobId) {
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

window.addEventListener('beforeunload', () => {
  if (heartbeatInterval) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
  }
  activeJobIds.clear();
});

function getTrackingId() {
  let trackingId = localStorage.getItem('yoink_trackingId');
  if (!trackingId) {
    trackingId = 'tid-' + Date.now() + '-' + Math.random().toString(36).slice(2, 18);
    localStorage.setItem('yoink_trackingId', trackingId);
  }
  return trackingId;
}

function clearTrackingId() {
  localStorage.removeItem('yoink_trackingId');
  localStorage.removeItem('yoink_last_daily_report');
}

async function deleteUserAnalyticsData() {
  const trackingId = localStorage.getItem('yoink_trackingId');
  if (!trackingId) return { deleted: false };
  
  try {
    const res = await fetch(`${API_BASE}/api/analytics/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ trackingId })
    });
    const result = await res.json();
    if (result.deleted) {
      clearTrackingId();
    }
    return result;
  } catch (e) {
    console.error('Failed to delete analytics data:', e);
    return { deleted: false, error: e.message };
  }
}

function getSettings() {
  try {
    const saved = localStorage.getItem('yoink_settings');
    if (saved) return JSON.parse(saved);
  } catch (e) {}
  return { analytics: true };
}

function isAnalyticsEnabled() {
  return getSettings().analytics !== false;
}

async function reportPageView(page) {
  if (!isAnalyticsEnabled()) return;
  
  try {
    await fetch(`${API_BASE}/api/analytics/track`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'pageview', page, trackingId: getTrackingId() })
    });
  } catch (e) {}
}

async function reportDailyUser() {
  if (!isAnalyticsEnabled()) return;
  
  const today = new Date().toISOString().split('T')[0];
  const lastReport = localStorage.getItem('yoink_last_daily_report');
  if (lastReport === today) return;
  
  try {
    await fetch(`${API_BASE}/api/analytics/track`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'dailyUser', trackingId: getTrackingId() })
    });
    localStorage.setItem('yoink_last_daily_report', today);
  } catch (e) {}
}





function showToast(message, type = 'info') {
  let container = document.querySelector('.toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'toast-container';
    document.body.appendChild(container);
  }
  
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  container.appendChild(toast);
  
  setTimeout(() => {
    toast.style.opacity = '0';
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.max(Math.floor(Math.log(bytes) / Math.log(k)), 0), sizes.length - 1);
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

document.addEventListener('DOMContentLoaded', () => {
  if (!('onpagereveal' in window)) {
    const transitionDir = sessionStorage.getItem('yoink_transition_dir');
    if (transitionDir === 'back') {
      document.documentElement.classList.add('view-transition-back');
    } else {
      document.documentElement.classList.remove('view-transition-back');
    }
    sessionStorage.removeItem('yoink_transition_dir');
  }
  
  if (isAnalyticsEnabled()) {
    initSession();
  }
  
  const currentPath = window.location.pathname.replace(/^\//, '').replace(/\.html$/, '') || 'index';
  document.querySelectorAll('.nav-link').forEach(link => {
    const linkPath = link.getAttribute('href').replace(/^\//, '').replace(/\.html$/, '') || 'index';
    if (linkPath === currentPath || (currentPath === 'index' && linkPath === '')) {
      link.classList.add('active');
    }
  });
  
  reportDailyUser();
  reportPageView(currentPath);
});

window.addEventListener('pagehide', () => {
  if (heartbeatInterval) {
    clearInterval(heartbeatInterval);
  }
});
