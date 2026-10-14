<script>
  let { options, value, name, onchange } = $props();
</script>

<div class="segmented-control" role="radiogroup" aria-label={name}>
  {#each options as option}
    <button
      type="button"
      class="segment"
      class:active={value === option.value}
      role="radio"
      aria-checked={value === option.value}
      onclick={() => onchange(option.value)}
    >
      {option.label}
    </button>
  {/each}
</div>

<style>
  .segmented-control {
    display: flex;
    flex-direction: row;
    gap: 0;
  }

  .segment {
    background: var(--surface);
    border: 1px solid var(--border);
    padding: 8px 16px;
    color: var(--text-secondary);
    font-family: var(--font-body);
    font-size: 0.85rem;
    cursor: pointer;
    transition: all 0.15s ease;
    outline: none;
  }

  .segment:first-child {
    border-radius: var(--radius-sm) 0 0 var(--radius-sm);
  }

  .segment:last-child {
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  }

  .segment:not(:first-child) {
    border-left: none;
  }

  .segment:hover:not(.active) {
    background: var(--surface-elevated);
  }

  .segment.active {
    background: var(--purple-900);
    border-color: var(--purple-500);
    color: var(--purple-400);
  }

  .segment.active + .segment {
    border-left: 1px solid var(--border);
  }
</style>
