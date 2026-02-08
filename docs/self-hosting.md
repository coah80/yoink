# self-hosting

yoink has two parts: a **svelte frontend** (static SPA) and a **node.js API** server. you can self-host both or just the API.

the discord bot is a separate private repo and is not included.

## requirements

- node.js 18+
- yt-dlp
- ffmpeg
- gallery-dl (optional, for image downloads)

### installing dependencies

**yt-dlp:**
```bash
# linux
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp

# mac
brew install yt-dlp
```

**ffmpeg:**
```bash
# linux
sudo apt install ffmpeg

# mac
brew install ffmpeg
```

**gallery-dl (optional):**
```bash
pip install gallery-dl
```

## quick start

```bash
git clone https://github.com/coah80/yoink.git
cd yoink/api
npm install

# copy example configs
cp ../docs/admin-config.example.js admin-config.js
cp ../docs/cors-origins.example.js cors-origins.js
cp ../docs/discord-config.example.js discord-config.js

# start the server
node index.js
```

API runs at `http://localhost:3001`

### building the frontend

```bash
cd frontend
npm install
npm run build
```

the built files go in `frontend/dist/`. serve them with any static host, or put them behind your reverse proxy.

for local dev:
```bash
cd frontend
npm run dev
```

this starts vite at `http://localhost:5173` with hot reload.

## configuration

all config files go in the `api/` folder. example files are in `docs/`. all of these are gitignored so your secrets never get committed.

### admin-config.js (optional)

enables the admin dashboard at `#/admin` with password authentication.

```js
module.exports = {
  ADMIN_PASSWORD: 'your-password-here',
  TOKEN_EXPIRY_MS: 86400000 // 24 hours
};
```

to set up:
1. copy `docs/admin-config.example.js` to `api/admin-config.js`
2. set `ADMIN_PASSWORD` to something secure
3. optionally adjust `TOKEN_EXPIRY_MS` (default: 24 hours)

the admin panel gives you:
- download/convert/compress analytics
- server status (memory, uptime, active jobs)
- banner management (maintenance notices, traffic warnings)
- connected client count and peak users

### cookies.txt (optional)

for authenticated downloads (youtube login, age-restricted content, etc.)

1. install a browser extension like "Get cookies.txt LOCALLY"
2. go to youtube.com and log in
3. export cookies using the extension
4. save as `api/cookies.txt`

without this, youtube will probably rate-limit or block you pretty quickly.

### cors-origins.js (optional)

restrict which domains can make requests to your API.

```js
module.exports = [
  "https://yourdomain.com",
  "https://www.yourdomain.com"
];
```

if this file doesn't exist, all origins are allowed (fine for local use).

### discord-config.js (optional)

get discord webhook alerts when errors occur or downloads fail.

```js
module.exports = {
  WEBHOOK_URL: 'https://discord.com/api/webhooks/your-webhook-url',
  PING_USER_ID: 'your-discord-user-id',
  ENABLED: true
};
```

to set up:
1. create a webhook in your discord server (server settings > integrations > webhooks)
2. paste the webhook URL
3. set your user ID for pings (enable developer mode, right-click yourself, copy ID)

### .env (optional)

environment variables for the API.

```bash
PORT=3001
COBALT_API_KEY=your-cobalt-api-key
BOT_SECRET=your-bot-auth-secret
```

## production deployment

### API server

use pm2 or systemd to keep the API running:

**pm2:**
```bash
cd api
npm install -g pm2
pm2 start index.js --name yoink
pm2 save
pm2 startup
```

**systemd:**
```ini
[Unit]
Description=yoink API
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/path/to/yoink/api
ExecStart=/usr/bin/node index.js
Restart=on-failure
RestartSec=5
Environment=NODE_ENV=production

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable yoink
sudo systemctl start yoink
```

### frontend

the frontend is a static SPA built with vite. after `npm run build`, the `frontend/dist/` folder can be deployed anywhere:

- **cloudflare pages** — connect your github repo, set build command to `cd frontend && npm install && npm run build`, output directory to `frontend/dist`
- **nginx** — serve the dist folder as static files (see nginx config below)
- **any static host** — netlify, vercel, github pages, etc.

### reverse proxy (nginx)

if you're running both the API and frontend on the same server:

```nginx
server {
    listen 443 ssl http2;
    server_name yoink.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/yoink.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yoink.yourdomain.com/privkey.pem;

    # serve the frontend
    root /path/to/yoink/frontend/dist;
    index index.html;

    # SPA fallback — all non-API routes serve index.html
    location / {
        try_files $uri $uri/ /index.html;
    }

    # proxy API requests to the node server
    location /api/ {
        proxy_pass http://localhost:3001;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;

        # SSE support (disable buffering for streaming progress)
        proxy_buffering off;
        proxy_cache off;
    }
}
```

### separate frontend + API hosts

if your frontend is on a different domain (e.g. cloudflare pages for frontend, VPS for API), make sure to:

1. set `cors-origins.js` to allow your frontend domain
2. the frontend's `lib/api.js` has the API base URL configured — by default it auto-detects based on hostname

## security notes

these files are all gitignored and should **never** be committed:
- `api/admin-config.js` — admin password
- `api/cors-origins.js` — CORS whitelist
- `api/discord-config.js` — webhook credentials
- `api/.env` — environment secrets
- `api/cookies.txt` — youtube session cookies
- `api/analytics.json` — analytics data
- `api/banner.json` — current banner state

if you fork the repo, double-check your `.gitignore` before pushing.

## API endpoints

### public
| method | endpoint | description |
|--------|----------|-------------|
| GET | `/api/health` | health check |
| GET | `/api/metadata?url=...` | fetch video metadata |
| GET | `/api/download?url=...&format=...` | download a video (SSE stream) |
| GET | `/api/download-playlist?url=...` | download a playlist as zip (SSE stream) |
| POST | `/api/gallery/download` | download an image gallery |
| POST | `/api/convert` | convert a file between formats |
| POST | `/api/compress` | compress a video to target size |
| GET | `/api/banner` | get current site banner |
| POST | `/api/analytics/track` | track a page view |
| POST | `/api/analytics/delete` | delete user analytics (GDPR) |

### admin (requires auth)
| method | endpoint | description |
|--------|----------|-------------|
| POST | `/api/admin/login` | login with password, get token |
| POST | `/api/admin/logout` | revoke token |
| GET | `/api/admin/verify` | check if token is valid |
| GET | `/api/admin/analytics` | get full analytics data |
| GET | `/api/admin/status` | server status (memory, uptime, jobs) |
| POST | `/api/admin/banner` | set site banner |
| DELETE | `/api/admin/banner` | clear site banner |

### bot (requires BOT_SECRET)
| method | endpoint | description |
|--------|----------|-------------|
| POST | `/api/bot/download` | start async download |
| POST | `/api/bot/download-playlist` | start async playlist download |
| GET | `/api/bot/status/:jobId` | check job status |
| GET | `/api/bot/download/:token` | download completed file |
