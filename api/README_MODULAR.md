# Yoink API - Modular Architecture

This is a fully modular version of the Yoink API where you can delete non-core features you don't need.

## Quick Start

```bash
npm install
npm start
```

## Modular Structure

```
api/
├── server.js                    [CORE] Main server entry point
├── config/
│   └── constants.js             [CORE] Configuration constants
├── services/
│   ├── cobaltDownloading.js     [OPTIONAL] Cobalt download service (YouTube)
│   ├── ytdlpDownloading.js      [OPTIONAL] yt-dlp download service (all sites)
│   ├── analytics.js             [OPTIONAL] Analytics tracking
│   ├── analyticsOptional.js     [CORE] Analytics wrapper (handles missing analytics.js)
│   └── banner.js                [OPTIONAL] Global banner system
├── utils/
│   ├── helpers.js               [CORE] Utility functions
│   └── progress.js              [OPTIONAL] Progress tracking utilities
├── middleware/
│   └── auth.js                  [OPTIONAL] Admin authentication
└── routes/
    └── (to be extracted)        [CORE] Route handlers
```

## Core vs Optional Files

### ✅ CORE Files (Required - DO NOT DELETE)
- `server.js` - Main entry point
- `config/constants.js` - Configuration
- `services/analyticsOptional.js` - Analytics wrapper (works even if analytics.js is deleted)
- `utils/helpers.js` - Common utilities
- `package.json` - Dependencies

### 🗑️ OPTIONAL Files (Can be safely deleted)

#### Analytics System
**Delete to disable:** `services/analytics.js`

The server will automatically detect if analytics.js is missing and disable all tracking. The analyticsOptional.js wrapper provides no-op functions so nothing breaks.

#### Cobalt Downloads (YouTube)
**Delete to disable:** `services/cobaltDownloading.js`

If you don't want to use Cobalt for YouTube downloads, delete this file. The server will fall back to yt-dlp for all URLs.

Features lost:
- Fast YouTube downloads via Cobalt API
- YouTube metadata via Cobalt

Fallback: yt-dlp will handle YouTube instead

#### yt-dlp Downloads (Non-YouTube sites)
**Delete to disable:** `services/ytdlpDownloading.js`

If you only want Cobalt and don't need support for non-YouTube sites, delete this file.

Features lost:
- Downloads from non-YouTube sites (Twitter, Reddit, etc.)
- Playlist downloads
- YouTube downloads if cobaltDownloading.js is also deleted

Note: If you delete BOTH cobaltDownloading.js and ytdlpDownloading.js, the server won't be able to download anything!

#### Banner System
**Delete to disable:** `services/banner.js`

Removes the global site banner feature from the admin panel.

#### Progress Tracking
**Delete to disable:** `utils/progress.js`

Removes progress percentage calculations. Downloads will still work but without progress updates.

#### Admin Authentication
**Delete to disable:** `middleware/auth.js`

Removes admin panel authentication. Admin routes will be disabled.

## How to Delete Optional Modules

1. **Delete the file** you don't want (e.g., `rm services/analytics.js`)
2. **Restart the server** - it will automatically detect the missing module
3. **No code changes needed** - the server gracefully handles missing optional modules

## Using the Download Services

### In your routes/code:

```javascript
// Try to load download services
let cobalt = null;
let ytdlp = null;

try {
  cobalt = require('./services/cobaltDownloading');
  console.log('[Downloads] Cobalt module loaded');
} catch (e) {
  console.log('[Downloads] Cobalt module not found');
}

try {
  ytdlp = require('./services/ytdlpDownloading');
  console.log('[Downloads] yt-dlp module loaded');
} catch (e) {
  console.log('[Downloads] yt-dlp module not found');
}

// Use them conditionally
if (cobalt && cobalt.shouldUseCobalt(url)) {
  // Use Cobalt for YouTube
  await cobalt.downloadViaCobalt(url, jobId, ...);
} else if (ytdlp) {
  // Use yt-dlp for everything else
  await ytdlp.downloadViaYtDlp(url, jobId, ...);
} else {
  throw new Error('No download services available');
}
```

### Analytics (always works even if module is deleted):

```javascript
const analytics = require('./services/analyticsOptional');

// These calls work even if analytics.js is deleted (they become no-ops)
analytics.trackDownload(format, site, country, trackingId);
analytics.trackConvert(fromFormat, toFormat, trackingId);
analytics.trackCompress(trackingId);

// Check if analytics is actually enabled
if (analytics.isAnalyticsEnabled()) {
  console.log('Analytics is tracking');
} else {
  console.log('Analytics is disabled');
}
```

## Migration from Monolithic server.js

The current `server.js` is a monolithic file with ~6000 lines. The modular structure is set up and ready to use, but the main server.js still contains all the code inline.

To complete the migration:

1. **Current state**: Modular services created, main server.js is still monolithic
2. **To migrate**: Extract route handlers into `routes/` folder
3. **Update imports**: Change server.js to use the new service modules

The download services (cobaltDownloading.js and ytdlpDownloading.js) are ready to use and can be integrated into the server.js file by replacing the inline download logic with calls to these modules.

## Environment Variables

```bash
PORT=3001                    # Server port
COBALT_API_KEY=your-key      # Optional: Cobalt API key
BOT_SECRET=your-secret       # Bot authentication secret
```

## Dependencies

All dependencies are the same as before:
- express
- cors
- multer
- archiver
- cookie-parser
- geoip-lite
- uuid
- dotenv

## Examples

### Example 1: Minimal setup (no analytics, Cobalt only)
```bash
# Delete analytics and yt-dlp
rm services/analytics.js
rm services/ytdlpDownloading.js

# Keep only Cobalt for YouTube downloads
# Server will work but only for YouTube URLs
```

### Example 2: yt-dlp only (no Cobalt, no analytics)
```bash
# Delete analytics and Cobalt
rm services/analytics.js
rm services/cobaltDownloading.js

# Keep only yt-dlp for all downloads
# Server will use yt-dlp for everything including YouTube
```

### Example 3: Full featured (everything enabled)
```bash
# Don't delete anything
# Server has all features: Cobalt, yt-dlp, analytics, banner, etc.
```

## Troubleshooting

**Q: I deleted a module and now the server won't start**
A: Check the error message. You may have deleted a CORE file. Only delete files marked [OPTIONAL] in this README.

**Q: Downloads aren't working after deleting cobaltDownloading.js**
A: Make sure ytdlpDownloading.js still exists. You need at least one download service.

**Q: Analytics is still running after I deleted analytics.js**
A: Restart the server. The analyticsOptional.js wrapper will detect the missing module on next startup.

**Q: How do I re-enable a feature I deleted?**
A: Restore the file from your backup or git history, then restart the server.

## License

Same as main Yoink project
