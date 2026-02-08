const downloadIframes = [];
const MAX_IFRAMES = 5;

function cleanupIframe(iframe) {
  if (iframe.parentNode) {
    iframe.parentNode.removeChild(iframe);
  }
  const idx = downloadIframes.indexOf(iframe);
  if (idx > -1) downloadIframes.splice(idx, 1);
}

function enforceIframeCap() {
  while (downloadIframes.length > MAX_IFRAMES) {
    const oldest = downloadIframes.shift();
    if (oldest && oldest.parentNode) oldest.parentNode.removeChild(oldest);
  }
}

export function triggerIframeDownload(url) {
  const iframe = document.createElement('iframe');
  iframe.style.display = 'none';
  iframe.src = url;
  document.body.appendChild(iframe);
  downloadIframes.push(iframe);
  enforceIframeCap();

  iframe.onload = () => setTimeout(() => cleanupIframe(iframe), 5000);
  setTimeout(() => cleanupIframe(iframe), 2 * 60 * 1000);
}

export function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
