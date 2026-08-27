import { expect, test } from "@playwright/test";

// Production smoke follows the public catalog's canonical display vocabulary.

type BrowserMetrics = {
  cls: number;
  imageMs?: number;
  interfaceMs: number;
  overflow: number;
};

const guestUnauthorized = (text: string) =>
  text.includes("401 (Unauthorized)") || /Failed to load resource[\s\S]*\b401\b/.test(text);

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
  await expect(page.locator(".storefront-grid:not(.storefront-skeleton)")).toBeVisible();
  await expect(page.locator(".storefront-grid:not(.storefront-skeleton) .storefront-card").first()).toBeVisible();
  const interfaceMs = Date.now() - started;
  await page.waitForTimeout(500);
  const metrics = await page.evaluate<BrowserMetrics>(() => {
    const state = window as typeof window & { __ficusinCLS?: number };
    return { cls: state.__ficusinCLS || 0, interfaceMs: 0, overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth };
  });
  metrics.interfaceMs = interfaceMs;
  console.log(`production metrics ${testInfo.project.name} catalogue ${JSON.stringify(metrics)}`);
  expect(failures).toEqual([]);
  expect(metrics.overflow).toBe(0);
  expect(metrics.cls).toBeLessThan(0.25);
  expect(await page.locator('img[src*="hero-monstera.png"]').count()).toBe(0);
});

test("production product 5 loads its optimized photo and review section", async ({ page }, testInfo) => {
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
    const instrumentationCSP = text.startsWith("Refused to execute a script because its hash, its nonce, or 'unsafe-inline'");
    if (message.type() === "error" && !guestUnauthorized(text) && !instrumentationCSP) consoleErrors.push(text);
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
  const reviewMeta = page.locator(".purchase-review-meta");
  await expect(reviewMeta).toBeVisible();
  const reviewCountText = await reviewMeta.innerText();
  await reviewMeta.click();
  await expect(page.locator("#reviews")).toBeVisible();
  const reviewCount = Number(reviewCountText.match(/(\d+)\s*отзыв/)?.[1] || 0);
  if (reviewCount > 0) await expect(page.locator("#reviews article").first()).toBeVisible();
  else await expect(page.locator("#reviews")).toContainText(/отзывов пока нет|будьте первым/i);
  await page.waitForTimeout(500);
  const metrics = await page.evaluate<BrowserMetrics>(() => {
    const state = window as typeof window & { __ficusinCLS?: number };
    return { cls: state.__ficusinCLS || 0, interfaceMs: 0, overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth };
  });
  metrics.interfaceMs = interfaceMs; metrics.imageMs = imageMs;
  console.log(`production metrics ${testInfo.project.name} product5 ${JSON.stringify(metrics)}`);
  expect(failures).toEqual([]); expect(consoleErrors).toEqual([]); expect(metrics.overflow).toBe(0); expect(metrics.cls).toBeLessThan(0.25);
});

test("production category and collection routes are real and unknown slugs are 404", async ({ page }) => {
  const categoriesResponse = await page.request.get("/api/v1/categories"); expect(categoriesResponse.ok()).toBeTruthy();
  const categories = (await categoriesResponse.json()).categories as Array<{ slug: string; name: string }>;
  const category = categories.find((item) => item.slug === "plants") || categories[0]; expect(category?.slug).toBeTruthy();
  await page.goto(`/catalog/${encodeURIComponent(category.slug)}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".catalog-landing-hero h1")).toContainText(category.name); await expect(page.locator(".storefront-grid:not(.storefront-skeleton)")).toBeVisible();
  const collectionsResponse = await page.request.get("/api/v1/collections"); expect(collectionsResponse.ok()).toBeTruthy();
  const collections = (await collectionsResponse.json()).collections as Array<{ slug: string; title: string; count: number }>;
  const collection = collections.find((item) => item.count > 0) || collections[0]; expect(collection?.slug).toBeTruthy();
  await page.goto(`/collections/${encodeURIComponent(collection.slug)}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".catalog-landing-hero h1")).toContainText(collection.title); await expect(page.locator(".storefront-grid:not(.storefront-skeleton)")).toBeVisible();
  await page.goto("/catalog/__production_unknown_category__", { waitUntil: "domcontentloaded" }); await expect(page.getByRole("heading", { name: "Страница не найдена" })).toBeVisible();
  await page.goto("/collections/__production_unknown_collection__", { waitUntil: "domcontentloaded" }); await expect(page.getByRole("heading", { name: "Страница не найдена" })).toBeVisible();
});

test("production category filters stay scoped and reset keeps the route", async ({ page }) => {
  await page.goto("/catalog/plants", { waitUntil: "domcontentloaded" }); await expect(page.locator(".catalog-landing-hero h1")).toContainText("Растения"); await page.getByRole("button", { name: /Фильтры/ }).click();
  const filters = page.locator(".storefront-attribute-filters").last();
  await expect(filters).toContainText("Освещённость");
  await expect(filters).not.toContainText("Тип кашпо"); await expect(filters).not.toContainText("Материал");
  await page.locator(".home-catalog-toolbar .storefront-check input").check();
  const reset = page.getByRole("button", { name: "Сбросить все" }).last(); await expect(reset).toBeVisible(); await reset.click(); await expect(page).toHaveURL(/\/catalog\/plants$/);
});
