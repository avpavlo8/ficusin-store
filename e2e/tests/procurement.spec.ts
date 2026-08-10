import { expect, test } from "@playwright/test";
import { horizontalOverflow, owner } from "./helpers";

async function mockProcurement(page: import("@playwright/test").Page) {
  const procurement = {
    summary: { openOrders: 1, unresolvedAliases: 12, availabilityChecks: 3, openRequests: 2 },
    suppliers: [{ id: 1, name: "ТК Ярославский", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }],
    orders: [{ id: 4, supplierId: 1, supplierName: "ТК Ярославский", orderNumber: "П3-11660", documentNumber: "", sourceKind: "payment_invoice", currency: "RUB", status: "draft", lines: 5, units: 154, total: 53900, unmatched: 2, createdAt: "2026-08-10T12:00:00Z" }],
    review: [{ id: 9, supplierId: 1, supplierName: "ТК Ярославский", rawName: "Фикус Лирата d 10", supplierArticle: "", potDiameterCm: 10, suggestedSabyId: "X7582076", suggestedSabyName: "Фикус Лирата D27", matchStatus: "suggested", confidence: 0.52, availabilityStatus: "unknown" }],
  };
  // A single handler avoids route precedence races in WebKit: every API
  // request used by this scenario is resolved deterministically here.
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/auth/me") return route.fulfill({ json: { user: owner.user } });
    if (path === "/api/v1/admin/dashboard") return route.fulfill({ json: {
      user: { fullName: "Александр" }, role: "owner",
      permissions: ["dashboard.read", "procurement.read", "procurement.edit"],
      dashboard: { products: 331, variants: 331, orders: 0, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
    } });
    if (path === "/api/v1/admin/procurement") return route.fulfill({ json: procurement });
    return route.fulfill({ json: {} });
  });
  // WebKit can miss the broad glob for requests started after a React state
  // transition. The exact regex is registered last so it has top priority.
  await page.route(/\/api\/v1\/admin\/procurement(?:\?.*)?$/, (route) =>
    route.fulfill({ json: procurement }));
}

test("@desktop procurement opens inside the existing admin panel", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Безопасный режим включён")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("П3-11660")).toBeVisible();
  await expect(page.getByText("Фикус Лирата d 10")).toBeVisible();
  await expect(page.getByText("2 не сопоставлено")).toBeVisible();
});

test("@phone procurement does not break the admin layout", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Безопасный режим включён")).toBeVisible({ timeout: 15_000 });
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  const addSupplier = page.getByRole("button", { name: "Добавить поставщика" });
  await expect(addSupplier).toBeVisible();
  await addSupplier.click({ force: true });
  await expect(page.getByRole("dialog", { name: "Новый поставщик" })).toBeVisible();
});
