#!/usr/bin/env python3
"""
Demucs stem splitter for yoink.
Spawned per job, so the model only sits in memory while a split is running.
Audio is split in fixed-size blocks and written as it goes, so memory stays flat
no matter how long the input is.

Progress JSON lines written to stderr:
  {"progress": 45, "message": "Splitting stems... 45%"}

Final result JSON written to stdout:
  {"success": true, "device": "cuda", "vocals": "...", "instrumental": "..."}
"""

import argparse
import json
import os
import subprocess
import sys
import time

BLOCK_SECONDS = 120
FADE_SECONDS = 4


def write_progress(progress, message):
    sys.stderr.write(json.dumps({"progress": progress, "message": message}) + "\n")
    sys.stderr.flush()


def write_result(success, error="", **fields):
    result = {"success": success, **fields}
    if error:
        result["error"] = error
    print(json.dumps(result))
    sys.stdout.flush()


def fail(message):
    write_result(False, error=message)
    sys.exit(1)


def load_audio(path, samplerate, channels):
    import torch

    proc = subprocess.Popen(
        ["ffmpeg", "-v", "error", "-i", path, "-vn", "-f", "f32le",
         "-ac", str(channels), "-ar", str(samplerate), "-"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    raw = bytearray()
    while chunk := proc.stdout.read(1 << 22):
        raw += chunk
    err = proc.stderr.read()
    if proc.wait() != 0:
        fail(f"Could not decode audio: {err.decode(errors='replace')[:200]}")
    if len(raw) < samplerate * channels * 4:
        fail("Audio is too short to split")
    return torch.frombuffer(raw, dtype=torch.float32).view(-1, channels).t()


def pick_device(requested, min_free_mb, vram_cap_mb):
    import torch

    if requested == "cpu" or not torch.cuda.is_available():
        return "cpu"
    free, total = torch.cuda.mem_get_info()
    if requested == "auto" and free < min_free_mb * 1024 * 1024:
        return "cpu"
    torch.cuda.set_per_process_memory_fraction(min(1.0, vram_cap_mb * 1024 * 1024 / total))
    return "cuda"


class BlockProgress:
    # demucs reports chunk progress through tqdm; this turns it into overall json progress.
    # it also idles between chunks so the gpu is left free for other services part of the time
    def __init__(self, blocks, duty):
        self.blocks = blocks
        self.block = 0
        self.last = -1
        self.idle_ratio = (1 - duty) / duty if 0 < duty < 1 else 0

    def tqdm(self, iterable, *args, **kwargs):
        items = list(iterable)
        for i, item in enumerate(items):
            started = time.monotonic()
            yield item
            if self.idle_ratio:
                time.sleep((time.monotonic() - started) * self.idle_ratio)
            pct = int((self.block + (i + 1) / len(items)) / self.blocks * 100)
            if pct != self.last:
                self.last = pct
                write_progress(pct, f"Splitting stems... {pct}%")


def split_blocks(model, wav, device, writers, vocals_idx, progress):
    import torch
    from demucs.apply import apply_model

    rate = model.samplerate
    length = wav.shape[-1]
    block = BLOCK_SECONDS * rate
    fade = FADE_SECONDS * rate
    ramp = torch.linspace(0, 1, fade)
    tail = None

    start = 0
    while start < length:
        end = min(length, start + block + fade)
        with torch.no_grad():
            sources = apply_model(model, wav[None, :, start:end], device=device, shifts=1,
                                  split=True, overlap=0.25, progress=True)[0]
        vocals = sources[vocals_idx]
        stems = (vocals, sources.sum(0) - vocals)
        del sources

        if tail is not None:
            n = min(fade, stems[0].shape[-1])
            stems = tuple(
                torch.cat([prev[:, :n] * (1 - ramp[:n]) + cur[:, :n] * ramp[:n], cur[:, n:]], dim=1)
                for prev, cur in zip(tail, stems)
            )

        last = end >= length
        keep = stems[0].shape[-1] if last else stems[0].shape[-1] - fade
        for writer, stem in zip(writers, stems):
            writer.write(stem[:, :keep].t().contiguous().numpy())
        tail = None if last else tuple(stem[:, keep:].clone() for stem in stems)

        progress.block += 1
        start += block


def main():
    parser = argparse.ArgumentParser(description="Demucs stem splitter")
    parser.add_argument("--input", required=True)
    parser.add_argument("--outdir", required=True)
    parser.add_argument("--model", default="htdemucs")
    parser.add_argument("--device", default="auto", choices=["auto", "cuda", "cpu"])
    parser.add_argument("--threads", type=int, default=4)
    parser.add_argument("--min-free-vram-mb", type=int, default=2048)
    parser.add_argument("--vram-cap-mb", type=int, default=3072)
    parser.add_argument("--max-cpu-minutes", type=float, default=10)
    parser.add_argument("--gpu-duty", type=float, default=0.3,
                        help="share of time the gpu is kept busy, 1 = no throttling")
    args = parser.parse_args()

    if not os.path.exists(args.input):
        fail(f"Input file not found: {args.input}")

    write_progress(0, "Loading stem model...")
    try:
        import soundfile
        import torch
        import demucs.apply
        from demucs.pretrained import get_model
    except ImportError as e:
        fail(f"demucs not installed ({e.name})")

    torch.set_num_threads(max(1, args.threads))

    try:
        model = get_model(args.model)
    except Exception as e:
        fail(f"Failed to load model: {e}")
    model.eval()

    names = list(model.sources)
    if "vocals" not in names:
        fail(f"Model {args.model} has no vocals stem")

    device = pick_device(args.device, args.min_free_vram_mb, args.vram_cap_mb)
    wav = load_audio(args.input, model.samplerate, model.audio_channels)
    minutes = wav.shape[-1] / model.samplerate / 60
    if device == "cpu" and minutes > args.max_cpu_minutes:
        fail(f"The stem splitter is busy right now, try again in a bit or use audio under {args.max_cpu_minutes:g} minutes")

    ref = wav.mean(0)
    mean, std = ref.mean().item(), ref.std().item() + 1e-8
    del ref
    wav.sub_(mean).div_(std)

    blocks = max(1, -(-wav.shape[-1] // (BLOCK_SECONDS * model.samplerate)))
    progress = BlockProgress(blocks, args.gpu_duty if device == "cuda" else 1)
    demucs.apply.tqdm.tqdm = progress.tqdm
    write_progress(1, f"Splitting stems on {device}...")

    os.makedirs(args.outdir, exist_ok=True)
    paths = {name: os.path.join(args.outdir, f"{name}.wav") for name in ("vocals", "instrumental")}
    writers = [soundfile.SoundFile(paths[name], "w", model.samplerate, model.audio_channels, subtype="FLOAT")
               for name in ("vocals", "instrumental")]

    class Denormalize:
        def __init__(self, writer):
            self.writer = writer

        def write(self, data):
            self.writer.write(data * std + mean)

    try:
        split_blocks(model, wav, device, [Denormalize(w) for w in writers], names.index("vocals"), progress)
    except torch.cuda.OutOfMemoryError:
        fail("The stem splitter ran out of GPU memory, try again in a bit")
    except Exception as e:
        fail(f"Stem split failed: {e}")
    finally:
        for w in writers:
            w.close()

    write_progress(100, "Stems ready")
    write_result(True, device=device, **paths)


if __name__ == "__main__":
    main()
