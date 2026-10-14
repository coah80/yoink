# TikTok and Instagram extractor

This page runs yoink's browser extractor, a small helper that opens TikTok and Instagram pages in a headless Chromium browser, so that downloads keep working when those sites block plain requests.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

The extractor is a separate Node.js service in the `extractor` folder, listening on port 3099. yoink calls `EXTRACTOR_URL` + `/health` before each TikTok or Instagram download. When the extractor is not running, yoink falls back to its other methods, so this page is optional.

</details>

## What you need

- Node.js 20 or newer (already installed in [Installing yoink](install.md))
- About 650 MB of disk space for Chromium

## Instructions

### Section I - Installing the extractor

In this section, you will install the extractor and the Chromium browser it drives.

1. Open a new terminal in the `yoink` folder
1. Run `cd extractor`
1. Run `npm ci`
    + You should see `added … packages` with no `ERR!` lines.
1. Run `npm run install-browser`
    + On Linux this also installs Chromium's system libraries with `apt`, so it asks for your `sudo` password.
    + It downloads about 310 MB and ends with three `… downloaded to …` lines: Chrome for Testing, FFmpeg and Chrome Headless Shell.
    + On a distribution without `apt`, run `npx playwright install chromium` instead and install the libraries Playwright lists with your package manager.

### Section II - Running the extractor

In this section, you will start the extractor and confirm yoink can reach it.

1. Run `npm start`
    + You should see `extractor running on port 3099, chromium …` within a few seconds.
    + If you see `failed to start: browserType.launch: Executable doesn't exist at …`, Chromium is missing. Return to [Section I, step 4](#section-i---installing-the-extractor).
1. Leave this terminal open, because closing it stops the extractor
1. In another terminal, run `curl http://localhost:3099/health`
    + You should see `{"status":"ok","uptime":…,"openPages":0}`.
1. Download a TikTok link from `http://localhost:3001`
    + The yoink terminal no longer shows `Extractor failed, falling back`.

> [!NOTE]
> To use another port, start it with `PORT=3199 npm start` and set `EXTRACTOR_URL=http://localhost:3199` in yoink's `.env`, then restart yoink. Check a port is free first with `ss -ltn | grep 3199`: a second copy on a busy port does not always print an error.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
