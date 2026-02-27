<script>
  import { formatBytes } from '../../lib/utils.js';

  let { name, size, onremove } = $props();

  let formattedSize = $derived(formatBytes(size));
</script>

<div class="file-info">
  <span class="file-icon" aria-hidden="true">&#128196;</span>
  <div class="file-details">
    <span class="file-name" title={name}>{name}</span>
    <span class="file-size">{formattedSize}</span>
  </div>
  <button type="button" class="file-remove" onclick={onremove} aria-label="Remove file">
    &times;
  </button>
</div>

<style>
  .file-info {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px 16px;
  }

  .file-icon {
    font-size: 1.4rem;
    flex-shrink: 0;
  }

  .file-details {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }

  .file-name {
    color: var(--text);
    font-size: 0.85rem;
    font-family: var(--font-body);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .file-size {
    color: var(--text-muted);
    font-size: 0.75rem;
    font-family: var(--font-body);
  }

  .file-remove {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 1.3rem;
    cursor: pointer;
    padding: 0 4px;
    line-height: 1;
    flex-shrink: 0;
    transition: color 0.15s ease;
  }

  .file-remove:hover {
    color: var(--error);
  }
</style>
