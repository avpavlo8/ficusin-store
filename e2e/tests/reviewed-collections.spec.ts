import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

const titles = ["На рабочий стол", "Цитрусы и плодовые", "Безопасно для питомцев", "Неприхотливые", "Переносят полутень", "Редкий полив", "Цветущие растения", "Для солнечного окна", "Ампельные — для полок и подвесов", "Для ванной комнаты", "Крупные — от 80 см", "Небольшие — до 40 см", "Бонсай без зимовки", "В подарок", "Необычные растения", "Пёстрые листья", "Зелёная вертикаль", "Деревце для дома", "Бонсай"];
const small = { id: "1", sku: "small-40", name: "Арека", latin: "Chrysalidocarpus lutescens", category: "Растения", categoryId: 4, catalogSection: "plants", size: "D10", heightCm: 40, price: 1490, stock: 2, image: "/assets/hero-monstera.webp", careLevel: "medium", collections: ["tall"], filterAttributes: [] };
const tall = { ...small, sku: "tall-150", size: "D24", heightCm: 150, price: 8990 };

test("@desktop @phone 19 подборок сохраняют порядок и скрывают старые только с главной", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [
    ...titles.map((title, index) => ({ slug: `reviewed-${index}`, title, note: "Условия подбора", count: 1, coverUrl: "/assets/collections/2026-10-19/bonsai.webp", coverPosition: "center 30%", showOnHome: true })),
    { slug: "office", title: "Для офиса", count: 1, showOnHome: false },
  ] } }));
  await page.goto("/");
  await expect(page.locator(".preset")).toHaveCount(19);
  await expect(page.locator(".preset-title")).toHaveText(titles);
  await expect(page.locator(".preset[href='/collections/office']")).toHaveCount(0);
  await expect(page.locator(".preset-cover").first()).toHaveCSS("object-position", "50% 30%");
  await expect(page.locator(".preset-cover").first()).toHaveCSS("object-fit", "contain");
  const frame = await page.locator(".preset").first().boundingBox();
  expect(frame).not.toBeNull();
  expect(frame!.width / frame!.height).toBeCloseTo(1.5, 1);
  await expect(page.locator(".preset").first()).not.toHaveAttribute("title");

  await expect(page.locator(".preset-cover").last()).toHaveAttribute("loading", "lazy");
  await expect.poll(() => page.locator(".preset-cover").first().evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await page.goto("/collections/office");
  await expect(page.getByRole("heading", { name: "Для офиса", exact: true }).first()).toBeVisible();
});

test("@desktop @phone подборка по высоте сохраняет подходящий размер, цену и SKU в ссылке", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [{ slug: "tall", title: "Крупные — от 80 см", note: "Высота выбранного варианта", count: 1, selectsVariant: true }] } }));
  await page.context().route("**/api/v1/catalog*", route => route.fulfill({ json: { products: [new URL(route.request().url()).searchParams.get("collection") === "tall" ? tall : small] } }));
  await page.goto("/collections/tall");
  const card = page.locator(".storefront-card");
  await expect(card).toHaveCount(1);
  await expect(card).toContainText("150 см");
  await expect(card).toContainText("D24");
  await expect(card.locator(".storefront-price")).toContainText(/8\s?990/);
  await expect(card.locator(".storefront-name")).toHaveAttribute("href", "/product/1?sku=tall-150");
  const saved = page.waitForRequest(request => request.url().endsWith("/api/v1/cart") && request.method() === "PUT");
  await card.getByRole("button", { name: "В корзину", exact: true }).click();
  expect((await saved).postDataJSON().items).toEqual({ "tall-150": 1 });

});

test("@desktop ошибка загрузки размера не подменяет его маленьким вариантом", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [{ slug: "tall", title: "Крупные", count: 1, selectsVariant: true }] } }));
  await page.context().route("**/api/v1/catalog*", route => new URL(route.request().url()).searchParams.has("collection") ? route.fulfill({ status: 503, json: { error: "Unavailable" } }) : route.fulfill({ json: { products: [small] } }));
  await page.goto("/collections/tall");
  await expect(page.getByRole("alert")).toContainText("Не удалось загрузить размеры подборки");
  await expect(page.locator(".storefront-card")).toHaveCount(0);
  await expect(page.getByText("Здесь пока пусто")).toHaveCount(0);
});

test("@desktop широкие карточки сохраняют полный кадр 3:2", async ({ page }) => {
  await mockApi(page);
  for (const width of [1024, 1440, 2560]) {
    await page.setViewportSize({ width, height: 1100 });
    await page.goto("/");
    await expect(page.locator(".preset").first()).toBeVisible();
    const frame = await page.locator(".preset").first().boundingBox();
    expect(frame!.width / frame!.height).toBeCloseTo(1.5, 1);
    await expect(page.locator(".preset-cover").first()).toHaveCSS("object-fit", "contain");
  }
});
