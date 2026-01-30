# yoink.tools

A simple, open-source media downloader powered by yt-dlp.

**Live:** [yoink.tools](https://yoink.tools)

## Features

- Download videos and audio from 1000+ sites
- Convert between formats
- Compress videos to a target file size

## Self-Hosting

### Requirements
- Node.js 18+
- yt-dlp
- FFmpeg

### Setup

```bash
git clone https://github.com/coah80/yoink.git
cd yoink/api
npm install
node server.js
```

In another terminal:
```bash
cd yoink/public
npx serve -p 3000
```

API runs on `:3001`, frontend on `:3000`.

## Credits

**Powered by:**
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - media downloading
- [FFmpeg](https://ffmpeg.org) - video processing

**Inspired by:**
- [cobalt.tools](https://cobalt.tools)
- [vert.sh](https://vert.sh)
- [8mb.video](https://8mb.video)

## Links

- [GitHub](https://github.com/coah80/yoink)
- [yoink.tools](https://yoink.tools)
- [coah80.com](https://coah80.com)
