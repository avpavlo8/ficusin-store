import { expect, test, type Page } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

const dashboard = {
  user: { fullName: "Александр" }, role: "owner",
  permissions: ["dashboard.read", "analytics.read", "orders.read", "products.read", "procurement.read", "customers.read"],
  dashboard: {
    products: 643, variants: 711, orders: 38, customers: 4, wholesalePending: 2,
    lastSync: { status: "success", itemsUpdated: 1000 },
    recentOrders: [
      { orderNumber: "TEST-NEW", customerName: "Покупатель", total: 3000, status: "new" },
      { orderNumber: "TEST-CANCELLED", customerName: "Покупатель", total: 2000, status: "cancelled" },
    ],
  },
};
const analytics = { revenue: 3000, orders: 1, daily: [{ date: "2026-09-08", revenue: 1000, orders: 0 }, { date: "2026-09-09", revenue: 2000, orders: 1 }] };

async function setup(page: Page) {
  await mockApi(page, owner);
  await page.route("**/api/v1/admin/dashboard", route => route.fulfill({ json: dashboard }));
  await page.route("**/api/v1/admin/analytics?days=*", route => route.fulfill({ json: analytics }));
}

test("@desktop @phone workspace preserves real navigation and filters recent orders", async ({ page }) => {
  await setup(page);
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Всё растёт." })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Разделы управления" }).getByRole("button")).toHaveCount(10);
  await expect(page.getByRole("button", { name: "Маркетплейсы", exact: true })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Склад", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "В работе", exact: true }).click();
  await expect(page.getByText("TEST-NEW", { exact: true })).toBeVisible();
  await expect(page.getByText("TEST-CANCELLED", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Отменённые", exact: true }).click();
  await expect(page.getByText("TEST-CANCELLED", { exact: true })).toBeVisible();
  await expect(page.getByText("TEST-NEW", { exact: true })).toHaveCount(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone Avito operations view surfaces risks and gates activation", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/avito", route => route.fulfill({ json: {
    state: { configured: true, publicationEnabled: false, feedUrl: "https://ficusin.ru/feeds/avito/test.xml", lastImportAt: "2026-10-04T08:00:00Z", lastWorkerAt: "2026-10-04T08:01:00Z", lastSuccessAt: "2026-10-04T08:01:00Z", lastError: "" },
    items: [
      { itemId: "101", title: "Дипсис Арека", status: "active", url: "https://www.avito.ru/items/101", remotePrice: 2490, desiredPublished: false, lastReconciledAt: "2026-10-04T08:01:00Z", lastError: "", products: [{ id: 1, name: "Дипсис Арека D12", slug: "areca", image: "", price: 2490, stock: 0 }] },
      { itemId: "102", title: "Фикус Бенджамина", status: "active", url: "https://www.avito.ru/items/102", remotePrice: 1990, desiredPublished: true, lastReconciledAt: "2026-10-04T08:01:00Z", lastError: "", products: [] },
    ],
  } }));
  await page.goto("/admin?section=avito");
  await expect(page.getByRole("heading", { name: "Авито", exact: true })).toBeVisible();
  await expect(page.getByText("Требуется внимание", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: /Не применилось · 1/ })).toBeVisible();
  await page.getByRole("button", { name: "Проверить готовность и включить" }).click();
  await expect(page.getByRole("heading", { name: "Проверка готовности" })).toBeVisible();
  await expect(page.getByText(/Без привязки: 1/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Включить управление" })).toBeDisabled();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone analytics failure does not trap the dashboard and can be retried", async ({ page }) => {
  await setup(page);
  let calls = 0;
  await page.route("**/api/v1/admin/analytics?days=*", route => ++calls === 1
    ? route.fulfill({ status: 503, json: { error: "Unavailable" } })
    : route.fulfill({ json: analytics }));
  await page.goto("/admin");
  await expect(page.getByText("Аналитика временно недоступна")).toBeVisible();
  await expect(page.getByText("TEST-NEW", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Попробовать снова" }).click();
  await expect(page.getByRole("img", { name: /Выручка всех каналов за 7 дней/ })).toBeVisible();
  await expect(page.getByText("Аналитика временно недоступна")).toHaveCount(0);
});

test("@desktop search opens permitted sections by keyboard", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/customers", route => route.fulfill({ json: { customers: [] } }));
  await page.goto("/admin");
  await page.getByRole("searchbox", { name: "Найти раздел" }).fill("Клиенты");
  await page.getByRole("searchbox", { name: "Найти раздел" }).press("Enter");
  await expect(page.getByRole("heading", { name: "Клиенты", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation").getByRole("button", { name: "Клиенты", exact: true })).toHaveAttribute("aria-current", "page");
});

test("@desktop restricted role has no inaccessible shortcuts or analytics requests", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/dashboard", route => route.fulfill({ json: { ...dashboard, role: "manager", permissions: ["dashboard.read", "products.read"] } }));
  let analyticsRequests = 0;
  page.on("request", request => { if (request.url().includes("/admin/analytics")) analyticsRequests++; });
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Всё растёт." })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Разделы управления" }).getByRole("button")).toHaveCount(2);
  await expect(page.getByRole("button", { name: "Настройки", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Открыть заказы" })).toHaveCount(0);
  await page.getByRole("searchbox", { name: "Найти раздел" }).fill("Настройки");
  await expect(page.getByText("Раздел не найден")).toBeVisible();
  expect(analyticsRequests).toBe(0);
});
