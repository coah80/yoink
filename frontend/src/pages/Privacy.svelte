<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
</script>

<HeaderSimple />

<main>
  <div class="page-header">
    <h1>privacy policy</h1>
    <p>the human-readable version</p>
  </div>

  <div class="tldr-box">
    <h3>tl;dr</h3>
    <p>i collect minimal anonymous stats, never your URLs or personal info. you can opt out anytime in <a href="#/settings">settings</a>.</p>
  </div>

  <div class="content-card">
    <h2>about</h2>
    <p>hey, im just one person who hasn't even graduated high school yet. i made yoink.tools in my free time because i felt inspired by cool websites like <a href="https://cobalt.tools" target="_blank" rel="noopener">cobalt.tools</a> and <a href="https://vert.sh" target="_blank" rel="noopener">vert.sh</a>, and i wanted an all-in-one website combining all the awesome frickin aspects of them. im not a company, i dont have investors, and i dont want anything from you.</p>
    <p>this privacy policy is written in plain english because legal jargon is annoying.</p>
  </div>

  <div class="content-card">
    <h2>what i collect</h2>
    <p>when you have analytics enabled (on by default), i collect:</p>
    <ul>
      <li><strong>format preferences</strong> - which file formats are popular (mp4, mp3, etc.)</li>
      <li><strong>download counts</strong> - how many downloads, conversions, and compressions happen</li>
      <li><strong>country data</strong> - derived from your IP address, but i dont store the IP itself</li>
      <li><strong>daily visitor counts</strong> - how many unique visitors per day</li>
    </ul>
    <p>that's it.</p>
  </div>

  <div class="content-card">
    <h2>what i DON'T collect</h2>
    <p>i never, ever collect:</p>
    <ul>
      <li><strong>URLs you download</strong> - your browsing is your business</li>
      <li><strong>video titles or content</strong> - nope</li>
      <li><strong>your IP address</strong> - i use it to get country, then forget it</li>
      <li><strong>cookies for tracking</strong> - only settings are stored locally</li>
      <li><strong>anything identifying you personally</strong> - i literally cant tell users apart</li>
    </ul>
  </div>

  <div class="content-card">
    <h2>why collect anything?</h2>
    <p>its useful to see what features people use so i can prioritize what to work on next. no monetization, no selling data, no ads.</p>
  </div>

  <div class="content-card">
    <h2>opting out</h2>
    <p>dont want to be part of the stats? head to <a href="#/settings">settings</a>, click the privacy tab, and flip the toggle.</p>
  </div>

  <div class="content-card">
    <h2>where data is stored</h2>
    <p>analytics data is stored on my server in a JSON file. your settings (quality preferences, etc.) are stored in your browser's localStorage and never leave your device.</p>
  </div>

  <div class="content-card">
    <h2>questions?</h2>
    <p>if you have questions or concerns, hit me up at <a href="https://coah80.com" target="_blank" rel="noopener">coah80.com</a> or check out the project on <a href="https://github.com/coah80/yoink" target="_blank" rel="noopener">github</a>.</p>
  </div>

  <div class="content-card">
    <h2>updates</h2>
    <p>if i change this policy, ill update it here. no sneaky changes. last updated: <strong>January 2026</strong>.</p>
  </div>
</main>

<FooterSimple />

<style>
  main {
    max-width: 700px;
    width: 100%;
    margin: 0 auto;
    padding: 32px 24px;
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: flex-start;
  }

  .page-header {
    text-align: center;
    margin-bottom: 32px;
  }

  .page-header h1 {
    font-family: var(--font-heading);
    font-weight: 800;
    font-size: 2.5rem;
    letter-spacing: -0.03em;
    margin-bottom: 8px;
  }

  .page-header p {
    color: var(--text-secondary);
    font-size: 1rem;
  }

  .tldr-box {
    background: rgba(139, 92, 246, 0.1);
    border: 1px solid var(--purple-500);
    border-radius: var(--radius-md);
    padding: 20px;
    margin-bottom: 24px;
  }

  .tldr-box h3 {
    font-family: var(--font-heading);
    font-size: 1rem;
    font-weight: 700;
    color: var(--purple-400);
    margin-bottom: 12px;
  }

  .tldr-box p {
    color: var(--text);
    line-height: 1.6;
    margin: 0;
  }

  .tldr-box a {
    color: var(--purple-400);
  }

  .content-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 32px;
    margin-bottom: 24px;
  }

  .content-card h2 {
    font-family: var(--font-heading);
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--purple-400);
    margin-bottom: 16px;
  }

  .content-card p {
    color: var(--text-secondary);
    line-height: 1.7;
    margin-bottom: 16px;
  }

  .content-card p:last-child {
    margin-bottom: 0;
  }

  .content-card ul {
    color: var(--text-secondary);
    line-height: 1.7;
    margin-left: 20px;
    margin-bottom: 16px;
  }

  .content-card li {
    margin-bottom: 8px;
  }

  .content-card strong {
    color: var(--text);
  }

  .content-card a {
    color: var(--purple-400);
    text-decoration: none;
  }

  .content-card a:hover {
    color: var(--purple-300);
  }

  @media (max-width: 600px) {
    main {
      padding: 20px 12px;
    }

    .page-header h1 {
      font-size: 1.8rem;
    }

    .content-card {
      padding: 20px 16px;
    }

    .content-card h2 {
      font-size: 1.1rem;
    }

    .content-card ul {
      margin-left: 16px;
    }

    .tldr-box {
      padding: 16px;
    }

    .content-card a {
      word-break: break-word;
    }
  }
</style>
