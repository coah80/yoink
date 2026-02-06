function toUserError(message) {
  const text = String(message || '');
  const msg = text.toLowerCase();

  if (msg.includes('cancelled')) return 'Download cancelled';
  if (msg.includes('content.video.unavailable') || msg.includes('video unavailable') || msg.includes('private video') || msg.includes('this content is private')) return 'Video unavailable or private';
  if (msg.includes('content.video.live') || msg.includes('live stream')) return 'Live streams cannot be downloaded';
  if (msg.includes('content.video.age') || msg.includes('age-restricted')) return 'Age-restricted video (sign-in required)';
  if (msg.includes('rate')) return 'Rate limited - please wait and try again';
  if (msg.includes('econnreset') || msg.includes('fetch failed') || msg.includes('connection')) return 'Connection interrupted - try again';
  if (msg.includes('processing failed') || msg.includes('encoding failed')) return 'Processing failed';
  if (msg.includes('download interrupted')) return 'Download interrupted';
  if (msg.includes('no videos were successfully downloaded')) return 'No videos were successfully downloaded';
  if (msg.includes('downloaded file not found')) return 'Download failed';

  return 'Download failed';
}

module.exports = { toUserError };
