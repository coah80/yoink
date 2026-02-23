<script>
  import { onMount } from 'svelte';
  import { navigate } from '../lib/router.js';

  onMount(() => {
    navigate('/?updates');
  });
</script>
