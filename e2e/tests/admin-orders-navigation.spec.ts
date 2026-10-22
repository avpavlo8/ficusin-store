import { expect, test, type Page } from "@playwright/test";
import { owner } from "./helpers";

async function mockOrders(page: Page, confirmedUnpaid = false) {
  const statuses = ["new", "confirmed", "assembling", "ready", "shipped", "completed", "cancelled"];
  const orders = statuses.map((status, index) => ({
    id: 51 + index,
    orderNumber: `0001-${51 + index}`,
    customerId: 1,
    customerName: ["Марина Новая", "Марина Подтверждённая", "Анна Сборка", "Вера Выдача", "Марина Отправленная", "Ольга Завершённая", "Дарья Отменённая"][index],
    phone: `+7999000000${index}`,
    email: `customer${index}@example.test`,
    address: "Рязань, Ботаническая улица, 5",
    comment: "Позвонить перед отправкой",
    deliveryMethod: "cdek",
    deliveryFee: 400,
    deliveryPayee: "carrier",
    deliveryFeePending: status === "new",
    repackRequested: false,
    hasPreorder: false,
    paymentMethod: "online",
    paymentStatus: status === "new" || (status === "confirmed" && confirmedUnpaid) ? "pending" : "paid",
    status,
    subtotal: 1490,
    total: 1490,
    createdAt: "2026-10-22T09:00:00Z",
    items: [{ id: 1, productId: 1, sku: "1001", variantLabel: "D12", productName: "Аглаонема Мария", unitPrice: 1490, quantity: 1 }],
  }));

  // Init-script interception also works after reload and in WebKit. Every API
  // request stays in the browser; a navigation regression cannot touch orders.
  await page.addInitScript(({ user, orders }) => {
    const originalFetch = window.fetch.bind(window);
    const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), {
      status, headers: { "Content-Type": "application/json" },
    }));
    window.fetch = async (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const path = new URL(raw, window.location.origin).pathname;
      const method = init?.method || (input instanceof Request ? input.method : "GET");
      if (path.startsWith("/api/v1/") && !["GET", "HEAD"].includes(method)) {
        const state = window as typeof window & { __orderWrites?: string[] };
        state.__orderWrites = [...(state.__orderWrites || []), `${method} ${path}`];
        return json({ error: "Navigation must not write business data" }, 409);
      }
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json({
        user: { fullName: "Александр" }, role: "owner",
        permissions: ["dashboard.read", "orders.read", "orders.edit"],
        dashboard: { products: 1, variants: 1, orders: orders.length, customers: 7, wholesalePending: 0, lastSync: null, recentOrders: [] },
      });
      if (path === "/api/v1/admin/orders") return json({ orders });
      const packingMatch = path.match(/^\/api\/v1\/admin\/orders\/(\d+)\/packing$/);
      if (packingMatch) {
        const order = orders.find((candidate) => candidate.id === Number(packingMatch[1]));
        if (!order) return json({ error: "Заказ не найден" }, 404);
        return json({ order, packing: {
          revision: 1, boxes: [], tariffCode: 136, tariffName: "Посылка склад-склад", deliveryFee: 400,
          deliveryPayee: order.deliveryPayee, officeCode: "RZN1", cityCode: 28, state: "draft", cdekState: "",
          trackNumber: "", paymentUrl: "", error: "", notification: { state: "not_queued", error: "" },
          blockers: ["В этом заказе доставка оплачивается перевозчику"],
        } });
      }
      const adjustmentMatch = path.match(/^\/api\/v1\/admin\/orders\/(\d+)\/adjustment$/);
      if (adjustmentMatch) {
        const order = orders.find((candidate) => candidate.id === Number(adjustmentMatch[1]));
        if (!order) return json({ error: "Заказ не найден" }, 404);
        const paid = order.paymentStatus === "paid" ? order.total : 0;
        return json({
          order: { ...order, shipmentOffers: [], cdekCreateState: "", cdekStatus: "", cdekStatusReason: "", cdekLastError: "" },
          payment: { total: order.total, paid, refunded: 0, netPaid: paid, due: order.total - paid, overpaid: 0, ready: !order.deliveryFeePending, paymentStatus: order.paymentStatus, issues: [] },
        });
      }
      if (path === "/api/v1/admin/products") return json({ products: [
        { id: 1, sku: "1001", slug: "aglaonema-mariya", variantLabel: "D12", name: "Аглаонема Мария", price: 1490, stock: 5, status: "published" },
      ] });
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, { user: owner.user, orders });
}

const orderLinks = (page: Page) => page.getByRole("link", { name: /^0001-\d+$/ });
const statusLink = (page: Page, label: string) => page.getByRole("link", { name: new RegExp(`^${label}(?:\\s|[0-9]|$)`) });

async function expectNoWrites(page: Page) {
  expect(await page.evaluate(() => (window as typeof window & { __orderWrites?: string[] }).__orderWrites || [])).toEqual([]);
}

test("@desktop статусы отделяют новые, рабочие и отменённые заказы", async ({ page }) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders");

  await expect(orderLinks(page)).toHaveText(["0001-51"]);
  for (const label of ["Новые", "В работе", "Завершённые", "Все", "Отменённые"]) {
    await expect(statusLink(page, label)).toBeVisible();
    await expect(statusLink(page, label)).toHaveAttribute("href", /section=orders/);
  }

  await statusLink(page, "В работе").click();
  await expect(page).toHaveURL(/status=in_progress/);
  await expect(orderLinks(page)).toHaveText(["0001-52", "0001-53", "0001-54", "0001-55"]);

  await statusLink(page, "Завершённые").click();
  await expect(page).toHaveURL(/status=completed/);
  await expect(orderLinks(page)).toHaveText(["0001-56"]);

  await statusLink(page, "Все").click();
  await expect(orderLinks(page)).toHaveText(["0001-51", "0001-52", "0001-53", "0001-54", "0001-55", "0001-56", "0001-57"]);
  if (process.env.FICUSIN_PREVIEW_OUTPUT_DIR) {
    await page.screenshot({ path: `${process.env.FICUSIN_PREVIEW_OUTPUT_DIR}/orders-list-desktop.png`, fullPage: true });
  }

  await statusLink(page, "Отменённые").click();
  await expect(page).toHaveURL(/status=cancelled/);
  await expect(orderLinks(page)).toHaveText(["0001-57"]);
  await statusLink(page, "Новые").click();
  await expect(orderLinks(page)).toHaveText(["0001-51"]);
  await expectNoWrites(page);
});

test("@desktop поиск, страница заказа и возврат сохраняют выбранную очередь", async ({ page }) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&status=in_progress");
  await page.getByRole("searchbox", { name: "Найти заказ" }).fill("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-52", "0001-55"]);
  await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe("Марина");

  const number = page.getByRole("link", { name: "0001-52", exact: true });
  await expect(number).toHaveAttribute("href", /order=52/);
  await number.click();
  await expect(page.getByRole("heading", { name: "Заказ 0001-52", exact: true })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "Найти заказ" })).toHaveCount(0);
  await expect(orderLinks(page)).toHaveCount(0);
  await expect(page.locator(".admin-order-editor")).toBeVisible();
  await expect(page).toHaveURL(/order=52/);

  await page.reload();
  await expect(page.getByRole("heading", { name: "Заказ 0001-52", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(page.getByRole("searchbox", { name: "Найти заказ" })).toHaveValue("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-52", "0001-55"]);
  expect(new URL(page.url()).searchParams.get("status")).toBe("in_progress");
  expect(new URL(page.url()).searchParams.has("order")).toBe(false);

  await page.getByRole("link", { name: "0001-55", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Заказ 0001-55", exact: true })).toBeVisible();
  await page.goBack();
  await expect(orderLinks(page)).toHaveText(["0001-52", "0001-55"]);
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Заказ 0001-55", exact: true })).toBeVisible();
  await expectNoWrites(page);
});

test("@desktop прямая и старая ссылки открывают заказ вне очереди новых", async ({ page }) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&order=56");
  await expect(page.getByRole("heading", { name: "Заказ 0001-56", exact: true })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "Найти заказ" })).toHaveCount(0);

  await page.goto("/admin?section=orders&customerOrderId=57");
  await expect(page.getByRole("heading", { name: "Заказ 0001-57", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(page.getByRole("searchbox", { name: "Найти заказ" })).toBeVisible();
  expect(new URL(page.url()).searchParams.has("customerOrderId")).toBe(false);
  await expectNoWrites(page);
});

test("@desktop неизвестный заказ объясняет ошибку и позволяет вернуться", async ({ page }) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&status=all&order=9999");
  await expect(page.getByText(/Заказ не найден/)).toBeVisible();
  await expect(page.locator(".admin-order-editor")).toHaveCount(0);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(orderLinks(page)).toHaveCount(7);
  await page.getByRole("searchbox", { name: "Найти заказ" }).fill("несуществующий заказ");
  await expect(page.getByText("Заказы не найдены", { exact: true })).toBeVisible();
  await expect(orderLinks(page)).toHaveCount(0);
  await expectNoWrites(page);
});

test("@phone заказ открывается отдельной страницей без горизонтального переполнения", async ({ page }) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders");
  await page.getByRole("link", { name: "0001-51", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Заказ 0001-51", exact: true })).toBeVisible();
  await expect(page.locator(".admin-order-editor")).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(orderLinks(page)).toHaveText(["0001-51"]);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1);
  await expectNoWrites(page);
});

test("@desktop подтвержденный неоплаченный остаётся в новых", async ({page})=>{
 await mockOrders(page,true);
 await page.goto("/admin?section=orders&status=new");
 await expect(orderLinks(page)).toHaveText(["0001-51","0001-52"]);
 await statusLink(page,"В работе").click();
 await expect(orderLinks(page)).toHaveText(["0001-53","0001-54","0001-55"]);
});
