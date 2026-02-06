require('dotenv').config();
const { createApp } = require('./app');
const { PORT } = require('./config/constants');
const { clearTempDir, startCleanupInterval } = require('./utils/files');
const { startRateLimitCleanup } = require('./middleware/rateLimit');
const { checkDependencies } = require('./utils/dependencies');

clearTempDir();

checkDependencies();

const app = createApp();

startCleanupInterval();
startRateLimitCleanup();

const server = app.listen(PORT, () => {
  console.log(`
╔════════════════════════════════════════╗
║              YOINK SERVER              ║
╠════════════════════════════════════════╣
║  Status:  ✓ Running                    ║
║  Port:    ${String(PORT).padEnd(27)}║
║  Mode:    ${(process.env.NODE_ENV || 'development').padEnd(27)}║
╚════════════════════════════════════════╝
  `);
});

process.on('SIGTERM', () => gracefulShutdown('SIGTERM'));
process.on('SIGINT', () => gracefulShutdown('SIGINT'));

let isShuttingDown = false;

async function gracefulShutdown(signal) {
  if (isShuttingDown) return;
  isShuttingDown = true;
  console.log(`\n[Shutdown] Received ${signal}, gracefully stopping...`);
  
  server.close(() => {
    console.log('[Shutdown] HTTP server closed');
    process.exit(0);
  });

  setTimeout(() => {
    console.log('[Shutdown] Forced exit after timeout');
    process.exit(1);
  }, 10000);
}
