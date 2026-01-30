const express = require('express');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

const app = express();
const PORT = process.env.PORT || 3000;

app.use(express.json({ limit: '500mb' }));
app.use(express.static(path.join(__dirname, 'public')));

function sanitizeFilename(filename) {
  return filename
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, '_')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200);
}

const QUALITY_FORMATS = {
  'best': 'bestvideo+bestaudio/best',
  '4k': 'bestvideo[height<=2160]+bestaudio/best[height<=2160]',
  '1440p': 'bestvideo[height<=1440]+bestaudio/best[height<=1440]',
  '1080p': 'bestvideo[height<=1080]+bestaudio/best[height<=1080]',
  '720p': 'bestvideo[height<=720]+bestaudio/best[height<=720]',
  '480p': 'bestvideo[height<=480]+bestaudio/best[height<=480]',
  '360p': 'bestvideo[height<=360]+bestaudio/best[height<=360]'
};

const CODEC_FORMATS = {
  'h264': '[vcodec^=avc]',
  'av1': '[vcodec^=av01]',
  'vp9': '[vcodec^=vp9]'
};

const CONTAINER_MIMES = {
  'mp4': 'video/mp4',
  'webm': 'video/webm',
  'mkv': 'video/x-matroska'
};

const AUDIO_MIMES = {
  'mp3': 'audio/mpeg',
  'm4a': 'audio/mp4',
  'opus': 'audio/opus',
  'flac': 'audio/flac',
  'wav': 'audio/wav'
};

function buildFormatString(quality, codec, isAudio) {
  if (isAudio) {
    return 'bestaudio/best';
  }

  let formatStr = QUALITY_FORMATS[quality] || QUALITY_FORMATS['1080p'];
  
  if (codec && CODEC_FORMATS[codec]) {
    const codecPref = CODEC_FORMATS[codec];
    formatStr = formatStr.replace('bestvideo', `bestvideo${codecPref}`);
  }

  return formatStr;
}

app.post('/api/metadata', async (req, res) => {
  const { url } = req.body;

  if (!url) {
    return res.status(400).json({ error: 'URL is required' });
  }

  const args = [
    '--no-playlist',
    '--print', '%(title)s',
    '--print', '%(ext)s',
    '--print', '%(id)s',
    '--print', '%(uploader)s',
    '--print', '%(duration)s',
    '--print', '%(thumbnail)s',
    url
  ];

  const ytdlp = spawn('yt-dlp', args);
  let output = '';
  let errorOutput = '';

  ytdlp.stdout.on('data', (data) => {
    output += data.toString();
  });

  ytdlp.stderr.on('data', (data) => {
    errorOutput += data.toString();
  });

  ytdlp.on('close', (code) => {
    if (code !== 0) {
      console.error('yt-dlp metadata error:', errorOutput);
      return res.status(500).json({ error: 'Failed to fetch metadata', details: errorOutput });
    }

    const lines = output.trim().split('\n');
    const title = lines[0] || 'download';
    const ext = lines[1] || 'mp4';
    const id = lines[2] || '';
    const uploader = lines[3] || '';
    const duration = lines[4] || '';
    const thumbnail = lines[5] || '';

    res.json({ title, ext, id, uploader, duration, thumbnail });
  });

  ytdlp.on('error', (err) => {
    console.error('Failed to spawn yt-dlp:', err);
    res.status(500).json({ error: 'yt-dlp not found. Please install yt-dlp.' });
  });
});

app.get('/api/download', (req, res) => {
  const { 
    url, 
    format = 'video', 
    filename,
    quality = '1080p',
    codec = 'h264',
    container = 'mp4',
    audioFormat = 'mp3',
    audioBitrate = '320'
  } = req.query;

  if (!url) {
    return res.status(400).json({ error: 'URL is required' });
  }

  const isAudio = format === 'audio';

  const args = [
    '--no-playlist',
    '-o', '-',
  ];

  if (isAudio) {
    args.push('-f', 'bestaudio/best');
    args.push('-x');
    args.push('--audio-format', audioFormat);
    args.push('--audio-quality', `${audioBitrate}K`);
  } else {
    const formatStr = buildFormatString(quality, codec, false);
    args.push('-f', formatStr);
    args.push('--merge-output-format', container);
  }

  args.push(url);

  console.log(`Starting download: ${url}`);
  console.log(`  Format: ${format}, Quality: ${quality}, Codec: ${codec}, Container: ${container}`);

  const ytdlp = spawn('yt-dlp', args);

  const safeFilename = sanitizeFilename(filename || 'download');
  const fileExt = isAudio ? audioFormat : container;
  const fullFilename = `${safeFilename}.${fileExt}`;

  const mimeType = isAudio 
    ? (AUDIO_MIMES[audioFormat] || 'audio/mpeg')
    : (CONTAINER_MIMES[container] || 'video/mp4');
  
  res.setHeader('Content-Type', mimeType);
  res.setHeader('Content-Disposition', `attachment; filename="${fullFilename}"; filename*=UTF-8''${encodeURIComponent(fullFilename)}`);
  res.setHeader('Transfer-Encoding', 'chunked');

  ytdlp.stdout.pipe(res);

  ytdlp.stderr.on('data', (data) => {
    console.error('yt-dlp stderr:', data.toString());
  });

  ytdlp.on('error', (err) => {
    console.error('yt-dlp spawn error:', err);
    if (!res.headersSent) {
      res.status(500).json({ error: 'Failed to start download' });
    }
  });

  ytdlp.on('close', (code) => {
    if (code !== 0) {
      console.error(`yt-dlp exited with code ${code}`);
    } else {
      console.log(`Download completed: ${fullFilename}`);
    }
  });

  req.on('close', () => {
    ytdlp.kill('SIGTERM');
    console.log('Client disconnected, killed yt-dlp process');
  });
});

app.get('/settings', (req, res) => {
  res.sendFile(path.join(__dirname, 'public', 'settings.html'));
});

app.get('/convert', (req, res) => {
  res.sendFile(path.join(__dirname, 'public', 'convert.html'));
});

const multer = require('multer');
const upload = multer({ 
  dest: os.tmpdir(),
  limits: { fileSize: 500 * 1024 * 1024 }
});

app.post('/api/convert', upload.single('file'), async (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No file uploaded' });
  }

  const { format = 'mp4' } = req.body;
  const inputPath = req.file.path;
  const originalName = req.file.originalname.replace(/\.[^.]+$/, '');
  const outputPath = path.join(os.tmpdir(), `${originalName}_converted.${format}`);

  console.log(`Converting: ${req.file.originalname} -> ${format}`);

  const args = ['-i', inputPath, '-y'];

  const isAudioOutput = ['mp3', 'm4a', 'wav', 'flac', 'opus', 'ogg'].includes(format);
  
  if (isAudioOutput) {
    args.push('-vn');
    if (format === 'mp3') {
      args.push('-acodec', 'libmp3lame', '-ab', '320k');
    } else if (format === 'm4a') {
      args.push('-acodec', 'aac', '-ab', '256k');
    } else if (format === 'wav') {
      args.push('-acodec', 'pcm_s16le');
    } else if (format === 'flac') {
      args.push('-acodec', 'flac');
    } else if (format === 'opus') {
      args.push('-acodec', 'libopus', '-ab', '128k');
    }
  } else {
    if (format === 'mp4') {
      args.push('-c:v', 'libx264', '-preset', 'fast', '-crf', '23');
      args.push('-c:a', 'aac', '-ab', '192k');
    } else if (format === 'webm') {
      args.push('-c:v', 'libvpx-vp9', '-crf', '30', '-b:v', '0');
      args.push('-c:a', 'libopus', '-ab', '128k');
    } else if (format === 'mkv') {
      args.push('-c:v', 'copy', '-c:a', 'copy');
    }
  }

  args.push(outputPath);

  const ffmpeg = spawn('ffmpeg', args);
  let errorOutput = '';

  ffmpeg.stderr.on('data', (data) => {
    errorOutput += data.toString();
  });

  ffmpeg.on('close', (code) => {
    fs.unlink(inputPath, () => {});

    if (code !== 0) {
      console.error('ffmpeg error:', errorOutput);
      fs.unlink(outputPath, () => {});
      return res.status(500).json({ error: 'Conversion failed', details: errorOutput });
    }

    const outputFilename = `${originalName}.${format}`;
    const mimeType = isAudioOutput 
      ? (AUDIO_MIMES[format] || 'audio/mpeg')
      : (CONTAINER_MIMES[format] || 'video/mp4');

    res.setHeader('Content-Type', mimeType);
    res.setHeader('Content-Disposition', `attachment; filename="${outputFilename}"; filename*=UTF-8''${encodeURIComponent(outputFilename)}`);

    const fileStream = fs.createReadStream(outputPath);
    fileStream.pipe(res);

    fileStream.on('end', () => {
      fs.unlink(outputPath, () => {});
      console.log(`Conversion completed: ${outputFilename}`);
    });

    fileStream.on('error', (err) => {
      console.error('File stream error:', err);
      fs.unlink(outputPath, () => {});
      if (!res.headersSent) {
        res.status(500).json({ error: 'Failed to send file' });
      }
    });
  });

  ffmpeg.on('error', (err) => {
    console.error('ffmpeg spawn error:', err);
    fs.unlink(inputPath, () => {});
    res.status(500).json({ error: 'ffmpeg not found. Please install ffmpeg.' });
  });
});

app.get('/api/health', (req, res) => {
  res.json({ status: 'ok', timestamp: new Date().toISOString() });
});

app.listen(PORT, () => {
  console.log(`🎯 yoink.tools running at http://localhost:${PORT}`);
});
