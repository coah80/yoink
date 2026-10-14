import { apiBase } from './api.js';

const CHUNK_SIZE = 50 * 1024 * 1024;

export async function uploadChunked(file, onProgress) {
  const base = apiBase();
  const totalChunks = Math.ceil(file.size / CHUNK_SIZE);

  const initRes = await fetch(`${base}/api/upload/init`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      fileName: file.name,
      fileSize: file.size,
      totalChunks,
    }),
  });

  if (!initRes.ok) {
    let errMsg = 'Failed to initialize upload';
    try {
      const err = await initRes.json();
      errMsg = err.error || errMsg;
    } catch {
      errMsg = await initRes.text().catch(() => errMsg);
    }
    throw new Error(errMsg);
  }

  const { uploadId } = await initRes.json();

  for (let i = 0; i < totalChunks; i++) {
    const start = i * CHUNK_SIZE;
    const end = Math.min(start + CHUNK_SIZE, file.size);
    const chunk = file.slice(start, end);

    const formData = new FormData();
    formData.append('chunk', chunk);

    const chunkRes = await fetch(`${base}/api/upload/chunk/${uploadId}/${i}`, {
      method: 'POST',
      body: formData,
    });

    if (!chunkRes.ok) {
      let errMsg = `Failed to upload chunk ${i + 1}`;
      try {
        const err = await chunkRes.json();
        errMsg = err.error || errMsg;
      } catch {
        errMsg = await chunkRes.text().catch(() => errMsg);
      }
      throw new Error(errMsg);
    }

    const progress = ((i + 1) / totalChunks) * 100;
    onProgress(progress);
  }

  const completeRes = await fetch(`${base}/api/upload/complete/${uploadId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });

  if (!completeRes.ok) {
    let errMsg = 'Failed to complete upload';
    try {
      const err = await completeRes.json();
      errMsg = err.error || errMsg;
    } catch {
      errMsg = await completeRes.text().catch(() => errMsg);
    }
    throw new Error(errMsg);
  }

  return await completeRes.json();
}
