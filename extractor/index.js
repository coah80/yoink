import express from "express";
import { launch, shutdown, getOpenPages } from "./lib/browser.js";
import { extractVideo, extractMusic } from "./extractors/tiktok.js";
import { extractMedia } from "./extractors/instagram.js";

const PORT = process.env.PORT || 3099;
const REQUEST_TIMEOUT = 30000;
const app = express();
const startTime = Date.now();

function withTimeout(fn) {
  return async (req, res) => {
    const timer = setTimeout(() => {
      if (!res.headersSent) {
        res.status(504).json({ success: false, error: "request timeout" });
      }
    }, REQUEST_TIMEOUT);

    try {
      await fn(req, res);
    } catch (err) {
      if (!res.headersSent) {
        res
          .status(500)
          .json({ success: false, error: err.message || "unknown error" });
      }
    } finally {
      clearTimeout(timer);
    }
  };
}

function requireUrl(req, res) {
  const url = req.query.url;
  if (!url) {
    res.status(400).json({ success: false, error: "url parameter required" });
    return null;
  }
  return url;
}

app.get("/health", (_req, res) => {
  res.json({
    status: "ok",
    uptime: Math.floor((Date.now() - startTime) / 1000),
    openPages: getOpenPages(),
  });
});

app.get(
  "/extract/tiktok",
  withTimeout(async (req, res) => {
    const url = requireUrl(req, res);
    if (!url) return;
    const data = await extractVideo(url);
    res.json({ success: true, data });
  })
);

app.get(
  "/extract/tiktok/music",
  withTimeout(async (req, res) => {
    const url = requireUrl(req, res);
    if (!url) return;
    const data = await extractMusic(url);
    res.json({ success: true, data });
  })
);

app.get(
  "/extract/instagram",
  withTimeout(async (req, res) => {
    const url = requireUrl(req, res);
    if (!url) return;
    const data = await extractMedia(url);
    res.json({ success: true, data });
  })
);

let server;

async function start() {
  const version = await launch();
  server = app.listen(PORT, () => {
    console.log(`extractor running on port ${PORT}, chromium ${version}`);
  });
}

async function stop() {
  console.log("shutting down...");
  await shutdown();
  if (server) server.close();
  process.exit(0);
}

process.on("SIGTERM", stop);
process.on("SIGINT", stop);

start().catch((err) => {
  console.error("failed to start:", err.message);
  process.exit(1);
});
