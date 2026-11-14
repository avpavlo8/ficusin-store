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

for (const catalogSection of ['plants', 'pots']) {
  test(`@desktop @phone PIM labels localize custom attributes in ${catalogSection} purchase details`, async ({ page }) => {
    await mockApi(page);
    await page.route('**/api/v1/products/pim-labels', route => route.fulfill({ json: { product: {
      id: 'pim-labels', name: 'Товар с настраиваемыми характеристиками', latin: '', catalogSection,
      description: 'Описание', images: [], passport: {}, importantWarnings: [], recommendations: [], reviews: [], rating: 0, reviewsCount: 0,
      attributes: [
        { code: 'care_level', name: 'Сложность ухода', value: 'easy', displayValue: 'Для начинающего цветовода', summaryPosition: 20 },
        { code: 'material', name: 'Материал', value: 'matte_ceramic_v2', displayValue: 'Матовая керамика', summaryPosition: 70 },
        { code: 'surface', name: 'Поверхность', value: ['velvet_custom', 'smooth_custom'], optionLabels: { velvet_custom: 'Бархатная', smooth_custom: 'Гладкая' }, summaryPosition: 80 },
        { code: 'drainage', name: 'Дренажное отверстие', value: false, summaryPosition: 90 },
      ],
      variants: [{ id: 501, sku: '501', label: 'D15', price: 2590, stock: 1, images: [], attributes: [{ code: 'vessel_volume_l', name: 'Объём', value: 1.5, unit: 'л', summaryPosition: 100 }] }],
    } } }));
    await page.goto('/product/pim-labels');
    const summary = page.locator('.pdp-key-characteristics');
    await expect(summary).toContainText('Матовая керамика');
    await expect(summary).toContainText('Бархатная, Гладкая');
    await expect(summary).toContainText('Нет');
    await expect(summary).toContainText('1.5 л');
    if (catalogSection === 'plants') await expect(summary).toContainText('Для начинающего цветовода');
    await expect(summary).not.toContainText(/matte.ceramic|velvet.custom|smooth.custom/);
    if (catalogSection === 'pots') {
      const details = page.locator('#pdp-panel-characteristics');
      await expect(details).toContainText('Матовая керамика');
      await expect(details).toContainText('Бархатная, Гладкая');
      await expect(details).toContainText('1.5 л');
    }
  });
}
