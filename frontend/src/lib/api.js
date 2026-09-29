export function apiBase() {
  return import.meta.env.VITE_API_URL?.replace(/\/$/, '') || '';
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
      errorMsg = `Server error ${res.status} - please try again`;
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
