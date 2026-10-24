# Configuration reference

This page lists every setting yoink reads from `.env`, so that you can look up what one does and what happens when you leave it empty.

yoink reads `.env` from the folder you start it in. Restart yoink after any change. An empty value means the default is used.

## Server

| Setting | Default | What it does |
|---|---|---|
| `PORT` | `3001` | Port the website and API listen on |
| `YOINK_TEMP_DIR` | `/var/tmp/yoink` (`.env.example` sets `./tmp`) | Where downloads and conversions are stored while they run. **Its subfolders are emptied on every start**, so use a folder only yoink touches. |
| `NODE_ENV` | `development` | Read but currently has no effect |
| `BOT_SECRET` | empty | Shared password for the Discord bot. Empty means the bot endpoints refuse every request. See [Discord bot](discord-bot.md). |
| `YOINK_ADMIN_TOKEN` | empty | Token for `POST /api/admin/drain` and `/api/admin/resume`, sent as `Authorization: Bearer <token>`. `scripts/deploy-yoink` uses them to stop new jobs before swapping the binary. Empty turns both endpoints off. |
| `REQUIRE_TEMP_MOUNT` | `false` | When `true`, yoink refuses to start unless `YOINK_TEMP_DIR` is its own mount point, for example an encrypted volume. This keeps files off the main disk if the mount failed at boot. |

Files next to `.env`:

| File | What it does |
|---|---|
| `cors-origins.txt` | One allowed website origin per line (for example `https://yoink.example.com`). Without it, any website may call the API, with credentials disabled. Only needed when the website and API are on different addresses. |

## Proxy

yoink sends YouTube requests through the proxy. Other sites use your normal connection.

| Setting | Default | What it does |
|---|---|---|
| `PROXY_HOST` | empty (no proxy) | Proxy host, or several separated by commas. yoink picks one at random per request. A value containing `://` is used as a full proxy URL as-is. |
| `PROXY_PORT` | `1080`, or `80` when `PROXY_PASSWORD` is set | Proxy port |
| `PROXY_SCHEME` | `socks5h`, or `http` when `PROXY_PASSWORD` is set | Proxy protocol |
| `PROXY_USER_PREFIX` | empty | Proxy username. Only used together with `PROXY_PASSWORD`. |
| `PROXY_PASSWORD` | empty | Proxy password |
| `PROXY_COUNT` | `0` | For providers with numbered users: above `0`, yoink logs in as `<prefix>-1` to `<prefix>-<count>`, picked at random |

Common setups:

| Provider | Settings |
|---|---|
| Mullvad relays | `PROXY_HOST=se-mma-wg-socks5-001.relays.mullvad.net,…` and `PROXY_PORT=1080`. See [Routing YouTube through Mullvad](mullvad.md). |
| Rotating HTTP proxy with one login | `PROXY_HOST=<host>`, `PROXY_USER_PREFIX=<user>`, `PROXY_PASSWORD=<password>`, plus `PROXY_PORT` if not 80 |
| Provider with numbered users (`user-1`, `user-2`, …) | As above, plus `PROXY_COUNT=<number-of-users>` |
| Full URL | `PROXY_HOST=socks5h://<user>:<password>@<host>:<port>` |

## YouTube authentication

See [YouTube cookies and PO tokens](youtube-auth.md) for setup.

| Setting | Default | What it does |
|---|---|---|
| `YOUTUBE_COOKIES_FILE` | `./youtube-cookies.txt` | Cookie file used for YouTube. Checked first. |
| `COOKIES_FILE` | `./cookies.txt` | Fallback cookie file when the YouTube one does not exist |
| `COOKIE_REFRESH_SCRIPT` | `./refresh-cookies.sh` | Script yoink runs to refresh cookies when YouTube flags a request. Without it, cookies are paused for 30 minutes instead. |
| `YOUTUBE_POT_PROVIDER_URL` | empty | Address of a bgutil PO token provider, for example `http://localhost:4416` |
| `YOUTUBE_PLAYER_CLIENT` | `mweb` | YouTube player client yt-dlp uses together with the PO token provider |
| `SESSION_GENERATOR_URL` | empty | Address of a yt-session-generator service. When set, yoink fetches session tokens from it in the background. |
| `SESSION_TOKEN_REFRESH_MIN` | `15` | Minutes between session token refreshes |

## Other services

| Setting | Default | What it does |
|---|---|---|
| `YTDLP_BIN` | `yt-dlp` | yt-dlp program to run. Set it to `scripts/mullvad-vpn/yt-dlp-vpn` for the [Mullvad Docker wrapper](mullvad-docker.md). |
| `WGET_BIN` | `wget` | wget program used for a few direct media downloads |
| `EXTRACTOR_URL` | `http://localhost:3099` | Address of the [TikTok and Instagram extractor](extractor.md) |
| `COBALT_APIS` | empty | Cobalt instances for Instagram, separated by commas. Empty skips Cobalt. Not used for YouTube. |
| `COBALT_API_KEY` | empty | API key sent to those Cobalt instances |
| `WHISPER_SCRIPT` | `scripts/transcribe.py` | Transcription helper script. See [Transcription](transcription.md). |
| `OPENAI_API_KEY` | empty | Enables the `large` transcription model through the OpenAI API |
| `STEMS_PYTHON` | `stems-venv/bin/python` next to yoink, else `python3` | Python that has Demucs installed. See [Vocal stems](vocal-stems.md). |
| `STEMS_SCRIPT` | `scripts/stems.py` | Stem splitter helper script |
| `STEMS_DEVICE` | `auto` | `auto`, `cuda` or `cpu` for stem splitting |
| `STEMS_THREADS` | `4` | CPU threads a split may use |
| `STEMS_MEMORY_MAX` | `5G` | Memory cap for a split's cgroup |
| `STEMS_CPU_QUOTA` | `400%` | CPU cap for a split's cgroup, 100% is one core |
| `STEMS_GPU_DUTY` | `30` | Percent of the time a split keeps the GPU busy. `100` is fastest but slows other GPU users. |
| `STEMS_MAX_MINUTES` | `60` | Longest audio that can be split |
| `STEMS_MAX_WAITING` | `1` | Stem downloads allowed to wait while one is splitting |
| `STEMS_TIMEOUT_MINUTES` | `20` | A split is stopped after this long |
| `STEMS_SANDBOX` | `true` | Set to `false` to skip the `systemd-run` cgroup |

## Discord

| Setting | Default | What it does |
|---|---|---|
| `DISCORD_TOKEN` | empty | Bot token, required by `yoink-bot` |
| `DISCORD_APP_ID` | empty | Application ID, required by `yoink-bot` |
| `YOINK_API_URL` | `http://localhost:3001` | Where `yoink-bot` reaches the yoink API |
| `YOINK_PUBLIC_URL` | same as `YOINK_API_URL` | Address used in download links the bot posts. Must be reachable by your Discord users. |
| `DISCORD_WEBHOOK_URL` | empty | Webhook for operator alerts (failed jobs, cookie and proxy problems). Alerts include the job ID, error and site name. |
| `DISCORD_PING_USER_ID` | empty | User ID to mention on important alerts |

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
