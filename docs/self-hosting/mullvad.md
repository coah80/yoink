# Routing YouTube through Mullvad

This page sends yoink's YouTube traffic through a pool of Mullvad SOCKS5 proxies, so that YouTube sees a different Mullvad IP address for each request instead of one blockable server address.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

A **SOCKS5 proxy** is a relay: yt-dlp sends its traffic to the proxy, and the proxy talks to YouTube for it. Mullvad runs one on every WireGuard server, and they are only reachable from inside a Mullvad VPN connection. That is why the machine running yoink has to be connected to Mullvad.

yoink only sends YouTube requests through the proxy. For each request it picks one relay from `PROXY_HOST` at random, and when a request fails it retries up to 3 times on a different relay. Removed or region-locked videos are not retried, since a new IP cannot fix them.

Every Mullvad server also has a local proxy at `10.64.0.1:1080`. Using only that one gives you the same IP as the VPN connection itself, so it is mostly useful for testing.

</details>

## Compatibility notes

This works anywhere the [Mullvad app](https://mullvad.net/download) runs, and needs a paid [Mullvad](https://mullvad.net) account.

> [!NOTE]
> Using a different proxy provider instead? Skip this page and set the `PROXY_*` settings described in [Configuration reference](configuration.md#proxy).

## What you need

- A Mullvad account number
- The [Mullvad app](https://mullvad.net/download) installed on the machine that runs yoink

## Instructions

### Section I - Connecting to Mullvad

In this section, you will connect the yoink machine to Mullvad, so that its proxies become reachable.

> [!WARNING]
> On a remote server, connecting the whole machine to a VPN can cut off your SSH session. Make sure you can reach the server another way first, such as your hosting provider's web console.

1. Open the Mullvad app and log in with your account number
    + **CLI (Linux servers):** run `mullvad account login <your-account-number>`.
1. Connect to any location
    + **CLI:** run `mullvad connect`, then `mullvad status`. You should see `Connected`.
1. Run `curl --socks5-hostname 10.64.0.1:1080 https://am.i.mullvad.net/connected`
    + You should see `You are connected to Mullvad (server …)`.
    + If it hangs or says `Connection refused`, Mullvad is not connected. Return to step 2.

### Section II - Choosing relays

In this section, you will pick the Mullvad servers whose IP addresses YouTube will see.

1. Run `mullvad relay list | grep -o '[a-z]*-[a-z]*-wg-[0-9]*' | head -20`
    + You should see relay names such as `se-mma-wg-001`.
    + Without the CLI, use [Mullvad's server list](https://mullvad.net/servers) instead.
1. Pick 3–10 relays, ideally in different cities
    + More relays means more IP addresses to rotate through when YouTube blocks one.
1. Turn each relay name into its proxy hostname by adding `socks5-` after `wg-` and `.relays.mullvad.net` at the end
    + `se-mma-wg-001` becomes `se-mma-wg-socks5-001.relays.mullvad.net`.
1. Run `curl --socks5-hostname <one-of-your-hostnames>:1080 https://am.i.mullvad.net/connected`
    + You should see `You are connected to Mullvad (server …)` naming that relay, not the one you connected to in Section I.
    + If you see `Could not resolve host`, check the hostname for typos. The `socks5-` part is easy to miss.

### Section III - Pointing yoink at the relays

In this section, you will add the relays to yoink's settings and restart it.

> [!IMPORTANT]
> Leave `PROXY_PASSWORD` and `PROXY_USER_PREFIX` empty for Mullvad. With a password set, yoink switches to an HTTP proxy on port 80, and Mullvad refuses the connection.

1. Open `.env` in the `yoink` folder in a text editor
1. Set `PROXY_HOST` to your hostnames separated by commas, and `PROXY_PORT` to `1080`:

   ```ini
   PROXY_HOST=se-mma-wg-socks5-001.relays.mullvad.net,nl-ams-wg-socks5-001.relays.mullvad.net,de-fra-wg-socks5-001.relays.mullvad.net
   PROXY_PORT=1080
   ```

1. Save the file
1. Stop yoink by pressing `Ctrl` + `C` in the terminal running it
1. Run `./yoink`
    + You should see `listening on :3001` as before.
1. Open `http://localhost:3001` and download `https://www.youtube.com/watch?v=jNQXAC9IVRw`
    + The download should finish like it did in [Installing yoink, Section IV](install.md#section-iv---checking-that-it-works).
    + If the terminal shows `yt-dlp retry 2/3 with a new proxy` and the download then succeeds, one relay was blocked and yoink moved on. **This is expected** now and then.
    + If every attempt fails with `Connection refused` or `timed out`, Mullvad has disconnected. Return to [Section I, step 3](#section-i---connecting-to-mullvad).
    + If you still see `YouTube is blocking this request`, those relays are blocked too. Add relays in other countries, or add [YouTube cookies or a PO token](youtube-auth.md).

> [!TIP]
> YouTube now goes through Mullvad. Keep the Mullvad app connected whenever yoink runs.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
