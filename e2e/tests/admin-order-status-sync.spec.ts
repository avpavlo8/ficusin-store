import { expect, test, type Page } from "@playwright/test";
import { owner } from "./helpers";

type MockStatus = "new" | "assembling" | "ready";
type SyncFixture = {
  documentId: string;
  rejectStatus: boolean;
  writes: Array<{ status: string; accepted: boolean }>;
};

declare global {
  interface Window {
    __statusSyncFixture: SyncFixture;
  }
}

const number = "TEST-STATUS-51";
const refusal = "Не удалось изменить статус тестового заказа.";

async function mockPickup(page: Page) {
  await page.addInitScript(
    ({ user, number, refusal }) => {
      const originalFetch = window.fetch.bind(window);
      const state = (window.__statusSyncFixture = {
        documentId: crypto.randomUUID(),
        rejectStatus: false,
        writes: [] as Array<{ status: string; accepted: boolean }>,
      });
      const workflow = (status: MockStatus) => ({
        status,
        queue: status === "new" ? "new" : "in_progress",
        readyForAssembly: status !== "new",
        stockConfirmed: true,
        packagingConfirmed: true,
        blockers:
          status === "new"
            ? [
                {
                  code: "pickup_payment",
                  message: "Оплата при получении — начните сборку вручную",
                  view: "items",
                },
              ]
            : [],
        manualStatuses: [
          status === "new"
            ? "assembling"
            : status === "assembling"
              ? "ready"
              : "completed",
          "cancelled",
        ],
      });
      const order = {
        id: 51,
        orderNumber: number,
        customerId: 1,
        customerName: "Тестовый покупатель",
        phone: "+79990000001",
        email: "status@example.test",
        address: "",
        comment: "",
        deliveryMethod: "pickup",
        deliveryFee: 0,
        deliveryPayee: "shop",
        deliveryFeePending: false,
        repackRequested: false,
        hasPreorder: false,
        paymentMethod: "on_delivery",
        paymentStatus: "on_delivery",
        status: "new" as MockStatus,
        subtotal: 1490,
        total: 1490,
        createdAt: "2026-11-12T10:00:00Z",
        workflow: workflow("new"),
        items: [
          {
            id: 1,
            productId: 1,
            productCode: "status-sync-plant",
            sku: "1001",
            variantLabel: "D12",
            productName: "Тестовое растение",
            unitPrice: 1490,
            quantity: 1,
          },
        ],
      };
      const json = (body: unknown, status = 200) =>
        Promise.resolve(
          new Response(JSON.stringify(body), {
            status,
            headers: { "Content-Type": "application/json" },
          }),
        );
      window.fetch = (input, init) => {
        const raw =
          typeof input === "string"
            ? input
            : input instanceof Request
              ? input.url
              : input.toString();
        const path = new URL(raw, location.origin).pathname;
        const method = (
          init?.method || (input instanceof Request ? input.method : "GET")
        ).toUpperCase();
        if (path === "/api/v1/admin/orders/51" && method === "PATCH") {
          const change = JSON.parse(String(init?.body || "{}")) as {
            status: MockStatus;
          };
          const accepted =
            !state.rejectStatus &&
            order.workflow.manualStatuses.includes(change.status);
          state.writes.push({ status: change.status, accepted });
          if (!accepted) return json({ error: refusal }, 409);
          order.status = change.status;
          order.workflow = workflow(change.status);
          return json({ order });
        }
        // No other mutation, including a navigation side effect, can reach a server.
        if (path.startsWith("/api/v1/") && !["GET", "HEAD"].includes(method))
          return json(
            { error: "Unexpected mutation in status-sync test" },
            409,
          );
        if (path === "/api/v1/auth/me") return json({ user });
        if (path === "/api/v1/admin/dashboard")
          return json({
            user: { fullName: "Тестовый владелец" },
            role: "owner",
            permissions: ["dashboard.read", "orders.read", "orders.edit"],
            dashboard: {
              products: 1,
              variants: 1,
              orders: 1,
              customers: 1,
              wholesalePending: 0,
              lastSync: null,
              recentOrders: [],
            },
          });
        if (path === "/api/v1/admin/orders") return json({ orders: [order] });
        if (path === "/api/v1/admin/orders/51/adjustment")
          return json({
            order: {
              ...order,
              shipmentOffers: [],
              cdekCreateState: "",
              cdekStatus: "",
              cdekStatusReason: "",
              cdekLastError: "",
            },
            payment: {
              total: 1490,
              paid: 0,
              refunded: 0,
              netPaid: 0,
              due: 1490,
              overpaid: 0,
              ready: true,
              paymentStatus: "on_delivery",
              issues: [],
            },
          });
        if (path === "/api/v1/admin/products")
          return json({
            products: [
              {
                id: 1,
                sku: "1001",
                name: "Тестовое растение",
                status: "published",
                price: 1490,
                stock: 5,
              },
            ],
          });
        if (path.startsWith("/api/v1/")) return json({});
        return originalFetch(input, init);
      };
    },
    { user: owner.user, number, refusal },
  );
}

const queueLink = (page: Page, name: string) =>
  page
    .getByRole("navigation", { name: "Статусы заказов" })
    .getByRole("link", { name: new RegExp(`^${name}(?:\\s|[0-9]|$)`) });
const workspace = (page: Page) => page.locator(".admin-order-workspace");
const statusControl = (page: Page) =>
  page.getByRole("combobox", { name: `Статус заказа ${number}`, exact: true });

async function expectCounts(page: Page, fresh: number, work: number) {
  await expect(queueLink(page, "Новые").locator("span")).toHaveText(
    String(fresh),
  );
  await expect(queueLink(page, "В работе").locator("span")).toHaveText(
    String(work),
  );
}

async function openOrder(page: Page) {
  await page.getByRole("link", { name: number, exact: true }).click();
  await expect(workspace(page)).toBeVisible();
  await expect(
    workspace(page).getByText("Тестовое растение", { exact: true }),
  ).toBeVisible();
  // Ensure adjustment's initial workflow has reached review state before PATCH.
  await expect(statusControl(page)).toBeEnabled();
}

test("@desktop @phone pickup status, next action and queues update without reload", async ({
  page,
}) => {
  await mockPickup(page);
  await page.goto("/admin?section=orders");
  const documentId = await page.evaluate(
    () => window.__statusSyncFixture.documentId,
  );
  await expectCounts(page, 1, 0);
  await expect(page.locator(".orders-card .payment-state")).toHaveText(
    "При получении",
  );
  await expect(page.locator(".orders-card .payment-state")).toHaveAttribute(
    "data-tone",
    "on-delivery",
  );
  await openOrder(page);
  await expect(
    workspace(page).getByText("Оплата при получении — начните сборку вручную", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    workspace(page).locator(".order-page-title .ws-pill"),
  ).toHaveText("Новый");
  await statusControl(page).focus();
  await workspace(page)
    .getByRole("button", { name: "Начать сборку", exact: true })
    .click();
  await expect(statusControl(page)).toHaveValue("assembling");
  await expect(
    workspace(page).locator(".order-page-title .ws-pill"),
  ).toHaveText("Собирается");
  await expect(
    workspace(page).getByRole("button", { name: "Заказ собран", exact: true }),
  ).toBeEnabled();
  await expect(
    workspace(page).getByRole("button", { name: "Начать сборку", exact: true }),
  ).toHaveCount(0);

  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expectCounts(page, 0, 1);
  await expect(
    page.getByRole("link", { name: number, exact: true }),
  ).toHaveCount(0);
  await queueLink(page, "В работе").click();
  await openOrder(page);
  await expect(statusControl(page)).toHaveValue("assembling");
  await statusControl(page).selectOption("ready");
  await expect(statusControl(page)).toHaveValue("ready");
  await expect(
    workspace(page).locator(".order-page-title .ws-pill"),
  ).toHaveText("Готов");
  await expect(
    workspace(page).getByRole("button", { name: "Заказ выдан", exact: true }),
  ).toBeEnabled();
  await expect(
    workspace(page).getByRole("button", { name: "Заказ собран", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expectCounts(page, 0, 1);
  await expect(
    page.locator(".orders-cell-status").locator(".orders-status-label"),
  ).toHaveText("Готов");
  expect(await page.evaluate(() => window.__statusSyncFixture.documentId)).toBe(
    documentId,
  );
  expect(await page.evaluate(() => window.__statusSyncFixture.writes)).toEqual([
    { status: "assembling", accepted: true },
    { status: "ready", accepted: true },
  ]);
});

test("@desktop @phone a rejected status PATCH preserves the current status and queues", async ({
  page,
}) => {
  await mockPickup(page);
  await page.goto("/admin?section=orders");
  const documentId = await page.evaluate(
    () => window.__statusSyncFixture.documentId,
  );
  await expectCounts(page, 1, 0);
  await expect(page.locator(".orders-card .payment-state")).toHaveText(
    "При получении",
  );
  await expect(page.locator(".orders-card .payment-state")).toHaveAttribute(
    "data-tone",
    "on-delivery",
  );
  await openOrder(page);
  await page.evaluate(() => {
    window.__statusSyncFixture.rejectStatus = true;
  });
  await statusControl(page).selectOption("assembling");
  await expect(page.getByRole("alert")).toContainText(refusal);
  await expect(statusControl(page)).toHaveValue("new");
  await expect(statusControl(page)).toBeEnabled();
  await expect(
    workspace(page).locator(".order-page-title .ws-pill"),
  ).toHaveText("Новый");
  await expect(
    workspace(page).getByRole("button", { name: "Начать сборку", exact: true }),
  ).toBeEnabled();
  await expect(
    workspace(page).getByRole("button", { name: "Заказ собран", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Закрыть сообщение об ошибке", exact: true })
    .click();
  await expect(page.getByRole("alert")).toHaveCount(0);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expectCounts(page, 1, 0);
  await expect(
    page.getByRole("link", { name: number, exact: true }),
  ).toBeVisible();
  expect(await page.evaluate(() => window.__statusSyncFixture.documentId)).toBe(
    documentId,
  );
  expect(await page.evaluate(() => window.__statusSyncFixture.writes)).toEqual([
    { status: "assembling", accepted: false },
  ]);
});
