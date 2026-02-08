<script>
  let { percent = 0, label = '' } = $props();

  let clampedPercent = $derived(Math.min(100, Math.max(0, percent)));
</script>

<div class="progress-wrapper">
  {#if label}
    <span class="progress-label">{label}</span>
  {/if}
  <div class="progress-track" role="progressbar" aria-valuenow={clampedPercent} aria-valuemin={0} aria-valuemax={100}>
    <div class="progress-fill" style="width: {clampedPercent}%"></div>
  </div>
</div>

<style>
  .progress-wrapper {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
  }

  .progress-label {
    font-size: 0.8rem;
    color: var(--text-secondary);
    font-family: var(--font-body);
  }

  .progress-track {
    width: 100%;
    height: 6px;
    background: var(--border);
    border-radius: var(--radius-full);
    overflow: hidden;
  }

  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--purple-600), var(--purple-400));
    border-radius: var(--radius-full);
    transition: width 0.3s ease;
  }
</style>
