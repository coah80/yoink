# yoink.tools

## codebase
- local: `/Users/cole/Documents/yoink`
- server: `/home/cole/yoink` (user `cole`, ip `162.192.96.49`)

## package manager
npm everywhere. `package-lock.json` in both `api/` and `frontend/`.

## env vars (api/.env)
- `PORT` — default 3001
- `BOT_SECRET` — discord bot auth
- `COBALT_API_KEY` — cobalt api auth
- `NODE_ENV` — production on server

## server-only config (gitignored, don't recreate)
- `api/.env`
- `api/discord-config.js` — discord webhook urls
- `api/cors-origins.js` — allowed CORS origins
- `api/cookies.txt` — yt-dlp cookies

## deploy

### frontend (cloudflare pages)
auto-deploys from github `main` branch. just push.
```bash
cd frontend && npm run build
git push origin main
```

### backend (rsync to server)
```bash
rsync -avz --delete \
  --exclude='node_modules' --exclude='.env' \
  --exclude='discord-config.js' --exclude='cors-origins.js' \
  --exclude='cookies.txt' \
  api/ cole@162.192.96.49:/home/cole/yoink/api/
```

### restart service
```bash
ssh cole@162.192.96.49 "printf '1532\n' | sudo -S systemctl restart yoink 2>/dev/null"
```

## server details (162.192.96.49)
- 32gb ram, LUKS-encrypted temp storage at `/var/tmp/yoink` (200g sparse file)
- files encrypted at rest, key held in ram only (`/tmp/yoink.key`), new key on every reboot
- file size limit: 8gb
- systemd services: `yoink-crypt.service` (LUKS setup) → `yoink.service` (app)
- cloudflare tunnel (`cloudflared.service`) handles routing
- no git on server, deploy via rsync
- casaos on port 80, docker containers on 25xxx ports

## key tools
- `yt-dlp` — media downloading
- `ffmpeg` / `ffprobe` — conversion/compression
- `gallery-dl` — image gallery downloading
- cobalt api — alternative download backend

## don't edit
- `api/discord-config.js` — server-only
- `api/cors-origins.js` — server-only
- `api/cookies.txt` — server-only

## writing style
write casually, all lowercase, comma-heavy, no semicolons or em dashes. simple vocabulary, straight to the point. no corporate speak, no marketing fluff, no filler words. write like a chill 18 year old who knows what he's talking about. never use "lmk", "utilize", "leverage", "streamline", or "robust".

## verification
after any code changes, run:
```bash
cd frontend && npm run build
cd api && node -e "require('./server.js')" # quick syntax check
```
