# Troubleshooting

This page covers common problems when running yoink yourself. If yours is not here, open an issue on [GitHub](https://github.com/coah80/yoink/issues) and include your OS, the output of `yt-dlp --version`, the exact error from the terminal running yoink, and what you already tried.

The website shows short error messages. The terminal running yoink shows the full one, so check there first.

## Building

### `npm: command not found` or `go: command not found`

**Cause:** Node.js or Go is not installed, or not on your `PATH`.

**Fix:** install it as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then open a new terminal and run `make build` again.

### `go: downloading go1.24.0` then a network error

**Cause:** your Go is older than 1.24 and could not download the newer toolchain yoink needs.

**Fix:** install Go 1.24 or newer from [go.dev](https://go.dev/doc/install), or retry with a working internet connection.

### `npm ci` fails with `EBADENGINE` or a syntax error

**Cause:** your Node.js is too old.

**Fix:** run `node --version`. If it is older than `v20`, install a newer version from [nodejs.org](https://nodejs.org/en/download).

## Starting

### `listen tcp :3001: bind: address already in use`

**Cause:** another program, or another copy of yoink, is already using port 3001.

**Fix:** set `PORT=3002` (or any free port) in `.env` and run `./yoink` again. To see what holds the port, run `ss -ltnp | grep 3001` (macOS: `lsof -i :3001`).

### `✗ yt-dlp not found (REQUIRED)`

**Cause:** yoink cannot find `yt-dlp`, `ffmpeg` or `ffprobe` on your `PATH`. The line names which one.

**Fix:** install it as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then run `./yoink` again from a new terminal.

### The website shows `404 page not found`

**Cause:** yoink cannot find the built website. It looks for `public/` next to the binary, then `frontend/public/` in the current folder.

**Fix:** run `make build`, then start yoink from the `yoink` folder with `./yoink`.

## Downloading

### `YouTube is blocking this request, try again later`

**Cause:** YouTube answered `Sign in to confirm you're not a bot`, which it does for many server and VPN IP addresses.

**Fix**, easiest first:
1. Update yt-dlp: `sudo yt-dlp -U` (Homebrew: `brew upgrade yt-dlp`)
1. Follow [Routing YouTube through Mullvad](mullvad.md), or set another proxy
1. Add [YouTube cookies or a PO token](youtube-auth.md)

### YouTube downloads fail or only come in low quality

**Cause:** Deno is missing, so yt-dlp cannot unlock YouTube's formats.

**Fix:** run `deno --version`. If it is not found, install it as described in [Installing yoink, Section I](install.md#section-i---installing-the-tools), then restart yoink from a new terminal.

### `Rate limited, please wait and try again`

**Cause:** the site (usually YouTube) answered `HTTP Error 429: Too Many Requests` for your IP address.

**Fix:** wait 15–60 minutes, or route YouTube through [Mullvad](mullvad.md) so that requests spread across several IP addresses.

### Every YouTube download fails with `Connection refused` or `timed out` after setting a proxy

**Cause:** yoink cannot reach the proxy. With Mullvad, this means the app is disconnected.

**Fix:** reconnect Mullvad, then check the proxy with `curl --socks5-hostname <proxy-host>:1080 https://am.i.mullvad.net/connected`. If you do not want a proxy, empty `PROXY_HOST` in `.env` and restart yoink.

### Downloads finish, but the file disappears from the queue later

**Cause:** yoink deletes temporary files 20 minutes after they were last used.

**Fix:** download the file from the queue soon after it finishes.

### `gallery-dl not installed on server`

**Cause:** `gallery-dl` is not installed. The startup output shows `- gallery-dl not found (optional)`.

**Fix:** install `gallery-dl` (`sudo apt install gallery-dl`, `brew install gallery-dl` or `pipx install gallery-dl`), then restart yoink.

## Transcription

### `Large model requires API configuration`

**Cause:** the `large` model runs through the OpenAI API, and `OPENAI_API_KEY` is not set.

**Fix:** pick `tiny`, `base`, `small` or `medium`, or set `OPENAI_API_KEY` as described in [Transcription](transcription.md).

### Transcription fails right away, and startup says `- whisper module not found (optional)`

**Cause:** the `python3` yoink found does not have Whisper installed.

**Fix:** start yoink from a terminal where the virtual environment is active, as described in [Transcription, Section II](transcription.md#section-ii---starting-yoink-with-whisper).

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
