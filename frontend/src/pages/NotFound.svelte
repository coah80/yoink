<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
</script>

<HeaderSimple />

<main>
  <div class="not-found">
    <h1>404</h1>
    <p>this page doesn't exist</p>
    <a href="#/" class="back-link">go back home</a>
  </div>
</main>

<style>
  main {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px 24px;
  }

  .not-found {
    text-align: center;
  }

  h1 {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 6rem;
    letter-spacing: -0.03em;
    color: var(--purple-400);
    margin-bottom: 8px;
  }

  p {
    color: var(--text-secondary);
    font-size: 1.2rem;
    margin-bottom: 24px;
  }

  .back-link {
    display: inline-block;
    padding: 12px 24px;
    font-family: var(--font-heading);
    font-weight: 700;
    color: white;
    background: linear-gradient(135deg, var(--purple-500), var(--purple-600));
    border-radius: var(--radius-md);
    transition: all 0.15s ease-out;
  }

  .back-link:hover {
    transform: translateY(-2px);
    box-shadow: 0 8px 24px rgba(139, 92, 246, 0.4);
  }
</style>
