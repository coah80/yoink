# Troubleshooting

This page covers common problems when running yoink yourself. If yours is not here, open an issue on [GitHub](https://github.com/coah80/yoink/issues) and include your OS, the output of `yt-dlp --version`, the exact error from the terminal running yoink, and what you already tried.

The website shows short error messages. The terminal running yoink shows the full one, so check there first.

## Building

### `npm: command not found` or `go: command not found`

**Cause:** Node.js or Go is not installed, or not on your `PATH`.

**Fix:** install it as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then open a new terminal and run `make build` again. Go installed from go.dev only appears after `source ~/.profile` or logging out and back in.

### `go: downloading go1.24.0 (linux/amd64)` then `dial tcp: lookup proxy.golang.org`

**Cause:** your Go is older than 1.24 and tried to download the newer toolchain yoink needs, but has no internet access.

**Fix:** retry with a working connection, or install Go 1.24 or newer from [go.dev](https://go.dev/doc/install).

### `go: go.mod requires go >= 1.24.0 (running go 1.22…; GOTOOLCHAIN=local)`

**Cause:** your Go is older than 1.24 and is set to never download a newer one. The official `golang` Docker images set this.

**Fix:** run `GOTOOLCHAIN=auto make build`, or install Go 1.24 or newer.

### `TypeError: crypto$2.getRandomValues is not a function`

**Cause:** your Node.js is too old. `npm ci` only warns about it (`npm WARN EBADENGINE Unsupported engine`), then the website build fails.

**Fix:** run `node --version`. If it is older than `v18`, install Node.js 20 or newer as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then run `make build` again.

## Starting

### `server failed: listen tcp :3001: bind: address already in use`

**Cause:** another program, or another copy of yoink, already uses port 3001.

**Fix:**
1. Run `ss -ltnp | grep 3001` (macOS: `lsof -i :3001`) to see what holds the port
1. If it is another copy of yoink, stop that one instead of starting a second
1. Otherwise set `PORT=3002` (or any free port) in `.env` and run `./yoink` again

A second copy of yoink empties the temp folder before it finds the port taken. If both copies share a temp folder, downloads running in the first one fail.

### `✗ yt-dlp not found (REQUIRED)`

**Cause:** yoink cannot find `yt-dlp`, `ffmpeg` or `ffprobe` on your `PATH`. The line names which one.

**Fix:** install it as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then run `./yoink` again from a new terminal.

### The website shows `404 page not found`

**Cause:** yoink cannot find the built website. It looks for `public/` next to the `yoink` binary, then for `frontend/public/` in the folder you started it from.

**Fix:** run `make build` in the `yoink` folder, then start yoink from there with `./yoink`.

### Settings in `.env` are ignored

**Cause:** yoink reads `.env` from the folder you start it in, not from where the binary is. Started from another folder, it also uses `/var/tmp/yoink` as its temp folder.

**Fix:** stop yoink, run `cd` to the `yoink` folder, and start it with `./yoink`.

## Downloading

### `YouTube is blocking this request, try again later`

**Cause:** YouTube answered `Sign in to confirm you're not a bot`, which it does for many server and VPN IP addresses.

**Fix**, easiest first:
1. Update yt-dlp: `sudo yt-dlp -U` (Homebrew: `brew upgrade yt-dlp`)
1. Add a [PO token provider](youtube-auth.md)
1. Follow [Routing YouTube through Mullvad](mullvad.md)
1. Add [YouTube cookies](youtube-auth.md#section-ii---adding-youtube-cookies-optional)

### `This site is temporarily rate limiting downloads`

**Cause:** the site (usually YouTube) answered `HTTP Error 429: Too Many Requests` for your IP address.

**Fix:** wait 15–60 minutes, or route YouTube through [Mullvad](mullvad.md).

### `Connection dropped, try again` after setting a proxy

**Cause:** yoink cannot reach the proxy. The terminal shows `Connection refused` (nothing listening there) or `Name or service not known` (the hostname does not resolve). With Mullvad's relay proxies, this also happens when the Mullvad app is disconnected.

**Fix:** check the proxy with `curl --socks5-hostname <proxy-host>:1080 https://am.i.mullvad.net/connected`. If you do not want a proxy, empty `PROXY_HOST` in `.env` and restart yoink.

### `gallery-dl not installed on server`

**Cause:** `gallery-dl` is not installed. The startup output shows `- gallery-dl not found (optional)`.

**Fix:** install `gallery-dl` (`sudo apt install gallery-dl`, `brew install gallery-dl` or `pipx install gallery-dl`), then restart yoink.

## Transcription

### `faster-whisper not installed`

**Cause:** the `python3` yoink found does not have `faster-whisper`. The startup output shows `- faster-whisper not found (optional, needed for local transcription)`.

**Fix:** start yoink from a terminal where the virtual environment is active, as described in [Transcription, Section II](transcription.md#section-ii---starting-yoink-with-faster-whisper).

### `Large model requires API configuration. Use a local model (tiny/base/small/medium).`

**Cause:** the `large` model runs through the OpenAI API, and `OPENAI_API_KEY` is not set.

**Fix:** pick `tiny`, `base`, `small` or `medium`, or set `OPENAI_API_KEY` as described in [Transcription](transcription.md).

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
