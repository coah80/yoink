<script>
  import { onMount } from 'svelte';
  import { navigate } from '../lib/router.js';

  onMount(() => {
    const params = new URLSearchParams(window.location.search);
    const sharedUrl = params.get('url') || params.get('text') || '';

    const urlMatch = sharedUrl.match(/https?:\/\/[^\s]+/);
    let extractedUrl = urlMatch ? urlMatch[0] : '';
    extractedUrl = extractedUrl.replace(/[.,!?;:)\]}>]+$/, '');

    if (extractedUrl) {
      try {
        const validated = new URL(extractedUrl);
        if (validated.protocol === 'http:' || validated.protocol === 'https:') {
          localStorage.setItem('yoink_shared_url', JSON.stringify({
            url: extractedUrl,
            timestamp: Date.now()
          }));
        }
      } catch {}
    }

    navigate('/');
  });
</script>

<main>
  <p>Redirecting...</p>
</main>

<style>
  main {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-secondary);
  }
</style>
