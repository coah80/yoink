export function apiBase() {
  const hostname = window.location.hostname;
  if (hostname === 'localhost' || hostname === '127.0.0.1') {
    return 'http://localhost:3001';
  }
  return 'https://yoink.coah80.com';
}

export async function fetchJson(url, opts = {}) {
  const res = await fetch(url, opts);
  const contentType = res.headers.get('content-type');

  if (!res.ok) {
    let errorMsg = `Request failed (${res.status})`;
    if (contentType && contentType.includes('application/json')) {
      const err = await res.json();
      errorMsg = err.error || errorMsg;
    } else {
      const text = await res.text();
      console.error('Non-JSON error response:', text.substring(0, 500));
      errorMsg = `Server error ${res.status} (likely VPN blocked)`;
    }
    throw new Error(errorMsg);
  }

  return res.json();
}

export async function post(path, body = {}) {
  return fetchJson(`${apiBase()}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
