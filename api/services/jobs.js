const {
  JOB_LIMITS,
  MAX_QUEUE_SIZE,
  HEARTBEAT_TIMEOUT_MS,
  HEAVY_JOB_TYPES,
  SESSION_IDLE_TIMEOUT_MS
} = require('../config/constants');

const activeJobsByType = {
  download: 0,
  playlist: 0,
  convert: 0,
  compress: 0
};

const jobQueue = [];
const clientSessions = new Map();
const jobToClient = new Map();
const activeProcesses = new Map();
const pendingJobs = new Map();
const finishEarlyRequests = new Set();
const progressClients = new Map();

let updatePeakUsersCallback = () => {};
let sendProgressCallback = () => {};
let cleanupJobFilesCallback = () => {};

function setCallbacks(callbacks) {
  if (callbacks.updatePeakUsers) updatePeakUsersCallback = callbacks.updatePeakUsers;
  if (callbacks.sendProgress) sendProgressCallback = callbacks.sendProgress;
  if (callbacks.cleanupJobFiles) cleanupJobFilesCallback = callbacks.cleanupJobFiles;
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

function cleanupStaleSessions() {
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
          sendProgressCallback(jobId, 'cancelled', 'Connection lost - task cancelled');
          activeProcesses.delete(jobId);
        }
        cleanupJobFilesCallback(jobId);
      }

      clientSessions.delete(clientId);
    } else if (!hasActiveJobs && now - session.lastActivity > SESSION_IDLE_TIMEOUT_MS) {
      console.log(`[Session] Client ${clientId.slice(0, 8)}... idle timeout`);
      clientSessions.delete(clientId);
    }
  }
}

setInterval(cleanupStaleSessions, 10000);

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

function getActiveProcess(jobId) {
  return activeProcesses.get(jobId);
}

function setActiveProcess(jobId, processInfo) {
  activeProcesses.set(jobId, processInfo);
}

function deleteActiveProcess(jobId) {
  activeProcesses.delete(jobId);
}

function getPendingJob(jobId) {
  return pendingJobs.get(jobId);
}

function setPendingJob(jobId, jobInfo) {
  pendingJobs.set(jobId, jobInfo);
}

function updatePendingJob(jobId, updates) {
  const job = pendingJobs.get(jobId);
  if (job) {
    Object.assign(job, updates);
  }
}

function deletePendingJob(jobId) {
  pendingJobs.delete(jobId);
}

function hasFinishEarlyRequest(jobId) {
  return finishEarlyRequests.has(jobId);
}

function addFinishEarlyRequest(jobId) {
  finishEarlyRequests.add(jobId);
}

function deleteFinishEarlyRequest(jobId) {
  finishEarlyRequests.delete(jobId);
}

function getProgressClient(jobId) {
  return progressClients.get(jobId);
}

function setProgressClient(jobId, res) {
  progressClients.set(jobId, res);
}

function deleteProgressClient(jobId) {
  progressClients.delete(jobId);
}

function getActiveJobsByType() {
  return { ...activeJobsByType };
}

function getClientSessionsCount() {
  return clientSessions.size;
}

module.exports = {
  setCallbacks,
  registerClient,
  updateHeartbeat,
  linkJobToClient,
  unlinkJobFromClient,
  canStartJob,
  addToJobQueue,
  processQueue,
  getQueueStatus,
  getActiveProcess,
  setActiveProcess,
  deleteActiveProcess,
  getPendingJob,
  setPendingJob,
  updatePendingJob,
  deletePendingJob,
  hasFinishEarlyRequest,
  addFinishEarlyRequest,
  deleteFinishEarlyRequest,
  getProgressClient,
  setProgressClient,
  deleteProgressClient,
  getActiveJobsByType,
  getClientSessionsCount
};
