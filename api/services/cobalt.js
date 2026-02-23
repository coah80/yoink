const fs = require('fs');
const path = require('path');
const { COBALT_API_KEY, COBALT_APIS, TEMP_DIRS } = require('../config/constants');

async function getCobaltDownloadUrl(videoUrl, isAudio = false, options = {}) {
  const { videoQuality = '1080' } = options;
  let lastError = null;

  for (const apiUrl of COBALT_APIS) {
    try {
      console.log(`[Cobalt] Getting URL from: ${apiUrl}`);

      const headers = {
        'Accept': 'application/json',
        'Content-Type': 'application/json'
      };
      if (COBALT_API_KEY) {
        headers['Authorization'] = `Api-Key ${COBALT_API_KEY}`;
      }

      const requestBody = {
        url: videoUrl,
        downloadMode: isAudio ? 'audio' : 'auto',
        filenameStyle: 'basic',
        videoQuality
      };

      const response = await fetch(apiUrl, {
        method: 'POST',
        headers,
        body: JSON.stringify(requestBody)
      });

      if (!response.ok) {
        const errorText = await response.text().catch(() => 'no body');
        try {
          const errorData = JSON.parse(errorText);
          if (errorData.error?.code) {
            throw new Error(errorData.error.code);
          }
        } catch (parseErr) {
          if (parseErr.message && !parseErr.message.includes('JSON')) {
            throw parseErr;
          }
        }
        throw new Error(`HTTP ${response.status}`);
      }

      const data = await response.json();

      if (data.status === 'error') {
        throw new Error(data.error?.code || 'Cobalt error');
      }

      let downloadUrl = null;
      let filename = data.filename || 'download';

      if (data.status === 'tunnel' || data.status === 'redirect') {
        downloadUrl = data.url;
      } else if (data.status === 'picker' && data.picker?.length > 0) {
        downloadUrl = data.picker[0].url;
      }

      if (!downloadUrl) {
        throw new Error('No download URL in response');
      }

      console.log(`[Cobalt] Got ${data.status} URL from ${apiUrl}`);
      return {
        url: downloadUrl,
        filename,
        status: data.status,
        apiUrl
      };

    } catch (err) {
      console.log(`[Cobalt] ${apiUrl} failed: ${err.message}`);
      lastError = err;
    }
  }

  throw lastError || new Error('All Cobalt instances failed');
}

async function fetchMetadataViaCobalt(videoUrl) {
  let lastError = null;

  for (const apiUrl of COBALT_APIS) {
    try {
      console.log(`[Metadata] Trying Cobalt: ${apiUrl}`);

      const headers = {
        'Accept': 'application/json',
        'Content-Type': 'application/json'
      };
      if (COBALT_API_KEY) {
        headers['Authorization'] = `Api-Key ${COBALT_API_KEY}`;
      }

      const response = await fetch(apiUrl, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          url: videoUrl,
          downloadMode: 'auto',
          filenameStyle: 'basic',
          videoQuality: '1080'
        })
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const data = await response.json();

      if (data.status === 'error') {
        throw new Error(data.error?.code || 'Cobalt error');
      }

      const filename = data.filename || 'download';
      const title = filename.replace(/\.[^.]+$/, '') || 'download';
      const ext = filename.match(/\.([^.]+)$/)?.[1] || 'mp4';

      console.log(`[Metadata] Cobalt success via ${apiUrl}`);
      return {
        title,
        ext,
        id: '',
        uploader: '',
        duration: '',
        thumbnail: '',
        isPlaylist: false,
        viaCobalt: true
      };

    } catch (err) {
      console.log(`[Metadata] Cobalt ${apiUrl} failed: ${err.message}`);
      lastError = err;
    }
  }

  throw lastError || new Error('All Cobalt instances failed');
}

async function downloadViaCobalt(videoUrl, jobId, isAudio = false, progressCallback = null, abortSignal = null, options = {}) {
  const { outputDir = TEMP_DIRS.download, maxRetries = 3, retryDelay = 2000 } = options;
  let lastError = null;
  let attemptCount = 0;
  const startTime = Date.now();

  for (let retry = 0; retry < maxRetries; retry++) {
    for (const apiUrl of COBALT_APIS) {
      attemptCount++;
      try {
        if (abortSignal?.aborted) {
          throw new Error('Cancelled');
        }

        console.log(`[Cobalt] [${jobId}] Attempt ${attemptCount} - Trying: ${apiUrl} (retry ${retry + 1}/${maxRetries})`);

        const headers = {
          'Accept': 'application/json',
          'Content-Type': 'application/json'
        };
        if (COBALT_API_KEY) {
          headers['Authorization'] = `Api-Key ${COBALT_API_KEY}`;
        }

        const requestBody = {
          url: videoUrl,
          downloadMode: isAudio ? 'audio' : 'auto',
          filenameStyle: 'basic',
          videoQuality: '1080'
        };

        console.log(`[Cobalt] [${jobId}] Request: (audio=${requestBody.downloadMode === 'audio'})`);

        const response = await fetch(apiUrl, {
          method: 'POST',
          headers,
          body: JSON.stringify(requestBody),
          signal: abortSignal
        });

        if (!response.ok) {
          const errorText = await response.text().catch(() => 'no body');
          console.log(`[Cobalt] [${jobId}] HTTP ${response.status}: ${errorText.substring(0, 200)}`);
          
          try {
            const errorData = JSON.parse(errorText);
            if (errorData.error?.code) {
              throw new Error(errorData.error.code);
            }
          } catch (parseErr) {
            if (parseErr.message && !parseErr.message.includes('JSON')) {
              throw parseErr;
            }
          }
          throw new Error(`HTTP ${response.status}`);
        }

        const data = await response.json();
        console.log(`[Cobalt] [${jobId}] Response status: ${data.status}`);

        if (data.status === 'error') {
          const errorCode = data.error?.code || 'unknown';
          const errorContext = data.error?.context || {};
          console.log(`[Cobalt] [${jobId}] Error response: code=${errorCode}, context=${JSON.stringify(errorContext)}`);
          throw new Error(errorCode);
        }

        let downloadUrl = data.url;
        if (data.status === 'tunnel' || data.status === 'redirect') {
          downloadUrl = data.url;
        } else if (data.status === 'picker' && data.picker?.length > 0) {
          downloadUrl = data.picker[0].url;
          console.log(`[Cobalt] [${jobId}] Picker mode - selected first of ${data.picker.length} options`);
        }

        if (!downloadUrl) {
          console.log(`[Cobalt] [${jobId}] No URL in response (status=${data.status})`);
          throw new Error('No download URL from Cobalt');
        }

        const ext = isAudio ? 'mp3' : 'mp4';
        const outputPath = path.join(outputDir, `${jobId}-cobalt.${ext}`);
        const partPath = outputPath + '.part';

        let startByte = 0;
        if (fs.existsSync(partPath)) {
          const stats = fs.statSync(partPath);
          startByte = stats.size;
          console.log(`[Cobalt] [${jobId}] Resuming from byte ${startByte}`);
        }

        const downloadHeaders = {};
        if (startByte > 0) {
          downloadHeaders['Range'] = `bytes=${startByte}-`;
        }

        console.log(`[Cobalt] [${jobId}] Starting file download...`);
        const fileResponse = await fetch(downloadUrl, { headers: downloadHeaders, signal: abortSignal });

        if (!fileResponse.ok && fileResponse.status !== 206) {
          if (fileResponse.status === 416 && startByte > 0) {
            fs.renameSync(partPath, outputPath);
            console.log(`[Cobalt] [${jobId}] File already complete`);
            return { filePath: outputPath, ext, downloadUrl };
          }
          console.log(`[Cobalt] [${jobId}] File download failed: HTTP ${fileResponse.status}`);
          throw new Error(`File download failed: HTTP ${fileResponse.status}`);
        }

        const contentLength = parseInt(fileResponse.headers.get('content-length') || '0');
        const totalSize = startByte + contentLength;
        console.log(`[Cobalt] [${jobId}] Content-Length: ${contentLength}, Total: ${totalSize}`);

        const writeStream = fs.createWriteStream(partPath, { flags: startByte > 0 ? 'a' : 'w' });
        const reader = fileResponse.body.getReader();

        let downloadedBytes = startByte;
        let lastProgressLog = 0;

        try {
          while (true) {
            if (abortSignal?.aborted) {
              reader.cancel();
              writeStream.destroy();
              throw new Error('Cancelled');
            }

            const { done, value } = await reader.read();
            if (done) break;

            writeStream.write(Buffer.from(value));
            downloadedBytes += value.length;

            if (progressCallback && totalSize > 0) {
              const progress = Math.min(100, Math.round((downloadedBytes / totalSize) * 100));
              progressCallback(progress, downloadedBytes, totalSize);

              if (progress - lastProgressLog >= 25) {
                console.log(`[Cobalt] [${jobId}] Progress: ${progress}% (${downloadedBytes}/${totalSize})`);
                lastProgressLog = progress;
              }
            }
          }
        } catch (readErr) {
          writeStream.destroy();
          console.log(`[Cobalt] [${jobId}] Stream read error: ${readErr.message}`);
          throw readErr;
        }

        writeStream.end();
        await new Promise(resolve => writeStream.on('finish', resolve));

        fs.renameSync(partPath, outputPath);

        const duration = ((Date.now() - startTime) / 1000).toFixed(1);
        const fileSize = fs.statSync(outputPath).size;
        console.log(`[Cobalt] [${jobId}] Success via ${apiUrl} in ${duration}s (${(fileSize / 1024 / 1024).toFixed(1)}MB)`);
        return { filePath: outputPath, ext, downloadUrl };

      } catch (err) {
        console.log(`[Cobalt] [${jobId}] ${apiUrl} failed: ${err.message}`);
        lastError = err;

        if (err.message === 'Cancelled') {
          throw err;
        }
      }
    }

    if (retry < maxRetries - 1) {
      const delay = retryDelay * Math.pow(2, retry);
      console.log(`[Cobalt] [${jobId}] All instances failed, retrying in ${delay}ms...`);
      await new Promise(resolve => setTimeout(resolve, delay));
    }
  }

  const duration = ((Date.now() - startTime) / 1000).toFixed(1);
  console.log(`[Cobalt] [${jobId}] All ${attemptCount} attempts failed after ${duration}s. Last error: ${lastError?.message}`);
  throw lastError || new Error('All Cobalt instances failed');
}

const { spawn } = require('child_process');

async function streamClipFromCobalt(videoUrl, jobId, startTimeSec, endTimeSec, outputPath, progressCallback = null) {
  console.log(`[Cobalt] [${jobId}] Getting stream URL for clip trim...`);
  
  const { url: streamUrl } = await getCobaltDownloadUrl(videoUrl, false);
  
  const duration = endTimeSec - startTimeSec;
  console.log(`[Cobalt] [${jobId}] Trimming ${startTimeSec}s to ${endTimeSec}s (${duration}s)`);
  
  return new Promise((resolve, reject) => {
    const ffmpegArgs = [
      '-accurate_seek',
      '-ss', startTimeSec.toString(),
      '-i', streamUrl,
      '-t', duration.toString(),
      '-c:v', 'libx264',
      '-preset', 'ultrafast', 
      '-crf', '18',
      '-c:a', 'aac',
      '-b:a', '192k',
      '-movflags', '+faststart',
      '-y',
      outputPath
    ];

    const ffmpeg = spawn('ffmpeg', ffmpegArgs);
    let stderr = '';

    ffmpeg.stderr.on('data', (data) => {
      stderr += data.toString();
      if (progressCallback) {
        const timeMatch = stderr.match(/time=(\d+):(\d+):(\d+)/);
        if (timeMatch) {
          const secs = parseInt(timeMatch[1]) * 3600 + parseInt(timeMatch[2]) * 60 + parseInt(timeMatch[3]);
          const progress = Math.min(100, Math.round((secs / duration) * 100));
          progressCallback(progress);
        }
      }
    });

    ffmpeg.on('close', (code) => {
      if (code === 0) {
        const stats = fs.statSync(outputPath);
        console.log(`[Cobalt] [${jobId}] Stream clip complete: ${(stats.size / 1024 / 1024).toFixed(2)}MB`);
        resolve({
          filePath: outputPath,
          ext: path.extname(outputPath).slice(1) || 'mp4'
        });
      } else {
        console.error(`[Cobalt] [${jobId}] ffmpeg stream trim failed: ${stderr.slice(-500)}`);
        reject(new Error('Stream trim failed'));
      }
    });

    ffmpeg.on('error', (err) => {
      reject(new Error(`ffmpeg error: ${err.message}`));
    });
  });
}

module.exports = {
  getCobaltDownloadUrl,
  fetchMetadataViaCobalt,
  downloadViaCobalt,
  streamClipFromCobalt
};
