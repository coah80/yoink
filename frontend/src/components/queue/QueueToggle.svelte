<script>
  import { queue } from '../../stores/queue.js';
  import QueueDropdown from './QueueDropdown.svelte';

  let open = $state(false);
  let menuRef;

  let items = $derived($queue);
  let count = $derived(items.length);

  function toggle(e) {
    e.stopPropagation();
    open = !open;
  }

  function handleClickOutside(e) {
    if (menuRef && !menuRef.contains(e.target)) {
      open = false;
    }
  }

  $effect(() => {
    document.addEventListener('click', handleClickOutside);
    return () => document.removeEventListener('click', handleClickOutside);
  });
</script>

<div class="queue-menu" bind:this={menuRef}>
  <button class="queue-toggle" onclick={toggle}>
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <line x1="8" y1="6" x2="21" y2="6"></line>
      <line x1="8" y1="12" x2="21" y2="12"></line>
      <line x1="8" y1="18" x2="21" y2="18"></line>
      <line x1="3" y1="6" x2="3.01" y2="6"></line>
      <line x1="3" y1="12" x2="3.01" y2="12"></line>
      <line x1="3" y1="18" x2="3.01" y2="18"></line>
    </svg>
    queue
    {#if count > 0}
      <span class="queue-badge">{count}</span>
    {/if}
  </button>
  {#if open}
    <div class="queue-dropdown">
      <QueueDropdown />
    </div>
  {/if}
</div>

<style>
  .queue-menu {
    position: relative;
  }

  .queue-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 500;
    padding: 10px 16px;
    border-radius: var(--radius-full);
    background: var(--surface);
    border: 1px solid var(--border);
    cursor: pointer;
    transition: all 0.15s ease-out;
  }

  .queue-toggle:hover {
    color: var(--text);
    border-color: var(--purple-500);
  }

  .queue-toggle svg {
    width: 18px;
    height: 18px;
  }

  .queue-badge {
    background: var(--purple-500);
    color: white;
    font-size: 0.75rem;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: var(--radius-full);
    min-width: 20px;
    text-align: center;
  }

  .queue-dropdown {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    width: 360px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    z-index: 100;
    animation: dropdown-in 0.15s ease-out;
  }

  @keyframes dropdown-in {
    from { opacity: 0; transform: translateY(-8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-width: 600px) {
    .queue-menu {
      display: none;
    }
  }
</style>
