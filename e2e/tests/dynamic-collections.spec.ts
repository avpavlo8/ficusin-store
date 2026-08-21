import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

test("@desktop серверная динамическая подборка появляется и фильтрует каталог", async ({ page }) => {
  await mockApi(page);
  const context = page.context();

  await context.route("**/api/v1/catalog", (route) => route.fulfill({ json: { products: [
    {
      id: "1", sku: "1", name: "Аглаонема Мария", latin: "Aglaonema", category: "Растения",
      price: 1490, image: "/assets/hero-monstera.png", size: "D12", stock: 5,
      catalogSection: "plants", categoryId: 4, rating: 4.8, reviewsCount: 12,
      collections: ["low-light-live"], filterAttributes: [],
    },
    {
      id: "2", sku: "2", name: "Фикус Бенджамина", latin: "Ficus benjamina", category: "Растения",
      price: 2490, image: "/assets/hero-monstera.png", size: "D14", stock: 3,
      catalogSection: "plants", categoryId: 5, rating: 4.7, reviewsCount: 8,
      collections: [], filterAttributes: [],
    },
  ] } }));
  await context.route("**/api/v1/collections", (route) => route.fulfill({ json: { collections: [
    { slug: "low-light-live", title: "Можно в тёмную комнату", note: "Автоматически из PIM", count: 1 },
  ] } }));

  await page.goto("/");
  const collection = page.locator(".preset", { hasText: "Можно в тёмную комнату" });
  await expect(collection).toBeVisible();
  await expect(collection).toContainText("1 растений");
  await collection.click();

  const grid = page.locator(".storefront-grid");
  await expect(grid.getByText("Аглаонема Мария")).toBeVisible();
  await expect(grid.getByText("Фикус Бенджамина")).toHaveCount(0);
});
