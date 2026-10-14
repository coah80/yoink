<div class="spinner" role="status" aria-label="Loading"></div>

<style>
  .spinner {
    width: 20px;
    height: 20px;
    border: 3px solid var(--border);
    border-top: 3px solid var(--purple-500);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }
</style>
