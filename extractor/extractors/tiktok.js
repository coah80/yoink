import { getPage } from "../lib/browser.js";
import { writeFile, mkdir, readdir, stat, unlink } from "fs/promises";
import { join } from "path";
import { randomUUID } from "crypto";

const TEMP_DIR = process.env.EXTRACTOR_TEMP || "/tmp/yoink-extractor";
const STALE_TEMP_MS = 30 * 60 * 1000;

function metadataFromItem(item) {
  const metadata = {
    title: item.desc || "",
    author: item.author?.uniqueId || "",
    duration: item.video?.duration || item.music?.duration || 0,
    audioUrl: item.music?.playUrl || null,
    thumbnail: item.video?.cover || item.imagePost?.cover?.imageURL?.urlList?.[0] || null,
  };
  if (item.imagePost?.images?.length > 0) {
    metadata.type = "slideshow";
    metadata.images = item.imagePost.images
      .map((img) => img.imageURL?.urlList?.[0])
      .filter(Boolean);
    metadata.thumbnail = metadata.thumbnail || metadata.images[0] || null;
  }
  return metadata;
}

async function ensureTempDir() {
  await mkdir(TEMP_DIR, { recursive: true });
}

async function cleanupStaleTempFiles() {
  await ensureTempDir();
  const now = Date.now();
  const entries = await readdir(TEMP_DIR).catch(() => []);
  await Promise.all(
    entries
      .filter((name) => name.startsWith("tiktok-") && name.endsWith(".mp4"))
      .map(async (name) => {
        const filePath = join(TEMP_DIR, name);
        try {
          const info = await stat(filePath);
          if (now - info.mtimeMs > STALE_TEMP_MS) {
            await unlink(filePath);
          }
        } catch {}
      })
  );
}

cleanupStaleTempFiles().catch(() => {});
setInterval(() => cleanupStaleTempFiles().catch(() => {}), 5 * 60 * 1000).unref();

export async function extractVideo(url) {
  await ensureTempDir();
  const page = await getPage({ blockMedia: false });
  try {
    let videoBuffer = null;
    let videoUrl = null;
    let metadata = null;

    page.on("response", async (resp) => {
      const respUrl = resp.url();

      if (!metadata) {
        const isItemApi =
          respUrl.includes("/api/post/item_list") ||
          respUrl.includes("/api/recommend/item_list") ||
          respUrl.includes("/api/related/item_list");
        if (isItemApi && resp.status() === 200) {
          try {
            const data = JSON.parse(await resp.text());
            const item = data.itemList?.[0];
            if (item) {
              metadata = metadataFromItem(item);
            }
          } catch {}
        }

        if (respUrl.includes("/api/item/detail") && resp.status() === 200) {
          try {
            const data = JSON.parse(await resp.text());
            const item = data.itemInfo?.itemStruct;
            if (item) {
              metadata = metadataFromItem(item);
            }
          } catch {}
        }
      }

      if (!videoBuffer) {
        const ct = resp.headers()["content-type"] || "";
        if (
          (ct.includes("video/mp4") || respUrl.includes("video/tos")) &&
          resp.status() === 200 &&
          !respUrl.includes("playback1.mp4")
        ) {
          try {
            const body = await resp.body();
            if (body.length > 50000) {
              videoBuffer = body;
              videoUrl = respUrl;
            }
          } catch {}
        }
      }
    });

    await page.goto(url, { waitUntil: "networkidle", timeout: 25000 });
    await page.waitForTimeout(3000);

    if (!videoBuffer) {
      const rehydration = await page.evaluate(() => {
        const el = document.getElementById("__UNIVERSAL_DATA_FOR_REHYDRATION__");
        return el ? el.textContent : null;
      });
      if (rehydration) {
        try {
          const parsed = JSON.parse(rehydration);
          const detail = parsed?.["__DEFAULT_SCOPE__"]?.["webapp.video-detail"];
          if (detail?.statusCode === 10204) throw new Error("video not found");
          const item = detail?.itemInfo?.itemStruct;
          if (item && !metadata) {
            metadata = metadataFromItem(item);
          }
        } catch (e) {
          if (e.message === "video not found") throw e;
        }
      }
    }

    if (!videoBuffer) {
      if (metadata?.type === "slideshow" && metadata.images?.length > 0) {
        return {
          type: "slideshow",
          filePath: "",
          fileSize: 0,
          title: metadata.title || "",
          author: metadata.author || "",
          duration: metadata.duration || 0,
          audioUrl: metadata.audioUrl || null,
          thumbnail: metadata.thumbnail || metadata.images[0] || null,
          images: metadata.images || [],
        };
      }
      throw new Error("could not capture video stream from TikTok");
    }

    const filename = `tiktok-${randomUUID()}.mp4`;
    const filePath = join(TEMP_DIR, filename);
    await writeFile(filePath, videoBuffer);

    return {
      type: metadata?.type || "video",
      filePath,
      fileSize: videoBuffer.length,
      title: metadata?.title || "",
      author: metadata?.author || "",
      duration: metadata?.duration || 0,
      audioUrl: metadata?.audioUrl || null,
      thumbnail: metadata?.thumbnail || null,
      images: metadata?.images || [],
    };
  } finally {
    await page.close();
  }
}

export async function extractMusic(url) {
  const page = await getPage({ blockMedia: false });
  try {
    let musicData = null;

    page.on("response", async (resp) => {
      if (musicData) return;
      if (!resp.url().includes("/api/music/detail") || resp.status() !== 200)
        return;
      try {
        const data = JSON.parse(await resp.text());
        const music = data.musicInfo?.music;
        if (music?.playUrl) {
          musicData = {
            audioUrl: music.playUrl,
            title: music.title || "",
            author: music.authorName || "",
            duration: music.duration || 0,
            thumbnail: music.coverLarge || music.coverMedium || null,
          };
        }
      } catch {}
    });

    await page.goto(url, { waitUntil: "networkidle", timeout: 20000 });

    if (!musicData)
      throw new Error("could not extract music data from TikTok");
    return musicData;
  } finally {
    await page.close();
  }
}
