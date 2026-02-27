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
- **clips** download specific youtube clips with timestamps
- **gifs** auto-detect and download as gif from twitter/x
- **pwa** install as a mobile app, share links directly from your phone
- **discord bot** — `/yoink`, `/convert`, `/compress` commands

## tech stack

- **backend** — go with chi router, single binary
- **frontend** — svelte 5 SPA with vite
- **discord bot** — go with discordgo, separate binary
- **tools** — yt-dlp, ffmpeg, gallery-dl, cobalt api

## self-hosting

```bash
git clone https://github.com/coah80/yoink.git
cd yoink
make build      # builds the server
make bot        # builds the discord bot
./yoink         # serves API + frontend on :3001
```

copy `.env.example` and configure your environment variables.

## credits

**powered by:**
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) — media downloading
- [gallery-dl](https://github.com/mikf/gallery-dl) — image downloading
- [ffmpeg](https://ffmpeg.org) — video processing
- [cobalt](https://github.com/imputnet/cobalt) — youtube fast path

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
