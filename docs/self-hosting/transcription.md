# Transcription

This page turns on the transcribe page, which makes transcripts, SRT/ASS subtitles and burned-in captions with OpenAI's Whisper models.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

yoink runs `scripts/transcribe.py` with whatever `python3` is first on your `PATH`. The `tiny`, `base`, `small` and `medium` models run on your machine with the `openai-whisper` package. The `large` model is sent to the OpenAI API instead and needs an API key.

</details>

## What you need

- Python 3.9 or newer (`python3 --version`)
- About 1–5 GB of disk space for the models. `base` is about 150 MB and `medium` about 1.5 GB.
- For `large` only: an [OpenAI API key](https://platform.openai.com/api-keys)

> [!NOTE]
> Local models are slow without a GPU. On a CPU, `base` takes roughly as long as the audio itself, and `medium` several times longer.

## Instructions

### Section I - Installing Whisper

In this section, you will install Whisper into a Python virtual environment, so that it does not clash with your system's Python packages.

1. Open a terminal in the `yoink` folder
1. Run `python3 -m venv .venv`
    + **Ubuntu/Debian:** if you see `ensurepip is not available`, run `sudo apt install -y python3-venv` and try again.
1. Run `source .venv/bin/activate`
    + Your prompt should now start with `(.venv)`.
1. Run `pip install -U openai-whisper`
    + This downloads PyTorch, which is about 1–2 GB, so it takes several minutes.
    + If you see `error: externally-managed-environment`, the virtual environment is not active. Return to step 3.
1. Optional, for the `large` model: run `pip install openai`

### Section II - Starting yoink with Whisper

In this section, you will start yoink from the virtual environment, so that its `python3` finds Whisper.

1. Stop yoink with `Ctrl` + `C` if it is running
1. Optional, for the `large` model: open `.env` and set `OPENAI_API_KEY=<your-api-key>`
    + The key starts with `sk-`.
1. Run `source .venv/bin/activate` in the terminal you will start yoink from
1. Run `./yoink`
    + You should see `✓ whisper module available` in the startup output.
    + If you see `- whisper module not found (optional)`, this terminal is not using the virtual environment. Return to step 3.
1. Open `http://localhost:3001/transcribe` and transcribe a short clip with the `base` model
    + The first run of each model downloads it to `~/.cache/whisper`, so it takes longer.
    + You should get an `.srt` file back.

> [!WARNING]
> Every time you start yoink, run `source .venv/bin/activate` first in that terminal. Otherwise transcription fails, because the system `python3` does not have Whisper.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
