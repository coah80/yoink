# self-hosting

the yoink API is fully self-hostable. the discord bot is private and not included.

## requirements

- node.js 18+
- yt-dlp
- ffmpeg
- gallery-dl (optional, for image downloads)

## quick start

```bash
git clone https://github.com/coah80/yoink.git
cd yoink/api
npm install

cp ../docs/cors-origins.example.js cors-origins.js
cp ../docs/admin-config.example.js admin-config.js
cp ../docs/discord-config.example.js discord-config.js

node server.js
```

access at `http://localhost:3001`

## configuration

all config files go in the `api/` folder. example files are in `docs/`.

### cookies.txt (optional)
for authenticated downloads (youtube login, age-restricted content, etc.)

1. install a browser extension like "Get cookies.txt LOCALLY"
2. go to youtube.com and log in
3. export cookies using the extension
4. save as `api/cookies.txt`

without this, youtube will probably block you

### cors-origins.js (optional)
restrict which domains can make requests to your api

```js
module.exports = [
  "https://yourdomain.com"
];
```

if this file doesn't exist, all origins are allowed (fine for local use)

to set up:
1. copy `docs/cors-origins.example.js` to `api/cors-origins.js`
2. replace the example domain with your actual domain
3. add multiple domains if needed (array format)

### admin-config.js (optional)
enable the admin dashboard for analytics

```js
module.exports = {
  ADMIN_PASSWORD: 'your-password-here',
  ADMIN_TOKEN_SECRET: 'random-secret-string'
};
```

to set up:
1. copy `docs/admin-config.example.js` to `api/admin-config.js`
2. set `ADMIN_PASSWORD` to something you'll remember
3. set `ADMIN_TOKEN_SECRET` to a random string (used for session tokens)

generate strong random values:
```bash
openssl rand -base64 24
```

access at `/admin`

### discord-config.js (optional)
get alerts when downloads fail or errors occur

```js
module.exports = {
  WEBHOOK_URL: 'https://discord.com/api/webhooks/your-webhook-url',
  PING_USER_ID: 'your-discord-user-id',
  ENABLED: true
};
```

to set up:
1. copy `docs/discord-config.example.js` to `api/discord-config.js`
2. create a webhook in your discord server (server settings > integrations > webhooks)
3. paste the webhook URL
4. get your user ID (enable developer mode in discord, right-click yourself, copy ID)
5. set `ENABLED` to `true`

## production

for production, you probably want:

1. a reverse proxy (nginx, caddy) for ssl
2. pm2 or systemd to keep it running
3. cors-origins.js configured for your domain

example nginx config:
```nginx
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name yoink.yourdomain.com;
    
    ssl_certificate /etc/letsencrypt/live/yoink.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yoink.yourdomain.com/privkey.pem;
    
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;
    
    location / {
        proxy_pass http://localhost:3001;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }
}
```
