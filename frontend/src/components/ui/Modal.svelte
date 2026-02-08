<script>
  import { untrack } from 'svelte';

  let { open = false, title = '', onclose, children } = $props();

  function handleKeydown(e) {
    if (e.key === 'Escape') {
      onclose();
    }
  }

  function handleOverlayClick(e) {
    if (e.target === e.currentTarget) {
      onclose();
    }
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="modal-overlay"
    role="dialog"
    aria-modal="true"
    aria-label={title || 'Modal'}
    onclick={handleOverlayClick}
    onkeydown={handleKeydown}
  >
    <div class="modal-card">
      {#if title}
        <div class="modal-header">
          <h2 class="modal-title">{title}</h2>
          <button type="button" class="modal-close" onclick={onclose} aria-label="Close modal">
            &times;
          </button>
        </div>
      {/if}
      <div class="modal-body">
        {@render children()}
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2000;
    animation: fade-in 0.2s ease;
  }

  .modal-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 24px;
    min-width: 320px;
    max-width: 520px;
    width: 90%;
    max-height: 85vh;
    overflow-y: auto;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }

  .modal-title {
    font-family: var(--font-heading);
    font-size: 1.1rem;
    font-weight: 600;
    color: var(--text);
  }

  .modal-close {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 1.5rem;
    cursor: pointer;
    padding: 0 4px;
    line-height: 1;
    transition: color 0.15s ease;
  }

  .modal-close:hover {
    color: var(--text);
  }

  .modal-body {
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  @keyframes fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
</style>
