import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

test("@desktop @phone customer order tabs retain active orders and paginate cancellation history", async ({
  page,
}) => {
  await mockApi(page, owner);
  const counts = { work: 2, completed: 1, cancelled: 60 };
  await page.route("**/api/v1/account/orders?*", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const view = params.get("view") || "work";
    const offset = Number(params.get("offset") || 0);
    const all = Array.from(
      { length: counts[view as keyof typeof counts] },
      (_, i) => ({
        orderNumber: `${view}-${i + 1}`,
        total: 1490,
        itemsCount: 1,
        status: view === "work" ? (i ? "shipped" : "assembling") : view,
        createdAt: "2026-11-12T09:00:00Z",
      }),
    );
    return route.fulfill({
      json: {
        orders: all.slice(offset, offset + 50),
        counts,
        hasMore: offset + 50 < all.length,
        offset,
        limit: 50,
      },
    });
  });
  await page.goto("/account");
  await expect(
    page.getByRole("tab", { name: "В работе 2", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  const rows = page.getByRole("tabpanel").getByRole("link");
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toHaveAttribute("href", "/account/orders/work-1");
  await expect(rows.first()).toContainText("Собирается");
  await expect(rows.last()).toContainText("В доставке");
  await expect(rows.first().locator(".customer-order-heading")).toHaveText(
    "№ work-1Собирается",
  );
  await expect(rows.last().locator(".customer-order-heading")).toHaveText(
    "№ work-2В доставке",
  );
  await page.getByRole("tab", { name: "Отменены 60", exact: true }).click();
  await expect(rows).toHaveCount(50);
  await page.getByRole("button", { name: "Показать ещё", exact: true }).click();
  await expect(rows).toHaveCount(60);
  await expect(
    page.getByRole("button", { name: "Показать ещё", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("tab", { name: "Выполнены 1", exact: true }).click();
  await expect(rows).toHaveCount(1);
  await expect(rows).toContainText("Выполнен");
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});
