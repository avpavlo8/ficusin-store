import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi, owner, setStoredCounts } from "./helpers";

const storePages = ["/", "/favorites", "/account"];

test("@phone the header keeps search, the menu and the bottom bar everywhere", async ({ page }) => {
  await setStoredCounts(page, ["1", "2"], { "1": 3 });
  await mockApi(page, owner);
  for (const path of storePages) {
    await page.goto(path);
    await expect(page.locator(".menu-button"), `меню на ${path}`).toBeVisible();
    await expect(page.locator(".tab-bar").getByRole("button", { name: /Поиск/ }), `поиск на ${path}`).toBeVisible();
    await expect(page.locator(".tab-bar"), `нижняя панель на ${path}`).toBeVisible();
    await expect(page.locator(".tab-bar > *")).toHaveCount(5);
  }
});

test("@phone the counters are readable, not hidden", async ({ page }) => {
  await setStoredCounts(page, ["1", "2"], { "1": 3 });
  await mockApi(page);
  await page.goto("/favorites");
  const badges = page.locator(".tab-bar .tab-icon b");
  await expect(badges).toHaveCount(2);
  await expect(badges.first()).toHaveText("2");
  await expect(badges.last()).toHaveText("3");
});

test("@phone the storefront search filters the catalogue", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toBeVisible();
  await page.locator(".tab-bar").getByRole("button", { name: /Поиск/ }).click();
  const field = page.locator(".mobile-catalog-search input");
  await expect(field).toBeVisible();
  await field.fill("бенджамина");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toHaveCount(0);
});

test("@phone the menu opens and lists the sections", async ({ page }) => {
  await mockApi(page); await page.goto("/"); await page.locator(".menu-button").click();
  const menu = page.locator(".mobile-menu");
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("link", { name: "Каталог" })).toBeVisible();
  await expect(menu.getByRole("link", { name: /Избранное/ })).toBeVisible();
  await menu.getByRole("button", { name: "Закрыть меню" }).click();
  await expect(menu).toHaveCount(0);
});

test("@phone the cart opens as a separate page and keeps its contents", async ({ page }) => {
  await mockApi(page); await page.goto("/");
  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await expect(page.locator(".tab-bar .tab-icon b").last()).toHaveText("1");
  await page.locator(".tab-bar > *").nth(3).click();
  await expect(page.locator(".drawer.open")).toBeVisible();
  await expect(page).toHaveURL(/\/cart$/);
  await expect.poll(async () => (await page.locator(".drawer.open").innerText()).replace(/\s+/g, " ").trim(), { message: "содержимое корзины после перехода" }).toContain("Аглаонема Мария");
  await expect(page.locator(".drawer.open .quantity span")).toHaveText("1");
});

test("@phone подбор по характеристикам свёрнут, товар виден сразу", async ({ page }) => {
  await mockApi(page); await page.goto("/catalog/plants");
  const filters = page.getByRole("button", { name: /Фильтры/ });
  await expect(filters).toBeVisible(); await expect(filters).toHaveAttribute("aria-expanded", "false"); await expect(page.locator(".home-filter-panel")).toHaveCount(0);
  const card = page.locator(".storefront-card").first(); await card.scrollIntoViewIfNeeded(); await expect(card).toBeVisible();
  await filters.click(); await expect(page.locator(".home-filter-panel .storefront-attribute-filters")).toBeVisible();
});

test("@phone hero не прячет витрину за рекламными экранами", async ({ page }) => {
  await mockApi(page);
  for (const width of [360, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/");
    const card = page.locator(".storefront-card").first();
    await expect(card).toBeVisible();
    const firstProductY = await card.evaluate((element) => element.getBoundingClientRect().y);
    expect(firstProductY, `первый товар слишком низко при ${width}px`).toBeLessThan(1500);
    expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1);
  }
});

test("@phone no page scrolls sideways", async ({ page }) => {
  await mockApi(page, owner);
  for (const path of [...storePages, "/cart", "/checkout", "/offer", "/privacy", "/login"]) { await page.goto(path); expect(await horizontalOverflow(page), `${path} шире экрана`).toBeLessThanOrEqual(1); }
});

test("@phone PDP сохраняет покупку и галерею на 360 и 390 пикселях", async ({ page }) => {
  await mockApi(page);
  for (const width of [360, 390]) {
    await page.setViewportSize({ width, height: 844 }); await page.goto("/product/1");
    await expect(page.locator(".pdp-image img")).toBeVisible(); await expect(page.locator(".pdp-commerce-box")).toBeVisible(); await expect(page.locator(".pdp-cart-button")).toBeVisible(); expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1);
  }
});

test("@phone the things a thumb taps are big enough", async ({ page }) => {
  await mockApi(page); await page.goto("/");
  for (const selector of [".menu-button", ".search-toggle", ".tab-bar > *"]) {
    for (const target of await page.locator(selector).all()) { const box = await target.boundingBox(); expect(box, selector).not.toBeNull(); expect(box!.height, `${selector} слишком низкий`).toBeGreaterThanOrEqual(40); }
  }
});

test("@phone the page leaves room under the bottom bar", async ({ page }) => {
  await mockApi(page); await page.goto("/"); const barHeight = (await page.locator(".tab-bar").boundingBox())!.height; const reserved = await page.evaluate(() => parseFloat(getComputedStyle(document.querySelector("main")!).paddingBottom)); expect(reserved, "контент уедет под нижнюю панель").toBeGreaterThanOrEqual(barHeight);
});

test("@desktop keeps the full header and hides the phone chrome", async ({ page }) => {
  await mockApi(page); await page.goto("/"); await expect(page.locator(".header-search input")).toBeVisible(); await expect(page.locator(".desktop-nav")).toBeVisible(); await expect(page.locator(".favorites-button")).toBeVisible(); await expect(page.locator(".cart-button")).toBeVisible(); await expect(page.locator(".tab-bar")).toBeHidden(); await expect(page.locator(".menu-button")).toBeHidden(); await expect(page.locator(".search-toggle")).toBeHidden();
});

test("@desktop the public notice and header fit the design-system checkpoints", async ({ page }) => {
  await mockApi(page);
  for (const width of [768, 1024, 1440, 1920]) { await page.setViewportSize({ width, height: 900 }); await page.goto("/"); await expect(page.locator(".development-notice")).toContainText("Сайт в режиме доработки"); expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1); const header = await page.locator(".store-header").boundingBox(); expect(header?.width, `ширина шапки ${width}px`).toBe(width); }
});

test("@desktop каталог компактен на контрольных ширинах", async ({ page }) => {
  await mockApi(page);
  for (const width of [768, 1024, 1440, 1920]) { await page.setViewportSize({ width, height: 900 }); await page.goto("/"); const hero = await page.locator(".home-hero").boundingBox(); const catalog = await page.locator("#catalog").boundingBox(); expect(hero?.height, `hero слишком высокий при ${width}px`).toBeLessThanOrEqual(470); expect(catalog?.y, `каталог ниже второго экрана при ${width}px`).toBeLessThan(1800); expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1); }
});

test("@phone the notice and header fit 360 and 390 pixels", async ({ page }) => { await mockApi(page); for (const width of [360, 390]) { await page.setViewportSize({ width, height: 844 }); await page.goto("/"); await expect(page.locator(".development-notice")).toBeVisible(); expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1); } });

test("@desktop the development notice never appears in admin", async ({ page }) => { await mockApi(page, owner); await page.goto("/admin"); await expect(page.locator(".development-notice")).toHaveCount(0); });
