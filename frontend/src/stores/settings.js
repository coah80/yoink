import { writable } from 'svelte/store';
import { DEFAULT_SETTINGS } from '../lib/constants.js';

function createSettingsStore() {
  let initial = { ...DEFAULT_SETTINGS };
  try {
    const saved = localStorage.getItem('yoink_settings');
    if (saved) {
      initial = { ...DEFAULT_SETTINGS, ...JSON.parse(saved) };
    }
  } catch {}

  const { subscribe, set, update } = writable(initial);

  function save(val) {
    localStorage.setItem('yoink_settings', JSON.stringify(val));
  }

  return {
    subscribe,
    set(val) {
      save(val);
      set(val);
    },
    update(fn) {
      update((val) => {
        const next = fn(val);
        save(next);
        return next;
      });
    },
    setSetting(key, value) {
      update((s) => {
        const next = { ...s, [key]: value };
        save(next);
        return next;
      });
    },
    reset() {
      const fresh = { ...DEFAULT_SETTINGS };
      save(fresh);
      set(fresh);
    },
  };
}

export const settings = createSettingsStore();
