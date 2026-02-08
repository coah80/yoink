self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'PLAYLIST_COMPLETE') {
    self.registration.showNotification('yoink', {
      body: event.data.message || 'playlist download ready!',
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      tag: 'playlist-' + event.data.jobId,
      data: { url: '/' }
    });
  }
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if (client.url.includes(self.location.origin)) {
          return client.focus();
        }
      }
      return clients.openWindow('/');
    })
  );
});
