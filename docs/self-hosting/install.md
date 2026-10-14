# Installing yoink

This page gets yoink built and running on your machine, so that you can open `http://localhost:3001` and download a video.

<details><summary>Technical details (optional)</summary>

- yoink is one Go binary that serves both the API and the website, which `make build` compiles into `public/`. It looks for `public/` next to the binary.
- The downloading and processing is done by tools the binary calls: yt-dlp, ffmpeg, ffprobe and, for images, gallery-dl.
- yt-dlp uses Deno to run YouTube's player code. yt-dlp calls YouTube support without it deprecated, and some formats may be missing.

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

Already have Go 1.22+, Node.js 20+, yt-dlp, ffmpeg and Deno? Skip to [Section II](#section-ii---getting-the-code).

<details><summary>Ubuntu / Debian</summary>

1. Run `sudo apt update && sudo apt install -y git make ffmpeg python3 curl unzip golang-go`
    + `ffmpeg` also installs `ffprobe`, which yoink needs too.
    + `golang-go` is Go 1.22 on Ubuntu 24.04. That is fine: the first build downloads the Go 1.24 that yoink asks for on its own.
    + If you see `Could not get lock`, another install is running. Wait a minute and run it again.
1. Run `curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh | bash`
    + This installs **nvm**, a Node.js version manager. [nodejs.org](https://nodejs.org/en/download) lists the current version of this command.
1. Run `. "$HOME/.nvm/nvm.sh" && nvm install 24`
    + You should see `Now using node v24.… (npm v11.…)`.
1. Run `sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp`
    + curl prints a progress table that ends at `100`.
    + This build of yt-dlp runs on the `python3` installed in step 1.
1. Run `sudo chmod a+rx /usr/local/bin/yt-dlp`
    + Nothing prints when it works.
1. Run `curl -fsSL https://deno.land/install.sh | sh`
    + You should see `Deno was installed successfully to /home/<your-username>/.deno/bin/deno`.
    + At `Edit shell configs to add deno to the PATH? (Y/n)`, press `Enter`, so that `deno` is on your `PATH`.
    + At `Set up completions?`, press `Enter` to skip.
    + If you see `Error: either unzip or 7z is required to install Deno`, run `sudo apt install -y unzip` and try again.
1. Close the terminal and open a new one, so that the new `PATH` takes effect
1. Optional: run `sudo apt install -y gallery-dl` to enable image and gallery downloads
    + Ubuntu 24.04 ships version 1.26 from 2024. For a current one, run `sudo apt install -y pipx && pipx install gallery-dl` instead.

</details>

<details><summary>macOS</summary>

1. Install [Homebrew](https://brew.sh) if you do not have it yet
1. Run `brew install git go node ffmpeg yt-dlp deno`
    + `ffmpeg` also installs `ffprobe`, which yoink needs too.
    + It ends back at your prompt with no `Error:` lines.
1. Optional: run `brew install gallery-dl` to enable image and gallery downloads

</details>

Now check that every tool is found:

1. Run `go version`
    + You should see a version line such as `go version go1.22.2 linux/amd64`. Anything from `go1.22` up works.
    + If you see `command not found`, open a new terminal. If you installed Go from go.dev instead of apt, run `source ~/.profile` or log out and back in.
1. Run `node --version`
    + You should see `v20` or newer. Node.js 18 also builds yoink, but it no longer gets security updates.
1. Run `yt-dlp --version`
    + You should see a date-style version such as `2026.08.19`.
    + Run `sudo yt-dlp -U` (Homebrew: `brew upgrade yt-dlp`) every few weeks, because YouTube changes often and old versions break. It prints `yt-dlp is up to date` when there is nothing new.
1. Run `deno --version`
    + The first line should be `deno 2.…`.
    + If you see `command not found`, open a new terminal. If it is still missing, add `~/.deno/bin` to your `PATH`.
1. Run `ffmpeg -version`
    + The first line should be `ffmpeg version …`.

### Section II - Getting the code

In this section, you will download yoink and create its settings file.

1. Run `git clone https://github.com/coah80/yoink.git`
    + You should see `Cloning into 'yoink'...` and then your prompt again.
1. Run `cd yoink`
1. Run `cp .env.example .env`
    + Nothing prints when it works.
    + `.env` holds your settings. The defaults work for a first run, so you do not need to edit it yet.

> [!CAUTION]
> Always start yoink **from the `yoink` folder**, and run **only one copy** at a time. yoink reads `.env` and puts its temp folder (`./tmp`) relative to the folder you start it from, and it empties its temp subfolders every time it starts. Started anywhere else, it skips `.env` and uses `/var/tmp/yoink` instead. A second copy empties the first copy's temp files before it notices the port is taken.

### Section III - Building and starting yoink

In this section, you will build the website and the server, then start it.

1. Run `make build`
    + The first run downloads the website's packages, Go modules and, with an older Go, Go 1.24 itself. This takes from under a minute to a few minutes depending on your connection.
    + It ends with `✓ built in …` from the website build, a `go build -ldflags … -o yoink ./cmd/yoink` line, then `mkdir -p public` and `cp -R frontend/public/. public/`.
    + npm's `… vulnerabilities` summary, an `allowScripts` warning about `esbuild`, and `[vite-plugin-svelte] … A11y` warnings also appear. **These are expected** and do not stop the build.
    + If you see `npm: command not found` or `go: command not found`, return to [Section I](#section-i---installing-the-tools) and check that tool.
1. Run `./yoink`
    + It starts with `BOT_SECRET not set, bot endpoints will reject requests`, then the yoink banner, then one line per tool, for example `✓ yt-dlp found: /usr/local/bin/yt-dlp`. **The BOT_SECRET warning is expected** on a local install.
    + Lines starting with `-` are optional tools. `- gallery-dl not found (optional)` only means image downloads are off.
    + If a line says `✗ … not found (REQUIRED)`, install that tool (see [Section I](#section-i---installing-the-tools)), then run `./yoink` again.
    + Next come `✓ Cleared temp directories` and `No cors-origins.txt found, allowing all origins`. **This is expected** too.
    + The last line ends with `listening on :3001`.
    + If a moment later you see `server failed: listen tcp :3001: bind: address already in use`, another program is using port 3001. Set `PORT=3002` in `.env` and run `./yoink` again, then use port 3002 in the rest of this guide.
1. Leave this terminal open, because closing it stops yoink

### Section IV - Checking that it works

In this section, you will download a short video to confirm everything is wired up.

1. Open `http://localhost:3001` in your browser
    + You should see the yoink home page with a link box.
    + If the page says `This site can't be reached`, check that the terminal from Section III still shows `listening on :3001`.
1. Paste `https://www.youtube.com/watch?v=jNQXAC9IVRw` into the link box
    + This is "Me at the zoo", the first YouTube video. It is 19 seconds long and only exists in 240p, so a small, low-resolution file is expected.
1. Click `yoink`
    + The download appears in the queue and finishes within about a minute. yt-dlp waits between YouTube requests on purpose, so a pause is normal.
    + Your browser saves `Me at the zoo.mp4`, about 730 KB.
    + If you see `YouTube is blocking this request, try again later`, YouTube is blocking your IP address. Follow [Routing YouTube through Mullvad](mullvad.md).
    + For anything else, check the terminal running yoink for the error and look it up in [Troubleshooting](troubleshooting.md).

> [!NOTE]
> yoink does not check for Deno at startup. Without it, downloads still work for now, but yt-dlp warns that some YouTube formats may be missing, and YouTube can start blocking requests without warning. Keep Deno installed.

> [!TIP]
> yoink is running. To start it again later, run `./yoink` from the `yoink` folder. After a `git pull`, run `make build` first.

---

> [!TIP]
> Return to [Running yoink yourself](README.md) to add optional features, or continue to [Routing YouTube through Mullvad](mullvad.md) if YouTube is blocking you.
