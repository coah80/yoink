<script>
  import { path } from '../../lib/router.js';

  let { href, children } = $props();

  let isActive = $derived($path === href);
</script>

<a
  class="nav-link"
  class:active={isActive}
  href="#{href}"
>
  {@render children()}
</a>

<style>
  .nav-link {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    text-decoration: none;
    font-size: 0.9rem;
    font-weight: 500;
    padding: 10px 16px;
    border-radius: var(--radius-full);
    background: var(--surface);
    border: 1px solid var(--border);
    transition: all 0.15s ease-out;
  }

  .nav-link:hover {
    color: var(--text);
    border-color: var(--purple-500);
  }

  .nav-link.active {
    color: var(--purple-400);
    border-color: var(--purple-500);
    background: var(--purple-900);
  }

  .nav-link :global(svg) {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }

</style>
