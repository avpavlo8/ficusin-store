import { expect, test } from "@playwright/test";
import { owner } from "./helpers";

async function mockOrderEditor(page: import("@playwright/test").Page) {
  const order = {
    id: 30,
    orderNumber: "0001-30",
    customerName: "Саша Александр",
    customerId: 1,
    phone: "+79156230887",
    email: "test@example.com",
    address: "Москва, 1-й Ботанический пр-д, 5",
    comment: "",
    deliveryMethod: "cdek",
    deliveryFeePending: false,
    paymentMethod: "online",
    paymentStatus: "pending",
    status: "new",
    total: 2480,
    createdAt: "2026-08-20T15:25:19Z",
    items: [
      { productId: "azaliya-d9", productName: "Азалия D9", unitPrice: 790, quantity: 1 },
      { productId: "alokaziya-d6", productName: "Алоказия D6", unitPrice: 690, quantity: 1 },
    ],
  };
  const adjustment = {
    id: 30,
    orderNumber: "0001-30",
    subtotal: 1480,
    deliveryFee: 1000,
    deliveryFeePending: false,
    hasPreorder: false,
    status: "new",
    items: [...order.items],
  };

  await page.addInitScript(({ user, order, adjustment }) => {
    const originalFetch = window.fetch.bind(window);
    const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), {
      status, headers: { "Content-Type": "application/json" },
    }));
    window.fetch = async (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const path = new URL(raw, window.location.origin).pathname;
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json({
        user: { fullName: "Александр" }, role: "owner",
        permissions: ["dashboard.read", "orders.read", "orders.edit"],
        dashboard: { products: 0, variants: 0, orders: 1, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
      });
      if (path === "/api/v1/admin/orders") return json({ orders: [order] });
      if (path === "/api/v1/admin/orders/30/adjustment") return json({
        order: adjustment,
        payment: { total: order.total, paid: 0, refunded: 0, netPaid: 0, due: order.total, overpaid: 0, ready: true, paymentStatus: "pending" },
      });
      if (path === "/api/v1/admin/products") return json({ products: [
        { id: 1, slug: "azaliya-d9", name: "Азалия D9", price: 790, stock: 4, status: "published" },
        { id: 2, slug: "alokaziya-d6", name: "Алоказия D6", price: 690, stock: 3, status: "published" },
        { id: 3, slug: "aglaonema-mariya-kristina-d12", name: "Аглаонема Мария Кристина D12", price: 1490, stock: 2, status: "published" },
      ] });
      if (path === "/api/v1/admin/orders/30/contents" && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body || "{}")) as {
          items: Array<{ productId: string; quantity: number }>;
          deliveryFee?: number;
        };
        (window as typeof window & { __savedOrderEdit?: typeof body }).__savedOrderEdit = body;
        adjustment.items = body.items.map((line) => {
          if (line.productId === "azaliya-d9") return { productId: line.productId, productName: "Азалия D9", unitPrice: 790, quantity: line.quantity };
          if (line.productId === "alokaziya-d6") return { productId: line.productId, productName: "Алоказия D6", unitPrice: 690, quantity: line.quantity };
          return { productId: line.productId, productName: "Аглаонема Мария Кристина D12", unitPrice: 1490, quantity: line.quantity };
        });
        const deliveryConfirmed = typeof body.deliveryFee === "number";
        if (deliveryConfirmed) adjustment.deliveryFee = body.deliveryFee as number;
        adjustment.subtotal = 2970;
        adjustment.deliveryFeePending = !deliveryConfirmed;
        order.items = [...adjustment.items];
        order.total = 2970 + adjustment.deliveryFee;
        order.deliveryFeePending = !deliveryConfirmed;
        return json({
          order,
          adjustment,
          payment: {
            total: order.total, paid: 0, refunded: 0, netPaid: 0,
            due: order.total, overpaid: 0, ready: deliveryConfirmed, paymentStatus: "pending",
          },
        });
      }
      if (path === "/api/v1/admin/orders/30/payment-link" && init?.method === "POST") {
        (window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls =
          ((window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls || 0) + 1;
        return json({
          confirmationUrl: "https://pay.example.test/order-30",
          payment: { total: 3970, paid: 0, refunded: 0, netPaid: 0, due: 3970, overpaid: 0, ready: true, paymentStatus: "pending" },
        });
      }
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, order, adjustment });

  return {
    savedEdit: async () => page.evaluate(() => (window as typeof window & {
      __savedOrderEdit?: { items: Array<{ productId: string; quantity: number }>; deliveryFee?: number };
    }).__savedOrderEdit),
    paymentLinkCalls: async () => page.evaluate(() => (window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls || 0),
  };
}

test("@desktop добавленный товар сохраняется, пересчитывает оплату и оставляет ссылку доступной", async ({ page }) => {
  const state = await mockOrderEditor(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Заказы", exact: true }).click();
  await page.getByText("0001-30").click();

  const editor = page.locator(".admin-order-editor");
  const payment = editor.locator(".admin-order-payment-block");
  await editor.locator("select").first().selectOption("aglaonema-mariya-kristina-d12");
  await editor.getByRole("button", { name: "Добавить", exact: true }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12")).toBeVisible();
  await expect(editor.locator(".admin-order-draft-total")).toContainText(/3.?970/);
  await expect(payment).toContainText(/Итого:\s*3.?970/);
  await expect(payment).toContainText("Сначала сохраните изменения");
  await expect(payment.getByRole("button", { name: "Создать ссылку на доплату" })).toHaveCount(0);

  await editor.getByRole("button", { name: "Сохранить изменения" }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12")).toBeVisible();
  await expect(page.locator(".admin-table tbody tr.clickable").first()).toContainText(/3.?970/);
  await expect(payment).toContainText(/Итого:\s*3.?970/);
  await expect(payment).not.toContainText("Оплата закрыта до подтверждения");
  await expect(payment.getByRole("button", { name: "Создать ссылку на доплату" })).toBeVisible();

  const saved = await state.savedEdit();
  expect(saved?.items).toHaveLength(3);
  expect(saved?.items[2]).toEqual({ productId: "aglaonema-mariya-kristina-d12", quantity: 1 });
  expect(saved?.deliveryFee).toBe(1000);

  await payment.getByRole("button", { name: "Создать ссылку на доплату" }).click();
  await expect(payment.getByRole("link", { name: "Ссылка на оплату" })).toHaveAttribute("href", "https://pay.example.test/order-30");
  expect(await state.paymentLinkCalls()).toBe(1);
});
