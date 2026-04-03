import { getPage } from "../lib/browser.js";

function extractShortcode(url) {
  const match = url.match(
    /instagram\.com\/(?:p|reel|reels|tv)\/([A-Za-z0-9_-]+)/
  );
  if (!match) throw new Error("could not extract shortcode from URL");
  return match[1];
}

async function tryEmbed(page, shortcode) {
  const embedUrl = `https://www.instagram.com/p/${shortcode}/embed/captioned/`;
  await page.goto(embedUrl, { waitUntil: "domcontentloaded", timeout: 15000 });

  return page.evaluate(() => {
    const video = document.querySelector("video");
    if (video?.src) {
      return { type: "video", urls: [video.src] };
    }

    const img =
      document.querySelector("img.EmbeddedMediaImage") ||
      document.querySelector(".EmbeddedMediaImage");
    if (img?.src) {
      return { type: "image", urls: [img.src] };
    }

    const scripts = document.querySelectorAll("script");
    for (const script of scripts) {
      const text = script.textContent || "";
      if (text.includes("window.__additionalDataLoaded")) {
        const jsonMatch = text.match(
          /window\.__additionalDataLoaded\s*\(\s*['"][^'"]*['"]\s*,\s*({.+?})\s*\)/s
        );
        if (jsonMatch) {
          try {
            const data = JSON.parse(jsonMatch[1]);
            const media = data?.graphql?.shortcode_media || data?.shortcode_media;
            if (media) {
              if (media.edge_sidecar_to_children) {
                const urls = media.edge_sidecar_to_children.edges.map(
                  (e) => e.node.video_url || e.node.display_url
                );
                return { type: "carousel", urls };
              }
              if (media.video_url) {
                return { type: "video", urls: [media.video_url] };
              }
              if (media.display_url) {
                return { type: "image", urls: [media.display_url] };
              }
            }
          } catch {
            // continue
          }
        }
      }
    }

    return null;
  });
}

async function tryOgMeta(page, url) {
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: 15000 });

  return page.evaluate(() => {
    const ogVideo = document.querySelector('meta[property="og:video"]');
    if (ogVideo?.content) {
      return { type: "video", urls: [ogVideo.content] };
    }

    const ogImage = document.querySelector('meta[property="og:image"]');
    if (ogImage?.content) {
      return { type: "image", urls: [ogImage.content] };
    }

    return null;
  });
}

export async function extractMedia(url) {
  const shortcode = extractShortcode(url);
  const page = await getPage();

  try {
    const embedResult = await tryEmbed(page, shortcode).catch(() => null);
    if (embedResult) {
      return {
        ...embedResult,
        title: "",
        author: "",
        thumbnail: embedResult.urls[0] || null,
      };
    }

    const ogResult = await tryOgMeta(page, url).catch(() => null);
    if (ogResult) {
      return {
        ...ogResult,
        title: "",
        author: "",
        thumbnail: ogResult.urls[0] || null,
      };
    }

    throw new Error("could not extract media from Instagram post");
  } finally {
    await page.close();
  }
}
