<script>
  import { toasts } from '../../stores/toast.js';
  import Toast from './Toast.svelte';
</script>

<div class="toast-container">
  {#each $toasts as toast (toast.id)}
    <Toast message={toast.message} type={toast.type} />
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    bottom: 24px;
    right: 24px;
    z-index: 1000;
    display: flex;
    flex-direction: column;
    gap: 8px;
    pointer-events: none;
  }

  .toast-container :global(.toast) {
    pointer-events: auto;
  }
</style>
