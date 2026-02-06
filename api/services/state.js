const { v4: uuidv4 } = require('uuid');
const {
  JOB_LIMITS,
  MAX_QUEUE_SIZE,
  HEARTBEAT_TIMEOUT_MS,
  HEAVY_JOB_TYPES,
  SESSION_IDLE_TIMEOUT_MS
} = require('../config/constants');

const activeDownloads = new Map();
const activeProcesses = new Map();
const pendingJobs = new Map();
const resumedJobs = new Map();
const finishEarlyRequests = new Set();
const clientSessions = new Map();
const jobToClient = new Map();
const jobQueue = [];
const rateLimitStore = new Map();
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

function canStartJob(jobType) {
  if (HEAVY_JOB_TYPES.includes(jobType)) {
    const totalActive = Object.values(activeJobsByType).reduce((a, b) => a + b, 0);
    if (totalActive > 1) return false;
  }
  return activeJobsByType[jobType] < JOB_LIMITS[jobType];
}

function addToJobQueue(jobFn, jobType, jobId, clientId) {
  return new Promise((resolve, reject) => {
    const job = {
      fn: jobFn,
      resolve,
      reject,
      jobType,
      jobId,
      clientId,
      addedAt: Date.now()
    };

    if (jobQueue.length >= MAX_QUEUE_SIZE) {
      reject(new Error('Server is too busy. Please try again later.'));
      return;
    }

    linkJobToClient(jobId, clientId);
    jobQueue.push(job);
    console.log(`[Queue] ${jobType} job ${jobId.slice(0, 8)}... added. Queue: ${jobQueue.length}`);
    processQueue();
  });
}

function processQueue() {
  jobQueue.sort((a, b) => {
    const aIsHeavy = HEAVY_JOB_TYPES.includes(a.jobType) ? 1 : 0;
    const bIsHeavy = HEAVY_JOB_TYPES.includes(b.jobType) ? 1 : 0;
    if (aIsHeavy !== bIsHeavy) return aIsHeavy - bIsHeavy;
    return a.addedAt - b.addedAt;
  });

  for (let i = 0; i < jobQueue.length; i++) {
    const job = jobQueue[i];

    if (canStartJob(job.jobType)) {
      jobQueue.splice(i, 1);
      activeJobsByType[job.jobType]++;

      console.log(`[Queue] Starting ${job.jobType} job ${job.jobId.slice(0, 8)}... Active: ${JSON.stringify(activeJobsByType)}`);

      job.fn()
        .then(result => {
          activeJobsByType[job.jobType]--;
          unlinkJobFromClient(job.jobId);
          job.resolve(result);
          processQueue();
        })
        .catch(err => {
          activeJobsByType[job.jobType]--;
          unlinkJobFromClient(job.jobId);
          job.reject(err);
          processQueue();
        });

      i = -1;
    }
  }
}

function getQueueStatus() {
  return {
    active: activeJobsByType,
    queued: jobQueue.length,
    limits: JOB_LIMITS
  };
}

function sendProgress(downloadId, stage, message, progress = null, extra = null) {
  const res = activeDownloads.get(downloadId);
  if (res) {
    const data = { stage, message };
    if (progress !== null) data.progress = progress;
    if (extra !== null) Object.assign(data, extra);
    data.queueStatus = getQueueStatus();
    res.write(`data: ${JSON.stringify(data)}\n\n`);
  }
  console.log(`[${downloadId}] ${stage}: ${message}`);
}

function sendQueuePosition(progressId) {
  const position = jobQueue.findIndex(j => j.progressId === progressId);
  if (position >= 0) {
    sendProgress(progressId, 'queued', `You are #${position + 1} in queue`, 0, {
      queuePosition: position + 1,
      estimatedWait: (position + 1) * 30
    });
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

function incrementJobType(type) {
  if (activeJobsByType[type] !== undefined) {
    activeJobsByType[type]++;
  }
}

function decrementJobType(type) {
  if (activeJobsByType[type] !== undefined) {
    activeJobsByType[type]--;
  }
}

function startSessionCleanup(cleanupJobFiles) {
  setInterval(() => {
    const now = Date.now();
    for (const [clientId, session] of clientSessions.entries()) {
      const hasActiveJobs = session.activeJobs.size > 0;

      if (hasActiveJobs && now - session.lastHeartbeat > HEARTBEAT_TIMEOUT_MS) {
        console.log(`[Session] Client ${clientId.slice(0, 8)}... heartbeat timeout, cancelling ${session.activeJobs.size} jobs`);

        for (const jobId of session.activeJobs) {
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
  finishEarlyRequests,
  clientSessions,
  jobToClient,
  jobQueue,
  rateLimitStore,
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
  canStartJob,
  addToJobQueue,
  processQueue,
  getQueueStatus,
  sendProgress,
  sendQueuePosition,
  registerPendingJob,
  updatePendingJob,
  removePendingJob,
  incrementJobType,
  decrementJobType,
  startSessionCleanup
};
