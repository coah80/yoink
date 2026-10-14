# Installing yoink

This page gets yoink built and running on your machine, so that you can open `http://localhost:3001` and download a video.

<details><summary>Technical details (optional)</summary>

- yoink is one Go binary that serves both the API and the website, which `make build` compiles into `public/`.
- The downloading and processing is done by tools the binary calls: yt-dlp, ffmpeg, ffprobe and, for images, gallery-dl.
- YouTube also needs Deno, because yt-dlp runs YouTube's player code to unlock the video streams.

</details>

## Compatibility notes

These steps work on Ubuntu/Debian Linux and on macOS with [Homebrew](https://brew.sh). Other Linux distributions work too: install the same tools with your package manager.

> [!WARNING]
> On Windows, install [WSL](https://learn.microsoft.com/windows/wsl/install) with Ubuntu first, then run every command below inside the Ubuntu terminal. Native Windows is not tested.

## What you need

- A terminal and `git`
- Permission to install software (`sudo` on Linux)

## Instructions

### Section I - Installing the tools

In this section, you will install the programs yoink needs to build and to download media.

Already have Go 1.24+, Node.js 20+, yt-dlp, ffmpeg and Deno? Skip to [Section II](#section-ii---getting-the-code).

<details><summary>Ubuntu / Debian</summary>

1. Run `sudo apt update && sudo apt install -y git make ffmpeg python3 curl unzip`
    + `ffmpeg` also installs `ffprobe`, which yoink needs too.
    + If you see `Could not get lock /var/lib/dpkg/lock`, another install is running. Wait a minute and run it again.
1. Install Go 1.24 or newer by following [the official Go install guide](https://go.dev/doc/install)
    + Ubuntu's own `golang` package is often older. That still works if you have internet access, because Go downloads the version yoink asks for on its own.
    + The check below in this section confirms the install.
1. Install Node.js 20 or newer from [nodejs.org](https://nodejs.org/en/download) (the "Linux" tab with `nvm` is the simplest)
    + The final check on that page prints the Node.js version. It should start with `v20` or higher.
1. Run `sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp`
    + You should see a progress bar reach 100%. If you see `Could not resolve host`, check your internet connection.
1. Run `sudo chmod a+rx /usr/local/bin/yt-dlp`
1. Run `curl -fsSL https://deno.land/install.sh | sh`
    + When it asks to edit your shell profile, answer `y`, so that `deno` is on your `PATH`.
    + It ends with `Deno was installed successfully`.
    + If you see `Error: unzip is required`, run `sudo apt install -y unzip` and try again.
1. Close the terminal and open a new one, so that the new `PATH` takes effect
1. Optional: run `sudo apt install -y gallery-dl` to enable image and gallery downloads
    + If you see `Unable to locate package gallery-dl`, run `sudo apt install -y pipx && pipx install gallery-dl` instead.

</details>

<details><summary>macOS</summary>

1. Install [Homebrew](https://brew.sh) if you do not have it yet
1. Run `brew install git go node ffmpeg yt-dlp deno`
    + `ffmpeg` also installs `ffprobe`, which yoink needs too.
    + This takes 5–15 minutes. It ends back at your prompt with no `Error:` lines.
1. Optional: run `brew install gallery-dl` to enable image and gallery downloads

</details>

Now check that every tool is found:

1. Run `go version`
    + You should see `go1.24` or newer, for example `go version go1.24.0 linux/amd64`.
    + If you see `command not found`, open a new terminal. If it is still missing, add `/usr/local/go/bin` to your `PATH` as the Go install guide describes.
1. Run `node --version`
    + You should see `v20` or newer.
    + If you see `v18` or older, install a newer version, since the website build needs it.
1. Run `yt-dlp --version`
    + You should see a date-style version such as `2026.09.16`. If it is more than a couple of months old, run `sudo yt-dlp -U` (Homebrew: `brew upgrade yt-dlp`), because YouTube changes often and old versions break.
1. Run `deno --version`
    + You should see `deno 2.x`.
    + If you see `command not found`, open a new terminal. If it is still missing, add `~/.deno/bin` to your `PATH`.
1. Run `ffmpeg -version`
    + You should see `ffmpeg version …` as the first line.

### Section II - Getting the code

In this section, you will download yoink and create its settings file.

1. Run `git clone https://github.com/coah80/yoink.git`
    + You should see `Cloning into 'yoink'...` and then your prompt again.
1. Run `cd yoink`
1. Run `cp .env.example .env`
    + Nothing prints when it works.
    + `.env` holds your settings. The defaults work for a first run, so you do not need to edit it yet.
    + The default temp folder is `./tmp` inside the `yoink` folder.

> [!CAUTION]
> If you change `YOINK_TEMP_DIR` in `.env`, point it at a folder **only yoink uses**. yoink deletes the contents of its temp subfolders every time it starts, so pointing it at a shared folder, or at another yoink install's folder, deletes those files.

### Section III - Building and starting yoink

In this section, you will build the website and the server, then start it.

1. Run `make build`
    + This downloads the website's packages and Go modules on the first run, which takes 1–3 minutes.
    + You should see `✓ built in …` from the website build, then a `go build` line, with no errors after it.
    + If you see `npm: command not found` or `go: command not found`, return to [Section I](#section-i---installing-the-tools) and check that tool.
1. Run `./yoink`
    + You should see the yoink banner, then one line per tool, for example `✓ yt-dlp found: /usr/local/bin/yt-dlp`.
    + Lines starting with `-` are optional tools. `- gallery-dl not found (optional)` only means image downloads are off.
    + If a line says `✗ … not found (REQUIRED)`, install that tool (see [Section I](#section-i---installing-the-tools)), then run `./yoink` again.
    + You will see `BOT_SECRET not set, bot endpoints will reject requests` and `No cors-origins.txt found, allowing all origins`. **Both are expected** on a local install.
    + The last line should be `listening on :3001`.
    + If you see `bind: address already in use`, another program is using port 3001. Set `PORT=3002` in `.env` and run `./yoink` again, then use port 3002 in the rest of this guide.
1. Leave this terminal open, because closing it stops yoink

### Section IV - Checking that it works

In this section, you will download a short video to confirm everything is wired up.

1. Open `http://localhost:3001` in your browser
    + You should see the yoink home page with a link box.
    + If the page says `This site can't be reached`, check that the terminal from Section III still shows `listening on :3001`.
1. Paste `https://www.youtube.com/watch?v=jNQXAC9IVRw` into the link box
    + This is "Me at the zoo", the first YouTube video. It is 19 seconds long.
1. Click `yoink`
    + The download appears in the queue and finishes within about a minute. yt-dlp waits between YouTube requests on purpose, so a short pause is normal.
    + Your browser saves an `.mp4` file named after the video.
    + If you see `YouTube is blocking this request, try again later`, YouTube is blocking your IP address. Follow [Routing YouTube through Mullvad](mullvad.md).
    + If YouTube downloads fail or come out in low quality, run `deno --version` in a new terminal. Without Deno, yt-dlp cannot unlock most YouTube formats.
    + For anything else, check the terminal running yoink for the error and look it up in [Troubleshooting](troubleshooting.md).

> [!TIP]
> yoink is running. To start it again later, run `./yoink` from the `yoink` folder. After a `git pull`, run `make build` first.

---

> [!TIP]
> Return to [Running yoink yourself](README.md) to add optional features, or continue to [Routing YouTube through Mullvad](mullvad.md) if YouTube is blocking you.
