import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { analyzeCssUsage, checkCssTree } from "../scripts/check-dead-css.mjs";

test("negative browser assertions cannot keep retired CSS alive, while runtime DOM consumers can", () => {
  const result = analyzeCssUsage({
    styles: [{ path: "styles.css", text: ".retired {} .live {} .toggled {}" }],
    sources: [
      { path: "e2e/tests/page.spec.ts", text: "expect(page.locator('.retired')).toHaveCount(0)" },
      { path: "backend/internal/page_test.go", text: "require.Contains(body, `retired`)" },
      {
        path: "frontend/src/Page.tsx",
        text: "<div className='live' />; body.classList.add('toggled')",
      },
    ],
  });
  assert.deepEqual(result, { declared: 3, dead: ["retired"] });
});

test("only selectors count as classes; SVG URLs and quoted attributes do not invent selectors", () => {
  const result = analyzeCssUsage({
    styles: [
      {
        path: "styles.css",
        text: '.live[href="https://example.invalid"] { background: url("data:image/svg+xml,<svg xmlns=\'http://www.w3.org/2000/svg\'/>") }',
      },
    ],
    sources: [{ path: "frontend/src/Page.tsx", text: "<div className='live' />" }],
  });
  assert.deepEqual(result, { declared: 1, dead: [] });
});

test("dynamic status selectors stay live without hiding an entire retired procurement or sync UI", () => {
  const result = analyzeCssUsage({
    styles: [
      {
        path: "styles.css",
        text: ".receipt-posted {} .sales-sync-running {} .sync-queued {} .procurement-reconciliation-matched {} .account-shipment-offer-expired {} .procurement-intro {} .sync-options {}",
      },
    ],
    sources: [],
  });
  assert.deepEqual(result.dead, ["procurement-intro", "sync-options"]);
});

test("tree inspection includes care and product styles outside styles/ and excludes test-only markup", (t) => {
  const root = mkdtempSync(join(tmpdir(), "ficusin-css-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const files = {
    "frontend/index.html": "<main class='shell'></main>",
    "frontend/src/Page.tsx": "<div className='live' />",
    "frontend/src/care/articles.css": ".care-retired {}",
    "frontend/src/product/plant.css": ".live {} .shell {}",
    "frontend/src/tests/Page.test.tsx": "<div className='care-retired' />",
    "backend/internal/runtime.go": "package runtime",
    "public/offline.html": "<main></main>",
  };
  for (const [name, text] of Object.entries(files)) {
    const path = join(root, name);
    mkdirSync(join(path, ".."), { recursive: true });
    writeFileSync(path, text);
  }
  assert.deepEqual(checkCssTree(root), { declared: 3, dead: ["care-retired"] });
});
