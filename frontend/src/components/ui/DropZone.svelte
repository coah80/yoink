<script>
  let { accept = '', hint = 'Drop a file here', onfile } = $props();

  let dragging = $state(false);
  let inputEl;

  function handleDragOver(e) {
    e.preventDefault();
    dragging = true;
  }

  function handleDragLeave(e) {
    e.preventDefault();
    dragging = false;
  }

  function handleDrop(e) {
    e.preventDefault();
    dragging = false;
    const file = e.dataTransfer?.files?.[0];
    if (file) {
      onfile(file);
    }
  }

  function handleClick() {
    inputEl?.click();
  }

  function handleInputChange(e) {
    const file = e.target.files?.[0];
    if (file) {
      onfile(file);
    }
    e.target.value = '';
  }
</script>

<button
  type="button"
  class="dropzone"
  class:dragging
  ondragover={handleDragOver}
  ondragleave={handleDragLeave}
  ondrop={handleDrop}
  onclick={handleClick}
  aria-label={hint}
>
  <span class="dropzone-hint">{hint}</span>
  <span class="dropzone-sub">or click to browse</span>
  <input
    bind:this={inputEl}
    type="file"
    accept={accept}
    onchange={handleInputChange}
    class="dropzone-input"
    tabindex="-1"
  />
</button>

<style>
  .dropzone {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    min-height: 140px;
    border: 2px dashed var(--border);
    border-radius: var(--radius-md);
    background: transparent;
    cursor: pointer;
    padding: 24px;
    transition: border-color 0.2s ease, background 0.2s ease;
    outline: none;
    font-family: var(--font-body);
  }

  .dropzone:hover,
  .dropzone.dragging {
    border-color: var(--purple-500);
    background: rgba(139, 92, 246, 0.05);
  }

  .dropzone-hint {
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .dropzone-sub {
    color: var(--text-muted);
    font-size: 0.8rem;
  }

  .dropzone-input {
    display: none;
  }
</style>
