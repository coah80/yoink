# Mullvad in Docker

This page runs yt-dlp inside a Docker container that is connected to Mullvad, so that only YouTube downloads use the VPN and the rest of your machine stays on its normal connection.

This page assumes you have finished [Installing yoink](install.md). It is the setup yoink.tools itself runs. If you would rather connect the whole machine to Mullvad, follow [Routing YouTube through Mullvad](mullvad.md) instead.

<details><summary>Technical details (optional)</summary>

- **Gluetun** is a Docker image that holds a VPN connection. Other containers can share its network, so their traffic goes through the VPN.
- `scripts/mullvad-vpn/yt-dlp-vpn` stands in for yt-dlp. For YouTube links it runs yt-dlp in a short-lived container on Gluetun's network. Every other site uses your normal yt-dlp.
- A **PO token** provider (bgutil) runs in the same VPN network, so YouTube sees tokens and requests coming from the same Mullvad IP address. Cookies are not used on this route.
- If YouTube blocks the Mullvad exit, the wrapper retries once with your normal yt-dlp, unless `YTDLP_REQUIRE_VPN=1` is set.
- `start-mullvad` picks a random Mullvad server in your country, then checks that YouTube works through it. It tries up to 5 servers.

</details>

## Compatibility notes

This page is for Linux with Docker. The scripts use `bash`, `systemd` (for the optional rotation timer) and Docker's `--network container:` mode.

## What you need

- A paid [Mullvad](https://mullvad.net) account number
- [Docker Engine](https://docs.docker.com/engine/install/) installed and running
- About 2 GB of disk space for the images

## Instructions

### Section I - Installing the tools

In this section, you will install the programs the scripts call.

1. Run `sudo apt install -y jq curl wireguard-tools`
    + `wireguard-tools` provides `wg`, which creates the WireGuard key.
    + Run `wg --version` afterwards. You should see `wireguard-tools v1.x`.
1. Run `sudo usermod -aG docker $USER`, then log out and back in
    + This lets the scripts run `docker` without `sudo`.
    + After logging back in, `groups` should list `docker`.
1. Run `docker run --rm hello-world`
    + You should see `Hello from Docker!`.
    + If you see `permission denied while trying to connect to the Docker daemon socket`, you have not logged out and back in since step 2.

### Section II - Creating the Mullvad config

In this section, you will register a WireGuard key with your Mullvad account and save the VPN config.

> [!CAUTION]
> The config file holds your **WireGuard private key**, which lets anyone connect as your Mullvad device. It is written to `~/yoink-vpn-ytdlp/config/gluetun.env` with owner-only permissions. Never commit or share it.

> [!NOTE]
> This uses one of your Mullvad device slots (5 per account). Running it again reuses the same key and device.

1. Open a terminal in the `yoink` folder
1. Optional: run `export YOINK_VPN_COUNTRY=Sweden` (or another country) to pick where the exits are
    + The default is `USA`. Use the country name as Mullvad lists it.
1. Run `scripts/mullvad-vpn/generate-mullvad-env`
1. Enter your Mullvad account number when asked
    + The number does not show while you type. **This is expected.**
    + You should see `Wrote /home/<your-username>/yoink-vpn-ytdlp/config/gluetun.env`.
    + If you see `Mullvad did not return an access token`, the account number is wrong or the account has expired.
    + If you see `Could not create Mullvad WireGuard device`, the most common cause is that all 5 device slots are used. Remove one in your [Mullvad account](https://mullvad.net/account/devices) and run the script again.

### Section III - Starting the VPN container

In this section, you will build the yt-dlp image and start the Mullvad container.

1. Run `scripts/mullvad-vpn/build-ytdlp-image`
    + This takes 3–10 minutes on the first run.
    + It ends with `naming to docker.io/library/yoink-ytdlp:latest`.
1. Run `scripts/mullvad-vpn/start-mullvad`
    + You should see `Selected Mullvad server: …`, then `yoink-mullvad is healthy`, then `yoink-mullvad youtube ok`.
    + If you see `YouTube blocked on this Mullvad exit`, the script tries another server on its own. **This is expected** now and then.
    + If it ends after 5 tries without `youtube ok`, YouTube is blocking that country's exits. Set a different `YOINK_VPN_COUNTRY` and run Section II and this step again.
1. Run `scripts/mullvad-vpn/check-mullvad`
    + You should see `You are connected to Mullvad (server …)`.

### Section IV - Pointing yoink at the wrapper

In this section, you will make yoink use the wrapper instead of plain yt-dlp.

1. Run `realpath scripts/mullvad-vpn/yt-dlp-vpn` and copy the output
    + You should see a full path ending in `/yoink/scripts/mullvad-vpn/yt-dlp-vpn`.
1. Open `.env` in the `yoink` folder and set `YTDLP_BIN=<the-path-from-step-1>`
    + Leave `PROXY_HOST` empty. The container already handles the VPN.
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again from the `yoink` folder
    + The wrapper mounts the folder yoink runs from into the container, so start yoink from the `yoink` folder.
1. Download `https://www.youtube.com/watch?v=jNQXAC9IVRw` from `http://localhost:3001`
    + The download should finish like it did in [Installing yoink, Section IV](install.md#section-iv---checking-that-it-works).
    + While it runs, `docker ps` shows a `yoink-ytdlp` container.
    + If you see `Unable to find image 'yoink-ytdlp:latest'`, return to [Section III, step 1](#section-iii---starting-the-vpn-container).

### Section V - Rotating exits automatically (optional)

In this section, you will add a timer that moves to a fresh Mullvad server about once an hour while yoink is idle, so that one blocked IP address does not stick around.

1. Run `mkdir -p ~/.config/systemd/user`
    + Nothing prints when it works.
1. Create `~/.config/systemd/user/yoink-vpn-rotate.service` with this content, replacing `<path-to-yoink>` with the full path of your `yoink` folder:

   ```ini
   [Unit]
   Description=Rotate Yoink Mullvad yt-dlp tunnel if idle

   [Service]
   Type=oneshot
   ExecStart=<path-to-yoink>/scripts/mullvad-vpn/rotate-mullvad-if-idle
   ```

1. Create `~/.config/systemd/user/yoink-vpn-rotate.timer` with this content:

   ```ini
   [Unit]
   Description=Run Yoink Mullvad yt-dlp tunnel rotation

   [Timer]
   OnBootSec=20min
   OnUnitActiveSec=55min
   RandomizedDelaySec=5min
   Unit=yoink-vpn-rotate.service

   [Install]
   WantedBy=timers.target
   ```

1. Run `systemctl --user daemon-reload && systemctl --user enable --now yoink-vpn-rotate.timer`
    + You should see `Created symlink … yoink-vpn-rotate.timer`.
    + If you see `Failed to connect to bus`, you are in a shell without a user session (for example `su`). Log in as that user directly over SSH.
1. Run `systemctl --user list-timers yoink-vpn-rotate.timer`
    + You should see the timer with a `NEXT` time about 20 minutes away.
    + Each run skips itself while yoink has active jobs, so rotation never cuts off a download.

> [!TIP]
> Running yoink as a systemd service? `scripts/mullvad-vpn/finish-mullvad-setup` does Sections II–V in one go. Set `YOINK_ENV_FILE` to your `.env` path, and `YOINK_SERVICE_NAME` if your service is not called `yoink-go.service`.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
