import { expect, test } from "@playwright/test";
import { mockApi, owner, setStoredCounts } from "./helpers";

test("@desktop autocomplete с клавиатуры работает вне главной", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/saby-1");
  const search = page.locator(".header-search input");
  await search.fill("фикус");
  await expect(page.getByRole("option", { name: /Фикус Бенджамина/ })).toBeVisible();
  await search.press("ArrowDown");
  await search.press("Enter");
  await expect(page).toHaveURL(/\/product\/saby-2$/);
});

test("@desktop autocomplete показывает товары и переход ко всей выдаче", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/account/profile");
  const search = page.locator(".header-search input");
  await search.fill("фикус");
  await expect(page.getByRole("option", { name: /Фикус Бенджамина/ })).toBeVisible();
  await page.getByRole("option", { name: /Все результаты/ }).click();
  await expect(page).toHaveURL(/\/\?q=%D1%84%D0%B8%D0%BA%D1%83%D1%81#catalog$/);
});

test("@desktop локальный поиск избранного не перехватывает переход ко всей выдаче", async ({ page }) => {
  await mockApi(page);
  await page.goto("/favorites");
  const search = page.locator(".header-search input");
  await search.fill("фикус");
  await page.getByRole("option", { name: /Все результаты/ }).click();
  await expect(page).toHaveURL(/\/\?q=%D1%84%D0%B8%D0%BA%D1%83%D1%81#catalog$/);
});

test("@desktop прямой URL корзины меняет количество, badge и сумму", async ({ page }) => {
  await mockApi(page);
  await setStoredCounts(page, [], { "saby-1": 1 });
  await page.goto("/cart");
  const drawer = page.locator(".drawer.open");
  await expect(page).toHaveURL(/\/cart$/);
  await expect(drawer.getByText("Аглаонема Мария")).toBeVisible();
  await drawer.getByRole("button", { name: "Увеличить" }).click();
  await expect(drawer.locator(".quantity span")).toHaveText("2");
  await expect(page.getByLabel(/Корзина, товаров: 2/)).toBeVisible();
  await expect(drawer.locator(".cart-summary-total strong")).toHaveText("2 980 ₽");
});

test("@desktop избранное добавляет в корзину без навигации", async ({ page }) => {
  await mockApi(page);
  await setStoredCounts(page, ["saby-1"], {});
  await page.goto("/favorites");
  await page.getByRole("button", { name: "В корзину" }).click();
  await expect(page).toHaveURL(/\/favorites$/);
  await expect(page.getByRole("button", { name: /Добавить ещё · 1/ })).toBeVisible();
  await expect(page.getByLabel(/Корзина, товаров: 1/)).toBeVisible();
});

test("@desktop удалённый товар не возвращается после reload", async ({ page }) => {
  await mockApi(page, owner);
  await setStoredCounts(page, [], { "saby-1": 1, "saby-2": 2 });
  await page.goto("/cart");
  const drawer = page.locator(".drawer.open");
  await expect(drawer.getByText("Аглаонема Мария")).toBeVisible();
  await expect(drawer.getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.getByLabel(/Корзина, товаров: 3/)).toBeVisible();
  await drawer.getByRole("button", { name: "Удалить Аглаонема Мария" }).click();
  await expect(page.getByLabel(/Корзина, товаров: 2/)).toBeVisible();
  await page.reload();
  await expect(page.getByLabel(/Корзина, товаров: 2/)).toBeVisible();
  await expect(drawer.getByText("Аглаонема Мария")).toHaveCount(0);
  await expect(drawer.getByText("Фикус Бенджамина")).toBeVisible();
});

test("@phone mobile autocomplete поддерживает тап и Search action", async ({ page, browserName }) => {
  await mockApi(page);
  await page.goto("/product/saby-1");
  await page.getByRole("button", { name: "Поиск по каталогу" }).click();
  const search = page.locator(".mobile-catalog-search input");
  await search.fill("фикус");
  // Touch keyboards have no arrow keys. Physical-keyboard navigation is
  // covered by the desktop scenario; the phone path verifies the real tap.
  if (browserName === "webkit") {
    // WebKit's iPhone emulation exposes the virtual Search action, not a
    // physical ArrowDown. It submits the query; the result opens from there.
    await search.press("Enter");
    await expect(page).toHaveURL(/\/\?q=%D1%84%D0%B8%D0%BA%D1%83%D1%81#catalog$/);
    return;
  } else {
    await page.getByRole("option", { name: /Фикус Бенджамина/ }).click();
  }
  await expect(page).toHaveURL(/\/product\/saby-2$/);
});

test("@desktop профиль сохраняет адресные подсказки после обновления UI", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/account/profile");
  await page.getByRole("button", { name: "Изменить" }).click();
  await expect(page.getByLabel("Адрес доставки")).toBeVisible();
});

test("@desktop запрет push объясняется без повторного запроса permission", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Notification, "permission", { configurable: true, get: () => "denied" });
  });
  await mockApi(page, owner);
  await page.route("**/api/v1/push/key", (route) => route.fulfill({ json: { enabled: true, publicKey: "unused" } }));
  await page.goto("/account/profile");
  await expect(page.getByText(/Уведомления запрещены в настройках браузера/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Включить" })).toBeDisabled();
});
