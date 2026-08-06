import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi, owner, setStoredCounts } from "./helpers";

// These tests exist because the phone layout broke silently once already:
// the search box and the favourites counter were switched off by a media
// query and nobody noticed until a customer looked.
//
// The @phone and @desktop tags decide which project runs what — see
// playwright.config.ts.
const storePages = ["/", "/favorites", "/account"];

test("@phone the header keeps search, the menu and the bottom bar everywhere", async ({ page }) => {
  await setStoredCounts(page, ["saby-1", "saby-2"], { "saby-1": 3 });
  await mockApi(page, owner);

  for (const path of storePages) {
    await page.goto(path);
    await expect(page.locator(".menu-button"), `меню на ${path}`).toBeVisible();
    await expect(page.locator(".search-toggle"), `поиск на ${path}`).toBeVisible();
    await expect(page.locator(".tab-bar"), `нижняя панель на ${path}`).toBeVisible();
    await expect(page.locator(".tab-bar > *")).toHaveCount(4);
  }
});

test("@phone the counters are readable, not hidden", async ({ page }) => {
  await setStoredCounts(page, ["saby-1", "saby-2"], { "saby-1": 3 });
  await mockApi(page);
  await page.goto("/favorites");

  const badges = page.locator(".tab-bar .tab-icon b");
  await expect(badges).toHaveCount(2);
  await expect(badges.first()).toHaveText("2");
  await expect(badges.last()).toHaveText("3");
});

test("@phone the magnifier opens a search field and filters the catalogue", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  await expect(page.getByText("Аглаонема Мария")).toBeVisible();

  await expect(page.locator(".mobile-search")).toHaveCount(0);
  await page.locator(".search-toggle").click();

  const field = page.locator(".mobile-search input");
  await expect(field).toBeVisible();
  await expect(field).toBeFocused();

  await field.fill("бенджамина");
  await expect(page.getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.getByText("Аглаонема Мария")).toHaveCount(0);
});

test("@phone the menu opens and lists the sections", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await page.locator(".menu-button").click();
  const menu = page.locator(".mobile-menu");
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("link", { name: "Каталог" })).toBeVisible();
  await expect(menu.getByRole("link", { name: /Избранное/ })).toBeVisible();

  // The menu covers the whole screen on a phone, so the close button is the
  // only way out — there is no strip of backdrop left to tap.
  await menu.getByRole("button", { name: "Закрыть меню" }).click();
  await expect(menu).toHaveCount(0);
});

// The storefront hands the cart back to the old page, so tapping the cart
// does move the address to ?cart=1. What must not happen is the cart being
// forgotten on the way: the drawer has to open with the plant still in it.
test("@phone the cart opens from the bottom bar and keeps its contents", async ({ page }) => {
  await setStoredCounts(page, [], { "saby-1": 1 });
  await mockApi(page);
  await page.goto("/");

  await page.locator(".tab-bar > *").nth(2).click();
  await expect(page.locator(".drawer.open")).toBeVisible();
  await expect(page).toHaveURL(/\?cart=1$/);
  // Дольше обычного: после перехода страница грузит каталог заново, и
  // название растения появляется в корзине только с ним.
  await expect(page.locator(".drawer.open").getByText("Аглаонема Мария")).toBeVisible({ timeout: 15000 });
});

test("@phone no page scrolls sideways", async ({ page }) => {
  await mockApi(page, owner);
  for (const path of [...storePages, "/offer", "/privacy", "/login"]) {
    await page.goto(path);
    expect(await horizontalOverflow(page), `${path} шире экрана`).toBeLessThanOrEqual(1);
  }
});

test("@phone the things a thumb taps are big enough", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  for (const selector of [".menu-button", ".search-toggle", ".tab-bar > *"]) {
    for (const target of await page.locator(selector).all()) {
      const box = await target.boundingBox();
      expect(box, selector).not.toBeNull();
      expect(box!.height, `${selector} слишком низкий`).toBeGreaterThanOrEqual(40);
    }
  }
});

test("@phone the page leaves room under the bottom bar", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const barHeight = (await page.locator(".tab-bar").boundingBox())!.height;
  const reserved = await page.evaluate(() =>
    parseFloat(getComputedStyle(document.querySelector("main")!).paddingBottom));
  expect(reserved, "контент уедет под нижнюю панель").toBeGreaterThanOrEqual(barHeight);
});

test("@desktop keeps the full header and hides the phone chrome", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await expect(page.locator(".header-search")).toBeVisible();
  await expect(page.locator(".desktop-nav")).toBeVisible();
  await expect(page.locator(".favorites-button")).toBeVisible();
  await expect(page.locator(".cart-button")).toBeVisible();
  await expect(page.locator(".tab-bar")).toBeHidden();
  await expect(page.locator(".menu-button")).toBeHidden();
  await expect(page.locator(".search-toggle")).toBeHidden();
});
