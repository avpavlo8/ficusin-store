import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

test("@desktop @phone характеристика зимовки видна рядом с покупкой только в настроенной категории", async ({ page }) => {
  await mockApi(page);
  let attributes = [
    { code: "zimovka", name: "Зимовка", value: "+5…+10 °C; светлая прохладная лоджия", summaryPosition: 45 },
    { code: "unassigned", name: "Скрытая из основных сведений", value: "Не выводить" },
    { code: "light_level", name: "Освещение", value: "diffused", summaryPosition: 10 },
  ];
  await page.route("**/api/v1/products/1", route => route.fulfill({ json: { product: {
    id: "1", name: "Вяз мелколистный", latin: "Ulmus parvifolia", catalogSection: "plants",
    description: "Описание", images: [], variants: [{ id: 1, sku: "1", label: "D15", price: 2590, stock: 1, images: [], attributes: [] }],
    attributes, passport: {}, importantWarnings: [], recommendations: [], reviews: [], rating: 0, reviewsCount: 0,
  } } }));
  await page.goto("/product/1");
  const summary = page.getByLabel("Основные характеристики", { exact: true });
  await expect(summary.getByText("Зимовка", { exact: true })).toBeVisible();
  await expect(summary.getByText("+5…+10 °C; светлая прохладная лоджия", { exact: true })).toBeVisible();
  await expect(summary.getByText("Освещение", { exact: true })).toHaveCount(1);
  await expect(summary.getByText("Скрытая из основных сведений")).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  attributes = [];
  await page.reload();
  await expect(summary.getByText("Зимовка", { exact: true })).toHaveCount(0);
});
