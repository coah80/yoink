<script>
  let { active = false, label, ontoggle } = $props();
</script>

<button
  type="button"
  class="toggle-wrapper"
  role="switch"
  aria-checked={active}
  aria-label={label}
  onclick={() => ontoggle(!active)}
>
  <span class="toggle-track" class:active>
    <span class="toggle-knob"></span>
  </span>
  {#if label}
    <span class="toggle-label">{label}</span>
  {/if}
</button>

<style>
  .toggle-wrapper {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
    outline: none;
  }

  .toggle-track {
    position: relative;
    width: 44px;
    height: 24px;
    background: var(--border);
    border-radius: var(--radius-full);
    transition: background 0.2s ease;
  }

  .toggle-track.active {
    background: var(--purple-500);
  }

  .toggle-knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    background: var(--text);
    border-radius: 50%;
    transition: transform 0.2s ease;
  }

  .toggle-track.active .toggle-knob {
    transform: translateX(20px);
  }

  .toggle-label {
    color: var(--text-secondary);
    font-family: var(--font-body);
    font-size: 0.85rem;
    user-select: none;
  }
</style>
