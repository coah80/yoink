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
    <p>yoink has no accounts, advertising trackers, or analytics scripts. files are processed on the server and cleaned up automatically. settings and your download queue stay in your browser. server logs and optional integrations are described below.</p>
  </div>

  <div class="content-card">
    <h2>who runs this</h2>
    <p>just me, one person. i made yoink because i wanted a simple tool that does what it says without being weird about your data. i was inspired by <a href="https://cobalt.tools" target="_blank" rel="noopener">cobalt.tools</a> and <a href="https://vert.sh" target="_blank" rel="noopener">vert.sh</a>, and wanted to combine the best parts into one site.</p>
    <p>im not a company, i have no investors, and i have no reason to collect your data.</p>
  </div>

  <div class="content-card">
    <h2>your files</h2>
    <p>files are stored temporarily on the server while downloading, converting, compressing, trimming, or transcribing. storage encryption depends on how the server is deployed, it is not configured by the app itself.</p>
    <p>ordinary download files are scheduled for deletion after they are sent. a cleanup job runs every 5 minutes and removes unused temporary files older than 20 minutes. files used by active jobs are kept. ordinary bot links expire after 5 minutes and playlist links after 12 hours. files for unexpired links are kept until they expire or are downloaded.</p>
    <p>the server writes request and diagnostic logs. these can include IP addresses, requested URLs, filenames, and errors. the server operator controls where logs are stored and how long they are kept.</p>
  </div>

  <div class="content-card">
    <h2>what we collect</h2>
    <p>the app needs some data to process requests and prevent abuse:</p>
    <ul>
      <li><strong>no analytics scripts</strong> - no advertising or analytics scripts are included</li>
      <li><strong>browser storage</strong> - settings and the download queue use local storage, and the temporary client ID uses session storage. the app does not set browser cookies</li>
      <li><strong>rate limits</strong> - IP addresses are held in memory to limit requests, and may also appear in server logs</li>
      <li><strong>processing data</strong> - URLs, job details, and temporary files are needed to complete your request</li>
      <li><strong>no accounts</strong> - there's nothing to sign up for, no emails collected</li>
      <li><strong>external services</strong> - the app loads fonts from Google Fonts and previews from media sites. downloads contact the source platform and may use configured extractors or proxies. optional Discord alerts can include job details and errors</li>
    </ul>
  </div>

  <div class="content-card">
    <h2>connections and transcription</h2>
    <p>external services may receive data when you use these features:</p>
    <ul>
      <li><strong>connections</strong>: HTTPS protects data between your browser and an HTTPS deployment of yoink. the server receives and processes the media. self-hosted deployments need to configure HTTPS separately.</li>
      <li><strong>transcription</strong>: local Whisper models process audio on the server. the large model uses the OpenAI API when configured, which sends audio to OpenAI for transcription.</li>
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
    <p>if this policy changes, it gets updated here. no sneaky edits. last updated: <strong>september 29, 2026</strong>.</p>
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
