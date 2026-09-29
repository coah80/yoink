<div align="center">
    <br/>
    <p>
        <img src="frontend/public/icons/icon-512.png" title="yoink" alt="yoink logo" width="100" />
    </p>
    <p>
        just paste the link and <i>yoink</i> it
        <br/>
        <a href="https://yoink.tools">
            yoink.tools
        </a>
    </p>
    <p>
        <a href="https://github.com/coah80/yoink">
            github
        </a>
        &bull;
        <a href="https://status.yoink.tools">
            status
        </a>
        &bull;
        <a href="https://coah80.com">
            coah80.com
        </a>
    </p>
    <br/>
</div>

yoink is an all-in-one media tool. download videos from 1000+ sites, convert between formats, and compress for discord. no ads or sign-up required.

<img width="4064" height="2354" alt="image" src="https://github.com/user-attachments/assets/3dd1dadc-618c-498d-9d81-37058d305ffa" />

## features

- **download** videos and audio from 1000+ sites (youtube, tiktok, twitter, reddit, etc.)
- **playlists** download entire youtube playlists as a zip
- **images** download image galleries from supported sites (gallery-dl)
- **convert** between formats with different codecs
- **compress** videos to a target file size for discord
- **clips** download youtube clips with their timestamps
- **trim & crop** cut videos and change aspect ratios
- **transcribe** create text transcripts, SRT/ASS subtitles, or burned-in captions
- **gifs** auto-detect and download as gif from twitter/x
- **pwa** install as a mobile app, share links directly from your phone
- **discord bot** — `/yoink`, `/convert`, `/compress` commands

## tech stack

- **backend** — go with chi router, single binary
- **frontend** — svelte 5 SPA with vite
- **discord bot** — go with discordgo, separate binary
- **tools** — yt-dlp for youtube and other supported sites, ffmpeg for processing, gallery-dl for images
- **optional services** — browser extractor for tiktok/instagram, configurable cobalt instances for instagram, local Whisper or the OpenAI API for transcription

## quality and limits

video downloads default to 1080p. yt-dlp selects the best available resolution up to the chosen quality before applying codec preferences. select **4k** for a 2160p cap or **best** for no resolution cap. quality depends on the streams the source makes available, and a preferred codec is not guaranteed. WebM downloads select compatible WebM streams.

the upload and fetched-file limit is **8 GiB**, playlists support up to **1000 videos per run**, and each client can have **3 active jobs**. site support depends on upstream availability, authentication, and access restrictions.

settings and the download queue are saved in browser storage. temporary media is processed on the server and cleaned up automatically. request logs, optional services, HTTPS, and storage encryption depend on the deployment. see the website's privacy page for details.

## self-hosting

install Go 1.24 or newer, Node.js 20.19 or newer, npm, [yt-dlp](https://github.com/yt-dlp/yt-dlp#installation), ffmpeg, and ffprobe. youtube extraction also needs a supported JavaScript runtime such as Deno and yt-dlp's EJS components. keep yt-dlp current, youtube extraction changes frequently.

```bash
git clone https://github.com/coah80/yoink.git
cd yoink
cp .env.example .env  # configure the values you need
make build           # builds the frontend and server, copies assets to public/
./yoink              # serves API + frontend on :3001
```

`frontend/src` contains the Svelte source. `frontend/static` contains source assets, and `npm run build` in `frontend` produces `frontend/public`, which is checked in for existing deployments. source was restored from `claude/sync-frontend-to-github-kdTHt` (commit `57e4c5b`), the branch containing the source for the previously checked-in bundle.

for development, run `make run` from the repo root and `npm run dev` from `frontend` in another terminal. Vite proxies `/api` to `localhost:3001`, and production uses the same origin as the website. set `VITE_API_URL` when building for a separate API host. if you change `PORT`, also update the Vite proxy target. when distributing the binary, put the built `public/` folder beside it, and include `scripts/transcribe.py` if you want transcription.

optional features:

- **images**: install gallery-dl.
- **transcription**: the included `scripts/transcribe.py` helper uses `python3`, install `openai-whisper` for local models, or `openai` and set `OPENAI_API_KEY` for the large API model. the API model sends audio to OpenAI.
- **browser extractor**: run `npm ci`, `npm run install-browser`, and `npm start` in `extractor`; configure `EXTRACTOR_URL` if it is not on `localhost:3099`.
- **discord bot**: set `DISCORD_TOKEN`, `DISCORD_APP_ID`, `BOT_SECRET`, `YOINK_API_URL`, and `YOINK_PUBLIC_URL`, then run `make bot` and `./yoink-bot`. use the same `BOT_SECRET` for the bot and API.
- **youtube authentication**: cookie files, proxies, and a PO token provider may be needed for restricted videos or blocked server IPs. the PO token provider requires the matching yt-dlp provider plugin.

configure HTTPS and any encrypted temporary storage separately. `.env.example` lists optional services and paths. `YOINK_TEMP_DIR` defaults to `/var/tmp/yoink`; use a directory dedicated to yoink, its contents are cleared at server startup.

## checks

```bash
go test ./...
go vet ./...
cd frontend && npm ci && npm run build
```

the format-selection regression tests use installed yt-dlp with offline fixtures, they do not download media or contact youtube. they are skipped if yt-dlp is unavailable.

## credits

**powered by:**
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) — media downloading
- [gallery-dl](https://github.com/mikf/gallery-dl) — image downloading
- [ffmpeg](https://ffmpeg.org) — video processing
- [cobalt](https://github.com/imputnet/cobalt) — optional instagram download backend, not used for youtube

**inspired by:**
- [cobalt.tools](https://cobalt.tools)
- [vert.sh](https://vert.sh)
- [8mb.video](https://8mb.video)

## license

[MIT](LICENSE)

## star history

<a href="https://www.star-history.com/#coah80/yoink&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=coah80/yoink&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=coah80/yoink&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=coah80/yoink&type=date&legend=top-left" />
 </picture>
</a>
