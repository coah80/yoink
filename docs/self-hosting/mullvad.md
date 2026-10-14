# Routing YouTube through Mullvad

This page connects the machine running yoink to Mullvad and adds a PO token provider, so that YouTube sees a Mullvad IP address and a real-looking player instead of a blockable server.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

- With the Mullvad app connected, all of the machine's traffic, including yt-dlp's, leaves through the Mullvad server you picked.
- A Mullvad IP address alone is often not enough: YouTube asks many VPN addresses to `Sign in to confirm you're not a bot`. A **PO token** provider (bgutil) makes yt-dlp's requests look like a real player, which gets past that.
- In testing on a Mullvad US server with the PO token provider, 3 of 3 downloads worked. Sent through other Mullvad relays' SOCKS5 proxies instead, every request was blocked without PO tokens, and 1 of 3 worked with them.

</details>

## Compatibility notes

This works anywhere the [Mullvad app](https://mullvad.net/download) runs, and needs a paid [Mullvad](https://mullvad.net) account.

> [!NOTE]
> Don't want the whole machine on the VPN? [Mullvad in Docker](mullvad-docker.md) puts only yt-dlp inside a VPN container, with PO tokens, and rotates exits on its own.

> [!NOTE]
> Using a different proxy provider instead? Set the `PROXY_*` settings described in [Configuration reference](configuration.md#proxy).

## What you need

- A Mullvad account number
- The [Mullvad app](https://mullvad.net/download) installed on the machine that runs yoink
- [Docker](https://docs.docker.com/engine/install/) for the PO token provider

## Instructions

### Section I - Connecting to Mullvad

In this section, you will connect the yoink machine to Mullvad.

> [!WARNING]
> On a remote server, connecting the whole machine to a VPN can cut off your SSH session. Make sure you can reach the server another way first, such as your hosting provider's web console, or use [Mullvad in Docker](mullvad-docker.md) instead.

1. Run `curl https://am.i.mullvad.net/connected`
    + You should see `You are not connected to Mullvad. Your IP address is …`. Note the address, so that you can tell it changed.
1. Log in to the Mullvad app with your account number
    + **CLI (Linux servers):** run `mullvad account login <your-account-number>`. You should see `Mullvad account "<your-account-number>" set`.
1. Connect to a location in the app
    + **CLI:** run `mullvad connect`, then `mullvad status`. The first line should be `Connected`.
1. Run `curl https://am.i.mullvad.net/connected` again
    + You should see `You are connected to Mullvad (server …). Your IP address is …`, with a different address than step 1.

### Section II - Adding a PO token provider

In this section, you will run the bgutil PO token provider and install its yt-dlp plugin.

> [!CAUTION]
> Publish the provider on `127.0.0.1` only, as in step 1. bgutil fixed a remote-code-execution issue in version 2.0.0, and exposing its port to the network gives anyone who can reach it a way in.

1. Run `docker run --name bgutil-provider -d --restart unless-stopped --init -p 127.0.0.1:4416:4416 brainicism/bgutil-ytdlp-pot-provider`
    + Docker prints a long container ID.
    + If you see `Bind for 127.0.0.1:4416 failed: port is already allocated`, something already uses port 4416, often a provider you started earlier. Run `docker ps` to find it, and skip this step if it is one.
1. Run `curl http://127.0.0.1:4416/ping`
    + You should see `{"server_uptime":…,"version":"2.0.2"}`.
1. Run `mkdir -p ~/.config/yt-dlp/plugins`
1. Run `curl -L -o ~/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/latest/download/bgutil-ytdlp-pot-provider.zip`
1. Run `yt-dlp -v --skip-download https://www.youtube.com/watch?v=jNQXAC9IVRw 2>&1 | grep "PO Token Providers"`
    + You should see `[debug] [youtube] [pot] PO Token Providers: bgutil:http-2.0.2 (external), …`.
    + If nothing prints, the plugin is not in `~/.config/yt-dlp/plugins`. Recheck step 4.

### Section III - Pointing yoink at the provider

In this section, you will tell yoink to use PO tokens and check that YouTube works.

1. Open `.env` in the `yoink` folder and set `YOUTUBE_POT_PROVIDER_URL=http://127.0.0.1:4416`
    + Leave `PROXY_HOST` empty. The Mullvad app already routes the traffic.
1. Stop yoink by pressing `Ctrl` + `C` in the terminal running it
1. Run `./yoink`
    + You should see `listening on :3001` as before.
1. Open `http://localhost:3001` and download `https://www.youtube.com/watch?v=jNQXAC9IVRw`
    + The download should finish like it did in [Installing yoink, Section IV](install.md#section-iv---checking-that-it-works).
    + If you see `YouTube is blocking this request, try again later`, that Mullvad server is blocked. Connect to a different city in the app and try again.

> [!TIP]
> YouTube now goes through Mullvad with PO tokens. Keep the Mullvad app connected and the `bgutil-provider` container running whenever yoink runs.

<details><summary>Rotating between Mullvad relays (optional, less reliable)</summary>

yoink can also send each YouTube request through a different Mullvad relay's SOCKS5 proxy, and retry up to 3 times on another relay when one is blocked. In testing with the PO token provider, only 1 of 3 downloads worked this way, so try it only if a single connected server keeps getting blocked.

1. Run `curl -s https://api.mullvad.net/www/relays/wireguard/ | jq -r '.[] | select(.active and .country_code=="us") | .socks_name' | head -5`
    + You should see hostnames such as `us-chi-wg-socks5-201.relays.mullvad.net`. Change `us` to another two-letter country code if you like.
1. Open `.env` and set `PROXY_HOST` to those hostnames separated by commas, and `PROXY_PORT=1080`
    + Leave `PROXY_PASSWORD` and `PROXY_USER_PREFIX` empty. With a password set, yoink switches to an HTTP proxy on port 80.
1. Restart yoink and download a YouTube video
    + The terminal shows `yt-dlp retry 2/3 with a new proxy` when a relay is blocked and yoink moves on.
    + If the terminal shows `Failed to establish a new connection: [Errno -2] Name or service not known`, your DNS cannot resolve the relay hostnames, or a hostname has a typo. Use the addresses from `getent hosts <hostname>` (they start with `10.124.`) in `PROXY_HOST` instead.

These proxies only answer while the Mullvad app is connected.

</details>

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
