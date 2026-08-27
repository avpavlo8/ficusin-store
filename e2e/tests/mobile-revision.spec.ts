import { expect, test, type Page } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

const widths = [320, 360, 375, 390, 412, 430, 768];

async function setPhoneViewport(page: Page, width: number) {
  await page.setViewportSize({ width, height: width === 768 ? 900 : 844 });
}

test("@phone mobile shell is stable at every audit width", async ({ page }) => {
  await mockApi(page);
  for (const width of widths) {
    await setPhoneViewport(page, width);
    await page.goto("/");
    await expect(page.locator(".development-notice")).toBeVisible();
    await expect(page.locator(".development-notice-mobile")).toBeVisible();
    await expect(page.locator(".store-header")).toBeVisible();
    await expect(page.locator(".tab-bar")).toBeVisible();
    await expect(page.locator(".tab-bar > *")).toHaveCount(5);
    await expect(page.locator(".storefront-card").first()).toBeVisible();
    const bar = await page.locator(".tab-bar").boundingBox();
    const mainPadding = await page.evaluate(() => parseFloat(getComputedStyle(document.querySelector("main")!).paddingBottom));
    expect(mainPadding, `нижняя панель не зарезервирована при ${width}px`).toBeGreaterThanOrEqual(bar?.height || 0);
    expect(await horizontalOverflow(page), `${width}px шире экрана`).toBeLessThanOrEqual(1);
    const firstProduct = await page.locator(".storefront-card").first().boundingBox();
    expect(firstProduct?.y, `каталог слишком далеко при ${width}px`).toBeLessThan(1500);
  }
});

test("@phone mobile controls remain usable at the narrowest and widest phone widths", async ({ page }) => {
  await mockApi(page);
  for (const width of [320, 430]) {
    await setPhoneViewport(page, width);
    await page.goto("/");
    await expect(page.locator(".menu-button")).toBeVisible();
    await page.locator(".menu-button").click();
    const menu = page.locator(".mobile-menu");
    await expect(menu).toBeVisible();
    await expect(menu.getByRole("button", { name: "Закрыть меню" })).toBeVisible();
    await menu.getByRole("button", { name: "Закрыть меню" }).click();
    await page.locator(".tab-bar").getByRole("button", { name: /Поиск/ }).click();
    const search = page.locator(".mobile-catalog-search input");
    await expect(search).toBeVisible();
    await expect(search).toBeFocused();
    await search.fill("фикус");
    await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
    expect(await horizontalOverflow(page), `${width}px controls overflow`).toBeLessThanOrEqual(1);
  }
});

test("@phone PDP fixed purchase and photo modal respect the bottom chrome", async ({ page }) => {
  await mockApi(page);
  for (const width of [320, 390, 430]) {
    await setPhoneViewport(page, width);
    await page.goto("/product/1");
    const buybar = page.locator(".pdp-mobile-buybar");
    await expect(buybar).toBeVisible();
    const position = await buybar.evaluate((element) => getComputedStyle(element).position);
    expect(position, `мобильная покупка не fixed при ${width}px`).toBe("fixed");
    const tab = await page.locator(".tab-bar").boundingBox();
    const bar = await buybar.boundingBox();
    expect(bar).not.toBeNull();
    expect(tab).not.toBeNull();
    expect(Math.abs((bar!.y + bar!.height) - tab!.y), `покупка перекрывает нижнюю панель при ${width}px`).toBeLessThanOrEqual(1);
    await page.locator(".pdp-image").click();
    const dialog = page.locator('.pdp-lightbox[role="dialog"]');
    await expect(dialog).toBeVisible();
    const dialogBox = await dialog.boundingBox();
    expect(dialogBox?.x).toBeGreaterThanOrEqual(0);
    expect(dialogBox?.y).toBeGreaterThanOrEqual(0);
    expect((dialogBox?.x || 0) + (dialogBox?.width || 0)).toBeLessThanOrEqual(width + 1);
    await dialog.getByRole("button", { name: "Закрыть" }).click();
    await expect(dialog).toHaveCount(0);
    expect(await horizontalOverflow(page), `PDP ${width}px шире экрана`).toBeLessThanOrEqual(1);
  }
});

test("@phone checkout focus has room for iOS keyboard and bottom navigation", async ({ page }) => {
  await mockApi(page);
  await setPhoneViewport(page, 390);
  await page.goto("/");
  await page.locator(".storefront-card", { hasText: "Аглаонема Мария" }).getByRole("button", { name: "В корзину" }).click();
  await page.locator(".tab-bar > *").nth(3).click();
  await expect(page).toHaveURL(/\/cart$/);
  await page.getByRole("button", { name: /Оформить заказ/ }).first().click();
  await expect(page).toHaveURL(/\/checkout$/);
  const field = page.locator(".checkout-page-panel input, .checkout-page-panel textarea, .checkout-page-panel select").first();
  await expect(field).toBeVisible();
  await field.focus();
  await expect(field).toBeFocused();
  const fontSize = await field.evaluate((element) => getComputedStyle(element).fontSize);
  expect(parseFloat(fontSize), "iOS должен видеть поле без принудительного zoom").toBeGreaterThanOrEqual(16);
  const scrollMarginBottom = await field.evaluate((element) => parseFloat(getComputedStyle(element).scrollMarginBottom));
  const tabHeight = (await page.locator(".tab-bar").boundingBox())!.height;
  expect(scrollMarginBottom, "поле не оставляет место под нижнюю панель").toBeGreaterThanOrEqual(tabHeight);
  await field.scrollIntoViewIfNeeded();
  const fieldBox = await field.boundingBox();
  expect(fieldBox?.y).toBeGreaterThanOrEqual(0);
  expect((fieldBox?.y || 0) + (fieldBox?.height || 0)).toBeLessThanOrEqual(900);
});

test("@phone footer and account stay above the fixed navigation", async ({ page }) => {
  await mockApi(page, owner);
  await setPhoneViewport(page, 390);
  await page.goto("/account");
  await expect(page.locator(".account-page")).toBeVisible();
  expect(await horizontalOverflow(page), "личный кабинет шире экрана").toBeLessThanOrEqual(1);
  await page.goto("/privacy");
  await expect(page.locator("footer")).toBeVisible();
  await page.evaluate(() => window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "instant" as ScrollBehavior }));
  await page.waitForFunction(() => window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 2);
  const footerLink = page.locator("footer a").last();
  const linkBox = await footerLink.boundingBox();
  const tabBox = await page.locator(".tab-bar").boundingBox();
  expect(linkBox?.y).toBeGreaterThanOrEqual(0);
  expect((linkBox?.y || 0) + (linkBox?.height || 0), "последний пункт подвала закрыт нижней панелью").toBeLessThanOrEqual(tabBox?.y || 0);
  expect(await horizontalOverflow(page), "подвал шире экрана").toBeLessThanOrEqual(1);
});

test("@phone safe-area and transient messages use the mobile chrome geometry", async ({ page }) => {
  await mockApi(page);
  await setPhoneViewport(page, 390);
  await page.goto("/");
  const viewport = await page.locator('meta[name="viewport"]').getAttribute("content");
  expect(viewport).toContain("viewport-fit=cover");
  const safeAreaRulePresent = await page.evaluate(() => Array.from(document.styleSheets).some((sheet) => {
    try {
      return Array.from(sheet.cssRules).some((rule) => rule.cssText.includes("safe-area-inset-bottom"));
    } catch {
      return false;
    }
  }));
  expect(safeAreaRulePresent).toBe(true);
  const toast = await page.evaluate(() => {
    const element = document.createElement("div");
    element.className = "toast";
    element.textContent = "Тестовое сообщение";
    document.body.appendChild(element);
    const style = getComputedStyle(element);
    const result = { position: style.position, bottom: parseFloat(style.bottom) };
    element.remove();
    return result;
  });
  const tabHeight = (await page.locator(".tab-bar").boundingBox())!.height;
  expect(toast.position).toBe("fixed");
  expect(toast.bottom, "toast может оказаться под нижней навигацией").toBeGreaterThanOrEqual(tabHeight);
});