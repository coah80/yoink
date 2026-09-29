import { writable, derived } from 'svelte/store';

function getPathFromUrl() {
  return window.location.pathname || '/';
}

function getQsFromUrl() {
  return window.location.search ? window.location.search.slice(1) : '';
}

// support old hash-based URLs by redirecting to clean paths
function migrateHashUrl() {
  const hash = window.location.hash;
  if (hash && hash.startsWith('#/')) {
    const newPath = hash.slice(1);
    history.replaceState(null, '', newPath);
  }
}

migrateHashUrl();

export const currentUrl = writable(getPathFromUrl());

window.addEventListener('popstate', () => {
  currentUrl.set(getPathFromUrl());
});

export const path = derived(currentUrl, ($url) => {
  const p = $url.split('?')[0];
  return p || '/';
});

export const queryString = derived(currentUrl, () => {
  return getQsFromUrl();
});

export const query = derived(queryString, ($qs) => {
  return Object.fromEntries(new URLSearchParams($qs));
});

export function navigate(to) {
  history.pushState(null, '', to);
  currentUrl.set(to);
}

export function getQuery() {
  const qs = getQsFromUrl();
  return Object.fromEntries(new URLSearchParams(qs));
}
