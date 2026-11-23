// Draw markers over the redacted CDEK screenshots.
// Run after `npm ci` in e2e and `npx playwright install chromium`.
import { chromium } from "../e2e/node_modules/@playwright/test/index.mjs";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = fileURLToPath(new URL("../", import.meta.url));
const content = JSON.parse(
  await readFile(
    path.join(root, "frontend/src/instructions/content.json"),
    "utf8",
  ),
);
const browser = await chromium.launch();
const page = await browser.newPage({
  viewport: { width: 1200, height: 760 },
  deviceScaleFactor: 1,
});
let count = 0;
for (const guide of content.guides) {
  const dir = path.join(root, "public/assets/instructions/v1", guide.id);
  await mkdir(dir, { recursive: true });
  for (const box of guide.boxLayouts ?? []) {
    await readFile(path.join(dir, box.imageFile));
    count++;
  }
  for (const step of guide.steps) {
    if (step.imageStyle === "text") continue;
    const assets = step.example ? [step, step.example] : [step];
    for (const asset of assets) {
      const bytes = await readFile(
        path.join(root, "scripts/instruction-screens", asset.screen),
      );
      const uri = `data:image/png;base64,${bytes.toString("base64")}`;
      const dim = await page.evaluate(async (src) => {
        const img = new Image();
        img.src = src;
        await img.decode();
        return { w: img.naturalWidth, h: img.naturalHeight };
      }, uri);
      await page.setViewportSize({ width: dim.w, height: dim.h });
      const outlines = asset.outlines
        .map(([x, y, w, h]) => {
          // Two rounded strokes give the outline the uneven edges of a marker.
          const right = x + w;
          const bottom = y + h;
          const midX = x + w / 2;
          const midY = y + h / 2;
          const stroke = `M${x + 32} ${y + 3} Q${midX} ${y - 11} ${right - 29} ${y + 1}
          Q${right + 10} ${y + 1} ${right + 5} ${midY}
          Q${right + 10} ${bottom + 7} ${right - 27} ${bottom + 4}
          Q${midX} ${bottom + 13} ${x + 24} ${bottom + 5}
          Q${x - 11} ${bottom + 2} ${x - 5} ${midY}
          Q${x - 11} ${y - 2} ${x + 47} ${y + 2}`;
          return `<path d="${stroke}" stroke="#ff9b23" stroke-width="34" opacity=".39"/>
                <path d="${stroke}" stroke="#e34624" stroke-width="17" opacity=".9" transform="translate(0 3)"/>`;
        })
        .join("");
      await page.setContent(
        `<html><head><style>html,body{margin:0;padding:0}</style></head><body><svg xmlns="http://www.w3.org/2000/svg" width="${dim.w}" height="${dim.h}"><image href="${uri}" width="${dim.w}" height="${dim.h}"/><g fill="none" stroke-linecap="round" stroke-linejoin="round">${outlines}</g></svg></body></html>`,
      );
      await page.evaluate(async () => {
        await Promise.all(
          Array.from(
            document.querySelectorAll("image"),
            (image) =>
              new Promise((resolve, reject) => {
                const probe = new Image();
                probe.onload = resolve;
                probe.onerror = reject;
                probe.src = image.getAttribute("href");
              }),
          ),
        );
      });
      await page.screenshot({
        path: path.join(dir, asset.imageFile),
        animations: "disabled",
      });
      count++;
    }
  }
}
await browser.close();
await writeFile(
  path.join(root, "public/assets/instructions/v1/manifest.json"),
  JSON.stringify({ version: 1, guides: content.guides.length, images: count }) +
    "\n",
);
console.log(`Built ${count} PNG images for ${content.guides.length} guides.`);
