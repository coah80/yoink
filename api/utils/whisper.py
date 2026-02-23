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


def write_ass(segments, output_path):
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
        f.write("Style: Default,Arial,72,&H0000FFFF,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,0,0,1,3,1.5,2,40,40,60,1\n")
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


def main():
    parser = argparse.ArgumentParser(description="Whisper transcription helper")
    parser.add_argument("--input", required=True, help="Input audio file path")
    parser.add_argument("--model", default="base", help="Whisper model size")
    parser.add_argument("--output-format", default="srt", choices=["srt", "ass", "txt"],
                        help="Output format")
    parser.add_argument("--output", required=True, help="Output file path")
    parser.add_argument("--language", default=None, help="Language hint (e.g. en, es, ja)")
    args = parser.parse_args()

    if not os.path.exists(args.input):
        write_result(False, error=f"Input file not found: {args.input}")
        sys.exit(1)

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

    try:
        transcribe_kwargs = {
            "vad_filter": True,
            "vad_parameters": {"min_silence_duration_ms": 500},
        }
        if args.language:
            transcribe_kwargs["language"] = args.language

        segments_iter, info = model.transcribe(args.input, **transcribe_kwargs)
        detected_language = info.language

        write_progress(5, f"Detected language: {detected_language}")

        segments = []
        for segment in segments_iter:
            segments.append({
                "start": segment.start,
                "end": segment.end,
                "text": segment.text,
            })

            if total_duration > 0:
                progress = min(85, int(5 + (segment.end / total_duration) * 80))
                write_progress(progress, f"Transcribing... {progress}%")

    except Exception as e:
        write_result(False, error=f"Transcription failed: {str(e)}")
        sys.exit(1)

    if not segments:
        write_result(False, error="No speech detected in audio")
        sys.exit(1)

    write_progress(88, "Writing output file...")

    try:
        if args.output_format == "srt":
            write_srt(segments, args.output)
        elif args.output_format == "ass":
            write_ass(segments, args.output)
        elif args.output_format == "txt":
            write_txt(segments, args.output)
    except Exception as e:
        write_result(False, error=f"Failed to write output: {str(e)}")
        sys.exit(1)

    write_progress(95, "Done!")
    write_result(True, language=detected_language, segment_count=len(segments))


if __name__ == "__main__":
    main()
