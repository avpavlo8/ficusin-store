import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

const adminOwner = {
  ...owner,
  user: { ...owner.user, adminRole: "owner" as const },
};

const adminProduct = {
  id: 1,
  sabyId: "saby-1",
  slug: "1",
  name: "Аглаонема Мария",
  latinName: "Aglaonema",
  shortDescription: "Неприхотливое растение",
  description: "Старое описание",
  careInstructions: "Поливать умеренно",
  status: "published",
  featured: false,
  image: "/assets/hero-monstera.webp",
  price: 1490,
  stock: 5,
  sku: "1",
  variantLabel: "D12",
  wholesaleMinQty: 1,
  overrideFields: [],
  sabyFields: ["stock"],
  sabyCode: "X100",
  catalogSection: "plants",
  categoryId: undefined,
  passport: { faq: [] },
  importantWarnings: [],
  externalIds: [{ provider: "saby", type: "id", externalId: "saby-1" }],
  attributes: {},
};

test("@desktop покупатель не видит инструменты редактирования PDP", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  await expect(page.getByText("Режим администратора")).toHaveCount(0);
});

test("@desktop владелец редактирует описание прямо из PDP", async ({ page }) => {
  await mockApi(page, adminOwner);
  await page.route("**/api/v1/admin/products", (route) => route.fulfill({ json: { products: [adminProduct] } }));
  await page.route("**/api/v1/admin/categories", (route) => route.fulfill({ json: { categories: [] } }));
  let update: Record<string, unknown> | undefined;
  await page.route("**/api/v1/admin/products/1", async (route) => {
    update = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ json: { product: { ...adminProduct, description: String(update.description || "") } } });
  });

  await page.goto("/product/1");
  await page.getByRole("button", { name: "Редактировать карточку" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Редактирование товара" })).toBeVisible();
  await expect(dialog.getByText("Помощник AI")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "✦ Сгенерировать раздел" })).toBeVisible();
  await expect(dialog.getByAltText("Текущая обложка")).toBeVisible();
  await dialog.getByRole("textbox", { name: "Описание", exact: true }).fill("Новое описание из PDP");
  await dialog.getByRole("button", { name: "Сохранить" }).click();

  expect(update?.description).toBe("Новое описание из PDP");
  await expect(dialog).toHaveCount(0);
});

test("@desktop владелец управляет галереей товара из PDP", async ({ page }) => {
  await mockApi(page, adminOwner);
  await page.route("**/api/v1/admin/products", (route) => route.fulfill({ json: { products: [adminProduct] } }));
  let uploadCount = 0;
  let deleted = 0;
  await page.route("**/api/v1/admin/products/1/media", async (route) => {
    if (route.request().method() === "POST") {
      uploadCount++;
      await route.fulfill({ status: 201, json: { media: { id: 8, url: "/uploaded.jpg", primary: false, sortOrder: 1 } } });
      return;
    }
    await route.fulfill({ json: { media: [{ id: 7, url: "/assets/hero-monstera.webp", primary: true, sortOrder: 0 }] } });
  });
  await page.route("**/api/v1/admin/products/1/media/7", async (route) => {
    deleted++;
    await route.fulfill({ json: { deleted: true } });
  });
  page.on("dialog", (dialog) => void dialog.accept());

  await page.goto("/product/1");
  await page.getByRole("button", { name: "Фотографии" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Главная")).toBeVisible();
  await dialog.locator('input[type="file"]').setInputFiles({
    name: "plant.png", mimeType: "image/png", buffer: Buffer.from("fake-image"),
  });
  await expect.poll(() => uploadCount).toBe(1);
  await dialog.getByRole("button", { name: "Удалить" }).click();
  await expect.poll(() => deleted).toBe(1);
});
