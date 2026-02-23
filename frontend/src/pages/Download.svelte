<script>
  import { onMount } from 'svelte';
  import { query } from '../lib/router.js';
  import { apiBase } from '../lib/api.js';
  import Spinner from '../components/ui/Spinner.svelte';

  let stage = $state('loading');
  let statusText = $state('preparing download...');

  let token = $derived($query.token || '');

  onMount(() => {
    const pathToken = window.location.pathname.replace('/download', '').replace(/^\//, '');

    const finalToken = token || pathToken;

    if (!finalToken) {
      stage = 'error';
      statusText = 'no download token provided';
      return;
    }

    const downloadUrl = `${apiBase()}/api/bot/download/${finalToken}`;
    statusText = 'starting download...';

    fetch(downloadUrl, { method: 'HEAD' })
      .then((response) => {
        if (response.ok) {
          statusText = 'download started';
          window.location.href = downloadUrl;

          setTimeout(() => {
            stage = 'success';
            statusText = 'you can close this page now';
          }, 2000);
        } else {
          throw new Error('Download not available');
        }
      })
      .catch(() => {
        stage = 'error';
        statusText = 'the download link has expired (5 minute limit)';
      });
  });
</script>

<main>
  <div class="download-container">
    {#if stage === 'loading'}
      <div class="spinner-wrap"><Spinner /></div>
      <h1>downloading...</h1>
      <p>your download should start automatically</p>
    {:else if stage === 'success'}
      <h1 class="success">done</h1>
      <p>download started successfully</p>
    {:else}
      <h1 class="error">download failed</h1>
      <p>this file is no longer available</p>
    {/if}
    <div class="status" class:error={stage === 'error'} class:success={stage === 'success'}>
      {statusText}
    </div>
  </div>
</main>

<style>
  main {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 40px 24px;
  }

  .download-container {
    max-width: 500px;
    width: 100%;
  }

  .spinner-wrap {
    display: flex;
    justify-content: center;
    margin-bottom: 24px;
  }

  .spinner-wrap :global(.spinner) {
    width: 60px;
    height: 60px;
    border-width: 4px;
  }

  h1 {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 2rem;
    margin-bottom: 12px;
    letter-spacing: -0.03em;
  }

  h1.success {
    color: var(--success);
  }

  h1.error {
    color: var(--error);
  }

  p {
    color: var(--text-secondary);
    font-size: 1rem;
    margin-bottom: 8px;
  }

  .status {
    margin-top: 24px;
    padding: 16px;
    background: var(--surface);
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    font-size: 0.9rem;
    color: var(--text-secondary);
  }

  .status.error {
    color: var(--error);
  }

  .status.success {
    color: var(--success);
  }
</style>
