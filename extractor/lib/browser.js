import { chromium } from "playwright";

let browser = null;
let context = null;
let openPages = 0;
let totalPages = 0;

const MAX_CONCURRENT = 5;
const RECYCLE_EVERY = 100;

const STEALTH_ARGS = [
  "--disable-blink-features=AutomationControlled",
  "--no-sandbox",
  "--disable-setuid-sandbox",
  "--disable-dev-shm-usage",
];

const UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

const BLOCKED_TYPES = new Set(["image", "stylesheet", "font", "media"]);

async function createContext() {
  if (context) await context.close().catch(() => {});

  context = await browser.newContext({
    userAgent: UA,
    viewport: { width: 1920, height: 1080 },
    locale: "en-US",
  });

  await context.addInitScript(() => {
    Object.defineProperty(navigator, "webdriver", {
      get: () => undefined,
    });
  });
}

export async function launch() {
  browser = await chromium.launch({
    headless: true,
    args: STEALTH_ARGS,
  });
  await createContext();
  return browser.version();
}

export async function shutdown() {
  if (context) await context.close().catch(() => {});
  if (browser) await browser.close().catch(() => {});
  context = null;
  browser = null;
}

export function getOpenPages() {
  return openPages;
}

export async function getPage(opts = {}) {
  if (!browser || !context) throw new Error("browser not launched");
  if (openPages >= MAX_CONCURRENT) throw new Error("too many concurrent pages");

  totalPages++;
  if (totalPages % RECYCLE_EVERY === 0) {
    await createContext();
  }

  const page = await context.newPage();
  openPages++;

  if (!opts.blockMedia === false) {
    await page.route("**/*", (route) => {
      if (BLOCKED_TYPES.has(route.request().resourceType())) {
        return route.abort();
      }
      return route.continue();
    });
  }

  const originalClose = page.close.bind(page);
  page.close = async (...args) => {
    openPages = Math.max(0, openPages - 1);
    return originalClose(...args);
  };

  return page;
}
