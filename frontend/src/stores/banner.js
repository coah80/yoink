import { writable } from 'svelte/store';
import { apiBase } from '../lib/api.js';

export const banner = writable(null);

let pollInterval;

export async function fetchBanner() {
  try {
    const res = await fetch(`${apiBase()}/api/banner`);
    const data = await res.json();
    banner.set(data.banner);
  } catch {
    banner.set(null);
  }
}

export function startBannerPolling() {
  fetchBanner();
  pollInterval = setInterval(fetchBanner, 60000);
}

export function stopBannerPolling() {
  clearInterval(pollInterval);
}
