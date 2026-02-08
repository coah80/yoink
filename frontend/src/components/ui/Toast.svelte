<script>
  let { message, type = 'info' } = $props();
</script>

<div class="toast {type}" role="alert">
  {message}
</div>

<style>
  .toast {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font-size: 0.9rem;
    padding: 12px 18px;
    color: var(--text);
    font-family: var(--font-body);
    min-width: 250px;
    max-width: 380px;
    animation: toast-in 0.3s ease forwards;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  }

  .toast.success {
    border-color: var(--success);
    color: var(--success);
  }

  .toast.error {
    border-color: var(--error);
    color: var(--error);
  }

  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateY(16px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
</style>
