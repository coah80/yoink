export function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.max(Math.floor(Math.log(bytes) / Math.log(k)), 0), sizes.length - 1);
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function escapeHtml(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

export function normalizeUrl(url) {
  try {
    const parsed = new URL(url);
    if (parsed.hostname === 'youtu.be') {
      const videoId = parsed.pathname.slice(1).split('?')[0];
      const listParam = parsed.searchParams.get('list');
      return `https://www.youtube.com/watch?v=${videoId}${listParam ? `&list=${listParam}` : ''}`;
    }
    if (parsed.hostname.includes('youtube.com')) {
      if (parsed.pathname.startsWith('/shorts/')) {
        const videoId = parsed.pathname.replace('/shorts/', '').split('?')[0];
        return `https://www.youtube.com/watch?v=${videoId}`;
      }
      if (parsed.searchParams.get('v')) {
        const listParam = parsed.searchParams.get('list');
        return `https://www.youtube.com/watch?v=${parsed.searchParams.get('v')}${listParam ? `&list=${listParam}` : ''}`;
      }
      if (parsed.pathname === '/playlist' && parsed.searchParams.get('list')) {
        return `https://www.youtube.com/playlist?list=${parsed.searchParams.get('list')}`;
      }
    }
    return url;
  } catch {
    return url;
  }
}

export function isYouTubeUrl(url) {
  try {
    const host = new URL(url).hostname;
    return host === 'youtu.be' || host.includes('youtube.com');
  } catch {
    return false;
  }
}

export function hasPlaylistParam(url) {
  try {
    return new URL(url).searchParams.has('list');
  } catch {
    return false;
  }
}

export function generateProgressId() {
  return Date.now().toString() + Math.random().toString(36).substr(2, 9);
}
