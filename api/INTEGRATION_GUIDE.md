# Integration Guide: Using Modular Services in server.js

This guide shows how to integrate the new modular download services into your existing server.js.

## Step 1: Load Download Services Conditionally

Add this code near the top of server.js (after require statements):

```javascript
// ============= MODULAR DOWNLOAD SERVICES =============
// These services are OPTIONAL and can be deleted independently

let cobaltService = null;
let ytdlpService = null;

try {
  cobaltService = require('./services/cobaltDownloading');
  console.log('[Server] ✓ Cobalt download service loaded');
} catch (e) {
  console.log('[Server] ✗ Cobalt download service not available');
}

try {
  ytdlpService = require('./services/ytdlpDownloading');
  console.log('[Server] ✓ yt-dlp download service loaded');
} catch (e) {
  console.log('[Server] ✗ yt-dlp download service not available');
}

// Verify at least one download service is available
if (!cobaltService && !ytdlpService) {
  console.error('[Server] ERROR: No download services available!');
  console.error('[Server] Please ensure at least one of these files exists:');
  console.error('[Server]   - services/cobaltDownloading.js (for YouTube via Cobalt)');
  console.error('[Server]   - services/ytdlpDownloading.js (for all sites via yt-dlp)');
  process.exit(1);
}
```

## Step 2: Replace Inline Cobalt Functions

Find these functions in server.js and **delete them** (they're now in cobaltDownloading.js):
- `async function fetchMetadataViaCobalt(videoUrl)`
- `async function downloadViaCobalt(...)`

Replace calls to these functions with:

```javascript
// OLD:
const metadata = await fetchMetadataViaCobalt(url);

// NEW:
if (!cobaltService) {
  throw new Error('Cobalt service not available');
}
const metadata = await cobaltService.fetchMetadataViaCobalt(url);

// OR with fallback:
let metadata;
if (cobaltService) {
  metadata = await cobaltService.fetchMetadataViaCobalt(url);
} else if (ytdlpService) {
  metadata = await ytdlpService.fetchMetadataViaYtDlp(url, false);
} else {
  throw new Error('No download services available');
}
```

## Step 3: Create Download Function Wrapper

Add this helper function to simplify download logic:

```javascript
/**
 * Download a video using the appropriate service (Cobalt or yt-dlp)
 * Automatically selects the best service based on URL and availability
 */
async function downloadVideo(url, jobId, options) {
  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');

  // Try Cobalt first for YouTube if available
  if (isYouTube && cobaltService) {
    try {
      console.log(`[${jobId}] Using Cobalt for YouTube download`);
      return await cobaltService.downloadViaCobalt(
        url,
        jobId,
        options.isAudio || false,
        options.progressCallback || null,
        options.abortSignal || null,
        {
          outputDir: options.outputDir || TEMP_DIRS.download,
          maxRetries: 3,
          retryDelay: 2000
        }
      );
    } catch (cobaltErr) {
      console.log(`[${jobId}] Cobalt failed, falling back to yt-dlp:`, cobaltErr.message);

      // If Cobalt failed and we don't have yt-dlp, re-throw error
      if (!ytdlpService) {
        throw cobaltErr;
      }

      // Fall through to yt-dlp
    }
  }

  // Use yt-dlp for non-YouTube or as fallback
  if (ytdlpService) {
    console.log(`[${jobId}] Using yt-dlp for download`);

    const tempFile = path.join(
      options.outputDir || TEMP_DIRS.download,
      `${jobId}.%(ext)s`
    );

    await ytdlpService.downloadViaYtDlp(url, jobId, {
      outputPath: tempFile,
      isAudio: options.isAudio || false,
      quality: options.quality || '1080p',
      container: options.container || 'mp4',
      qualityHeight: QUALITY_HEIGHT,
      onProgress: options.progressCallback || null,
      processInfo: options.processInfo || null,
      request: options.request || null
    });

    // Find the downloaded file
    const files = fs.readdirSync(options.outputDir || TEMP_DIRS.download);
    const downloadedFile = files.find(f =>
      f.startsWith(jobId) &&
      !f.endsWith('.part') &&
      !f.includes('.part-Frag')
    );

    if (!downloadedFile) {
      throw new Error('Downloaded file not found');
    }

    const filePath = path.join(options.outputDir || TEMP_DIRS.download, downloadedFile);
    const ext = path.extname(downloadedFile).slice(1);

    return { filePath, ext };
  }

  throw new Error('No download services available');
}
```

## Step 4: Replace Download Logic

Find download code like this:

```javascript
// OLD INLINE CODE (delete this):
if (isYouTube && !downloadPlaylist) {
  try {
    const cobaltResult = await downloadViaCobalt(url, downloadId, isAudio, ...);
    downloadedPath = cobaltResult.filePath;
    downloadedExt = cobaltResult.ext;
  } catch (cobaltErr) {
    // fallback to yt-dlp...
    const ytdlp = spawn('yt-dlp', ytdlpArgs);
    // ... lots of code ...
  }
} else {
  // yt-dlp inline code...
  const ytdlp = spawn('yt-dlp', ytdlpArgs);
  // ... lots of code ...
}
```

Replace with:

```javascript
// NEW MODULAR CODE:
try {
  const result = await downloadVideo(url, downloadId, {
    outputDir: TEMP_DIRS.download,
    isAudio,
    quality,
    container,
    progressCallback: (progress) => {
      sendProgress(downloadId, 'downloading', `Downloading... ${progress.toFixed(0)}%`, progress);
    },
    processInfo,
    request: req
  });

  downloadedPath = result.filePath;
  downloadedExt = result.ext;

} catch (err) {
  console.error(`[${downloadId}] Download failed:`, err.message);
  throw err;
}
```

## Step 5: Update Analytics (Already done!)

Analytics is already modular. Just change:

```javascript
// OLD:
const { trackDownload, trackConvert } = require('./services/analytics');

// NEW:
const analytics = require('./services/analyticsOptional');

// Usage stays the same:
analytics.trackDownload(format, site, country, trackingId);
```

The `analyticsOptional.js` wrapper automatically handles missing analytics.js.

## Step 6: Testing

1. **Test with all modules**:
   ```bash
   npm start
   # Should see: "Cobalt download service loaded" and "yt-dlp download service loaded"
   # Test YouTube and non-YouTube downloads
   ```

2. **Test without Cobalt**:
   ```bash
   mv services/cobaltDownloading.js services/cobaltDownloading.js.disabled
   npm start
   # Should see: "Cobalt download service not available"
   # Test YouTube downloads (should use yt-dlp)
   ```

3. **Test without yt-dlp**:
   ```bash
   mv services/cobaltDownloading.js.disabled services/cobaltDownloading.js
   mv services/ytdlpDownloading.js services/ytdlpDownloading.js.disabled
   npm start
   # Should see: "yt-dlp download service not available"
   # Test YouTube downloads (should use Cobalt)
   # Non-YouTube downloads should fail gracefully
   ```

4. **Test without analytics**:
   ```bash
   mv services/analytics.js services/analytics.js.disabled
   npm start
   # Should see: "Analytics module not found - tracking disabled"
   # All features should work, just no tracking
   ```

## Full Example: Metadata Endpoint

Here's a complete example of updating the metadata endpoint:

```javascript
app.get('/api/metadata', async (req, res) => {
  const { url, playlist } = req.query;
  const downloadPlaylist = playlist === 'true';

  const urlCheck = validateUrl(url);
  if (!urlCheck.valid) {
    return res.status(400).json({ error: urlCheck.error });
  }

  const isYouTube = url.includes('youtube.com') || url.includes('youtu.be');

  // Try Cobalt for YouTube if available
  if (isYouTube && !downloadPlaylist && cobaltService) {
    try {
      const metadata = await cobaltService.fetchMetadataViaCobalt(url);
      return res.json({ ...metadata, usingCookies: false });
    } catch (cobaltErr) {
      console.error('[Metadata] Cobalt failed:', cobaltErr.message);
      // Fall through to yt-dlp
    }
  }

  // Use yt-dlp as fallback or for non-YouTube
  if (ytdlpService) {
    try {
      const metadata = await ytdlpService.fetchMetadataViaYtDlp(url, downloadPlaylist);
      return res.json(metadata);
    } catch (ytdlpErr) {
      console.error('[Metadata] yt-dlp failed:', ytdlpErr.message);
      return res.status(500).json({ error: 'Failed to fetch metadata' });
    }
  }

  return res.status(503).json({ error: 'No download services available' });
});
```

## Benefits of This Approach

✅ **Truly modular** - Delete cobaltDownloading.js or ytdlpDownloading.js without breaking the server
✅ **Automatic fallbacks** - If Cobalt fails, automatically tries yt-dlp
✅ **Graceful degradation** - Missing services are detected and handled cleanly
✅ **No code duplication** - Download logic is centralized in service modules
✅ **Easy testing** - Rename files to .disabled to test different configurations
✅ **Better organization** - Download logic is separate from route handling

## Next Steps

1. Replace inline Cobalt functions with calls to cobaltService
2. Replace inline yt-dlp spawn code with calls to ytdlpService
3. Test all combinations of available services
4. Update routes to use the downloadVideo() wrapper function
5. Clean up unused inline code

The modular structure is ready to use - just integrate it into your routes!
