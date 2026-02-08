import { writable } from 'svelte/store';

let id = 0;

function createToastStore() {
  const { subscribe, update } = writable([]);

  return {
    subscribe,
    add(message, type = 'info', duration = 3000) {
      const toast = { id: id++, message, type };
      update((t) => [...t, toast]);
      setTimeout(() => {
        update((t) => t.filter((item) => item.id !== toast.id));
      }, duration);
    },
    remove(toastId) {
      update((t) => t.filter((item) => item.id !== toastId));
    },
  };
}

export const toasts = createToastStore();

export function addToast(msg, type, duration) {
  toasts.add(msg, type, duration);
}
