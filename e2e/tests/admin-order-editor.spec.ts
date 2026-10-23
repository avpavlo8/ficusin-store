import { expect, test } from "@playwright/test";
import { owner } from "./helpers";

async function mockOrderEditor(page: import("@playwright/test").Page, stockShortage = false, packingMode?: "success" | "retry" | "destination", dimensionsKnown = false, refundRole?: "owner" | "manager") {
  const dimensions = dimensionsKnown ? { packageLengthCm: 20, packageWidthCm: 20, packageHeightCm: 40, packageWeightGrams: 700 } : {};
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
    deliveryPayee: packingMode ? "shop" : "carrier",
    repackRequested: Boolean(packingMode),
    deliveryFeePending: true,
    paymentMethod: "online",
    paymentStatus: "pending",
    status: "new",
    total: 1480,
    createdAt: "2026-08-20T15:25:19Z",
    items: [
      { id: 11, productId: 1, sku: "1001", variantLabel: "D9", productName: "Азалия D9", unitPrice: 790, quantity: 1, ...dimensions },
      { id: 12, productId: 2, sku: "1002", variantLabel: "D6", productName: "Алоказия D6", unitPrice: 690, quantity: 1, ...dimensions },
    ],
  };
  const adjustment = {
    id: 30,
    orderNumber: "0001-30",
    subtotal: 1480,
    discountType: "fixed", discountValue: 0, discountAmount: 0,
    deliveryFee: packingMode ? 0 : 1000,
    deliveryPayee: order.deliveryPayee,
    deliveryMethod: "cdek",
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

  await page.addInitScript(({ user, order, adjustment, packingMode, refundRole }) => {
    const originalFetch = window.fetch.bind(window);
    const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), {
      status, headers: { "Content-Type": "application/json" },
    }));
    const packing = {
      revision: 1, boxes: [] as unknown[], tariffCode: 136, tariffName: "Посылка склад-склад",
      deliveryFee: 0, deliveryPayee: order.deliveryPayee, officeCode: packingMode === "destination" ? "" : "RZN1", cityCode: packingMode === "destination" ? 0 : 28,
      state: "draft", cdekState: "", trackNumber: "", paymentUrl: "", error: "",
      notification: { state: "unavailable", error: "Отправка писем не настроена" },
      push: { state: "not_queued", error: "" },
      quoteBlockers: packingMode === "destination" ? ["В заказе не указан пункт выдачи СДЭК", "В заказе не указан город СДЭК"] : [],
      blockers: packingMode === "destination" ? ["В заказе не указан пункт выдачи СДЭК", "В заказе не указан город СДЭК"] : packingMode ? [] : ["В этом заказе доставка оплачивается перевозчику"],
    };
    const refunds = refundRole ? [{ id: 1, amount: 100, status: "succeeded", providerId: "refund-1", actorId: 1, needsReview: false, receiptStatus: "pending", lastError: "" }, { id: 2, amount: 40, status: "pending", providerId: "refund-2", actorId: 1, needsReview: false, receiptStatus: "pending", lastError: "" }] : [];
    let finalized = false;
    let polls = 0;
    let finalizationAttempts = 0;
    window.fetch = async (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const url = new URL(raw, window.location.origin);
      const path = url.pathname;
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json({
        user: { fullName: "Александр" }, role: "owner",
        permissions: ["dashboard.read", "orders.read", "orders.edit", ...(refundRole === "owner" ? ["payments.refund"] : [])],
        dashboard: { products: 0, variants: 0, orders: 1, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
      });
      if (path === "/api/v1/admin/orders") return json({ orders: [order] });
      if (path === "/api/v1/delivery/cdek" && url.searchParams.get("action") === "cities") return json({ cities: [{ code: 44, city: "Москва", region: "Москва" }] });
      if (path === "/api/v1/delivery/cdek" && url.searchParams.get("action") === "offices") return json({ offices: [{ code: "MSK1", name: "Пункт Ленина", location: { address: "Москва, ул. Ленина, 3" } }] });
      if (path === "/api/v1/admin/orders/30/packing") {
        if (finalized) {
          polls += 1;
          (window as typeof window & { __packingPolls?: number }).__packingPolls = polls;
          const phase = (window as typeof window & { __packingPhase?: number }).__packingPhase || 0;
          if (phase === 1) packing.state = "waiting_cdek";
          if (phase >= 2) {
            packing.state = "preparing_payment";
            packing.cdekState = "registered";
            packing.trackNumber = "103399900001";
          }
          if (phase >= 3) {
            packing.state = "notification_queued";
            packing.paymentUrl = "https://pay.example.test/order-30-packed";
            packing.notification.state = "queued";
          }
          if (phase >= 4) { packing.state = "notification_queued"; packing.notification.state = "failed"; packing.notification.error = "SMTP temporary failure"; packing.push.state = "sent"; }
        }
        return json({ packing, order });
      }
      if (path === "/api/v1/admin/orders/30/packing/quote" && init?.method === "POST") {
        const body = JSON.parse(String(init.body || "{}"));
        (window as typeof window & { __packingQuote?: unknown }).__packingQuote = body;
        if (body.boxes.some((box: { contents: Array<{ quantity: number }> }) => box.contents.some((line) => line.quantity <= 0))) return json({ error: "некорректный состав коробки 1" }, 400);
        return json({ revision: 1, quotes: [{ tariffCode: 136, tariffName: "Посылка склад-склад", price: 590, daysMin: 2, daysMax: 4 }] });
      }
      if (path === "/api/v1/admin/orders/30/packing/finalize" && init?.method === "POST") {
        const body = JSON.parse(String(init.body || "{}"));
        const state = window as typeof window & { __packingFinalizations?: unknown[] };
        state.__packingFinalizations = [...(state.__packingFinalizations || []), body];
        finalizationAttempts += 1;
        if (packingMode === "retry" && finalizationAttempts === 1) return json({ error: "СДЭК временно недоступен. Упаковка не сохранена, повторите попытку." }, 409);
        finalized = true;
        packing.state = "queued";
        packing.boxes = body.boxes;
        packing.deliveryFee = 590;
        order.deliveryFeePending = false;
        order.status = "confirmed";
        adjustment.status = "confirmed";
        order.total = 2070;
        adjustment.deliveryFee = 590;
        adjustment.deliveryFeePending = false;
        return json({ packing, order }, 202);
      }
      if (path === "/api/v1/admin/orders/30/adjustment") return json({
        order: adjustment,
        payment: { total: order.total, paid: refundRole ? 1480 : 0, refunded: refundRole ? 100 : 0, netPaid: refundRole ? 1380 : 0, due: order.total, overpaid: 0, ready: false, paymentStatus: "pending", refunds },
      });
      if (path === "/api/v1/admin/products") return json({ products: [
        { id: 1, slug: "azaliya-d9", sku: "1001", variantLabel: "D9", name: "Азалия D9", price: 790, stock: 4, status: "published" },
        { id: 2, slug: "alokaziya-d6", sku: "1002", variantLabel: "D6", name: "Алоказия D6", price: 690, stock: 3, status: "published" },
        { id: 3, slug: "aglaonema-mariya-kristina-d12", sku: "1003", variantLabel: "D12", name: "Аглаонема Мария Кристина D12", price: 1490, stock: 2, status: "published" },
      ] });
      if (path === "/api/v1/admin/orders/30/contents" && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body || "{}")) as {
          items: Array<{ sku: string; quantity: number }>;
          deliveryFee?: number;
          discount?: { type: "fixed" | "percent"; value: number };
        };
        (window as typeof window & { __savedOrderEdit?: typeof body }).__savedOrderEdit = body;
        adjustment.items = body.items.map((line) => {
          if (line.sku === "1001") return { id: 11, productId: 1, sku: line.sku, variantLabel: "D9", productName: "Азалия D9", unitPrice: 790, quantity: line.quantity };
          if (line.sku === "1002") return { id: 12, productId: 2, sku: line.sku, variantLabel: "D6", productName: "Алоказия D6", unitPrice: 690, quantity: line.quantity };
          return { id: 13, productId: 3, sku: line.sku, variantLabel: "D12", productName: "Аглаонема Мария Кристина D12", unitPrice: 1490, quantity: line.quantity };
        });
        if(body.deliveryFee != null) { adjustment.deliveryFee=body.deliveryFee; adjustment.deliveryFeePending=false; }
        const gross = adjustment.items.reduce((sum,item)=>sum+item.unitPrice*item.quantity,0);
        adjustment.discountType=body.discount?.type ?? "fixed";
        adjustment.discountValue=body.discount?.value ?? 0;
        adjustment.discountAmount=adjustment.discountType === "percent" ? Math.round(gross*adjustment.discountValue)/100 : adjustment.discountValue;
        adjustment.subtotal=gross-adjustment.discountAmount;
        order.items = [...adjustment.items];
        order.total = adjustment.subtotal + (order.deliveryPayee === "shop" ? adjustment.deliveryFee : 0);
        order.deliveryFeePending = adjustment.deliveryFeePending;
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
  }, { user: owner.user, order, adjustment, packingMode, refundRole });

  return {
    savedEdit: async () => page.evaluate(() => (window as typeof window & {
      __savedOrderEdit?: { items: Array<{ sku: string; quantity: number }>; deliveryFee: number };
    }).__savedOrderEdit),
    paymentLinkCalls: async () => page.evaluate(() => (window as typeof window & { __paymentLinkCalls?: number }).__paymentLinkCalls || 0),
    packingFinalizations: async () => page.evaluate(() => (window as typeof window & {
      __packingFinalizations?: Array<{ revision: number; idempotencyKey: string; tariffCode: number; boxes: Array<{ lengthCm: number; widthCm: number; heightCm: number; weightGrams: number; contents: Array<{ orderItemId: number; quantity: number }> }> }>;
    }).__packingFinalizations || []),
    packingPolls: async () => page.evaluate(() => (window as typeof window & { __packingPolls?: number }).__packingPolls || 0),
  };
}

test("@desktop скидка в рублях и процентах сохраняется в итог заказа", async ({page}) => {
 const state=await mockOrderEditor(page);
 await page.goto("/admin?section=orders&order=30");
 const editor=page.locator(".admin-order-editor");
 await expect(page.getByText("Заполните размеры коробки",{exact:true})).toBeVisible();
 await expect(editor.getByText("Стоимость доставки вручную",{exact:true})).toHaveCount(0);
 await expect(editor.getByText("Частичные отправки",{exact:true})).toHaveCount(0);
 await expect(editor.getByText("Оплата и возвраты",{exact:true})).toHaveCount(0);
 await editor.getByRole("combobox",{name:"Добавить товар в заказ"}).selectOption("1003");
 await editor.getByRole("button",{name:"Добавить",exact:true}).click();
 await editor.getByRole("spinbutton",{name:"Скидка на заказ"}).fill("100");
 await expect(editor.locator(".admin-order-draft-total")).toContainText(/2.?870/);
 await editor.getByRole("combobox",{name:"Единица скидки"}).selectOption("percent");
 await editor.getByRole("spinbutton",{name:"Скидка на заказ"}).fill("10");
 await expect(editor.locator(".admin-order-draft-total")).toContainText(/2.?673/);
 await editor.getByRole("button",{name:"Сохранить изменения",exact:true}).click();
 await expect(editor.getByRole("spinbutton",{name:"Скидка на заказ"})).toHaveValue("10");
 await expect(editor.locator(".admin-order-draft-total")).toContainText(/2.?673/);
 expect(await state.savedEdit()).toMatchObject({discount:{type:"percent",value:10}});
 expect(await state.savedEdit()).not.toHaveProperty("deliveryFee");
 expect(await state.paymentLinkCalls()).toBe(0);
});

test("@desktop админка объясняет остановку оплаченных отправок из-за остатка", async ({ page }) => {
  await mockOrderEditor(page, true);
  await page.goto("/admin");
  await page.getByRole("button", { name: "Заказы", exact: true }).click();
  await page.getByRole("link", { name: "0001-30", exact: true }).click();

  const editor = page.locator(".admin-order-editor");
  await expect(page.getByRole("region", { name: "Почему требуется менеджер" })).toContainText("остатка растений недостаточно");
  await editor.locator("summary").filter({ hasText: "Отправления" }).click();
  await expect(editor.getByText(/Отправка №73 оплачена, но остатка растений недостаточно/)).toBeVisible();
  await expect(editor.getByText(/Проверьте заказ 0001-30 в кабинете СДЭК/)).toHaveCount(0);
});

test("@desktop известные размеры не подменяют неизвестную причину расчёта доставки", async ({ page }) => {
  await mockOrderEditor(page, false, undefined, true);
  await page.goto("/admin?section=orders&order=30");
  await expect(page.locator(".admin-order-editor")).toBeVisible();
  const reasons = page.getByRole("region", { name: "Почему требуется менеджер" });
  await expect(reasons).toContainText("Рассчитать доставку");
  await expect(reasons).not.toContainText("Заполните размеры коробки");
  await expect(reasons).not.toContainText("Подтвердить наличие растений");
});

test("@desktop несохранённый состав защищён при возврате к списку заказов", async ({ page }) => {
  const state = await mockOrderEditor(page);
  await page.goto("/admin?section=orders&order=30");
  const quantity = page.getByRole("spinbutton", { name: "Количество Азалия D9", exact: true });
  await quantity.fill("2");
  await expect(page.getByRole("combobox", { name: "Статус заказа 0001-30", exact: true })).toBeDisabled();

  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(quantity).toHaveValue("2");
  await expect(page).toHaveURL(/order=30/);

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("link", { name: "К заказам", exact: true }).click();
  await expect(page.getByRole("link", { name: "0001-30", exact: true })).toBeVisible();
  expect(await state.savedEdit()).toBeUndefined();
  expect(await state.paymentLinkCalls()).toBe(0);
});

async function fillPackingBox(page: import("@playwright/test").Page) {
  const packing = page.getByRole("region", { name: "Подготовить заказ к отправке" });
  await expect(packing.getByRole("button", { name: "Рассчитать доставку", exact: true })).toBeDisabled();
  await packing.getByRole("textbox", { name: "Длина коробки, см", exact: true }).fill("40");
  await packing.getByRole("textbox", { name: "Ширина коробки, см", exact: true }).fill("30");
  await packing.getByRole("textbox", { name: "Высота коробки, см", exact: true }).fill("60");
  await packing.getByRole("textbox", { name: "Вес с упаковкой, кг", exact: true }).fill("2.5");
  return packing;
}

async function preparePacking(page: import("@playwright/test").Page) {
  const packing = await fillPackingBox(page);
  await packing.getByRole("button", { name: "Рассчитать доставку", exact: true }).click();
  await expect(packing.getByRole("radio")).toBeChecked();
  await expect(packing).toContainText(/590/);
  await expect(packing.locator(".order-packing-total")).toContainText(/Итого с доставкой\s*2.?070/);
  return packing;
}

test("@desktop упаковка регистрируется один раз и показывает отдельные результаты СДЭК, оплаты и письма", async ({ page }) => {
  const state = await mockOrderEditor(page, false, "success");
  await page.clock.install({ time: new Date("2026-10-22T12:30:00Z") });
  await page.goto("/admin?section=orders&order=30");
  await expect(page.getByRole("region", { name: "Почему требуется менеджер" })).toContainText("Собрать в одну коробку и рассчитать доставку");
  const packing = await preparePacking(page);

  if (process.env.FICUSIN_PREVIEW_OUTPUT_DIR) {
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({ path: `${process.env.FICUSIN_PREVIEW_OUTPUT_DIR}/orders-detail-desktop.png`, fullPage: true });
  }

  await packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true }).click();
  await expect(packing.getByText("Заказ принят в обработку", { exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Статус заказа 0001-30", exact: true })).toHaveValue("confirmed");
  await expect(packing.getByText("Отправка писем не настроена", { exact: true })).toBeVisible();
  await expect(packing.getByText("40 × 30 × 60 см", { exact: true })).toBeVisible();
  await expect(packing.getByText("Трек в процессе создания", { exact: true })).toBeVisible();
  await expect(packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true })).toHaveCount(0);
  const requests = await state.packingFinalizations();
  expect(requests).toHaveLength(1);
  expect(requests[0].idempotencyKey).toBeTruthy();
  expect(requests[0].revision).toBe(1);
  expect(requests[0].tariffCode).toBe(136);
  expect(requests[0]).toMatchObject({ quotedDeliveryFee: 590 });
  expect(requests[0].boxes).toEqual([{ lengthCm: 40, widthCm: 30, heightCm: 60, weightGrams: 2500, contents: [{ orderItemId: 11, quantity: 1 }, { orderItemId: 12, quantity: 1 }] }]);

  await page.evaluate(() => { (window as typeof window & { __packingPhase?: number }).__packingPhase = 1; });
  await page.clock.runFor(4100);
  await expect(packing.getByText("Регистрируем отправление в СДЭК", { exact: true })).toBeVisible();
  await page.evaluate(() => { (window as typeof window & { __packingPhase?: number }).__packingPhase = 2; });
  await page.clock.runFor(4100);
  await expect(packing.getByText("103399900001", { exact: true })).toBeVisible();
  await expect(packing.getByText("СДЭК:", { exact: true })).toBeVisible();
  await expect(packing).not.toContainText("Заявка зарегистрирована. Трек:");
  await expect(packing.getByText("Ссылка ещё не готова", { exact: true })).toBeVisible();
  await page.evaluate(() => { (window as typeof window & { __packingPhase?: number }).__packingPhase = 3; });
  await page.clock.runFor(4100);
  await expect(packing.getByRole("link", { name: "Ссылка на оплату готова" })).toHaveAttribute("href", "https://pay.example.test/order-30-packed");
  await expect(packing.getByText("В очереди на отправку", { exact: true })).toBeVisible();
  await page.evaluate(() => { (window as typeof window & { __packingPhase?: number }).__packingPhase = 4; });
  await page.clock.runFor(4100);
  await expect(packing.getByText("Отправлен в push-сервис", { exact: true })).toBeVisible();
  expect(await state.packingFinalizations()).toHaveLength(1);
  expect(await state.paymentLinkCalls()).toBe(0);
  expect(await state.packingPolls()).toBeGreaterThanOrEqual(4);
});

test("@desktop ошибка отправки сохраняет коробку и повтор использует тот же ключ", async ({ page }) => {
  const state = await mockOrderEditor(page, false, "retry");
  await page.goto("/admin?section=orders&order=30");
  const packing = await preparePacking(page);
  await packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true }).click();
  await expect(packing.getByRole("alert")).toContainText("Упаковка не сохранена, повторите попытку");
  await expect(packing.getByRole("textbox", { name: "Длина коробки, см", exact: true })).toHaveValue("40");
  await expect(packing.getByRole("textbox", { name: "Вес с упаковкой, кг", exact: true })).toHaveValue("2.5");
  await expect(packing.getByRole("radio")).toBeChecked();
  expect(await state.packingFinalizations()).toHaveLength(1);
  await packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true }).click();
  await expect(packing.getByText("Заказ принят в обработку", { exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Статус заказа 0001-30", exact: true })).toHaveValue("confirmed");
  const requests = await state.packingFinalizations();
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
});

test("@phone упаковка и выбор тарифа помещаются на экране телефона", async ({ page }, testInfo) => {
  await mockOrderEditor(page, false, "success");
  await page.goto("/admin?section=orders&order=30");
  const packing = await preparePacking(page);
  await expect(packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true })).toBeEnabled();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1);
  if (process.env.FICUSIN_PREVIEW_OUTPUT_DIR) {
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({ path: `${process.env.FICUSIN_PREVIEW_OUTPUT_DIR}/orders-detail-${testInfo.project.name}.png`, fullPage: true });
  }
});

test("@desktop заказ без пункта СДЭК требует выбора города и пункта до расчёта", async ({ page }) => {
  const state = await mockOrderEditor(page, false, "destination");
  await page.goto("/admin?section=orders&order=30");
  const packing = await fillPackingBox(page);
  await expect(packing.getByRole("button", { name: "Рассчитать доставку", exact: true })).toBeDisabled();
  await packing.getByRole("textbox", { name: "Город получения СДЭК", exact: true }).fill("Москва");
  await packing.getByRole("button", { name: /^Москва/ }).click();
  await packing.getByRole("combobox", { name: "Пункт выдачи СДЭК", exact: true }).selectOption("MSK1");
  await packing.getByRole("button", { name: "Рассчитать доставку", exact: true }).click();
  await expect(packing.getByRole("radio")).toBeChecked();
  await packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true }).click();
  await expect(packing.getByText("Заказ принят в обработку", { exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Статус заказа 0001-30", exact: true })).toHaveValue("confirmed");
  const requests = await state.packingFinalizations();
  expect(requests).toHaveLength(1);
  expect(requests[0]).toMatchObject({ cityCode: 44, officeCode: "MSK1", quotedDeliveryFee: 590 });
});

test("@desktop число коробок ограничено числом позиций заказа", async ({ page }) => {
 await mockOrderEditor(page, false, "success");
 await page.goto("/admin?section=orders&order=30");
 const packing=page.getByRole("region",{name:"Подготовить заказ к отправке"});
 await packing.getByRole("button",{name:"Добавить коробку",exact:true}).click();
 await expect(packing.locator(".order-packing-box")).toHaveCount(2);
 await expect(packing.getByRole("button",{name:"Добавить коробку",exact:true})).toHaveCount(0);
 await packing.getByRole("button",{name:"Удалить коробку",exact:true}).last().click();
 await expect(packing.getByRole("button",{name:"Добавить коробку",exact:true})).toBeVisible();
 await page.getByRole("button",{name:"Удалить",exact:true}).last().click();
 await page.getByRole("button",{name:"Сохранить изменения",exact:true}).click();
 await expect(packing.getByRole("button",{name:"Добавить коробку",exact:true})).toHaveCount(0);
});

test("@desktop дробный вес в килограммах сохраняется в граммах без округления до килограмма", async ({ page }) => {
 const state=await mockOrderEditor(page,false,"success");
 await page.goto("/admin?section=orders&order=30");
 const packing=await fillPackingBox(page);
 const weight=packing.getByRole("textbox",{name:"Вес с упаковкой, кг",exact:true});
 await weight.fill(""); await weight.pressSequentially("0.125");
 await expect(weight).toHaveValue("0.125");
 await packing.getByRole("button",{name:"Рассчитать доставку",exact:true}).click();
 await expect(packing.getByRole("radio")).toBeChecked();
 await packing.getByRole("button",{name:"Сохранить и подтвердить",exact:true}).click();
 await expect(packing.getByText("Заказ принят в обработку",{exact:true})).toBeVisible();
 expect((await state.packingFinalizations())[0].boxes[0].weightGrams).toBe(125);
 const title=await page.getByRole("heading",{name:"Заказ 0001-30",exact:true}).boundingBox();
 const status=await page.getByRole("combobox",{name:"Статус заказа 0001-30",exact:true}).boundingBox();
 expect(Math.abs(title!.y-status!.y)).toBeLessThan(50);
 expect(status!.x-title!.x-title!.width).toBeLessThan(30);
});


test("@desktop @phone размеры вводятся строкой, а вес с запятой передаётся в граммах", async ({page}, testInfo) => {
 const state = await mockOrderEditor(page, false, "success");
 await page.goto("/admin?section=orders&status=new&order=30");
 const packing = await fillPackingBox(page);
 const length = packing.getByRole("textbox", {name:"Длина коробки, см",exact:true});
 await expect(length).toHaveAttribute("inputmode","numeric");
 await length.press("ArrowUp"); await expect(length).toHaveValue("40");
 const weight = packing.getByRole("textbox", {name:"Вес с упаковкой, кг",exact:true});
 await expect(weight).toHaveAttribute("inputmode","decimal");
 await weight.fill("0,125"); await expect(weight).toHaveValue("0,125");
 await packing.getByRole("button",{name:"Рассчитать доставку",exact:true}).click();
 await expect(packing.getByRole("radio")).toBeChecked();
 await packing.screenshot({path:testInfo.outputPath("packing-text-fields.png")});
 await packing.getByRole("button",{name:"Сохранить и подтвердить",exact:true}).click();
 await expect.poll(async()=> (await state.packingFinalizations()).length).toBe(1);
 expect((await state.packingFinalizations())[0].boxes[0].weightGrams).toBe(125);
});

test("@desktop @phone возврат и чек ожидают отдельного подтверждения ЮKassa", async ({page},testInfo) => {
 await mockOrderEditor(page,false,undefined,false,"owner");
 await page.goto("/admin?section=orders&order=30");
 const section=page.getByRole("region",{name:"Оплата и возвраты"});
 await expect(section.getByText("Деньги возвращены",{exact:false})).toBeVisible();
 await expect(section.getByText("Ожидает подтверждения ЮKassa",{exact:false})).toBeVisible();
 await section.getByText("Вернуть деньги покупателю",{exact:true}).click();
 await expect(section.getByRole("button",{name:"Вернуть через ЮKassa"})).toBeDisabled();
 if(process.env.FICUSIN_PREVIEW_OUTPUT_DIR) await section.screenshot({path:`${process.env.FICUSIN_PREVIEW_OUTPUT_DIR}/refund-${testInfo.project.name}.png`});
 await expect(section.getByText("Ожидает подтверждения ЮKassa",{exact:false})).toBeVisible();
});
test("@desktop менеджер видит возврат без финансовых действий", async ({page})=>{
 await mockOrderEditor(page,false,undefined,false,"manager");
 await page.goto("/admin?section=orders&order=30");
 const section=page.getByRole("region",{name:"Оплата и возвраты"});
 await expect(section).toBeVisible();
 await expect(section.getByText("Вернуть деньги покупателю",{exact:true})).toHaveCount(0);
 await expect(section.getByRole("button",{name:"Чек уже сформирован"})).toHaveCount(0);
});


test("@desktop @phone распределение на две коробки исключает нули при расчёте и подтверждении", async ({ page }) => {
  const state = await mockOrderEditor(page, false, "success");
  await page.goto("/admin?section=orders&order=30");
  const packing = await fillPackingBox(page);
  await packing.getByRole("button", { name: "Добавить коробку", exact: true }).click();
  await packing.getByRole("spinbutton", { name: "Алоказия D6 в коробке 1", exact: true }).fill("0");
  await packing.getByRole("spinbutton", { name: "Азалия D9 в коробке 2", exact: true }).fill("0");
  await packing.getByRole("spinbutton", { name: "Алоказия D6 в коробке 2", exact: true }).fill("1");
  for (const [name, value] of [["Длина коробки 2, см", "40"], ["Ширина коробки 2, см", "15"], ["Высота коробки 2, см", "15"], ["Вес с упаковкой коробки 2, кг", "1"]]) {
    await packing.getByRole("textbox", { name, exact: true }).fill(value);
  }
  await packing.getByRole("button", { name: "Рассчитать доставку", exact: true }).click();
  await expect(packing.getByRole("radio")).toBeChecked();
  const quoted = await page.evaluate(() => (window as typeof window & { __packingQuote?: { boxes: Array<{ contents: unknown[] }> } }).__packingQuote);
  expect(quoted?.boxes.map((box) => box.contents)).toEqual([[{ orderItemId: 11, quantity: 1 }], [{ orderItemId: 12, quantity: 1 }]]);
  await packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true }).click();
  await expect(packing.getByText("Заказ принят в обработку", { exact: true })).toBeVisible();
  expect((await state.packingFinalizations())[0].boxes.map((box) => box.contents)).toEqual(quoted?.boxes.map((box) => box.contents));
});

test("@desktop @phone атрибуты заполняют отдельные коробки и сбрасывают прежний тариф", async ({ page }, testInfo) => {
  await mockOrderEditor(page, false, "success", true);
  await page.goto("/admin?section=orders&order=30");
  const packing = await fillPackingBox(page);
  await packing.getByRole("button", { name: "Рассчитать доставку", exact: true }).click();
  await expect(packing.getByRole("radio")).toBeChecked();
  await packing.getByRole("button", { name: "Заполнить из атрибутов растений", exact: true }).click();
  await expect(packing.getByRole("radio")).toHaveCount(0);
  await expect(packing.getByRole("button", { name: "Сохранить и подтвердить", exact: true })).toHaveCount(0);
  await expect(packing.locator(".order-packing-box")).toHaveCount(2);
  await expect(packing.getByRole("textbox", { name: "Длина коробки 1, см", exact: true })).toHaveValue("20");
  await expect(packing.getByRole("textbox", { name: "Высота коробки 2, см", exact: true })).toHaveValue("40");
  await expect(packing.getByRole("textbox", { name: "Вес с упаковкой коробки 2, кг", exact: true })).toHaveValue("0.7");
  await expect(packing.getByRole("spinbutton", { name: "Азалия D9 в коробке 2", exact: true })).toHaveValue("0");
  await packing.getByRole("button", { name: "Рассчитать доставку", exact: true }).click();
  await expect(packing.getByRole("radio")).toBeChecked();
  await packing.screenshot({ path: testInfo.outputPath("packing-attributes.png") });
});

test("@desktop без атрибутов коробки распределены, но расчёт ждёт размеров и веса", async ({ page }) => {
  await mockOrderEditor(page, false, "success");
  await page.goto("/admin?section=orders&order=30");
  const packing = page.getByRole("region", { name: "Подготовить заказ к отправке" });
  await packing.getByRole("button", { name: "Заполнить из атрибутов растений", exact: true }).click();
  await expect(packing.locator(".order-packing-box")).toHaveCount(2);
  await expect(packing.getByRole("button", { name: "Рассчитать доставку", exact: true })).toBeDisabled();
  await expect(packing.getByRole("spinbutton", { name: "Азалия D9 в коробке 1", exact: true })).toHaveValue("1");
});

test('@desktop @phone частичный возврат передаёт выбранные позиции и точную сумму',async({page})=>{
 await mockOrderEditor(page,false,undefined,false,'owner');
 await page.addInitScript(()=>{
  const original=window.fetch.bind(window);const requests:unknown[]=[];
  Object.defineProperty(window,'fiscalRefundRequests',{value:requests});
  window.fetch=async(input,init)=>{
   const path=typeof input==='string'?input:input instanceof URL?input.pathname:input.url;
   if(path==='/api/v1/admin/orders/30/refund'&&init?.method==='POST'){
    requests.push(JSON.parse(String(init.body)));return new Response(JSON.stringify({payment:{}}),{status:200,headers:{'Content-Type':'application/json'}});
   }
   const response=await original(input,init);
   if(path==='/api/v1/admin/orders/30/adjustment'){
    const body=await response.json();body.payment.refunds=[];
    body.payment.refundOptions=[{paymentId:11,itemIndex:0,quantity:2,name:'Растение из платежа',price:100.01},{paymentId:11,itemIndex:1,quantity:1,name:'Доставка',price:25}];
    return new Response(JSON.stringify(body),{status:200,headers:{'Content-Type':'application/json'}});
   }
   return response;
  };
 });
 await page.goto('/admin?section=orders&order=30');
 const section=page.getByRole('region',{name:'Оплата и возвраты'});
 await section.getByText('Вернуть деньги покупателю',{exact:true}).click();
 await section.getByLabel('Вернуть: Растение из платежа, платёж 11').fill('1');
 await expect(section.getByLabel('Сумма возврата, ₽')).toHaveValue('100.01');
 await expect(section.getByLabel('Сумма возврата, ₽')).toBeDisabled();
 await section.getByLabel('Причина возврата',{exact:true}).fill('Возврат одной позиции');
 page.on('dialog',dialog=>dialog.accept());
 await section.getByRole('button',{name:'Вернуть через ЮKassa',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>Reflect.get(window,'fiscalRefundRequests').length)).toBe(1);
 const [request]=await page.evaluate(()=>Reflect.get(window,'fiscalRefundRequests'));
 expect(request.amount).toBe(100.01);expect(request.selections).toEqual([{paymentId:11,itemIndex:0,quantity:1}]);expect(request.requestId.length).toBeGreaterThanOrEqual(16);
});

test('@desktop @phone чек зачёта ЮKassa создаётся только после подтверждения владельца',async({page},info)=>{
 await mockOrderEditor(page,false,undefined,false,'owner');
 await page.addInitScript(()=>{
  const original=window.fetch.bind(window);const requests:unknown[]=[];
  Object.defineProperty(window,'settlementRequests',{value:requests});
  window.fetch=async(input,init)=>{
   const path=typeof input==='string'?input:input instanceof URL?input.pathname:input.url;
   if(path==='/api/v1/admin/orders/30/payments/11/settlement-receipt'&&init?.method==='POST'){
    requests.push(JSON.parse(String(init.body)));return new Response(JSON.stringify({ok:true}),{status:200,headers:{'Content-Type':'application/json'}});
   }
   const response=await original(input,init);
   if(path==='/api/v1/admin/orders/30/adjustment'){
    const body=await response.json();body.payment.refunds=[];
    body.payment.fiscalTasks=[{paymentId:11,kind:'income',status:'succeeded',reference:''},{paymentId:11,kind:'settlement',status:requests.length?'pending':'ready',reference:''}];
    return new Response(JSON.stringify(body),{status:200,headers:{'Content-Type':'application/json'}});
   }
   return response;
  };
 });
 await page.goto('/admin?section=orders&order=30');
 const section=page.getByRole('region',{name:'Оплата и возвраты'});
 const button=section.getByRole('button',{name:'Сформировать чек зачёта в ЮKassa',exact:true});
 await expect(button).toBeVisible();
 page.once('dialog',dialog=>dialog.dismiss());await button.click();
 expect(await page.evaluate(()=>Reflect.get(window,'settlementRequests').length)).toBe(0);
 page.once('dialog',dialog=>dialog.accept());await button.click();
 await expect.poll(()=>page.evaluate(()=>Reflect.get(window,'settlementRequests').length)).toBe(1);
 expect(await page.evaluate(()=>Reflect.get(window,'settlementRequests')[0])).toEqual({confirmed:true});
 await expect(section.getByText('Ожидает регистрации ЮKassa',{exact:true})).toBeVisible();
 await expect(section.getByRole('button',{name:'Чек уже сформирован'})).toHaveCount(0);
 await section.screenshot({path:info.outputPath('yookassa-only-fiscal-state.png')});
});
