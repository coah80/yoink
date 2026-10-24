# Vocal stems

This page turns on vocal stems, which split audio downloads into vocals and instrumental with Demucs.

This page assumes you have finished [Installing yoink](install.md).

<details><summary>Technical details (optional)</summary>

When someone downloads audio with **vocal stems** set to `vocals`, `instrumental` or `both` in settings, yoink starts `scripts/stems.py` for that one download. The script loads the `htdemucs` model, splits the audio on your NVIDIA GPU (or the CPU if there is none), writes the stems and exits, so the model only uses memory while a split is running. `both` sends a zip with the two files.

A split is kept from slowing down the rest of the server:

- One split runs at a time and one more can wait. A third stem download is turned away right away with `The stem splitter is busy right now`, so stem jobs never fill every download slot.
- When `systemd-run --user` works, each split gets its own cgroup with `MemoryMax=5G`, no swap, `CPUQuota=400%` and a low IO weight. A split that runs out of memory is the only thing killed. Without systemd it still runs under `nice -n 19`.
- PyTorch uses 4 CPU threads, the process runs at nice 19, and audio is split in 2-minute blocks, so memory stays around 2 to 4 GB even for an hour of audio.
- On the GPU it keeps itself busy only 30% of the time and caps its own VRAM at 3 GB. With another model on the same GPU, that model keeps about 80% of its speed during a split. If less than 2 GB of VRAM is free, the split runs on the CPU, which only takes audio up to 10 minutes.
- Audio longer than 60 minutes is refused, and a split is stopped after 20 minutes. If yoink stops, the split stops with it.

yoink looks for Python in this order: `STEMS_PYTHON`, then `stems-venv/bin/python` next to the yoink program, then `python3`.

</details>

## What you need

- Python 3.10 to 3.12 (`python3 --version`)
- About 3 GB of disk space for PyTorch and Demucs, plus 80 MB for the model
- Optional: an NVIDIA GPU with 2 GB of free memory. On an RTX 3060 a 3-minute song splits in about 30 seconds with the default GPU limits. On a CPU it takes a few minutes.

## Instructions

### Section I - Installing Demucs

In this section, you will install Demucs into its own virtual environment next to the yoink program.

1. Open a terminal in the folder that holds the `yoink` program
1. Run `python3 -m venv stems-venv`
    + Nothing prints when it works.
1. Install PyTorch:
    + **NVIDIA GPU:** run `stems-venv/bin/pip install torch==2.5.1 torchaudio==2.5.1 --index-url https://download.pytorch.org/whl/cu124`
    + **CPU only:** run `stems-venv/bin/pip install torch==2.5.1 torchaudio==2.5.1 --index-url https://download.pytorch.org/whl/cpu`
1. Run `stems-venv/bin/pip install demucs==4.0.1 soundfile`
    + This ends with `Successfully installed … demucs-4.0.1 …`.
1. Optional, GPU only: run `stems-venv/bin/python -c "import torch; print(torch.cuda.is_available())"`
    + You should see `True`. `False` means splits will run on the CPU.

### Section II - Checking it works

1. Restart yoink
    + You should see `✓ demucs available: …/stems-venv/bin/python` in the startup output.
    + If you see `- demucs not found (optional, needed for vocal stems)`, the virtual environment is not next to the yoink program. Move it there or set `STEMS_PYTHON` in `.env` to its `bin/python`.
1. Open `http://localhost:3001/settings`, go to the **audio** tab and set **vocal stems** to `both`
1. Download a song as audio
    + The first split downloads the model into `~/.cache/torch/hub`, so it takes longer.
    + You should get a zip with `… (vocals).mp3` and `… (instrumental).mp3`.
    + If the download fails with `demucs not installed`, return to Section I.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
