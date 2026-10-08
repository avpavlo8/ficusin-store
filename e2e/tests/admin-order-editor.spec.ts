import { expect, test } from "@playwright/test";
import { owner } from "./helpers";

async function mockOrderEditor(page: import("@playwright/test").Page, stockShortage = false) {
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
    deliveryFeePending: true,
    paymentMethod: "online",
    paymentStatus: "pending",
    status: "new",
    total: 1480,
    createdAt: "2026-08-20T15:25:19Z",
    items: [
      { productId: 1, sku: "1001", variantLabel: "D9", productName: "Азалия D9", unitPrice: 790, quantity: 1 },
      { productId: 2, sku: "1002", variantLabel: "D6", productName: "Алоказия D6", unitPrice: 690, quantity: 1 },
    ],
  };
  const adjustment = {
    id: 30,
    orderNumber: "0001-30",
    subtotal: 1480,
    deliveryFee: 1000,
    deliveryPayee: "carrier",
    deliveryFeePending: true,
    hasPreorder: false,
    status: "new",
    items: [...order.items],
    cdekCreateState: stockShortage ? "manual_review" : "",
    cdekLastError: stockShortage ? "Оплаченный заказ: остатка растений недостаточно для отправки. Проверьте наличие и решите вручную." : "",
    shipmentOffers: stockShortage ? [{
      id: 73, status: "paid", deliveryFee: 590, deliveryPayee: "shop", total: 1380,
      cdekCreateState: "manual_review", cdekLastError: "Оплаченная отправка: остатка растений недостаточно. Проверьте наличие и решите вручную.",
      cdekTrackNumber: "", cdekStatus: "", cdekStatusReason: "", boxes: [], items: [],
    }] : [],
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
        payment: { total: order.total, paid: 0, refunded: 0, netPaid: 0, due: order.total, overpaid: 0, ready: false, paymentStatus: "pending" },
      });
      if (path === "/api/v1/admin/products") return json({ products: [
        { id: 1, slug: "azaliya-d9", sku: "1001", variantLabel: "D9", name: "Азалия D9", price: 790, stock: 4, status: "published" },
        { id: 2, slug: "alokaziya-d6", sku: "1002", variantLabel: "D6", name: "Алоказия D6", price: 690, stock: 3, status: "published" },
        { id: 3, slug: "aglaonema-mariya-kristina-d12", sku: "1003", variantLabel: "D12", name: "Аглаонема Мария Кристина D12", price: 1490, stock: 2, status: "published" },
      ] });
      if (path === "/api/v1/admin/orders/30/contents" && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body || "{}")) as {
          items: Array<{ sku: string; quantity: number }>;
          deliveryFee: number;
        };
        (window as typeof window & { __savedOrderEdit?: typeof body }).__savedOrderEdit = body;
        adjustment.items = body.items.map((line) => {
          if (line.sku === "1001") return { productId: 1, sku: line.sku, variantLabel: "D9", productName: "Азалия D9", unitPrice: 790, quantity: line.quantity };
          if (line.sku === "1002") return { productId: 2, sku: line.sku, variantLabel: "D6", productName: "Алоказия D6", unitPrice: 690, quantity: line.quantity };
          return { productId: 3, sku: line.sku, variantLabel: "D12", productName: "Аглаонема Мария Кристина D12", unitPrice: 1490, quantity: line.quantity };
        });
        adjustment.deliveryFee = body.deliveryFee;
        adjustment.subtotal = 2970;
        adjustment.deliveryFeePending = false;
        order.items = [...adjustment.items];
        order.total = 2970;
        order.deliveryFeePending = false;
        return json({
          order,
          adjustment,
          payment: {
            total: order.total, paid: 0, refunded: 0, netPaid: 0,
            due: order.total, overpaid: 0, ready: true, paymentStatus: "pending",
          },
        });
      }
      if (path === "/api/v1/admin/orders/30/payment-link" && init?.method === "POST") {
        (window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls =
          ((window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls || 0) + 1;
        return json({
          confirmationUrl: "https://pay.example.test/order-30",
          payment: { total: 2970, paid: 0, refunded: 0, netPaid: 0, due: 2970, overpaid: 0, ready: true, paymentStatus: "pending" },
        });
      }
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, order, adjustment });

  return {
    savedEdit: async () => page.evaluate(() => (window as typeof window & {
      __savedOrderEdit?: { items: Array<{ sku: string; quantity: number }>; deliveryFee: number };
    }).__savedOrderEdit),
    paymentLinkCalls: async () => page.evaluate(() => (window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls || 0),
  };
}

test("@desktop сохранение менеджером обновляет состав и открывает оплату только растений", async ({ page }) => {
  const state = await mockOrderEditor(page);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Заказы", exact: true }).click();
  await page.getByText("0001-30").click();

  const editor = page.locator(".admin-order-editor");
  const payment = editor.locator(".admin-order-payment-block");

  await expect(editor.getByRole("checkbox")).toHaveCount(0);
  await expect(editor.getByText("После сохранения эта сумма станет итогом заказа")).toBeVisible();
  await expect(editor.getByText("Доставка оплачивается перевозчику при получении и не входит в сумму оплаты растений.")).toBeVisible();

  await editor.locator("select").first().selectOption("1003");
  await editor.getByRole("button", { name: "Добавить", exact: true }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12", { exact: true })).toBeVisible();
  await expect(editor.locator(".admin-order-draft-total")).toContainText(/2.?970/);
  await expect(payment).toContainText(/Растения к оплате:\s*2.?970/);
  await expect(payment).toContainText("Сначала сохраните изменения");
  await expect(payment.getByRole("button", { name: "Создать ссылку на оплату" })).toHaveCount(0);

  await editor.getByRole("button", { name: "Сохранить изменения" }).click();

  await expect(editor.getByText("Аглаонема Мария Кристина D12", { exact: true })).toBeVisible();
  await expect(page.locator(".admin-table tbody tr.clickable").first()).toContainText(/2.?970/);
  await expect(payment).toContainText(/Растения к оплате:\s*2.?970/);
  await expect(payment.getByRole("button", { name: "Создать ссылку на оплату" })).toBeVisible();

  const saved = await state.savedEdit();
  expect(saved?.items).toHaveLength(3);
  expect(saved?.items[2]).toEqual({ sku: "1003", quantity: 1 });
  expect(saved?.deliveryFee).toBe(1000);

  await payment.getByRole("button", { name: "Создать ссылку на оплату" }).click();
  await expect(payment.getByRole("link", { name: "Ссылка на оплату" })).toHaveAttribute("href", "https://pay.example.test/order-30");
  expect(await state.paymentLinkCalls()).toBe(1);
});

test("@desktop админка объясняет остановку оплаченных отправок из-за остатка", async ({ page }) => {
  await mockOrderEditor(page, true);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Заказы", exact: true }).click();
  await page.getByText("0001-30").click();

  const editor = page.locator(".admin-order-editor");
  await expect(editor.getByText(/Отправка оплаченного заказа 0001-30 остановлена: остатка растений недостаточно/)).toBeVisible();
  await expect(editor.getByText(/Отправка №73 оплачена, но остатка растений недостаточно/)).toBeVisible();
  await expect(editor.getByText(/Проверьте заказ 0001-30 в кабинете СДЭК/)).toHaveCount(0);
});
