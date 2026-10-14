import { expect, test } from "@playwright/test";

test("PDP purchase layout, nested menu and gallery swipe work live", async ({ page }) => {
  await page.goto("/product/33");
  await expect(page.getByRole("heading", { level:1, name:"Антуриум", exact:true })).toBeVisible();
  await expect(page.locator(".pdp-order-column .pdp-commerce-box")).toBeVisible();
  await expect(page.locator(".pdp-summary .pdp-related")).toHaveCount(1);
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
    await catalog.getByText("Растения", {exact:true}).click();
    await expect(catalog.locator(".header-submenu[open] > div").first()).toBeVisible();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});
