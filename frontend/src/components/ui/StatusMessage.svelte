<script>
  import Spinner from './Spinner.svelte';

  let { type = null, message = '' } = $props();
</script>

{#if type}
  <div class="status-message {type}" role="status">
    {#if type === 'loading'}
      <Spinner />
    {/if}
    <span class="status-text">{message}</span>
  </div>
{/if}

<style>
  .status-message {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 0.85rem;
    font-family: var(--font-body);
    padding: 8px 0;
  }

  .status-message.loading .status-text {
    color: var(--text-secondary);
  }

  .status-message.error .status-text {
    color: var(--error);
  }

  .status-message.success .status-text {
    color: var(--success);
  }
</style>
