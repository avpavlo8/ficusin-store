import { expect, test } from "@playwright/test";

test("PDP purchase layout, nested menu and gallery swipe work live", async ({ page }) => {
  await page.goto("/product/33");
  await expect(page.getByRole("heading", { level:1, name:"Антуриум", exact:true })).toBeVisible();
  await expect(page.locator(".pdp-order-column .pdp-commerce-box")).toBeVisible();
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
  await expect(page.locator(".storefront-shell")).toHaveCount(0);
  await search.getByRole("combobox").press("Enter");
  await expect(page.getByRole("heading", {name:"Результаты поиска"})).toBeVisible();
});
