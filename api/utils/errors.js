function toUserError(message) {
  const text = String(message || '');
  const msg = text.toLowerCase();

  if (msg.includes('cancelled') || msg.includes('canceled')) return 'Download cancelled';

  if (msg.includes('content.video.unavailable') || msg.includes('video unavailable') || msg.includes('private video') || msg.includes('this content is private')) return 'This video is unavailable or has been removed';
  if (msg.includes('content.video.live') || msg.includes('live stream')) return "Live streams can't be downloaded yet";
  if (msg.includes('content.video.age') || msg.includes('age-restricted') || msg.includes('age restricted')) return 'This video is age-restricted';
  if (msg.includes('content.too_long') || msg.includes('too_long')) return 'Video is too long (3+ hours)';
  if (msg.includes('api.youtube.login') || msg.includes('youtube.login')) return 'YouTube requires login for this video';
  if (msg.includes('api.rate_limited')) return 'Rate limited — try again in a minute';
  if (msg.includes('api.link.unsupported')) return "This link type isn't supported";

  if (msg.includes('sign in to confirm') || msg.includes('sign in to verify')) return 'YouTube is blocking this request — try again later';
  if (msg.includes('geo restricted') || msg.includes('geo-restricted') || msg.includes('not available in your country')) return "This video isn't available in the server's region";
  if (msg.includes('copyright')) return 'This video was removed for copyright';
  if (msg.includes('members only') || msg.includes('members-only')) return 'This is a members-only video';
  if (msg.includes('premium')) return 'This video requires YouTube Premium';
  if (msg.includes('http error 403') || msg.includes('403 forbidden')) return 'Access denied — the site is blocking downloads';
  if (msg.includes('http error 404') || msg.includes('404 not found')) return 'Video not found — it may have been deleted';
  if (msg.includes('unsupported url')) return "This website isn't supported";
  if (msg.includes('no video formats') || msg.includes('requested format not available')) return 'No downloadable formats found';

  if (msg.includes('rate') && !msg.includes('format')) return 'Rate limited — please wait and try again';
  if (msg.includes('econnreset') || msg.includes('fetch failed') || msg.includes('connection') && !msg.includes('connected')) return 'Connection dropped — try again';
  if (msg.includes('etimedout') || msg.includes('timed out') || msg.includes('timeout')) return 'Connection timed out — try again';
  if (msg.includes('enotfound') || msg.includes('dns')) return "Couldn't reach the server — try again";

  if (msg.includes('processing failed') || msg.includes('encoding failed')) return 'Processing failed';
  if (msg.includes('download interrupted')) return 'Download interrupted';
  if (msg.includes('no videos were successfully downloaded')) return 'No videos were successfully downloaded';
  if (msg.includes('downloaded file not found') || msg.includes('file not found')) return 'Download failed';
  if (msg.includes('playlist too large')) return text;
  if (msg.includes('too many active jobs')) return text;

  return 'Download failed';
}

module.exports = { toUserError };
