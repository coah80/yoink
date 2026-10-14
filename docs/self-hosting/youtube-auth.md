# YouTube PO tokens and cookies

This page gives yt-dlp proof that it is a real YouTube player (PO tokens) or a signed-in session (cookies), so that downloads keep working on servers YouTube distrusts.

This page assumes you have finished [Installing yoink](install.md). Start with Section I: PO tokens need no Google account. Add cookies (Section II) only for age-restricted videos.

<details><summary>Technical details (optional)</summary>

- A **PO token** (proof-of-origin token) is a value YouTube's player generates to show a request comes from a real browser. YouTube refuses some streams, or the whole video, without one.
- The **bgutil provider** is a small server that generates PO tokens, plus a yt-dlp plugin that asks it for them. yt-dlp's [PO Token Guide](https://github.com/yt-dlp/yt-dlp/wiki/PO-Token-Guide) recommends this setup with the `mweb` client, which is what yoink uses when `YOUTUBE_POT_PROVIDER_URL` is set.
- yoink adds the cookie file to YouTube requests when it exists, reading `youtube-cookies.txt` first and `cookies.txt` second.

</details>

## What you need

- [Docker](https://docs.docker.com/engine/install/) for the PO token provider
- For cookies: a browser signed in to YouTube with a spare Google account

## Instructions

### Section I - Adding a PO token provider

In this section, you will run the bgutil PO token provider, install its yt-dlp plugin, and point yoink at it.

> [!CAUTION]
> Publish the provider on `127.0.0.1` only, as in step 1. bgutil fixed a remote-code-execution issue in version 2.0.0, and exposing its port to the network gives anyone who can reach it a way in.

1. Run `docker run --name bgutil-provider -d --restart unless-stopped --init -p 127.0.0.1:4416:4416 brainicism/bgutil-ytdlp-pot-provider`
    + Docker prints a long container ID.
    + If you see `Bind for 127.0.0.1:4416 failed: port is already allocated`, something already uses port 4416, often a provider you started earlier. Run `docker ps` to find it, and skip this step if it is one.
1. Run `curl http://127.0.0.1:4416/ping`
    + You should see `{"server_uptime":…,"version":"2.0.2"}`. Note the version.
1. Run `mkdir -p ~/.config/yt-dlp/plugins`
1. Run `curl -L -o ~/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/latest/download/bgutil-ytdlp-pot-provider.zip`
    + This works for every way of installing yt-dlp, because yt-dlp loads plugins from this folder.
1. Run `yt-dlp -v --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw 2>&1 | grep "PO Token Providers"`
    + You should see `[debug] [youtube] [pot] PO Token Providers: bgutil:http-2.0.2 (external), …`.
    + If nothing prints, the plugin is not in `~/.config/yt-dlp/plugins`. Recheck step 4.
1. Run `yt-dlp -v --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw 2>&1 | grep "different versions"`
    + Nothing should print.
    + If you see `The provider plugin and the HTTP server are on different versions`, run `docker pull brainicism/bgutil-ytdlp-pot-provider`, then `docker rm -f bgutil-provider`, and repeat steps 1 and 4.
1. Open `.env` in the `yoink` folder and set `YOUTUBE_POT_PROVIDER_URL=http://127.0.0.1:4416`
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again
1. Download `https://www.youtube.com/watch?v=jNQXAC9IVRw` from `http://localhost:3001`
    + The download should finish as before.

### Section II - Adding YouTube cookies (optional)

In this section, you will export a YouTube session from your browser into a file yoink passes to yt-dlp, so that age-restricted videos work.

> [!CAUTION]
> Use a **spare Google account**, not your main one. Google can lock accounts that download heavily, and anyone with the cookie file can use that account.

1. Sign in to YouTube in Firefox or Chrome with the spare account, on the machine that runs yoink
1. Open a terminal in the `yoink` folder
1. Run `yt-dlp --cookies-from-browser firefox --cookies youtube-cookies.txt --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw`
    + **Chrome:** replace `firefox` with `chrome`. On Linux, your desktop's keyring may ask for your password, because Chrome encrypts its cookies.
    + You should see `Extracting cookies from firefox`, and a `youtube-cookies.txt` file appears in the `yoink` folder.
    + If you see `ERROR: could not find firefox cookies database in …`, Firefox has no profile for this user on this machine.
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again
1. Download an age-restricted video to check it works
    + If the terminal shows `[Cookies] Temporarily disabled cookie auth for 30m`, YouTube still flagged the request, so yoink stops sending the cookies for 30 minutes. Export them again from a fresh sign-in, then restart yoink.

> [!NOTE]
> YouTube expires cookies after a while. When age-restricted videos start failing again, repeat this section.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
