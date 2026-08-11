import { expect, test } from "@playwright/test";
import { horizontalOverflow, owner } from "./helpers";

async function mockProcurement(page: import("@playwright/test").Page) {
  const procurement = {
    summary: { openOrders: 1, unresolvedAliases: 12, availabilityChecks: 3, openRequests: 2 },
    integrations: { wb: true, ozon: false, saby: false },
    settings: { version: 1, defaultExchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 63700, trolleyVolumeCm3: 1, trolleyFillRatio: 1, returnLossRate: 0, marketplaceCostRate: 0, taxRate: 0, reserveRate: 0, packageRub: 0, priceChangeThreshold: .1, domesticRetailMultiplier: 1, internationalCostMultiplier: 1, internationalRetailMultiplier: 1, marketplaceStrikeMarkup: 0, retailRoundStep: 1, avoidRoundHundreds: false, recommendationDays: 30, targetCoverDays: 30 },
    suppliers: [{ id: 1, name: "Тестовый поставщик", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }],
    orders: [{ id: 4, supplierId: 1, supplierName: "Тестовый поставщик", orderNumber: "TEST-100", documentNumber: "", sourceKind: "payment_invoice", currency: "RUB", status: "draft", lines: 5, units: 20, total: 10000, unmatched: 2, createdAt: "2026-08-10T12:00:00Z" }],
    documents: [{ id: 7, supplierId: 1, supplierName: "Тестовый поставщик", orderId: 4, fileName: "test.pdf", parserKind: "domestic_payment_invoice", parseStatus: "review", arithmeticStatus: "ok", documentNumber: "TEST-100", documentDate: "2026-08-07", currency: "RUB", lines: 5, units: 20, productSubtotal: 10000, packageTotal: 0, documentTotal: 10000, calculatedTotal: 10000, parseError: "", createdAt: "2026-08-10T12:00:00Z" }],
    review: [{ id: 9, supplierId: 1, supplierName: "Тестовый поставщик", rawName: "Тестовая строка D10", supplierArticle: "", potDiameterCm: 10, suggestedSabyId: "TEST-SABY-1", suggestedSabyName: "Тестовый товар D10", matchStatus: "suggested", confidence: 0.52, availabilityStatus: "unknown" }],
    requests: [], availability: [], recommendations: [],
  };
  const orderDetail = {
    order: procurement.orders[0], costs: { exchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 0, deliveryToRyazanRub: 0 },
    validation: { canCalculate: false, canPrepareActions: false, blockers: ["Не сопоставлено строк: 2"], arithmeticMismatch: 0, comparisonMismatch: 0, missingDimensions: 0, missingLoadUnits: 0, invalidLines: 0, unmatched: 2, trolleyCount: 0, expectedTrolleyRub: 0, allocatedTrolleyRub: 0, expectedRyazanRub: 0, allocatedRyazanRub: 0 },
    lines: [], batches: [],
  };
  const dashboard = {
      user: { fullName: "Тестовый владелец" }, role: "owner",
      permissions: ["dashboard.read", "procurement.read", "procurement.edit"],
      dashboard: { products: 331, variants: 331, orders: 0, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
  };
  // Install the API stub inside the page before React starts. WebKit handles
  // route interception differently after state transitions, while fetch is
  // identical across the browsers this layout suite covers.
  await page.addInitScript(({ user, dashboard, procurement, orderDetail }) => {
    const originalFetch = window.fetch.bind(window);
    const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), {
      status: 200, headers: { "Content-Type": "application/json" },
    }));
    window.fetch = (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const path = new URL(raw, window.location.origin).pathname;
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json(dashboard);
      if (path === "/api/v1/admin/procurement") return json(procurement);
      if (path === "/api/v1/admin/procurement/orders/4") return json(orderDetail);
      if (path === "/api/v1/admin/procurement/nomenclature") return json({ items: [{ sabyId: "TEST-SABY-1", code: "TEST-001", article: "TEST-ARTICLE", name: "Тестовый товар D10", balance: 4, price: 100 }] });
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, dashboard, procurement, orderDetail });
}

test("@desktop procurement opens inside the existing admin panel", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Изменения только после подтверждения")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("TEST-100").first()).toBeVisible();
  await expect(page.getByText("Российский счёт")).toBeVisible();
  await expect(page.getByText("Суммы сходятся")).toBeVisible();
  await expect(page.getByText("Тестовая строка D10")).toBeVisible();
  await expect(page.getByText("2 не сопоставлено")).toBeVisible();
  await page.getByRole("button", { name: "Сопоставить" }).click();
  await expect(page.getByRole("dialog", { name: "Сопоставить товар" })).toBeVisible();
  await expect(page.getByText("Тестовый товар D10")).toBeVisible();
  await expect(page.getByRole("button", { name: "Это новый товар" })).toBeVisible();
});

test("@phone procurement does not break the admin layout", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();

  await expect(page.getByRole("heading", { name: "Закупки", exact: true })).toBeVisible();
  await expect(page.getByText("Изменения только после подтверждения")).toBeVisible({ timeout: 15_000 });
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  const addSupplier = page.getByRole("button", { name: "Добавить поставщика" });
  await expect(addSupplier).toBeVisible();
  await addSupplier.click({ force: true });
  await expect(page.getByRole("dialog", { name: "Новый поставщик" })).toBeVisible();
});

test("@desktop procurement blocks calculation until invoice checks pass", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.getByText("TEST-100").first().click();
  await expect(page.getByText("Расчёт заблокирован")).toBeVisible();
  await expect(page.getByText("Не сопоставлено строк: 2")).toBeVisible();
  await expect(page.getByRole("button", { name: "Рассчитать" })).toBeDisabled();
});
