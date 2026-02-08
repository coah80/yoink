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
- **admin panel** built-in dashboard for analytics, banners, and server status

## tech stack

- **frontend** — svelte 5 SPA with vite, deployed on cloudflare pages
- **api** — node.js + express with modular route/service architecture
- **tools** — yt-dlp, ffmpeg, gallery-dl, cobalt api

## project structure

```
yoink/
├── frontend/              svelte 5 frontend (SPA)
│   ├── src/
│   │   ├── pages/         Home, Download, Convert, Compress, Settings, Admin, etc.
│   │   ├── components/    layout, queue, ui components
│   │   ├── stores/        queue, settings, banner, analytics, session, toast
│   │   └── lib/           api, router, sse, upload, constants, utils
│   └── public/            icons, sw.js, manifest
│
├── api/                   express API server
│   ├── index.js           entry point
│   ├── app.js             express app factory
│   ├── config/
│   │   └── constants.js   all configuration (port, limits, paths)
│   ├── routes/
│   │   ├── download.js    single video downloads + metadata
│   │   ├── playlist.js    playlist downloads (zip)
│   │   ├── gallery.js     image gallery downloads
│   │   ├── convert.js     format conversion
│   │   ├── bot.js         discord bot API endpoints
│   │   ├── admin.js       admin panel + analytics + banner API
│   │   └── core.js        health, sse, session management
│   ├── services/
│   │   ├── cobalt.js      cobalt API integration (youtube fast path)
│   │   ├── downloader.js  shared download logic (yt-dlp wrapper)
│   │   ├── processor.js   ffmpeg processing + file streaming
│   │   ├── state.js       in-memory state (sessions, jobs, connections)
│   │   ├── banner.js      banner system with auto traffic warnings
│   │   ├── analytics.js   JSON-based analytics tracking
│   │   └── youtube.js     youtube URL helpers
│   ├── middleware/
│   │   ├── auth.js        admin authentication (token-based)
│   │   ├── cors.js        CORS configuration
│   │   └── rateLimit.js   rate limiting
│   └── utils/             errors, files, ffmpeg, validation, cookies, helpers, ip
│
└── docs/                  example config files + self-hosting guide
```

## self-hosting

see [docs/self-hosting.md](docs/self-hosting.md) for full setup guide

```bash
git clone https://github.com/coah80/yoink.git
cd yoink/api
npm install
cp ../docs/admin-config.example.js admin-config.js
node index.js
```

the API serves on `http://localhost:3001`. for the frontend:

```bash
cd frontend
npm install
npm run build
```

the built frontend goes in `frontend/dist/` — serve it with any static host or reverse proxy.

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
