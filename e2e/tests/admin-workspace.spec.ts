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
  await expect(page.getByRole("navigation", { name: "Разделы управления" }).getByRole("button")).toHaveCount(9);
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
  await expect(page.getByRole("img", { name: /Выручка сайта за 7 дней/ })).toBeVisible();
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
