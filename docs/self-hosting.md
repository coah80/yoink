# self-hosting

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
node server.js
```

access at `http://localhost:3001`

## configuration

all config files go in the `api/` folder:

### cookies.txt (optional)
for authenticated downloads (youtube login, age-restricted content, etc.)

export from your browser using a cookie extension, save as `cookies.txt`

otherwise, youtube may block you from downloads. so will other sites.

### cors-origins.js (optional)
restrict which domains can make requests to your api

```js
module.exports = [
  "https://yourdomain.com"
];
```

if this file doesn't exist, all origins are allowed (fine for local use)

### admin-config.js (optional)
enable the admin dashboard for analytics

```js
module.exports = {
  ADMIN_PASSWORD: 'your-password-here',
  ADMIN_TOKEN_SECRET: 'random-secret-string'
};
```

**security:** generate strong random values for production:
```bash
openssl rand -hex 32  # use output for ADMIN_TOKEN_SECRET
openssl rand -base64 24  # use output for ADMIN_PASSWORD
```

do not commit these values to source control. consider using environment variables or a secrets manager instead.

access at `/admin`

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
