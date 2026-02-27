<script>
  import { onMount } from 'svelte';
  import { navigate } from '../../lib/router.js';
  import { CURRENT_VERSION, changelog } from '../../lib/changelog.js';

  let show = $state(false);
  let latest = changelog[0];

  onMount(() => {
    const dismissed = localStorage.getItem('yoink-update-dismissed');
    if (dismissed !== CURRENT_VERSION) {
      show = true;
    }
  });

  function dismiss() {
    localStorage.setItem('yoink-update-dismissed', CURRENT_VERSION);
    show = false;
  }

  function goToPost() {
    localStorage.setItem('yoink-update-dismissed', CURRENT_VERSION);
    show = false;
    navigate('/?updates');
  }
</script>

{#if show}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="banner-overlay"
    role="dialog"
    aria-modal="true"
    aria-label="What's new"
    onclick={(e) => { if (e.target === e.currentTarget) dismiss(); }}
    onkeydown={(e) => { if (e.key === 'Escape') dismiss(); }}
  >
    <div class="banner-card">
      <div class="banner-meta">
        <span class="banner-version">{latest.version}</span>
        <span class="banner-date">{latest.date}</span>
      </div>
      <h2 class="banner-title">{latest.title}</h2>
      <img class="banner-image" src={latest.image} alt={latest.title} />
      <p class="banner-summary">{latest.summary}</p>
      <div class="banner-actions">
        <button class="banner-btn primary" onclick={goToPost}>go to post</button>
        <button class="banner-btn" onclick={dismiss}>dismiss</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .banner-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.7);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 3000;
    padding: 20px;
    animation: fade-in 0.25s ease;
  }

  .banner-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 28px;
    max-width: 480px;
    width: 100%;
    max-height: 85vh;
    overflow-y: auto;
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.5);
    animation: slide-up 0.3s ease;
  }

  .banner-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 8px;
  }

  .banner-version {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 0.85rem;
    color: var(--text);
    background: var(--surface-elevated);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 3px 8px;
  }

  .banner-date {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .banner-title {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 1.3rem;
    color: var(--text);
    margin: 0 0 16px 0;
    line-height: 1.3;
  }

  .banner-image {
    width: 100%;
    border-radius: var(--radius-md);
    margin-bottom: 16px;
    border: 1px solid var(--border);
  }

  .banner-summary {
    font-size: 0.9rem;
    color: var(--text-secondary);
    line-height: 1.6;
    margin: 0 0 20px 0;
  }

  .banner-actions {
    display: flex;
    gap: 8px;
  }

  .banner-btn {
    flex: 1;
    padding: 12px 20px;
    font-family: var(--font-body);
    font-size: 0.9rem;
    font-weight: 600;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all 0.15s ease-out;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
  }

  .banner-btn:hover {
    background: var(--surface-elevated);
    color: var(--text);
  }

  .banner-btn.primary {
    background: linear-gradient(135deg, var(--purple-500), var(--purple-600));
    border: none;
    color: white;
  }

  .banner-btn.primary:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 16px rgba(139, 92, 246, 0.4);
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes slide-up {
    from { opacity: 0; transform: translateY(20px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-width: 600px) {
    .banner-card {
      padding: 20px;
    }

    .banner-title {
      font-size: 1.15rem;
    }

    .banner-actions {
      flex-direction: column;
    }
  }
</style>
