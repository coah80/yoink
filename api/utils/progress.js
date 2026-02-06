const activeDownloads = new Map();
const lastStage = new Map();

function registerDownload(downloadId, res) {
  activeDownloads.set(downloadId, res);
}

function unregisterDownload(downloadId) {
  activeDownloads.delete(downloadId);
  lastStage.delete(downloadId);
}

function sendProgress(downloadId, stage, message, progress = null, extra = null) {
  const res = activeDownloads.get(downloadId);
  if (res) {
    const previousStage = lastStage.get(downloadId);
    const stageChanged = previousStage && previousStage !== stage;

    // Reset progress to 0 when stage changes, unless progress is explicitly provided
    if (stageChanged && progress === null) {
      progress = 0;
    }

    lastStage.set(downloadId, stage);

    const data = { stage, message };
    if (progress !== null) data.progress = progress;
    if (extra !== null) Object.assign(data, extra);

    // Note: queueStatus will need to be added by the caller if needed
    res.write(`data: ${JSON.stringify(data)}\n\n`);
  }
  console.log(`[${downloadId}] ${stage}: ${message} ${progress !== null ? `(${progress}%)` : ''}`);
}

function sendProgressWithQueue(downloadId, stage, message, progress = null, extra = null, getQueueStatus) {
  const res = activeDownloads.get(downloadId);
  if (res) {
    const previousStage = lastStage.get(downloadId);
    const stageChanged = previousStage && previousStage !== stage;

    if (stageChanged && progress === null) {
      progress = 0;
    }

    lastStage.set(downloadId, stage);

    const data = { stage, message };
    if (progress !== null) data.progress = progress;
    if (extra !== null) Object.assign(data, extra);
    if (getQueueStatus) data.queueStatus = getQueueStatus();

    res.write(`data: ${JSON.stringify(data)}\n\n`);
  }
  console.log(`[${downloadId}] ${stage}: ${message} ${progress !== null ? `(${progress}%)` : ''}`);
}

module.exports = {
  registerDownload,
  unregisterDownload,
  sendProgress,
  sendProgressWithQueue,
  activeDownloads,
  lastStage
};
