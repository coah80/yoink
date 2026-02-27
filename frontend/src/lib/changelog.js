export const CURRENT_VERSION = '2.1';

export const changelog = [
  {
    version: '2.1',
    date: 'february 23, 2026',
    title: 'trim & crop tool, cobalt for youtube across all tools',
    image: '/updates/v2.1.png',
    summary: `new trim & crop tool for cutting videos and changing aspect ratios. youtube links now use cobalt across convert, compress, transcribe, and trim for faster, more reliable downloads.`,
    content: `new tool and a big quality-of-life fix for youtube links.

## trim & crop

new tool for cutting and cropping videos. upload a file or paste a link, set your start and end times with a video preview, pick an aspect ratio if you want to crop (16:9, 9:16, 1:1, etc.), and download the result. good for clipping highlights or reformatting videos for different platforms.

## cobalt everywhere

pasting youtube links into convert, compress, transcribe, and trim now uses cobalt instead of yt-dlp. same fast downloads you get on the main download page, now everywhere. non-youtube links still use yt-dlp as before.

## other stuff

- crop aspect ratios: 16:9, 9:16, 1:1, 4:3, 4:5
- trim shows a video preview with set start/end buttons
- crop overlay preview shows what gets cut
- manual time inputs for URL mode (no preview)`,
  },
  {
    version: '2.0',
    date: 'february 23, 2026',
    title: 'transcription, encryption, and fixes',
    image: '/updates/v2.png',
    summary: `transcription, server-side encryption, gallery-dl auto-fallback, and a ton of fixes across the board.`,
    content: `big update. transcription, server encryption, and a bunch of stuff got cleaned up.

## transcribe

the biggest new thing. drop a video or audio file and get back a plain text transcript, subtitle file (SRT or ASS), or a version with captions burned right into the video. four model sizes from tiny to medium, auto language detection, and new caption formatting controls so you can set max words per caption, line wrapping, minimum duration, and gaps between captions. works with anything ffmpeg can read.

## encryption

all files on the server are now encrypted at rest using LUKS. the encryption key only lives in RAM, so if the server reboots, everything is gone. temp files are processed in encrypted storage and cleaned up after. the server also sanitizes all logs now, no URLs, filenames, or IPs are stored anywhere.

## gallery-dl fallback

when yt-dlp can't handle something (like image-only tweets), yoink now automatically falls back to gallery-dl. you don't have to do anything different, it just works.

## queue improvements

downloads now process one at a time instead of all at once, which is way more reliable. convert and compress jobs show up in the queue now too. playlists resume from where they left off if you retry after an error instead of starting over from scratch.

## other stuff

- switched from hash routing to clean URLs (/convert instead of #/convert)
- disk space checks before starting any job
- job limits so the server doesn't get overwhelmed
- rewrote the privacy page in plain english
- a bunch of bug fixes and cleanup`,
  },
];
