import { expect, test } from "@playwright/test";

type BrowserMetrics = {
  cls: number;
  imageMs?: number;
  interfaceMs: number;
  overflow: number;
};

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const state = window as typeof window & { __ficusinCLS?: number };
    state.__ficusinCLS = 0;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries() as Array<PerformanceEntry & { hadRecentInput?: boolean; value?: number }>) {
        if (!entry.hadRecentInput) state.__ficusinCLS = (state.__ficusinCLS || 0) + (entry.value || 0);
      }
    }).observe({ type: "layout-shift", buffered: true });
  });
});

test("production catalogue is usable without shifts or failed first-party requests", async ({ page }, testInfo) => {
  const failures: string[] = [];
  page.on("requestfailed", (request) => {
    const abortedAnalytics = request.url().endsWith("/api/v1/analytics/events") && request.failure()?.errorText === "net::ERR_ABORTED";
    if (!abortedAnalytics && /ficusin\.ru|twcstorage\.ru/.test(request.url())) failures.push(`${request.url()}: ${request.failure()?.errorText}`);
  });
  page.on("response", (response) => {
    const expectedGuest = response.status() === 401 && response.url().endsWith("/api/v1/auth/me");
    if (!expectedGuest && response.status() >= 400 && /ficusin\.ru|twcstorage\.ru/.test(response.url())) failures.push(`${response.status()} ${response.url()}`);
  });

  const started = Date.now();
  await page.goto("/", { waitUntil: "domcontentloaded" });
  await expect(page.locator(".storefront-grid")).toBeVisible();
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  const interfaceMs = Date.now() - started;
  await page.waitForTimeout(500);
  const metrics = await page.evaluate<BrowserMetrics>(() => {
    const state = window as typeof window & { __ficusinCLS?: number };
    return {
      cls: state.__ficusinCLS || 0,
      interfaceMs: 0,
      overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    };
  });
  metrics.interfaceMs = interfaceMs;
  console.log(`production metrics ${testInfo.project.name} catalogue ${JSON.stringify(metrics)}`);

  expect(failures).toEqual([]);
  expect(metrics.overflow).toBe(0);
  expect(metrics.cls).toBeLessThan(0.25);
  expect(await page.locator('img[src*="hero-monstera.png"]').count()).toBe(0);
});

test("production product 5 loads its optimized photo and review", async ({ page }, testInfo) => {
  const failures: string[] = [];
  const consoleErrors: string[] = [];
  page.on("requestfailed", (request) => {
    const abortedAnalytics = request.url().endsWith("/api/v1/analytics/events") && request.failure()?.errorText === "net::ERR_ABORTED";
    if (!abortedAnalytics && /ficusin\.ru|twcstorage\.ru/.test(request.url())) failures.push(`${request.url()}: ${request.failure()?.errorText}`);
  });
  page.on("response", (response) => {
    const expectedGuest = response.status() === 401 && response.url().endsWith("/api/v1/auth/me");
    if (!expectedGuest && response.status() >= 400 && /ficusin\.ru|twcstorage\.ru/.test(response.url())) failures.push(`${response.status()} ${response.url()}`);
  });
  page.on("console", (message) => {
    const text = message.text();
    const expectedGuest = text.includes("401 (Unauthorized)");
    const instrumentationCSP = text.startsWith("Refused to execute a script because its hash, its nonce, or 'unsafe-inline'");
    if (message.type() === "error" && !expectedGuest && !instrumentationCSP) consoleErrors.push(text);
  });

  const started = Date.now();
  await page.goto("/product/5", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { level: 1, name: "Аглаонема Мария" })).toBeVisible();
  const interfaceMs = Date.now() - started;
  const mainImage = page.getByRole("button", { name: "Открыть фотографию на весь экран" }).locator("img");
  await expect(mainImage).toBeVisible();
  await expect.poll(() => mainImage.evaluate((image) => image.complete && image.naturalWidth > 0)).toBe(true);
  const imageMs = Date.now() - started;
  await expect(mainImage).toHaveAttribute("src", /-large\.jpg$/);

  await page.getByRole("button", { name: /Отзывы 1/ }).click();
  await expect(page.getByText("Отличное растение!", { exact: true })).toBeVisible();
  await page.waitForTimeout(500);
  const metrics = await page.evaluate<BrowserMetrics>(() => {
    const state = window as typeof window & { __ficusinCLS?: number };
    return {
      cls: state.__ficusinCLS || 0,
      interfaceMs: 0,
      overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    };
  });
  metrics.interfaceMs = interfaceMs;
  metrics.imageMs = imageMs;
  console.log(`production metrics ${testInfo.project.name} product5 ${JSON.stringify(metrics)}`);

  expect(failures).toEqual([]);
  expect(consoleErrors).toEqual([]);
  expect(metrics.overflow).toBe(0);
  expect(metrics.cls).toBeLessThan(0.25);
});
