let swRegistered = false;

export async function registerServiceWorker() {
  if (swRegistered || !('serviceWorker' in navigator)) return;
  try {
    await navigator.serviceWorker.register('/sw.js');
    swRegistered = true;
  } catch {}
}

export async function requestNotificationPermission() {
  if (!('Notification' in window)) return 'unsupported';
  if (Notification.permission === 'granted') return 'granted';
  if (Notification.permission === 'denied') return 'denied';
  return await Notification.requestPermission();
}

export async function sendPlaylistNotification(title, jobId) {
  if (!('Notification' in window) || Notification.permission !== 'granted') return;

  await registerServiceWorker();

  if ('serviceWorker' in navigator) {
    try {
      const reg = await navigator.serviceWorker.ready;
      if (reg.active) {
        reg.active.postMessage({
          type: 'PLAYLIST_COMPLETE',
          message: `${title} is ready to download!`,
          jobId
        });
        return;
      }
    } catch {}
  }

  new Notification('yoink', {
    body: `${title} is ready to download!`,
    icon: '/icons/icon-192.png'
  });
}
