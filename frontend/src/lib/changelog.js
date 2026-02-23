export const CURRENT_VERSION = '2.0';

export const changelog = [
  {
    version: '2.0',
    date: 'february 23, 2026',
    title: 'transcription, encryption, and fixes',
    image: '/updates/v2.png',
    summary: `yoink can now transcribe your videos and audio files into text, subtitles, or burned-in captions. all files on the server are encrypted at rest, gallery-dl auto-fallback for stuff yt-dlp can't handle, and a ton of fixes across the board.`,
    content: `big update. yoink can now transcribe your videos and audio files, everything on the server is encrypted, and a bunch of stuff got cleaned up.

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
