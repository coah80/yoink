const express = require('express');
const cors = require('cors');
const cookieParser = require('cookie-parser');
const { loadCorsConfig } = require('./middleware/cors');
const { rateLimitMiddleware } = require('./middleware/rateLimit');
const { isGalleryDlAvailable } = require('./utils/dependencies');

const coreRoutes = require('./routes/core');
const downloadRoutes = require('./routes/download');
const playlistRoutes = require('./routes/playlist');
const galleryRoutes = require('./routes/gallery');
const convertRoutes = require('./routes/convert');
const botRoutes = require('./routes/bot');
const adminRoutes = require('./routes/admin');

function createApp() {
  const app = express();
  app.set('trust proxy', true);

  const corsConfig = loadCorsConfig();
  app.use(cors(corsConfig));
  app.use(cookieParser());
  app.use(express.json({ limit: '500mb' }));

  app.use('/api/download', rateLimitMiddleware);
  app.use('/api/download-playlist', rateLimitMiddleware);
  app.use('/api/convert', rateLimitMiddleware);
  app.use('/api/compress', rateLimitMiddleware);

  app.use(coreRoutes);
  app.use(downloadRoutes);
  app.use(playlistRoutes);
  if (isGalleryDlAvailable()) {
    app.use('/api/gallery', galleryRoutes);
  }
  app.use(convertRoutes);
  app.use(botRoutes);
  app.use(adminRoutes);

  return app;
}

module.exports = { createApp };
