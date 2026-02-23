<script>
  import HeaderSimple from '../components/layout/HeaderSimple.svelte';
  import FooterSimple from '../components/layout/FooterSimple.svelte';
</script>

<HeaderSimple />

<main>
  <div class="page-header">
    <h1>privacy policy</h1>
    <p>no legal jargon, just the truth</p>
  </div>

  <div class="tldr-box">
    <h3>tl;dr</h3>
    <p>yoink doesn't collect anything. no tracking, no cookies, no analytics. your files are stored on an encrypted volume with a key that only exists in RAM, and get wiped automatically.</p>
  </div>

  <div class="content-card">
    <h2>who runs this</h2>
    <p>just me, one person. i made yoink because i wanted a simple tool that does what it says without being weird about your data. i was inspired by <a href="https://cobalt.tools" target="_blank" rel="noopener">cobalt.tools</a> and <a href="https://vert.sh" target="_blank" rel="noopener">vert.sh</a>, and wanted to combine the best parts into one site.</p>
    <p>im not a company, i have no investors, and i have no reason to collect your data.</p>
  </div>

  <div class="content-card">
    <h2>your files</h2>
    <p>files are stored on a LUKS2 encrypted volume on the server. the encryption key is randomly generated at every boot, stored only in RAM, and never written to disk — so if the server ever powers off, the key is gone and the data is unrecoverable by design.</p>
    <p>once your download or conversion is done, the file gets deleted immediately from that encrypted volume. there's also a cleanup job that runs every 5 minutes and wipes anything older than 20 minutes no matter what, just in case something slips through.</p>
    <p>we don't log what you download, what you convert, what you compress, or any filenames or URLs. none of that gets saved anywhere.</p>
  </div>

  <div class="content-card">
    <h2>what we collect</h2>
    <p>nothing. literally zero.</p>
    <ul>
      <li><strong>no analytics</strong> - no tracking scripts, no usage stats, nothing</li>
      <li><strong>no cookies</strong> - we don't set a single cookie</li>
      <li><strong>no IP logging</strong> - your IP isn't stored or logged</li>
      <li><strong>no content logging</strong> - URLs, filenames, and what you do here are not recorded</li>
      <li><strong>no accounts</strong> - there's nothing to sign up for, no emails collected</li>
      <li><strong>no third party stuff</strong> - no google analytics, no facebook pixel, no ad networks</li>
    </ul>
  </div>

  <div class="content-card">
    <h2>encryption</h2>
    <p>two layers:</p>
    <ul>
      <li><strong>in transit</strong> — everything goes through HTTPS, so your connection to yoink is encrypted end to end. nobody between you and the server can see what you're doing.</li>
      <li><strong>at rest</strong> — all temporary files are stored on a LUKS2 encrypted volume (AES-256). the decryption key is generated fresh at every server boot using /dev/urandom and kept only in RAM. there's no way to recover files from the disk after a reboot, and no way to read them without the key that lives purely in memory.</li>
    </ul>
  </div>

  <div class="content-card">
    <h2>open source</h2>
    <p>the whole thing is on <a href="https://github.com/coah80/yoink" target="_blank" rel="noopener">github</a>. you can read every line of code yourself if you want to verify any of this. if you find something off, please tell me.</p>
  </div>

  <div class="content-card">
    <h2>contact</h2>
    <p>questions, concerns, or just want to say hi, reach me at <a href="https://coah80.com" target="_blank" rel="noopener">coah80.com</a> or open an issue on <a href="https://github.com/coah80/yoink" target="_blank" rel="noopener">github</a>.</p>
  </div>

  <div class="content-card">
    <h2>changes</h2>
    <p>if this policy changes, it gets updated here. no sneaky edits. last updated: <strong>february 23, 2026</strong>.</p>
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
