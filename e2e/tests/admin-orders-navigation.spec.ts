import { expect, test, type Page } from "@playwright/test";
import { owner } from "./helpers";

async function mockOrders(
  page: Page,
  confirmedUnpaid = false,
  productAvailable = true,
  longNames = false,
  sameNameItems = false,
) {
  const productName = longNames
    ? "Аглаонема Мария с длинным названием декоративного сорта для проверки состава заказа"
    : "Аглаонема Мария";
  const statuses = [
    "new",
    "confirmed",
    "assembling",
    "ready",
    "shipped",
    "completed",
    "cancelled",
  ];
  const orders = statuses.map((status, index) => ({
    id: 51 + index,
    orderNumber: `0001-${51 + index}`,
    customerId: 1,
    customerName: [
      "Марина Новая",
      "Марина Подтверждённая",
      "Марина Сборка",
      "Марина Выдача",
      "Марина Отправленная",
      "Ольга Завершённая",
      "Дарья Отменённая",
    ][index],
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
    paymentStatus:
      status === "new" || (status === "confirmed" && confirmedUnpaid)
        ? "pending"
        : "paid",
    status,
    workflow: {
      status,
      queue: ["new", "confirmed"].includes(status)
        ? "new"
        : ["assembling", "ready"].includes(status)
          ? "in_progress"
          : status === "shipped"
            ? "in_delivery"
            : status,
      readyForAssembly: ["assembling", "ready"].includes(status),
      stockConfirmed: status !== "confirmed",
      packagingConfirmed: status !== "new",
      blockers:
        status === "new"
          ? [
              { code: "payment", message: "Заказ не оплачен", view: "payment" },
              {
                code: "dimensions",
                orderItemIds: [1],
                message: `Не заполнены габариты и вес: ${productName}`,
                view: "delivery",
              },
              {
                code: "delivery_fee",
                message: "Нужно рассчитать и подтвердить стоимость доставки",
                view: "delivery",
              },
            ]
          : status === "confirmed"
            ? [
                ...(confirmedUnpaid
                  ? [
                      {
                        code: "payment",
                        message: "Заказ не оплачен",
                        view: "payment",
                      },
                    ]
                  : []),
                {
                  code: "stock_short",
                  orderItemIds: [1],
                  message: `Не хватает в наличии: ${productName}`,
                  view: "items",
                },
              ]
            : [],
      manualStatuses: [],
    },
    subtotal: longNames && index === 0 ? 1234567.89 : 1490,
    total: longNames && index === 0 ? 1234567.89 : 1490,
    createdAt: "2026-10-22T09:00:00Z",
    items: [
      {
        id: 1,
        productId: productAvailable ? 1 : undefined,
        productCode: productAvailable ? "aglaonema-mariya" : undefined,
        sku: "1001",
        variantLabel: "D12",
        productName,
        unitPrice: longNames && index === 0 ? 1234567.89 : 1490,
        quantity: 1,
      },
    ],
  }));

  if (sameNameItems) {
    const order = orders[0];
    order.items.push({
      ...order.items[0],
      id: 2,
      sku: "1002",
      variantLabel: "D24",
    });
    order.subtotal = order.total = 2980;
    order.workflow.stockConfirmed = false;
    order.workflow.blockers.push({
      code: "stock_unknown",
      orderItemIds: [2],
      view: "items",
      message: `Нет свежего остатка СБИС: ${productName}`,
    });
  }

  // Init-script interception also works after reload and in WebKit. Every API
  // request stays in the browser; a navigation regression cannot touch orders.
  await page.addInitScript(
    ({ user, orders }) => {
      const originalFetch = window.fetch.bind(window);
      const json = (body: unknown, status = 200) =>
        Promise.resolve(
          new Response(JSON.stringify(body), {
            status,
            headers: { "Content-Type": "application/json" },
          }),
        );
      window.fetch = async (input, init) => {
        const raw =
          typeof input === "string"
            ? input
            : input instanceof Request
              ? input.url
              : input.toString();
        const path = new URL(raw, window.location.origin).pathname;
        const method =
          init?.method || (input instanceof Request ? input.method : "GET");
        if (path.startsWith("/api/v1/") && !["GET", "HEAD"].includes(method)) {
          const state = window as typeof window & { __orderWrites?: string[] };
          state.__orderWrites = [
            ...(state.__orderWrites || []),
            `${method} ${path}`,
          ];
          return json(
            { error: "Navigation must not write business data" },
            409,
          );
        }
        if (path === "/api/v1/auth/me") return json({ user });
        if (path === "/api/v1/admin/dashboard")
          return json({
            user: { fullName: "Александр" },
            role: "owner",
            permissions: ["dashboard.read", "orders.read", "orders.edit"],
            dashboard: {
              products: 1,
              variants: 1,
              orders: orders.length,
              customers: 7,
              wholesalePending: 0,
              lastSync: null,
              recentOrders: [],
            },
          });
        if (path === "/api/v1/admin/orders") return json({ orders });
        const packingMatch = path.match(
          /^\/api\/v1\/admin\/orders\/(\d+)\/packing$/,
        );
        if (packingMatch) {
          const order = orders.find(
            (candidate) => candidate.id === Number(packingMatch[1]),
          );
          if (!order) return json({ error: "Заказ не найден" }, 404);
          return json({
            order,
            packing: {
              revision: 1,
              boxes: [],
              tariffCode: 136,
              tariffName: "Посылка склад-склад",
              deliveryFee: 400,
              deliveryPayee: order.deliveryPayee,
              officeCode: "RZN1",
              cityCode: 28,
              state: "draft",
              cdekState: "",
              trackNumber: "",
              paymentUrl: "",
              error: "",
              notification: { state: "not_queued", error: "" },
              blockers: ["В этом заказе доставка оплачивается перевозчику"],
            },
          });
        }
        const adjustmentMatch = path.match(
          /^\/api\/v1\/admin\/orders\/(\d+)\/adjustment$/,
        );
        if (adjustmentMatch) {
          const order = orders.find(
            (candidate) => candidate.id === Number(adjustmentMatch[1]),
          );
          if (!order) return json({ error: "Заказ не найден" }, 404);
          const paid = order.paymentStatus === "paid" ? order.total : 0;
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
              total: order.total,
              paid,
              refunded: 0,
              netPaid: paid,
              due: order.total - paid,
              overpaid: 0,
              ready: !order.deliveryFeePending,
              paymentStatus: order.paymentStatus,
              issues: [],
            },
          });
        }
        if (path === "/api/v1/admin/products")
          return json({
            products: [
              {
                id: 1,
                sku: "1001",
                slug: "aglaonema-mariya",
                variantLabel: "D12",
                name: "Аглаонема Мария",
                price: 1490,
                stock: 5,
                status: "published",
              },
            ],
          });
        if (path.startsWith("/api/v1/")) return json({});
        return originalFetch(input, init);
      };
    },
    { user: owner.user, orders },
  );
}

const orderLinks = (page: Page) =>
  page.locator(".orders-card .orders-number-link");
const statusLink = (page: Page, label: string) =>
  page.getByRole("link", { name: new RegExp(`^${label}(?:\\s|[0-9]|$)`) });

async function expectNoWrites(page: Page) {
  expect(
    await page.evaluate(
      () =>
        (window as typeof window & { __orderWrites?: string[] })
          .__orderWrites || [],
    ),
  ).toEqual([]);
}

test("@desktop @phone orders keep table semantics, full blockers and independent links at every width", async ({
  page,
}) => {
  await mockOrders(page, false, true, true);
  await page.goto("/admin?section=orders");
  const table = page.getByRole("table", { name: "Заказы", exact: true });
  await expect(table).toBeVisible();
  await expect(table.getByRole("columnheader")).toHaveText([
    "Заказ",
    "Товары",
    "Покупатель",
    "Доставка",
    "Статус",
    "Оплата",
    "Сумма",
  ]);
  await expect(table.getByRole("row")).toHaveCount(3);
  const row = table.getByRole("row").filter({
    has: page.getByRole("link", { name: "0001-51", exact: true }),
  });
  await expect(row.getByRole("cell")).toHaveCount(7);
  await expect(
    row.locator(".orders-cell-status").locator(".orders-status-label"),
  ).toHaveText("Новый");
  await expect(
    row.getByRole("link", {
      name: "Аглаонема Мария с длинным названием декоративного сорта для проверки состава заказа",
      exact: true,
    }),
  ).toHaveAttribute("href", "/product/aglaonema-mariya?sku=1001");
  await expect(table.locator("a a")).toHaveCount(0);
  const itemIssue = row.locator(".orders-card-product .orders-item-issue");
  await expect(itemIssue).toHaveText("Нет габаритов");
  await expect(itemIssue).toHaveAttribute(
    "title",
    "Не заполнены габариты и вес: Аглаонема Мария с длинным названием декоративного сорта для проверки состава заказа",
  );
  const reasons = row.getByRole("list", {
    name: "Почему заказ ещё не на сборке",
  });
  await expect(reasons).toHaveCount(0);
  await row.locator("details.orders-attention > summary").click();
  await expect(reasons.getByRole("listitem")).toHaveText([
    "Нужно рассчитать и подтвердить стоимость доставки",
  ]);
  for (const reason of await reasons.getByRole("listitem").all()) {
    await expect(reason).toBeVisible();
    expect(
      await reason.evaluate(
        (element) =>
          element.scrollHeight <= element.clientHeight + 1 &&
          element.scrollWidth <= element.clientWidth + 1,
      ),
      "The complete blocker must fit without clipping or an inner scrollbar",
    ).toBe(true);
  }
  expect(
    await table.evaluate(
      (element) => element.scrollWidth <= element.clientWidth + 1,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
  await expectNoWrites(page);
});

test("@desktop @phone item issues stay on the matching order item when product names are identical", async ({
  page,
}) => {
  await mockOrders(page, false, true, false, true);
  await page.goto("/admin?section=orders");
  const row = page
    .getByRole("row")
    .filter({ has: page.getByRole("link", { name: "0001-51", exact: true }) });
  const items = row.locator(".orders-card-product");
  await expect(items).toHaveCount(2);
  await expect(
    items.getByRole("link", { name: "Аглаонема Мария", exact: true }),
  ).toHaveCount(2);
  const first = items.filter({ hasText: "D12" });
  const second = items.filter({ hasText: "D24" });
  await expect(first.locator(".orders-item-issue")).toHaveText("Нет габаритов");
  await expect(first.locator(".orders-item-issue")).toHaveAttribute(
    "title",
    "Не заполнены габариты и вес: Аглаонема Мария",
  );
  await expect(second.locator(".orders-item-issue")).toHaveText(
    "Проверить остаток",
  );
  await expect(second.locator(".orders-item-issue")).toHaveAttribute(
    "title",
    "Нет свежего остатка СБИС: Аглаонема Мария",
  );
  await expect(first).not.toContainText("Проверить остаток");
  await expect(second).not.toContainText("Нет габаритов");
  await row.locator("details.orders-attention > summary").click();
  await expect(
    row
      .getByRole("list", { name: "Почему заказ ещё не на сборке" })
      .getByRole("listitem"),
  ).toHaveText(["Нужно рассчитать и подтвердить стоимость доставки"]);
  await expectNoWrites(page);
});

test("@desktop очереди отделяют новые с причинами, сборку, доставку и завершённые заказы", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders");

  await expect(orderLinks(page)).toHaveText(["0001-51", "0001-52"]);
  for (const label of [
    "Новые",
    "В работе",
    "В доставке",
    "Завершённые",
    "Все",
    "Отменённые",
  ]) {
    await expect(statusLink(page, label)).toBeVisible();
    await expect(statusLink(page, label)).toHaveAttribute(
      "href",
      /section=orders/,
    );
  }

  const newOrder = page.locator(".orders-card").filter({
    has: page.getByRole("link", { name: "0001-51", exact: true }),
  });
  await newOrder.locator("details.orders-attention > summary").click();
  const newReasons = newOrder.getByRole("list", {
    name: "Почему заказ ещё не на сборке",
  });
  await expect(newReasons.getByRole("listitem")).toHaveText([
    "Нужно рассчитать и подтвердить стоимость доставки",
  ]);
  const confirmed = page
    .locator(".orders-card")
    .filter({ has: page.getByRole("link", { name: "0001-52", exact: true }) });
  await expect(confirmed.locator(".payment-state")).toHaveText("Оплачен");
  await expect(confirmed.locator(".payment-state")).toHaveAttribute(
    "data-tone",
    "paid",
  );
  await expect(
    confirmed.locator(".orders-card-product .orders-item-issue"),
  ).toHaveText("Не хватает в наличии");
  await expect(
    confirmed.locator(".orders-card-product .orders-item-issue"),
  ).toHaveAttribute("title", "Не хватает в наличии: Аглаонема Мария");
  await expect(confirmed.locator("details.orders-attention")).toHaveCount(0);

  await statusLink(page, "В работе").click();
  await expect(page).toHaveURL(/status=in_progress/);
  await expect(orderLinks(page)).toHaveText(["0001-53", "0001-54"]);
  await expect(
    page.getByRole("list", { name: "Почему заказ ещё не на сборке" }),
  ).toHaveCount(0);

  await statusLink(page, "В доставке").click();
  await expect(page).toHaveURL(/status=in_delivery/);
  await expect(orderLinks(page)).toHaveText(["0001-55"]);
  await expect(
    page.getByRole("list", { name: "Почему заказ ещё не на сборке" }),
  ).toHaveCount(0);

  await statusLink(page, "Завершённые").click();
  await expect(page).toHaveURL(/status=completed/);
  await expect(orderLinks(page)).toHaveText(["0001-56"]);

  await statusLink(page, "Все").click();
  await expect(orderLinks(page)).toHaveText([
    "0001-51",
    "0001-52",
    "0001-53",
    "0001-54",
    "0001-55",
    "0001-56",
    "0001-57",
  ]);
  if (process.env.FICUSIN_PREVIEW_OUTPUT_DIR) {
    await page.screenshot({
      path: `${process.env.FICUSIN_PREVIEW_OUTPUT_DIR}/orders-list-desktop.png`,
      fullPage: true,
    });
  }

  await statusLink(page, "Отменённые").click();
  await expect(page).toHaveURL(/status=cancelled/);
  await expect(orderLinks(page)).toHaveText(["0001-57"]);
  await statusLink(page, "Новые").click();
  await expect(orderLinks(page)).toHaveText(["0001-51", "0001-52"]);
  await expectNoWrites(page);
});

test("@desktop поиск, страница заказа и возврат сохраняют выбранную очередь", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&status=in_progress");
  await page.getByRole("searchbox", { name: "Найти заказ" }).fill("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-53", "0001-54"]);
  await expect
    .poll(() => new URL(page.url()).searchParams.get("q"))
    .toBe("Марина");

  const number = page.getByRole("link", { name: "0001-53", exact: true });
  await expect(number).toHaveAttribute("href", /order=53/);
  await number.click();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-53", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("searchbox", { name: "Найти заказ" }),
  ).toHaveCount(0);
  await expect(orderLinks(page)).toHaveCount(0);
  await expect(page.locator(".admin-order-editor")).toBeVisible();
  await expect(page).toHaveURL(/order=53/);

  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-53", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(
    page.getByRole("searchbox", { name: "Найти заказ" }),
  ).toHaveValue("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-53", "0001-54"]);
  expect(new URL(page.url()).searchParams.get("status")).toBe("in_progress");
  expect(new URL(page.url()).searchParams.has("order")).toBe(false);

  await page.getByRole("link", { name: "0001-54", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-54", exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(orderLinks(page)).toHaveText(["0001-53", "0001-54"]);
  await page.goForward();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-54", exact: true }),
  ).toBeVisible();
  await expectNoWrites(page);
});

test("@desktop поиск и возврат сохраняют очередь доставки", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&status=in_delivery");
  await page.getByRole("searchbox", { name: "Найти заказ" }).fill("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-55"]);
  await expect
    .poll(() => new URL(page.url()).searchParams.get("q"))
    .toBe("Марина");
  await orderLinks(page).click();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-55", exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-55", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(
    page.getByRole("searchbox", { name: "Найти заказ" }),
  ).toHaveValue("Марина");
  await expect(orderLinks(page)).toHaveText(["0001-55"]);
  expect(new URL(page.url()).searchParams.get("status")).toBe("in_delivery");
  expect(new URL(page.url()).searchParams.has("order")).toBe(false);
  await expectNoWrites(page);
});

test("@desktop прямая и старая ссылки открывают заказ вне очереди новых", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&order=56");
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-56", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("searchbox", { name: "Найти заказ" }),
  ).toHaveCount(0);

  await page.goto("/admin?section=orders&customerOrderId=57");
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-57", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(
    page.getByRole("searchbox", { name: "Найти заказ" }),
  ).toBeVisible();
  expect(new URL(page.url()).searchParams.has("customerOrderId")).toBe(false);
  await expectNoWrites(page);
});

test("@desktop неизвестный заказ объясняет ошибку и позволяет вернуться", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders&status=all&order=9999");
  await expect(page.getByText(/Заказ не найден/)).toBeVisible();
  await expect(page.locator(".admin-order-editor")).toHaveCount(0);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(orderLinks(page)).toHaveCount(7);
  await page
    .getByRole("searchbox", { name: "Найти заказ" })
    .fill("несуществующий заказ");
  await expect(
    page.getByText("Заказы не найдены", { exact: true }),
  ).toBeVisible();
  await expect(orderLinks(page)).toHaveCount(0);
  await expectNoWrites(page);
});

test("@phone заказ открывается отдельной страницей без горизонтального переполнения", async ({
  page,
}) => {
  await mockOrders(page);
  await page.goto("/admin?section=orders");
  await page.getByRole("link", { name: "0001-51", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-51", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".admin-order-editor")).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(1);
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(orderLinks(page)).toHaveText(["0001-51", "0001-52"]);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(1);
  await expectNoWrites(page);
});

test("@desktop подтвержденный неоплаченный остаётся в новых", async ({
  page,
}) => {
  await mockOrders(page, true);
  await page.goto("/admin?section=orders&status=new");
  await expect(orderLinks(page)).toHaveText(["0001-51", "0001-52"]);
  const confirmed = page
    .locator(".orders-card")
    .filter({ has: page.getByRole("link", { name: "0001-52", exact: true }) });
  await expect(confirmed.locator(".payment-state")).toHaveText(
    "Ожидает оплаты",
  );
  await expect(
    confirmed.locator(".orders-card-product .orders-item-issue"),
  ).toHaveText("Не хватает в наличии");
  await expect(
    confirmed.locator(".orders-card-product .orders-item-issue"),
  ).toHaveAttribute("title", "Не хватает в наличии: Аглаонема Мария");
  await expect(confirmed.locator("details.orders-attention")).toHaveCount(0);
  await statusLink(page, "В работе").click();
  await expect(orderLinks(page)).toHaveText(["0001-53", "0001-54"]);
  await statusLink(page, "В доставке").click();
  await expect(orderLinks(page)).toHaveText(["0001-55"]);
  await expectNoWrites(page);
});

for (const queue of [
  { status: "new", number: "0001-51", id: 51 },
  { status: "in_progress", number: "0001-53", id: 53 },
  { status: "in_delivery", number: "0001-55", id: 55 },
  { status: "completed", number: "0001-56", id: 56 },
]) {
  test(`@desktop @phone явные ссылки заказа и товара разделены в очереди ${queue.status}`, async ({
    page,
  }) => {
    await mockOrders(page);
    await page.goto(`/admin?section=orders&status=${queue.status}`);
    const card = page.locator(".orders-card").filter({
      has: page.getByRole("link", { name: queue.number, exact: true }),
    });
    await expect(card).toContainText("Аглаонема Мария");
    await expect(card).toContainText("1 шт");
    await expect(card.locator("a")).toHaveCount(2);
    await expect(card.locator("button,input,select")).toHaveCount(0);
    const product = card.getByRole("link", {
      name: "Аглаонема Мария",
      exact: true,
    });
    await expect(product).toHaveAttribute(
      "href",
      "/product/aglaonema-mariya?sku=1001",
    );
    await expect(product).toHaveAttribute("target", "_blank");
    await expect(product).toHaveAttribute("rel", "noopener noreferrer");
    const order = card.getByRole("link", { name: queue.number, exact: true });
    const href = await order.getAttribute("href");
    expect(href).not.toBeNull();
    const url = new URL(href!, page.url());
    expect(url.searchParams.get("order")).toBe(String(queue.id));
    expect(url.searchParams.get("status")).toBe(queue.status);
    await expect(card.locator("a a")).toHaveCount(0);
    await order.click();
    await expect(
      page.getByRole("heading", { name: `Заказ ${queue.number}`, exact: true }),
    ).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`order=${queue.id}(?:&|$)`));
    await expectNoWrites(page);
  });
}

test("@desktop @phone недоступный товар остаётся текстом без неверной ссылки", async ({
  page,
}) => {
  await mockOrders(page, false, false);
  await page.goto("/admin?section=orders");
  const card = page.locator(".orders-card").first();
  await expect(card.locator(".orders-card-product strong")).toHaveText(
    "Аглаонема Мария",
  );
  await expect(card.locator(".order-product-link")).toHaveCount(0);
  await expect(card.locator(".orders-card-product a")).toHaveCount(0);
  await expect(card.locator("a")).toHaveCount(1);
  await card.getByRole("link", { name: "0001-51", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Заказ 0001-51", exact: true }),
  ).toBeVisible();
  await expectNoWrites(page);
});
