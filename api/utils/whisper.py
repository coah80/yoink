#!/usr/bin/env python3
"""
Whisper transcription helper for yoink.
Spawned as a child process by Node.js.

Progress JSON lines written to stderr:
  {"progress": 45, "message": "Transcribing... 45%"}

Final result JSON written to stdout:
  {"success": true, "language": "en", "segmentCount": 42}
"""

import argparse
import json
import sys
import os


def write_progress(progress, message):
    """Write progress update to stderr as JSON line."""
    sys.stderr.write(json.dumps({"progress": progress, "message": message}) + "\n")
    sys.stderr.flush()


def write_result(success, language="", segment_count=0, error=""):
    """Write final result to stdout as JSON."""
    result = {"success": success, "language": language, "segmentCount": segment_count}
    if error:
        result["error"] = error
    print(json.dumps(result))
    sys.stdout.flush()


def format_timestamp_srt(seconds):
    """Format seconds as SRT timestamp: HH:MM:SS,mmm"""
    hours = int(seconds // 3600)
    minutes = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    millis = int((seconds % 1) * 1000)
    return f"{hours:02d}:{minutes:02d}:{secs:02d},{millis:03d}"


def format_timestamp_ass(seconds):
    """Format seconds as ASS timestamp: H:MM:SS.cc"""
    hours = int(seconds // 3600)
    minutes = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    centis = int((seconds % 1) * 100)
    return f"{hours}:{minutes:02d}:{secs:02d}.{centis:02d}"


def write_srt(segments, output_path):
    """Write segments as SRT subtitle file."""
    with open(output_path, "w", encoding="utf-8") as f:
        for i, seg in enumerate(segments, 1):
            f.write(f"{i}\n")
            f.write(f"{format_timestamp_srt(seg['start'])} --> {format_timestamp_srt(seg['end'])}\n")
            f.write(f"{seg['text'].strip()}\n\n")


def write_ass(segments, output_path, font_size=72):
    """Write segments as ASS subtitle file with yellow captions styling."""
    with open(output_path, "w", encoding="utf-8") as f:
        f.write("[Script Info]\n")
        f.write("Title: yoink transcription\n")
        f.write("ScriptType: v4.00+\n")
        f.write("PlayResX: 1920\n")
        f.write("PlayResY: 1080\n")
        f.write("WrapStyle: 0\n")
        f.write("ScaledBorderAndShadow: yes\n")
        f.write("\n")
        f.write("[V4+ Styles]\n")
        f.write("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
        f.write(f"Style: Default,Arial,{font_size},&H0000FFFF,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,0,0,1,3,3,2,40,40,60,1\n")
        f.write("\n")
        f.write("[Events]\n")
        f.write("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

        for seg in segments:
            start = format_timestamp_ass(seg["start"])
            end = format_timestamp_ass(seg["end"])
            text = seg["text"].strip().replace("\n", "\\N")
            f.write(f"Dialogue: 0,{start},{end},Default,,0,0,0,,{text}\n")


def write_txt(segments, output_path):
    """Write segments as plain text transcript."""
    with open(output_path, "w", encoding="utf-8") as f:
        texts = [seg["text"].strip() for seg in segments]
        f.write(" ".join(texts))


def get_audio_duration(input_path):
    """Get audio duration using ffprobe."""
    import subprocess
    try:
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration",
             "-of", "csv=p=0", input_path],
            capture_output=True, text=True, timeout=30
        )
        return float(result.stdout.strip())
    except Exception:
        return 0


def split_by_word_count(segments, max_words):
    """Split segments with more than max_words into sub-segments using word timestamps.
    Falls back to proportional timing when word timestamps aren't available."""
    result = []
    for seg in segments:
        words = seg.get("words")
        if words:
            if len(words) <= max_words:
                result.append(seg)
                continue
            for i in range(0, len(words), max_words):
                chunk = words[i:i + max_words]
                text = "".join(w["word"] for w in chunk).strip()
                result.append({
                    "start": chunk[0]["start"],
                    "end": chunk[-1]["end"],
                    "text": text,
                })
        else:
            # fallback: split by text words with proportional timing
            text_words = seg["text"].strip().split()
            if len(text_words) <= max_words:
                result.append(seg)
                continue
            total_dur = seg["end"] - seg["start"]
            word_dur = total_dur / len(text_words) if text_words else 0
            for i in range(0, len(text_words), max_words):
                chunk = text_words[i:i + max_words]
                chunk_start = seg["start"] + i * word_dur
                chunk_end = seg["start"] + min(i + max_words, len(text_words)) * word_dur
                result.append({
                    "start": chunk_start,
                    "end": chunk_end,
                    "text": " ".join(chunk),
                })
    return result


def enforce_min_duration(segments, min_dur):
    """Extend short segments so they last at least min_dur seconds."""
    for i, seg in enumerate(segments):
        if seg["end"] - seg["start"] < min_dur:
            desired = seg["start"] + min_dur
            # clamp to not overlap next segment
            if i + 1 < len(segments):
                desired = min(desired, segments[i + 1]["start"])
            if desired > seg["start"]:
                seg["end"] = desired
    return segments


def apply_gap(segments, gap):
    """Shrink each segment's end time so there's at least gap seconds before the next."""
    for i in range(len(segments) - 1):
        space = segments[i + 1]["start"] - segments[i]["end"]
        if space < gap:
            segments[i]["end"] = max(segments[i]["start"], segments[i + 1]["start"] - gap)
    return segments


def wrap_lines(segments, max_chars):
    """Insert \\n line breaks via greedy word-wrap."""
    for seg in segments:
        words = seg["text"].strip().split()
        if not words:
            continue
        lines = []
        current = words[0]
        for w in words[1:]:
            if len(current) + 1 + len(w) <= max_chars:
                current += " " + w
            else:
                lines.append(current)
                current = w
        lines.append(current)
        seg["text"] = "\n".join(lines)
    return segments


def transcribe_local(args):
    """Run transcription using local faster-whisper model. Returns (segments, language)."""
    write_progress(0, "Loading whisper model...")

    try:
        from faster_whisper import WhisperModel
    except ImportError:
        write_result(False, error="faster-whisper not installed")
        sys.exit(1)

    try:
        model = WhisperModel(args.model, device="cpu", compute_type="int8")
    except Exception as e:
        write_result(False, error=f"Failed to load model: {str(e)}")
        sys.exit(1)

    write_progress(2, "Model loaded, starting transcription...")

    total_duration = get_audio_duration(args.input)
    need_word_timestamps = args.max_words_per_caption > 0

    transcribe_kwargs = {
        "vad_filter": True,
        "vad_parameters": {"min_silence_duration_ms": 500},
    }
    if args.language:
        transcribe_kwargs["language"] = args.language
    if need_word_timestamps:
        transcribe_kwargs["word_timestamps"] = True

    segments_iter, info = model.transcribe(args.input, **transcribe_kwargs)
    detected_language = info.language

    write_progress(5, f"Detected language: {detected_language}")

    segments = []
    for segment in segments_iter:
        seg_dict = {
            "start": segment.start,
            "end": segment.end,
            "text": segment.text,
        }
        if need_word_timestamps and segment.words:
            seg_dict["words"] = [
                {"start": w.start, "end": w.end, "word": w.word}
                for w in segment.words
            ]
        segments.append(seg_dict)

        if total_duration > 0:
            progress = min(85, int(5 + (segment.end / total_duration) * 80))
            write_progress(progress, f"Transcribing... {progress}%")

    return segments, detected_language


def transcribe_api(args):
    """Run transcription using OpenAI Whisper API. Returns (segments, language)."""
    import subprocess

    try:
        import openai
    except ImportError:
        write_result(False, error="openai package not installed (pip install openai)")
        sys.exit(1)

    api_key = os.environ.get("OPENAI_API_KEY")
    if not api_key:
        write_result(False, error="OPENAI_API_KEY environment variable not set")
        sys.exit(1)

    write_progress(2, "Preparing audio for OpenAI...")

    # Compress WAV to MP3 to stay under OpenAI's 25MB limit
    base, _ = os.path.splitext(args.input)
    mp3_path = base + "_api.mp3"
    try:
        result = subprocess.run(
            ["ffmpeg", "-y", "-i", args.input, "-ac", "1", "-ar", "16000", "-b:a", "48k", mp3_path],
            capture_output=True, text=True, timeout=120
        )
        if result.returncode != 0:
            write_result(False, error=f"MP3 compression failed: {result.stderr[:200]}")
            sys.exit(1)
    except Exception as e:
        write_result(False, error=f"MP3 compression failed: {str(e)}")
        sys.exit(1)

    mp3_size = os.path.getsize(mp3_path)
    if mp3_size > 25 * 1024 * 1024:
        os.unlink(mp3_path)
        write_result(False, error=f"Audio too long for API ({mp3_size // (1024 * 1024)}MB compressed, max 25MB)")
        sys.exit(1)

    write_progress(5, "Transcribing with OpenAI...")

    need_word_timestamps = args.max_words_per_caption > 0

    try:
        client = openai.OpenAI(api_key=api_key)

        with open(mp3_path, "rb") as audio_file:
            kwargs = {
                "model": "whisper-1",
                "file": audio_file,
                "response_format": "verbose_json",
                "timestamp_granularities": ["segment", "word"] if need_word_timestamps else ["segment"],
            }
            if args.language:
                kwargs["language"] = args.language

            response = client.audio.transcriptions.create(**kwargs)

        write_progress(80, "Processing results...")

        detected_language = getattr(response, "language", "") or ""

        api_segments = getattr(response, "segments", []) or []
        api_words = getattr(response, "words", []) or []

        segments = []
        for seg in api_segments:
            seg_dict = {
                "start": seg.start,
                "end": seg.end,
                "text": seg.text,
            }
            segments.append(seg_dict)

        # Assign words to segments in a single pass (O(n+m))
        if need_word_timestamps and api_words:
            sorted_words = sorted(api_words, key=lambda w: w.start)
            word_idx = 0
            for seg_dict in segments:
                seg_words = []
                while word_idx < len(sorted_words):
                    w = sorted_words[word_idx]
                    if w.start >= seg_dict["end"] + 0.01:
                        break
                    if w.start >= seg_dict["start"] - 0.01:
                        seg_words.append({"start": w.start, "end": w.end, "word": w.word})
                    word_idx += 1
                if seg_words:
                    seg_dict["words"] = seg_words

    except openai.APIError as e:
        write_result(False, error=f"OpenAI API error: {str(e)}")
        sys.exit(1)
    except Exception as e:
        write_result(False, error=f"OpenAI transcription failed: {str(e)}")
        sys.exit(1)
    finally:
        try:
            os.unlink(mp3_path)
        except OSError:
            pass

    return segments, detected_language


def main():
    parser = argparse.ArgumentParser(description="Whisper transcription helper")
    parser.add_argument("--input", required=True, help="Input audio file path")
    parser.add_argument("--model", default="base", help="Whisper model size")
    parser.add_argument("--output-format", default="srt", choices=["srt", "ass", "txt"],
                        help="Output format")
    parser.add_argument("--output", required=True, help="Output file path")
    parser.add_argument("--language", default=None, help="Language hint (e.g. en, es, ja)")
    parser.add_argument("--max-words-per-caption", type=int, default=0,
                        help="Max words per caption segment (0 = unlimited)")
    parser.add_argument("--max-chars-per-line", type=int, default=0,
                        help="Max characters per line before wrapping (0 = unlimited)")
    parser.add_argument("--min-duration", type=float, default=0,
                        help="Minimum caption duration in seconds (0 = disabled)")
    parser.add_argument("--gap", type=float, default=0,
                        help="Gap between captions in seconds (0 = none)")
    parser.add_argument("--font-size", type=int, default=72,
                        help="ASS subtitle font size (default 72)")
    parser.add_argument("--use-api", action="store_true",
                        help="Use OpenAI API instead of local model")
    args = parser.parse_args()

    if not os.path.exists(args.input):
        write_result(False, error=f"Input file not found: {args.input}")
        sys.exit(1)

    try:
        if args.use_api:
            segments, detected_language = transcribe_api(args)
        else:
            segments, detected_language = transcribe_local(args)
    except SystemExit:
        raise
    except Exception as e:
        write_result(False, error=f"Transcription failed: {str(e)}")
        sys.exit(1)

    if not segments:
        write_result(False, error="No speech detected in audio")
        sys.exit(1)

    # Post-processing: split → min duration → gap → wrap lines
    if args.max_words_per_caption > 0:
        segments = split_by_word_count(segments, args.max_words_per_caption)
    if args.min_duration > 0:
        segments = enforce_min_duration(segments, args.min_duration)
    if args.gap > 0:
        segments = apply_gap(segments, args.gap)
    if args.max_chars_per_line > 0:
        segments = wrap_lines(segments, args.max_chars_per_line)

    write_progress(88, "Writing output file...")

    try:
        if args.output_format == "srt":
            write_srt(segments, args.output)
        elif args.output_format == "ass":
            write_ass(segments, args.output, font_size=args.font_size)
        elif args.output_format == "txt":
            write_txt(segments, args.output)
    except Exception as e:
        write_result(False, error=f"Failed to write output: {str(e)}")
        sys.exit(1)

    write_progress(95, "Done!")
    write_result(True, language=detected_language, segment_count=len(segments))


if __name__ == "__main__":
    main()
