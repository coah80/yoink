const API_BASE = window.location.hostname === 'localhost' 
  ? 'http://localhost:3001' 
  : 'https://yoink.coah80.com';



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
      clientId = 'local-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
      sessionStorage.setItem('yoink_clientId', clientId);
    }
  }
  
  startHeartbeat();
  
  return clientId;
}

function startHeartbeat() {
  if (heartbeatInterval) return;
  
  heartbeatInterval = setInterval(async () => {
    if (!clientId) return;
    
    try {
      await fetch(`${API_BASE}/api/heartbeat/${clientId}`, { method: 'POST' });
    } catch (err) {
    }
  }, 15000);
  
  if (clientId && !clientId.startsWith('local-')) {
    fetch(`${API_BASE}/api/heartbeat/${clientId}`, { method: 'POST' }).catch(() => {});
  }
}

function getClientId() {
  return clientId;
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
      body: JSON.stringify({ type: 'pageview', page })
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
      body: JSON.stringify({ type: 'dailyUser' })
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
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
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
  
  initSession();
  setupNavigation();
  
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
