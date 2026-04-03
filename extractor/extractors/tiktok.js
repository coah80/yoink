import { getPage } from "../lib/browser.js";
import { writeFile, mkdir } from "fs/promises";
import { join } from "path";
import { randomUUID } from "crypto";

const TEMP_DIR = process.env.EXTRACTOR_TEMP || "/tmp/yoink-extractor";

async function ensureTempDir() {
  await mkdir(TEMP_DIR, { recursive: true });
}

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
              metadata = {
                title: item.desc || "",
                author: item.author?.uniqueId || "",
                duration: item.video?.duration || 0,
                audioUrl: item.music?.playUrl || null,
                thumbnail: item.video?.cover || null,
              };
              if (item.imagePost?.images?.length > 0) {
                metadata.type = "slideshow";
                metadata.images = item.imagePost.images
                  .map((img) => img.imageURL?.urlList?.[0])
                  .filter(Boolean);
              }
            }
          } catch {}
        }

        if (respUrl.includes("/api/item/detail") && resp.status() === 200) {
          try {
            const data = JSON.parse(await resp.text());
            const item = data.itemInfo?.itemStruct;
            if (item) {
              metadata = {
                title: item.desc || "",
                author: item.author?.uniqueId || "",
                duration: item.video?.duration || 0,
                audioUrl: item.music?.playUrl || null,
                thumbnail: item.video?.cover || null,
              };
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
            metadata = {
              title: item.desc || "",
              author: item.author?.uniqueId || "",
              duration: item.video?.duration || 0,
              audioUrl: item.music?.playUrl || null,
              thumbnail: item.video?.cover || null,
            };
          }
        } catch (e) {
          if (e.message === "video not found") throw e;
        }
      }
    }

    if (!videoBuffer) {
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
