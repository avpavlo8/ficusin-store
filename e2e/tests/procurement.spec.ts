import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

async function mockProcurement(page: import("@playwright/test").Page) {
  await mockApi(page, owner);
  await page.route("**/api/v1/admin/dashboard", (route) => route.fulfill({ json: {
    user: { fullName: "Александр" }, role: "owner",
    permissions: ["dashboard.read", "procurement.read", "procurement.edit"],
    dashboard: { products: 331, variants: 331, orders: 0, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
  } }));
  await page.route("**/api/v1/admin/procurement", (route) => route.fulfill({ json: {
    summary: { openOrders: 1, unresolvedAliases: 12, availabilityChecks: 3, openRequests: 2 },
    suppliers: [{ id: 1, name: "ТК Ярославский", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }],
    orders: [{ id: 4, supplierId: 1, supplierName: "ТК Ярославский", orderNumber: "П3-11660", documentNumber: "", sourceKind: "payment_invoice", currency: "RUB", status: "draft", lines: 5, units: 154, total: 53900, unmatched: 2, createdAt: "2026-08-10T12:00:00Z" }],
    review: [{ id: 9, supplierId: 1, supplierName: "ТК Ярославский", rawName: "Фикус Лирата d 10", supplierArticle: "", potDiameterCm: 10, suggestedSabyId: "X7582076", suggestedSabyName: "Фикус Лирата D27", matchStatus: "suggested", confidence: 0.52, availabilityStatus: "unknown" }],
  } }));
}

test("@desktop procurement opens inside the existing admin panel", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки" })).toBeVisible();
  await expect(page.getByText("Безопасный режим включён")).toBeVisible();
  await expect(page.getByText("П3-11660")).toBeVisible();
  await expect(page.getByText("Фикус Лирата d 10")).toBeVisible();
  await expect(page.getByText("2 не сопоставлено")).toBeVisible();
});

test("@phone procurement does not break the admin layout", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки" })).toBeVisible();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await page.getByRole("button", { name: "Добавить поставщика" }).click();
  await expect(page.getByRole("dialog", { name: "Новый поставщик" })).toBeVisible();
});
