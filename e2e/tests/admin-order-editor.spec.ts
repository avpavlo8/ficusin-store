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
  let savedItems: Array<{ productId: string; quantity: number }> = [];

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
        const body = JSON.parse(String(init.body || "{}")) as { items: Array<{ productId: string; quantity: number }> };
        (window as typeof window & { __savedOrderItems?: typeof body.items }).__savedOrderItems = body.items;
        adjustment.items = body.items.map((line) => {
          if (line.productId === "azaliya-d9") return { productId: line.productId, productName: "Азалия D9", unitPrice: 790, quantity: line.quantity };
          if (line.productId === "alokaziya-d6") return { productId: line.productId, productName: "Алоказия D6", unitPrice: 690, quantity: line.quantity };
          return { productId: line.productId, productName: "Аглаонема Мария Кристина D12", unitPrice: 1490, quantity: line.quantity };
        });
        adjustment.subtotal = 2970;
        adjustment.deliveryFeePending = true;
        order.items = [...adjustment.items];
        order.total = 3970;
        order.deliveryFeePending = true;
        return json({
          order,
          adjustment,
          payment: { total: 3970, paid: 0, refunded: 0, netPaid: 0, due: 3970, overpaid: 0, ready: false, paymentStatus: "pending" },
        });
      }
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, order, adjustment });

  return {
    savedItems: async () => page.evaluate(() => (window as typeof window & { __savedOrderItems?: Array<{ productId: string; quantity: number }> }).__savedOrderItems || []),
  };
}

test("@desktop добавленный товар сохраняется и сумма заказа пересчитывается", async ({ page }) => {
  const state = await mockOrderEditor(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Заказы" }).click();
  await page.getByText("0001-30").click();

  const editor = page.locator(".admin-order-editor");
  await editor.locator("select").first().selectOption("aglaonema-mariya-kristina-d12");
  await editor.getByRole("button", { name: "Добавить", exact: true }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12")).toBeVisible();
  await expect(editor.locator(".admin-order-draft-total")).toContainText(/3.?970/);

  await editor.getByRole("button", { name: "Сохранить изменения" }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12")).toBeVisible();
  await expect(page.locator(".admin-table tbody tr.clickable").first()).toContainText(/3.?970/);
  savedItems = await state.savedItems();
  expect(savedItems).toHaveLength(3);
  expect(savedItems[2]).toEqual({ productId: "aglaonema-mariya-kristina-d12", quantity: 1 });
});
