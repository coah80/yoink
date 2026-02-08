<footer>
  <div class="footer-left">
    <div>this is a <a href="https://github.com/yt-dlp/yt-dlp" target="_blank" rel="noopener">yt-dlp</a> wrapper</div>
    <div>inspired by <a href="https://cobalt.tools" target="_blank" rel="noopener">cobalt</a>, love u wukko</div>
    <div>youtube powered by <a href="https://patriick.dev/" target="_blank" rel="noopener">patrick</a> at <a href="https://cobalt.meowing.de" target="_blank" rel="noopener">cobalt.meowing.de</a></div>
    <div class="legal">by using this site you agree to our <a href="#/privacy">privacy policy</a></div>
  </div>
  <div class="footer-right">
    <a href="https://github.com/coah80/yoink" target="_blank" rel="noopener">github</a>
    <span>·</span>
    <a href="https://coah80.com" target="_blank" rel="noopener">coah80.com</a>
    <span>·</span>
    <a href="#/privacy">privacy</a>
  </div>
</footer>

<style>
  footer {
    padding: 20px 24px;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    color: var(--text-muted);
    font-size: 0.8rem;
    line-height: 1.6;
  }

  .footer-left {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .footer-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .legal {
    margin-top: 8px;
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  footer a {
    color: var(--purple-400);
    text-decoration: none;
    transition: color 0.15s ease-out;
  }

  footer a:hover {
    color: var(--purple-300);
  }

  @media (max-width: 600px) {
    footer {
      flex-direction: column;
      align-items: center;
      text-align: center;
      gap: 8px;
      padding: 16px 16px 24px;
      font-size: 0.75rem;
    }

    .footer-left {
      align-items: center;
    }

    .footer-right {
      gap: 6px;
    }
  }
</style>
