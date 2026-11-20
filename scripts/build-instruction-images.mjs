// Code-native diagrams and annotations; the original screenshots stay unchanged.
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
const esc = (text) =>
  String(text)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
function lines(text, max) {
  const result = [];
  let line = "";
  for (const word of String(text).split(/\s+/)) {
    if ((line + " " + word).trim().length > max && line) {
      result.push(line);
      line = word;
    } else line = (line + " " + word).trim();
  }
  if (line) result.push(line);
  return result;
}
function label(text, x, y, width, size = 24, color = "#41464e", weight = 400) {
  const wrapped = lines(text, Math.floor(width / (size * 0.53)));
  return `<text x="${x}" y="${y}" fill="${color}" font-size="${size}" font-weight="${weight}">${wrapped.map((line, i) => `<tspan x="${x}" dy="${i ? size * 1.35 : 0}">${esc(line)}</tspan>`).join("")}</text>`;
}
const browser = await chromium.launch();
const page = await browser.newPage({
  viewport: { width: 1200, height: 760 },
  deviceScaleFactor: 1,
});
let count = 0;
for (const guide of content.guides) {
  const service = content.services.find((s) => s.id === guide.service);
  const dir = path.join(root, "public/assets/instructions/v1", guide.id);
  await mkdir(dir, { recursive: true });
  for (const [i, step] of guide.steps.entries()) {
    let visual = "";
    if (step.screen) {
      const bytes = await readFile(
        path.join(root, "public/assets/instructions/source", step.screen),
      );
      // Obtain image dimensions through the browser without altering the image.
      const uri = `data:image/jpeg;base64,${bytes.toString("base64")}`;
      const dim = await page.evaluate(async (src) => {
        const img = new Image();
        img.src = src;
        await img.decode();
        return { w: img.naturalWidth, h: img.naturalHeight };
      }, uri);
      const crop = step.crop || [0, 0, dim.w, dim.h];
      const scale = Math.min(735 / crop[2], 340 / crop[3]);
      const w = crop[2] * scale,
        h = crop[3] * scale,
        x = 38 + (735 - w) / 2,
        y = 242 + (340 - h) / 2;
      const f = step.focus || [0, 0, 1, 1];
      const fx = x + f[0] * w,
        fy = y + f[1] * h,
        fw = f[2] * w,
        fh = f[3] * h;
      visual = `<rect x="28" y="232" width="755" height="366" rx="12" fill="#f3f4f6"/>
        <svg x="${x}" y="${y}" width="${w}" height="${h}" viewBox="${crop.join(" ")}"><image href="${uri}" width="${dim.w}" height="${dim.h}"/></svg>
        <rect x="${fx}" y="${fy}" width="${fw}" height="${fh}" rx="5" fill="#11804f" fill-opacity=".06" stroke="#11804f" stroke-width="4"/>
        <path d="M826 294 Q780 240 ${fx + fw / 2} ${fy + fh / 2}" fill="none" stroke="#11804f" stroke-width="4" marker-end="url(#arrow)"/>
        ${label("Куда смотреть", 824, 268, 330, 25, "#0b6a40", 700)}
        ${step.items.map((item, index) => `<circle cx="836" cy="${329 + index * 88}" r="15" fill="#e6f4ec"/>${label(index + 1, 830, 336 + index * 88, 40, 18, "#0b6a40", 700)}${label(item, 864, 336 + index * 88, 285, 22)}`).join("")}
        ${label(step.screen.startsWith("flowwow") ? "Экран из официальной справки Flowwow Seller" : step.screen.startsWith("site") ? "Интерфейс Фикусина · демонстрационные данные" : "Экран кабинета · 17 ноября 2026", 44, 625, 1080, 18, "#676c75")}`;
    } else {
      visual = `${label("Схема действий", 44, 237, 1060, 22, "#676c75")}
        ${step.items
          .map((item, index) => {
            const y = 260 + index * 116;
            return `<rect x="100" y="${y}" width="1000" height="100" rx="12" fill="${index === 2 ? "#e6f4ec" : "#f3f4f6"}"/>
            <circle cx="148" cy="${y + 50}" r="22" fill="#11804f"/>
            ${label(index + 1, 140, y + 59, 45, 24, "white", 700)}
            ${label(item, 195, y + 61, 840, 27, "#0e1013", 600)}
            ${index < step.items.length - 1 ? `<path d="M600 ${y + 104}v20" stroke="#11804f" stroke-width="3" marker-end="url(#arrow)"/>` : ""}`;
          })
          .join("")}`;
    }
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="760" viewBox="0 0 1200 760"><defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0 0 6 3 0 6" fill="none" stroke="#11804f" stroke-width="1.5"/></marker></defs>
      <rect width="1200" height="760" fill="white"/>
      <g font-family="Arial, sans-serif">
        <circle cx="62" cy="61" r="27" fill="#11804f"/>${label(i + 1, 54, 71, 45, 29, "white", 700)}
        ${label(step.title, 108, 60, 1030, 30, "#0e1013", 700)}
        ${label(`${service.label} · ${guide.title} · ${i + 1} / ${guide.steps.length}`, 108, 111, 1040, 18, "#676c75")}
        ${label(step.text, 44, 152, 1100, 22)}
        ${visual}
        <rect x="28" y="655" width="1144" height="76" rx="12" fill="#e6f4ec"/>
        ${label("Проверка", 48, 687, 150, 19, "#0b6a40", 700)}
        ${label(step.result, 202, 685, 935, 21, "#0b6a40")}
      </g></svg>`;
    await page.setContent(
      `<html><head><style>html,body{margin:0;padding:0;width:1200px;height:760px}</style></head><body>${svg}</body></html>`,
    );
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({
      path: path.join(dir, `${i + 1}.png`),
      animations: "disabled",
    });
    count++;
  }
}
await browser.close();
await writeFile(
  path.join(root, "public/assets/instructions/v1/manifest.json"),
  JSON.stringify({ version: 1, guides: content.guides.length, images: count }) +
    "\n",
);
console.log(`Built ${count} PNG images for ${content.guides.length} guides.`);
