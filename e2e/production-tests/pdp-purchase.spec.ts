import { expect, test } from "@playwright/test";

test("PDP purchase layout, nested menu and gallery swipe work live", async ({ page, request }) => {
  const response = await request.get("/api/v1/products/33");
  expect(response.ok()).toBeTruthy();
  const { product } = await response.json();
  expect(typeof product?.name).toBe("string");
  expect(product.name.trim().length).toBeGreaterThan(0);
  await page.goto("/product/33");
  await expect(page.getByRole("heading", { level:1, name:product.name, exact:true })).toBeVisible();
  await expect(page.locator(".pdp-order-column .pdp-commerce-box")).toBeVisible();
  await expect(page.locator(".pdp-key-characteristics .care-scale")).toHaveCount(0);
  await expect(page.locator(".pdp-summary .pdp-related")).toHaveCount(1);
  await expect(page.locator(".pdp-summary .pdp-related-track")).toHaveCSS("display","flex");
  const cards = page.locator(".pdp-summary .related-card");
  const first = (await cards.nth(0).boundingBox())!;
  const second = (await cards.nth(1).boundingBox())!;
  expect(second.y).toBeCloseTo(first.y,0);
  await expect(page.locator(".pdp-lead")).toHaveCount(0);
  await expect(page.getByText("Артикул", { exact:true })).toHaveCount(0);
  const image = page.getByRole("button", { name:"Открыть фотографию на весь экран" });
  const count = page.locator(".pdp-image-count");
  await expect(count).toHaveText(/^1\s*\/\s*\d+$/);
  await image.dispatchEvent("touchstart", {touches:[{identifier:1,clientX:280,clientY:350}]});
  await image.dispatchEvent("touchmove", {touches:[{identifier:1,clientX:200,clientY:353}]});
  await expect(image.locator(".pdp-photo-track")).toHaveCSS("transition-duration","0s");
  expect(await image.locator(".pdp-photo-track").evaluate(el=>new DOMMatrix(getComputedStyle(el).transform).m41)).toBeCloseTo(-80,0);
  await expect(count).toHaveText(/^1\s*\/\s*\d+$/);
  await image.dispatchEvent("touchend", {touches:[],changedTouches:[{identifier:1,clientX:65,clientY:355}]});
  await expect(count).toHaveText(/^2\s*\/\s*\d+$/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  if ((await page.viewportSize())!.width > 850) {
    const catalog = page.locator(".header-dropdown").first();
    await catalog.locator(":scope > summary").click();
    await catalog.locator(".header-submenu > summary").filter({hasText:"Растения"}).first().click();
    await expect(catalog.locator(".header-submenu[open] > div").first()).toBeVisible();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

test("Search puts results first and offers suggestions live", async ({page}) => {
  await page.goto("/?q=фикус");
  await expect(page.getByRole("heading", {name:"Результаты поиска"})).toBeVisible();
  await expect(page.locator(".home-hero,.catalog-landing-hero,.storefront-tree")).toHaveCount(0);
  await expect(page.locator(".storefront-side")).toBeHidden();
  const phone = (await page.viewportSize())!.width <= 850;
  if (phone) await page.getByRole("button", {name:"Поиск по каталогу",exact:true}).first().click();
  const search = page.locator(phone ? ".mobile-catalog-search" : ".header-search");
  await search.getByRole("combobox").fill("фику");
  await expect(search.getByRole("listbox")).toBeVisible();
  await expect(search.getByRole("option").first()).toBeVisible();
  await expect(page.locator(".storefront-shell")).toBeVisible();
  await expect(page.locator(".storefront-head")).toContainText("фикус");
  await search.getByRole("combobox").press("Enter");
  await expect(page.getByRole("heading", {name:"Результаты поиска"})).toBeVisible();
});

test("Product tabs keep all text mounted and show one section at a time live", async ({ page, request }, testInfo) => {
  const response = await request.get("/api/v1/products/33");
  expect(response.ok()).toBeTruthy();
  const { product } = await response.json();
  await page.goto("/product/33");
  const tabs = page.getByRole("tablist", { name: "Разделы товара" });
  await expect(tabs).toBeVisible();
  await expect(page.locator("#pdp-panel-care #plant-passport")).toBeAttached();
  const care = Object.values(product.passport).find(value => typeof value === "string" && value.trim());
  expect(care).toBeTruthy();
  if (product.passport.watering?.trim()) await expect(page.locator("#pdp-panel-care")).toContainText(product.passport.watering);
  await expect(page.locator("#pdp-panel-care")).toBeHidden();
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  await tabs.evaluate(element => element.scrollIntoView({ block: "start", behavior: "instant" }));
  await page.screenshot({ path: testInfo.outputPath("product-tabs-about.png") });
  const before = await page.evaluate(() => window.scrollY);
  await page.getByRole("tab", { name: "Уход", exact: true }).click();
  await expect(page.locator("#pdp-panel-care")).toBeVisible();
  await expect(page.locator("#pdp-panel-about")).toBeHidden();
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  expect(await page.evaluate(() => window.scrollY)).toBeCloseTo(before, 0);
  await page.screenshot({ path: testInfo.outputPath("product-tabs-care.png") });
  await page.getByRole("tab", { name: /Отзывы/ }).click();
  await expect(page.locator("#pdp-panel-reviews")).toBeVisible();
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  await page.getByRole("tab", { name: "О растении", exact: true }).click();
  await expect(page.locator("#pdp-panel-about")).toBeVisible();
});
