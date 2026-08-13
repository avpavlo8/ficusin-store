import { expect, test } from "@playwright/test";
import { horizontalOverflow, owner } from "./helpers";

async function mockProcurement(page: import("@playwright/test").Page, options: { blockers?: string[] | null; currency?: "RUB" | "EUR" } = {}) {
  const procurement = {
    summary: { openOrders: 1, unresolvedAliases: 12, availabilityChecks: 3, openRequests: 2 },
    integrations: { wb: true, ozon: false, saby: false },
    settings: { version: 1, defaultExchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 63700, trolleyVolumeCm3: 1, trolleyFillRatio: 1, returnLossRate: 0, marketplaceCostRate: 0, taxRate: 0, reserveRate: 0, packageRub: 0, priceChangeThreshold: .1, domesticRetailMultiplier: 1, internationalCostMultiplier: 1, internationalRetailMultiplier: 1, marketplaceStrikeMarkup: 0, retailRoundStep: 1, avoidRoundHundreds: false, recommendationDays: 30, targetCoverDays: 30, retailMarkupMultiplier: 2.1, roundPrices: true },
    suppliers: [{ id: 1, name: "Тестовый поставщик", kind: "domestic", countryCode: "RU", defaultCurrency: "RUB", active: true, createdAt: "2026-08-10T12:00:00Z" }],
    orders: [{ id: 4, supplierId: 1, supplierName: "Тестовый поставщик", orderNumber: "TEST-100", documentNumber: "", sourceKind: "payment_invoice", currency: options.currency || "RUB", status: "draft", lines: 5, units: 20, total: 10000, unmatched: 2, createdAt: "2026-08-10T12:00:00Z" }],
    documents: [{ id: 7, supplierId: 1, supplierName: "Тестовый поставщик", orderId: 4, fileName: "test.pdf", parserKind: "domestic_payment_invoice", parseStatus: "review", arithmeticStatus: "ok", documentNumber: "TEST-100", documentDate: "2026-08-07", currency: "RUB", lines: 5, units: 20, productSubtotal: 10000, packageTotal: 0, documentTotal: 10000, calculatedTotal: 10000, parseError: "", createdAt: "2026-08-10T12:00:00Z" }],
    review: [{ id: 9, supplierId: 1, supplierName: "Тестовый поставщик", rawName: "Тестовая строка D10", supplierArticle: "", potDiameterCm: 10, suggestedSabyId: "TEST-SABY-1", suggestedSabyName: "Тестовый товар D10", matchStatus: "suggested", confidence: 0.52, availabilityStatus: "unknown" }],
    requests: [], availability: [], recommendations: [
      { aliasId: 9, supplierId: 1, sabyId: "TEST-SABY-1", name: "Тестовый товар D10", supplierArticle: "SUP-1", availability: "available", balance: 2, incoming: 0, siteSales: 2, sabySales: 5, wbSales: 1, ozonSales: 0, totalSales: 8, customerRequests: 1, staffRequests: 0, openRequests: 1, minimumOrderQty: 6, orderMultiple: 6, suggestedQty: 12, dailySales: 0.27, daysOfCover: 7.5, status: "recommended", reason: "Есть товар под заказ клиента" },
      { aliasId: 10, supplierId: 1, sabyId: "TEST-SABY-2", name: "Товар уже едет", supplierArticle: "SUP-2", availability: "available", balance: 0, incoming: 6, siteSales: 0, sabySales: 4, wbSales: 0, ozonSales: 0, totalSales: 4, customerRequests: 0, staffRequests: 0, openRequests: 0, minimumOrderQty: 1, orderMultiple: 1, suggestedQty: 0, dailySales: 0.13, daysOfCover: 0, status: "already_ordered", reason: "Уже заказано 6 шт.; повторная закупка исключена" },
    ],
    salesSync: [
      { channel: "saby", status: "ok", lastSuccessAt: "2026-08-10T12:00:00Z", lastError: "", rowsSynced: 120, periodFrom: "2025-08-11", periodTo: "2026-08-10", latestSale: "2026-08-10" },
      { channel: "site", status: "ok", lastSuccessAt: "2026-08-10T12:00:00Z", lastError: "", rowsSynced: 12, periodFrom: "2025-08-11", periodTo: "2026-08-10", latestSale: "2026-08-09" },
      { channel: "wb", status: "disabled", lastError: "", rowsSynced: 0, periodFrom: "", periodTo: "", latestSale: "" },
      { channel: "ozon", status: "disabled", lastError: "", rowsSynced: 0, periodFrom: "", periodTo: "", latestSale: "" },
    ],
    integrationHealth: [
      { channel: "wb", configured: true, lastError: "" },
      { channel: "ozon", configured: true, lastError: "" },
      { channel: "saby", configured: false, lastError: "" },
    ],
  };
  const orderDetail = {
    order: procurement.orders[0], costs: { exchangeRate: 1, trolleyCostCurrency: 0, trolleyCostRub: 0, deliveryToMoscowRub: 0, deliveryToRyazanRub: 0 },
    validation: { canCalculate: false, canPrepareActions: false, blockers: options.blockers === undefined ? ["Не сопоставлено строк: 2"] : options.blockers, arithmeticMismatch: 0, comparisonMismatch: 0, missingDimensions: 0, missingLoadUnits: 0, invalidLines: 0, unmatched: 2, trolleyCount: 0, expectedTrolleyRub: 0, allocatedTrolleyRub: 0, expectedRyazanRub: 0, allocatedRyazanRub: 0 },
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
      if (path === "/api/v1/admin/procurement/suppliers/1" && init?.method === "DELETE") {
        procurement.suppliers = [];
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      if (path === "/api/v1/admin/procurement/integrations/wb/check" && init?.method === "POST") {
        return json({ integration: { channel: "wb", configured: true, lastCheckedAt: "2026-08-13T12:00:00Z", lastSuccessAt: "2026-08-13T12:00:00Z", lastError: "" } });
      }
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
  const addSupplier = page.getByRole("button", { name: "Поставщики" });
  await expect(addSupplier).toBeVisible();
  await addSupplier.click({ force: true });
  await expect(page.getByRole("dialog", { name: "Поставщики" })).toBeVisible();
});

test("@desktop supplier deletion uses the site dialog and handles an empty success response", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.getByRole("button", { name: "Поставщики" }).click();
  await page.getByRole("button", { name: "Удалить" }).click();

  const confirmation = page.getByRole("alertdialog", { name: "Удалить поставщика?" });
  await expect(confirmation).toBeVisible();
  await expect(confirmation.getByText("Тестовый поставщик")).toBeVisible();
  await confirmation.getByRole("button", { name: "Удалить" }).click();
  await expect(confirmation).toBeHidden();
  await expect(page.getByRole("dialog", { name: "Поставщики" }).getByText("Тестовый поставщик")).toHaveCount(0);
});

test("@desktop marketplace check shows its result immediately", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.getByRole("button", { name: "Интеграции" }).click();
  const wb = page.locator(".integration-health-grid article").filter({ hasText: "Wildberries" });
  await wb.getByRole("button", { name: "Проверить подключение" }).click();
  await expect(wb.getByRole("status")).toHaveText("Wildberries: подключение работает");
  await expect(wb).toHaveClass(/connected/);
});

test("@desktop procurement blocks calculation until invoice checks pass", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.locator(".procurement-orders").getByText("TEST-100").click();
  await expect(page.getByText("Расчёт заблокирован")).toBeVisible();
  await expect(page.getByText("Не сопоставлено строк: 2")).toBeVisible();
  await expect(page.getByRole("button", { name: "Рассчитать" })).toBeDisabled();
});

test("@desktop procurement opens an order with no validation blockers", async ({ page }) => {
  await mockProcurement(page, { blockers: null, currency: "EUR" });
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.locator(".procurement-orders").getByText("TEST-100").click();

  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByText("Проверки пройдены")).toBeVisible();

  const delivery = page.getByLabel("Москва → Рязань, весь инвойс, ₽");
  await delivery.click();
  await delivery.pressSequentially("14000");
  await expect(delivery).toHaveValue("14000");
});

test("@desktop procurement shows one markup and clear rounding settings", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.getByRole("button", { name: "Формула v1" }).click();

  await expect(page.getByLabel("Наценка на закупочную стоимость, %")).toHaveValue("110");
  await expect(page.getByLabel("Менять цену при отклонении более, %")).toHaveValue("10");
  await expect(page.getByLabel("Округлять цены до ближайших 50 или 90")).toBeChecked();
  await expect(page.getByLabel("База Голландии")).toHaveCount(0);
  await expect(page.getByLabel("Объём телеги, см³")).toHaveCount(0);
});

test("@desktop procurement separates actionable and already ordered recommendations", async ({ page }) => {
  await mockProcurement(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Закупки" }).click();
  await page.getByRole("button", { name: "Что заказать" }).click();

  await expect(page.getByText("Тестовый товар D10")).toBeVisible();
  await expect(page.getByText("12 шт.")).toBeVisible();
  await page.getByRole("button", { name: "Уже заказано 1" }).click();
  await expect(page.getByText("Товар уже едет")).toBeVisible();
  await expect(page.getByText("повторная закупка исключена")).toBeVisible();
});
