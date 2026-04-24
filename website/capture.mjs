import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import puppeteer from "puppeteer";
import { createServer } from "vite";

const rootDir = path.dirname(fileURLToPath(import.meta.url));
const outputDir = path.join(rootDir, ".captures");

const viewports = [
  {
    name: "desktop",
    width: 1440,
    height: 1500,
    deviceScaleFactor: 1,
    isMobile: false,
  },
  {
    name: "mobile",
    width: 430,
    height: 2200,
    deviceScaleFactor: 2,
    isMobile: true,
    hasTouch: true,
  },
];

async function capturePage(browser, baseURL, viewport) {
  const page = await browser.newPage();
  await page.emulateMediaFeatures([{ name: "prefers-reduced-motion", value: "reduce" }]);
  await page.setViewport(viewport);
  await page.goto(baseURL, { waitUntil: "networkidle0" });
  await page.waitForFunction(() => document.readyState === "complete");
  await page.waitForSelector(".hero");
  await page.waitForSelector(".glass-card");
  await page.evaluate(async () => {
    if (document.fonts?.ready) {
      try {
        await document.fonts.ready;
      } catch {
        // Continue when remote fonts are unavailable.
      }
    }

    window.scrollTo(0, 0);
  });

  const outputPath = path.join(outputDir, `${viewport.name}.png`);
  await page.screenshot({
    fullPage: true,
    path: outputPath,
  });
  await page.close();
  return outputPath;
}

async function main() {
  await mkdir(outputDir, { recursive: true });

  const server = await createServer({
    root: rootDir,
    logLevel: "error",
    server: {
      host: "127.0.0.1",
      port: 4173,
      strictPort: false,
    },
  });

  await server.listen();

  const resolvedURLs = server.resolvedUrls?.local || [];
  const baseURL = resolvedURLs[0] || "http://127.0.0.1:4173/";
  const browser = await puppeteer.launch({
    headless: true,
    args: process.platform === "linux" ? ["--no-sandbox"] : [],
  });

  try {
    const outputs = [];
    for (const viewport of viewports) {
      outputs.push(await capturePage(browser, baseURL, viewport));
    }

    for (const outputPath of outputs) {
      console.log(path.relative(rootDir, outputPath));
    }
  } finally {
    await browser.close();
    await server.close();
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
