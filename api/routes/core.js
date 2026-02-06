const express = require('express');
const router = express.Router();
const { v4: uuidv4 } = require('uuid');
const { JOB_LIMITS } = require('../config/constants');
const { cleanupJobFiles } = require('../utils/files');
const {
  activeDownloads,
  activeProcesses,
  resumedJobs,
  clientSessions,
  getQueueStatus,
  registerClient,
  updateHeartbeat,
  getClientSession,
  sendProgress
} = require('../services/state');

router.get('/health', (req, res) => {
  res.json({
    status: 'ok',
    version: '1.0.0',
    queue: getQueueStatus()
  });
});

router.post('/api/heartbeat/:clientId', (req, res) => {
  const { clientId } = req.params;

  if (!clientId) {
    return res.status(400).json({ error: 'Client ID required' });
  }

  registerClient(clientId);
  updateHeartbeat(clientId);

  const session = getClientSession(clientId);
  res.json({
    success: true,
    activeJobs: session ? session.activeJobs.size : 0
  });
});

router.post('/api/connect', (req, res) => {
  const clientId = uuidv4();
  registerClient(clientId);
  res.json({ clientId });
});

router.get('/api/queue-status', (req, res) => {
  res.json(getQueueStatus());
});

router.get('/api/limits', (req, res) => {
  res.json({
    limits: JOB_LIMITS,
    maxFileSize: 15 * 1024 * 1024 * 1024,
    maxPlaylistVideos: 1000,
    maxVideoDuration: 4 * 60 * 60
  });
});

router.get('/api/progress/:id', (req, res) => {
  const { id } = req.params;

  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.flushHeaders();

  const resumedJob = resumedJobs.get(id);
  if (resumedJob) {
    res.write(`data: ${JSON.stringify({
      stage: 'resuming',
      message: 'Reconnected! Resuming download...',
      progress: resumedJob.progress || 0
    })}\n\n`);

    resumedJob.clientReconnected = true;
    resumedJob.response = res;
  } else {
    res.write(`data: ${JSON.stringify({ stage: 'connected', message: 'Connected to progress stream' })}\n\n`);
  }

  activeDownloads.set(id, res);

  req.on('close', () => {
    activeDownloads.delete(id);
  });
});

router.post('/api/cancel/:id', (req, res) => {
  const { id } = req.params;

  const processInfo = activeProcesses.get(id);
  if (processInfo) {
    console.log(`[${id}] Cancelling download...`);
    processInfo.cancelled = true;

    if (processInfo.abortController) {
      processInfo.abortController.abort();
    }

    if (processInfo.process) {
      try {
        processInfo.process.kill('SIGTERM');
      } catch (e) {
        console.error(`[${id}] Error killing process:`, e);
      }
    }

    activeProcesses.delete(id);
    sendProgress(id, 'cancelled', 'Download cancelled');

    setTimeout(() => cleanupJobFiles(id), 1000);

    res.json({ success: true, message: 'Download cancelled' });
  } else {
    res.json({ success: false, message: 'Download not found or already completed' });
  }
});

router.post('/api/finish-early/:id', (req, res) => {
  const { id } = req.params;

  const processInfo = activeProcesses.get(id);
  if (processInfo) {
    console.log(`[${id}] Finishing playlist early...`);
    processInfo.finishEarly = true;

    if (processInfo.process) {
      try {
        processInfo.process.kill('SIGTERM');
      } catch (e) {
        console.error(`[${id}] Error stopping current download:`, e);
      }
    }

    sendProgress(id, 'finishing-early', 'Finishing early, packaging downloaded videos...');
    res.json({ success: true, message: 'Finishing early' });
  } else {
    res.json({ success: false, message: 'Download not found or already completed' });
  }
});

module.exports = router;
