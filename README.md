<div align="center">
    <br/>
    <p>
        <img src="public/icons/icon-512.png" title="yoink" alt="yoink logo" width="100" />
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
        &nbsp;&bull;&nbsp;
        <a href="https://coah80.com">
            coah80.com
        </a>
    </p>
    <br/>
</div>

yoink is an all-in-one media tool. download videos from 1000+ sites, convert between formats, and compress for Discord. no ads, no trackers, free and fast, forever.


<img width="4064" height="2354" alt="image" src="https://github.com/user-attachments/assets/3dd1dadc-618c-498d-9d81-37058d305ffa" />

## features

- **download** videos and audio from 1000+ sites (powered by yt-dlp)
- **playlists** download entire playlists as a zip
- **images** download image galleries from supported sites
- **convert** between formats with different codecs
- **compress** videos to a target file size
- **gifs** download as gif from supported sites
- **pwa (mobile only)** install as an app, share links directly from your phone.

## self-hosting

see [docs/self-hosting.md](docs/self-hosting.md) for full setup guide

```bash
git clone https://github.com/coah80/yoink.git
cd yoink/api
npm install
node server.js
```

access at `http://localhost:3001`

## credits

**powered by:**
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - media downloading
- [gallery-dl](https://github.com/mikf/gallery-dl) - image downloading  
- [ffmpeg](https://ffmpeg.org) - video processing

**inspired by:**
- [cobalt.tools](https://cobalt.tools)
- [vert.sh](https://vert.sh)
- [8mb.video](https://8mb.video)

## license

[MIT](LICENSE)
