# Transcription

This page turns on the transcribe page, which makes transcripts, SRT/ASS subtitles and burned-in captions with Whisper speech-recognition models.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

yoink runs `scripts/transcribe.py` with whatever `python3` is first on your `PATH`. The script runs the `tiny`, `base`, `small` and `medium` models on your CPU with the `faster-whisper` package. The `large` model is sent to the OpenAI API instead, which needs an API key and sends your audio to OpenAI.

</details>

## What you need

- Python 3.9 or newer (`python3 --version`)
- About 500 MB of disk space for `faster-whisper`, plus room for the models you use:

  | Model | Download |
  |---|---|
  | `tiny` | 72 MB |
  | `base` | 138 MB |
  | `small` | 461 MB |
  | `medium` | 1.5 GB |

- For `large` only: an [OpenAI API key](https://platform.openai.com/api-keys)

## Instructions

### Section I - Installing faster-whisper

In this section, you will install `faster-whisper` into a Python virtual environment, so that it does not clash with your system's Python packages.

1. Open a terminal in the `yoink` folder
1. Run `python3 -m venv .venv`
    + Nothing prints when it works.
    + **Ubuntu/Debian:** if you see `The virtual environment was not created successfully because ensurepip is not available`, run the `apt install python3.X-venv` command printed below it with `sudo`, then try again.
1. Run `source .venv/bin/activate`
    + Your prompt now starts with `(.venv)`.
1. Run `pip install -U faster-whisper`
    + This takes under a minute and ends with `Successfully installed … faster-whisper-…`.
    + If you see `error: externally-managed-environment`, the virtual environment is not active. Return to step 3.
1. Optional, for the `large` model: run `pip install openai`

### Section II - Starting yoink with faster-whisper

In this section, you will start yoink from the virtual environment, so that its `python3` finds `faster-whisper`.

1. Stop yoink with `Ctrl` + `C` if it is running
1. Optional, for the `large` model: open `.env` and set `OPENAI_API_KEY=<your-api-key>`
    + The key starts with `sk-`.
1. Run `source .venv/bin/activate` in the terminal you will start yoink from
1. Run `./yoink`
    + You should see `✓ faster-whisper available` in the startup output.
    + If you see `- faster-whisper not found (optional, needed for local transcription)`, this terminal is not using the virtual environment. Return to step 3.
1. Open `http://localhost:3001/transcribe` and transcribe a short clip with the `base` model
    + The first run of each model downloads it into `~/.cache/huggingface/hub`, so it takes longer.
    + You should get an `.srt` file back. On a recent CPU, `base` transcribes a 20-second clip in about 4 seconds.
    + If the job fails with `faster-whisper not installed`, return to step 3.
    + If you pick `large` without an API key, you will see `Large model requires API configuration. Use a local model (tiny/base/small/medium).`

> [!WARNING]
> Run `source .venv/bin/activate` every time before you start yoink in a new terminal. Otherwise transcription fails, because the system `python3` does not have `faster-whisper`.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
