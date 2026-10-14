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

yoink is an all-in-one media tool. download videos from 1000+ sites, convert between formats, and compress for discord. no ads, no trackers, free and fast, forever.

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

you can run your own yoink in about 20 minutes. the [self-hosting guide](docs/self-hosting/README.md) walks you through it step by step:

- [installing yoink](docs/self-hosting/install.md) on linux, macOS or windows (WSL)
- [routing youtube through mullvad](docs/self-hosting/mullvad.md) when youtube blocks your server
- [youtube cookies and PO tokens](docs/self-hosting/youtube-auth.md), [transcription](docs/self-hosting/transcription.md), the [tiktok/instagram extractor](docs/self-hosting/extractor.md) and the [discord bot](docs/self-hosting/discord-bot.md)
- [working on the code](docs/self-hosting/development.md), the [configuration reference](docs/self-hosting/configuration.md) and [troubleshooting](docs/self-hosting/troubleshooting.md)

the short version, if you already have Go 1.24+, Node.js 20+, yt-dlp, ffmpeg and Deno:

```bash
git clone https://github.com/coah80/yoink.git
cd yoink
cp .env.example .env
make build
./yoink    # open http://localhost:3001
```

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
