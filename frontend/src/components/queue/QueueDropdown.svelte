<script>
  import { queue } from '../../stores/queue.js';
  import QueueItem from './QueueItem.svelte';
  import { addToast } from '../../stores/toast.js';

  let items = $derived($queue);
  let hasItems = $derived(items.length > 0);
</script>

<div class="queue-dropdown-header">
  <span class="queue-dropdown-title">queue</span>
  <div class="queue-header-actions">
    <button class="queue-start-all" onclick={() => queue.startAllQueued()} title="Start all queued">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polygon points="5 3 19 12 5 21 5 3"></polygon>
      </svg>
    </button>
    {#if hasItems}
      <button class="queue-clear" onclick={() => { queue.clear(); addToast('Queue cleared', 'info'); }}>clear all</button>
    {/if}
  </div>
</div>
<div class="queue-list">
  {#if hasItems}
    {#each items as item (item.id)}
      <QueueItem {item} />
    {/each}
  {:else}
    <div class="queue-empty">nothing here yet</div>
  {/if}
</div>

<style>
  .queue-dropdown-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 8px;
  }

  .queue-dropdown-title {
    font-family: var(--font-heading);
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .queue-header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .queue-start-all {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: var(--purple-900);
    border: 1px solid var(--purple-500);
    border-radius: var(--radius-sm);
    color: var(--purple-400);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .queue-start-all:hover {
    background: var(--purple-500);
    color: white;
  }

  .queue-start-all svg {
    width: 14px;
    height: 14px;
  }

  .queue-clear {
    font-family: var(--font-body);
    font-size: 0.8rem;
    color: var(--text-muted);
    background: none;
    border: none;
    cursor: pointer;
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    transition: all 0.15s ease-out;
  }

  .queue-clear:hover {
    color: var(--error);
    background: rgba(248, 113, 113, 0.1);
  }

  .queue-list {
    max-height: 300px;
    overflow-y: auto;
  }

  .queue-empty {
    text-align: center;
    padding: 24px 16px;
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  @media (max-width: 600px) {
    .queue-dropdown-header {
      padding: 10px 16px;
    }

    .queue-start-all {
      width: 36px;
      height: 36px;
    }

    .queue-start-all svg {
      width: 16px;
      height: 16px;
    }

    .queue-clear {
      padding: 8px 12px;
      font-size: 0.85rem;
    }

    .queue-list {
      max-height: 50vh;
    }
  }
</style>
