const fs = require('fs');
const { v4: uuidv4 } = require('uuid');
const {
  JOB_LIMITS,
  HEARTBEAT_TIMEOUT_MS,
  SESSION_IDLE_TIMEOUT_MS,
  DISK_SPACE_MIN_GB,
  TEMP_DIR
} = require('../config/constants');

const activeDownloads = new Map();
const activeProcesses = new Map();
const pendingJobs = new Map();
const resumedJobs = new Map();
const clientSessions = new Map();
const jobToClient = new Map();
const botDownloads = new Map();
const asyncJobs = new Map();
const uploadSessions = new Map();

const activeJobsByType = {
  download: 0,
  playlist: 0,
  convert: 0,
  compress: 0
};

let galleryDlAvailable = false;
let updatePeakUsersCallback = () => {};

function setGalleryDlAvailable(available) {
  galleryDlAvailable = available;
}

function isGalleryDlAvailable() {
  return galleryDlAvailable;
}

function setUpdatePeakUsersCallback(cb) {
  updatePeakUsersCallback = cb;
}

function registerClient(clientId) {
  if (!clientSessions.has(clientId)) {
    clientSessions.set(clientId, {
      lastHeartbeat: Date.now(),
      lastActivity: Date.now(),
      activeJobs: new Set()
    });
    console.log(`[Session] Client ${clientId.slice(0, 8)}... connected`);
    updatePeakUsersCallback(clientSessions.size);
  } else {
    clientSessions.get(clientId).lastActivity = Date.now();
  }
}

function updateHeartbeat(clientId) {
  const session = clientSessions.get(clientId);
  if (session) {
    session.lastHeartbeat = Date.now();
    return true;
  }
  return false;
}

function getClientSession(clientId) {
  return clientSessions.get(clientId);
}

function linkJobToClient(jobId, clientId) {
  if (clientId && clientSessions.has(clientId)) {
    const session = clientSessions.get(clientId);
    session.activeJobs.add(jobId);
    session.lastActivity = Date.now();
    jobToClient.set(jobId, clientId);
  }
}

function unlinkJobFromClient(jobId) {
  const clientId = jobToClient.get(jobId);
  if (clientId) {
    const session = clientSessions.get(clientId);
    if (session) {
      session.activeJobs.delete(jobId);
      session.lastActivity = Date.now();
    }
    jobToClient.delete(jobId);
  }
}

function getClientJobCount(clientId) {
  const session = clientSessions.get(clientId);
  return session ? session.activeJobs.size : 0;
}

function getDiskSpace() {
  try {
    const stats = fs.statfsSync(TEMP_DIR);
    const availableGB = (stats.bavail * stats.bsize) / (1024 * 1024 * 1024);
    return { availableGB: Math.round(availableGB * 100) / 100 };
  } catch {
    return { availableGB: Infinity };
  }
}

function canStartJob(type) {
  const limit = JOB_LIMITS[type];
  if (limit !== undefined && activeJobsByType[type] >= limit) {
    return { ok: false, reason: `Too many active ${type} jobs (limit: ${limit})` };
  }
  const { availableGB } = getDiskSpace();
  if (availableGB < DISK_SPACE_MIN_GB) {
    return { ok: false, reason: `Low disk space (${availableGB.toFixed(1)}GB free, need ${DISK_SPACE_MIN_GB}GB)` };
  }
  return { ok: true };
}

function getQueueStatus() {
  const { availableGB } = getDiskSpace();
  return {
    active: activeJobsByType,
    queued: 0,
    limits: JOB_LIMITS,
    diskSpaceGB: availableGB
  };
}

const lastLoggedProgress = new Map();

function sendProgress(downloadId, stage, message, progress = null, extra = null) {
  const res = activeDownloads.get(downloadId);
  if (res) {
    const data = { stage, message };
    if (progress !== null) data.progress = progress;
    if (extra !== null) Object.assign(data, extra);
    data.queueStatus = getQueueStatus();
    res.write(`data: ${JSON.stringify(data)}\n\n`);
  }

  const lastProg = lastLoggedProgress.get(downloadId) || -1;
  const isProgressMessage = progress !== null && message.includes('%');

  if (isProgressMessage) {
    if (progress >= 100 || progress - lastProg >= 25) {
      console.log(`[${downloadId.slice(0, 8)}] ${stage}: ${message}`);
      lastLoggedProgress.set(downloadId, progress);
      if (progress >= 100) lastLoggedProgress.delete(downloadId);
    }
  } else {
    console.log(`[${downloadId.slice(0, 8)}] ${stage}: ${message}`);
    lastLoggedProgress.delete(downloadId);
  }
}

function registerPendingJob(jobId, jobData) {
  pendingJobs.set(jobId, {
    ...jobData,
    createdAt: Date.now(),
    resumable: true
  });
}

function updatePendingJob(jobId, updates) {
  const job = pendingJobs.get(jobId);
  if (job) {
    Object.assign(job, updates);
  }
}

function removePendingJob(jobId) {
  pendingJobs.delete(jobId);
}

function startSessionCleanup(cleanupJobFiles) {
  setInterval(() => {
    const now = Date.now();
    for (const [clientId, session] of clientSessions.entries()) {
      const hasActiveJobs = session.activeJobs.size > 0;

      if (hasActiveJobs && now - session.lastHeartbeat > HEARTBEAT_TIMEOUT_MS) {
        console.log(`[Session] Client ${clientId.slice(0, 8)}... heartbeat timeout, cancelling ${session.activeJobs.size} jobs`);

        for (const jobId of session.activeJobs) {
          const asyncJob = asyncJobs.get(jobId);
          if (asyncJob && asyncJob.type === 'playlist') {
            session.activeJobs.delete(jobId);
            jobToClient.delete(jobId);
            continue;
          }
          const processInfo = activeProcesses.get(jobId);
          if (processInfo) {
            processInfo.cancelled = true;
            if (processInfo.process) {
              processInfo.process.kill('SIGTERM');
            }
            sendProgress(jobId, 'cancelled', 'Connection lost - task cancelled');
            activeProcesses.delete(jobId);
          }
          cleanupJobFiles(jobId);
        }

        clientSessions.delete(clientId);
      } else if (!hasActiveJobs && now - session.lastActivity > SESSION_IDLE_TIMEOUT_MS) {
        console.log(`[Session] Client ${clientId.slice(0, 8)}... idle timeout`);
        clientSessions.delete(clientId);
      }
    }
  }, 10000);
}

module.exports = {
  activeDownloads,
  activeProcesses,
  pendingJobs,
  resumedJobs,
  clientSessions,
  jobToClient,
  botDownloads,
  asyncJobs,
  uploadSessions,
  activeJobsByType,

  setGalleryDlAvailable,
  isGalleryDlAvailable,
  setUpdatePeakUsersCallback,
  registerClient,
  updateHeartbeat,
  getClientSession,
  linkJobToClient,
  unlinkJobFromClient,
  getClientJobCount,
  getDiskSpace,
  canStartJob,
  getQueueStatus,
  sendProgress,
  registerPendingJob,
  updatePendingJob,
  removePendingJob,
  startSessionCleanup
};
