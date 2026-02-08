import { writable, derived } from 'svelte/store';

export const hash = writable(window.location.hash.slice(1) || '/');

window.addEventListener('hashchange', () => {
  hash.set(window.location.hash.slice(1) || '/');
});

export const path = derived(hash, ($hash) => {
  const p = $hash.split('?')[0];
  return p || '/';
});

export const queryString = derived(hash, ($hash) => {
  const idx = $hash.indexOf('?');
  return idx >= 0 ? $hash.slice(idx + 1) : '';
});

export const query = derived(queryString, ($qs) => {
  return Object.fromEntries(new URLSearchParams($qs));
});

export function navigate(to) {
  window.location.hash = '#' + to;
}

export function getQuery() {
  const h = window.location.hash.slice(1) || '/';
  const idx = h.indexOf('?');
  const qs = idx >= 0 ? h.slice(idx + 1) : '';
  return Object.fromEntries(new URLSearchParams(qs));
}
