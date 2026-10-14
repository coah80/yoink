# YouTube cookies and PO tokens

This page gives yt-dlp a signed-in YouTube session (cookies) or proof it is a real player (PO tokens), so that age-restricted videos and heavily blocked servers work.

This page assumes you have finished [Installing yoink](install.md). If YouTube blocks your IP address, try [Routing YouTube through Mullvad](mullvad.md) first, since it needs no YouTube account.

<details><summary>Technical details (optional)</summary>

A **PO token** (proof-of-origin token) is a value YouTube's player generates to show the request comes from a real browser. YouTube refuses some streams without one. The `bgutil` provider generates them for yt-dlp.

yoink adds both to every YouTube request when they are configured: the PO token provider first, then the cookie file. It reads `youtube-cookies.txt` first and falls back to `cookies.txt`.

</details>

## What you need

- For cookies: a browser signed in to YouTube, ideally with a spare Google account
- For PO tokens: [Docker](https://docs.docker.com/get-docker/), or Node.js to run the provider directly

## Instructions

### Section I - Adding YouTube cookies

In this section, you will export a YouTube session from your browser into a file yoink passes to yt-dlp.

> [!CAUTION]
> Use a **spare Google account**, not your main one. Google can lock or ban accounts that download heavily, and anyone with the cookie file can use that account.

1. Sign in to YouTube in Firefox or Chrome with the spare account
1. Close the browser completely, so that yt-dlp can read its cookie database
1. Open a terminal in the `yoink` folder
1. Run `yt-dlp --cookies-from-browser firefox --cookies youtube-cookies.txt --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw`
    + **Chrome:** replace `firefox` with `chrome`.
    + You should now have a `youtube-cookies.txt` file in the `yoink` folder.
    + If you see `could not find firefox cookies database`, the browser is not installed for this user. Chrome on Linux may also ask for your keyring password.
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again
1. Download an age-restricted video to check it works
    + If the terminal shows `[Cookies] Temporarily disabled cookie auth for 30m`, YouTube flagged the request even with cookies, so yoink stops sending them for 30 minutes. Export them again from a fresh sign-in, then restart yoink.

> [!NOTE]
> YouTube expires cookies after a while. When downloads that used to work start failing with `YouTube is blocking this request`, repeat this section.

### Section II - Adding a PO token provider

In this section, you will run the bgutil PO token provider and connect yt-dlp and yoink to it.

1. Run `docker run --name bgutil-provider -d -p 4416:4416 --init brainicism/bgutil-ytdlp-pot-provider`
    + Without Docker, follow the "Native" setup in the [bgutil-ytdlp-pot-provider README](https://github.com/Brainicism/bgutil-ytdlp-pot-provider).
1. Run `curl http://localhost:4416/ping`
    + You should see a JSON reply with a `version` field.
1. Install the matching yt-dlp plugin by following the "Installing the plugin" section of the [bgutil-ytdlp-pot-provider README](https://github.com/Brainicism/bgutil-ytdlp-pot-provider)
    + Install the plugin the same way you installed yt-dlp. For the standalone `/usr/local/bin/yt-dlp`, that is the zip download into `~/.config/yt-dlp/plugins/`.
1. Run `yt-dlp -v --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw 2>&1 | grep -i bgutil`
    + You should see the bgutil provider listed among the PO token providers.
    + If nothing prints, the plugin is not installed where yt-dlp looks. Recheck step 3.
1. Open `.env` and set `YOUTUBE_POT_PROVIDER_URL=http://localhost:4416`
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again
1. Download `https://www.youtube.com/watch?v=jNQXAC9IVRw` from `http://localhost:3001`
    + The download should finish as before.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
