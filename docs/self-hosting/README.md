# Running yoink yourself

This guide walks you through running your own copy of yoink on your computer or a server, from a fresh install to optional extras like Mullvad and transcription.

> [!TIP]
> Estimated time: about 20 minutes for the basic install. Each optional feature adds 5–15 minutes.

## What do I get?

- The full yoink website at `http://localhost:3001`, served by one Go binary
- Downloads from YouTube and the 1000+ other sites yt-dlp supports
- Convert, compress, trim and playlist downloads, all processed on your machine

## What do I need?

- Linux or macOS. On Windows, run yoink inside [WSL](https://learn.microsoft.com/windows/wsl/install) (Ubuntu) and follow the Linux steps.
- Free disk space for the tools and build, plus room for the files you download
- An internet connection during the build, so that npm and Go can download their packages

## What should I know before starting?

- `<angle brackets>` mean: replace this with your own value, without the brackets.
- yoink **clears its temp folder every time it starts**. The setup below points it at a folder of its own, so nothing else gets deleted.
- YouTube blocks some servers. A home connection usually works without extra setup, while a VPS often needs [PO tokens](youtube-auth.md) and [Mullvad](mullvad.md).

## Pages

Follow [Installing yoink](install.md) first. Everything else is optional and can be added later, in any order.

| Page | Read it when |
|---|---|
| [Installing yoink](install.md) | You are starting out. Everything else builds on it. |
| [Routing YouTube through Mullvad](mullvad.md) | YouTube keeps saying `YouTube is blocking this request` on your connection |
| [Mullvad in Docker](mullvad-docker.md) | You want only YouTube on the VPN, with exits that rotate on their own |
| [YouTube PO tokens and cookies](youtube-auth.md) | YouTube blocks your server, or age-restricted videos fail |
| [Transcription](transcription.md) | You want the transcribe page (subtitles and captions) to work |
| [Vocal stems](vocal-stems.md) | You want audio downloads split into vocals and instrumental |
| [TikTok and Instagram extractor](extractor.md) | TikTok or Instagram downloads fail |
| [Discord bot](discord-bot.md) | You want `/yoink`, `/convert` and `/compress` in your Discord server |
| [Working on the code](development.md) | You want to change yoink and see your changes live |
| [Configuration reference](configuration.md) | You want to look up what a setting in `.env` does |
| [Troubleshooting](troubleshooting.md) | Something failed and the page you were on did not cover it |

---

> [!TIP]
> Continue to [Installing yoink](install.md)
