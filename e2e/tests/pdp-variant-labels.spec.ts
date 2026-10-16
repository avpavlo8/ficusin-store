import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

for (const screen of ["@desktop", "@phone"]) {
  test(`${screen} варианты одного диаметра различаются высотой и формой`, async ({ page }) => {
    await mockApi(page);
    await page.route("**/api/v1/products/1", route => route.fulfill({ json: { product: {
      id: "1", name: "Замиокулькас", latin: "Zamioculcas zamiifolia", catalogSection: "plants",
      description: "Описание", images: [], recommendations: [], attributes: [], passport: {},
      importantWarnings: [], reviews: [], rating: 0, reviewsCount: 0,
      variants: [
        { id: 1, sku: "1", label: "30 см · D12", potDiameterCm: 12, price: 750, stock: 2, images: [], attributes: [] },
        { id: 2, sku: "2", label: "50 см · D12", potDiameterCm: 12, price: 1290, stock: 3, images: [], attributes: [] },
        { id: 3, sku: "3", label: "Переплетённые стволы", potDiameterCm: 12, price: 1690, stock: 1, images: [], attributes: [] },
        { id: 4, sku: "4", label: "D14", potDiameterCm: 14, price: 2050, stock: 1, images: [], attributes: [] },
      ],
    } } }));
    await page.goto("/product/1");
    const sizes = page.getByRole("group", { name: "Размер растения" });
    await expect(sizes.getByRole("button", { name: "30 см · Ø 12 см 750 ₽", exact: true })).toBeVisible();
    const taller = sizes.getByRole("button", { name: "50 см · Ø 12 см 1 290 ₽", exact: true });
    await expect(taller).toBeVisible();
    await expect(sizes.getByRole("button", { name: "Переплетённые стволы · Ø 12 см 1 690 ₽", exact: true })).toBeVisible();
    await expect(sizes.getByRole("button", { name: "Ø 14 см 2 050 ₽", exact: true })).toBeVisible();
    await taller.click();
    await expect(taller).toHaveClass("active");
    await expect(page.locator(".pdp-price-row > strong")).toHaveText("1 290 ₽");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  });
}
