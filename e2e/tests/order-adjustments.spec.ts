import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

test("@desktop кабинет показывает возврат и оплачивает только остаток", async ({ page }) => {
  await mockApi(page, owner);
  await page.route("**/api/v1/account/orders/0001-7", (route) => route.fulfill({
    json: {
      order: {
        orderNumber: "0001-7",
        deliveryMethod: "cdek",
        address: "Рязань, пункт СДЭК",
        comment: "",
        customerName: "Александр",
        phone: "+79990000000",
        email: "test@example.com",
        status: "confirmed",
        paymentStatus: "partially_paid",
        paymentMethod: "online",
        deliveryFee: 500,
        deliveryFeePending: false,
        repackRequested: false,
        hasPreorder: false,
        subtotal: 2500,
        total: 3000,
        paidAmount: 2000,
        refundedAmount: 500,
        amountDue: 1500,
        paymentReady: true,
        createdAt: "2026-08-20T12:00:00Z",
        items: [
          { productName: "Апельсин", unitPrice: 1000, quantity: 1 },
          { productName: "Ананас", unitPrice: 1500, quantity: 1 },
        ],
      },
    },
  }));

  await page.goto("/account/orders/0001-7");

  await expect(page.getByText("Оплачено", { exact: true })).toBeVisible();
  await expect(page.getByText("Возвращено", { exact: true })).toBeVisible();
  await expect(page.getByText("К доплате", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: /Оплатить 1.?500/ })).toBeVisible();
});

test("@desktop кабинет не предлагает оплату до подтверждения менеджером", async ({ page }) => {
  await mockApi(page, owner);
  await page.route("**/api/v1/account/orders/0001-8", (route) => route.fulfill({
    json: {
      order: {
        orderNumber: "0001-8",
        deliveryMethod: "post",
        address: "Адрес требует проверки",
        comment: "",
        customerName: "Александр",
        phone: "+79990000000",
        email: "test@example.com",
        status: "new",
        paymentStatus: "pending",
        paymentMethod: "online",
        deliveryFee: 0,
        deliveryFeePending: true,
        repackRequested: false,
        hasPreorder: false,
        subtotal: 2500,
        total: 2500,
        paidAmount: 0,
        refundedAmount: 0,
        amountDue: 2500,
        paymentReady: false,
        createdAt: "2026-08-20T12:00:00Z",
        items: [{ productName: "Апельсин", unitPrice: 2500, quantity: 1 }],
      },
    },
  }));

  await page.goto("/account/orders/0001-8");

  await expect(page.getByText(/Менеджер проверит состав и доставку/)).toBeVisible();
  await expect(page.getByRole("button", { name: /Оплатить/ })).toHaveCount(0);
});

test("@desktop открытая карточка сама показывает оплату после подтверждения менеджером", async ({ page }) => {
  await mockApi(page, owner);
  let confirmed = false;
  await page.route("**/api/v1/account/orders/0001-9", (route) => route.fulfill({
    json: {
      order: {
        orderNumber: "0001-9",
        deliveryMethod: "cdek",
        address: "Москва, пункт СДЭК",
        comment: "",
        customerName: "Александр",
        phone: "+79990000000",
        email: "test@example.com",
        status: confirmed ? "confirmed" : "new",
        paymentStatus: "pending",
        paymentMethod: "online",
        deliveryFee: confirmed ? 1000 : 0,
        deliveryFeePending: !confirmed,
        repackRequested: false,
        hasPreorder: false,
        subtotal: 2970,
        total: confirmed ? 3970 : 2970,
        paidAmount: 0,
        refundedAmount: 0,
        amountDue: confirmed ? 3970 : 2970,
        paymentReady: confirmed,
        createdAt: "2026-08-20T12:00:00Z",
        items: [{ productName: "Аглаонема", unitPrice: 2970, quantity: 1 }],
      },
    },
  }));

  await page.goto("/account/orders/0001-9");
  await expect(page.getByText(/Менеджер проверит состав и доставку/)).toBeVisible();
  await expect(page.getByRole("button", { name: /Оплатить/ })).toHaveCount(0);

  // Имитируем сохранение заказа менеджером, пока клиент не закрывал вкладку.
  // Сначала ждём, пока React установит listener focus, чтобы тест проверял
  // поведение продукта, а не гонку между commit и useEffect.
  confirmed = true;
  await page.waitForTimeout(100);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));

  await expect(page.getByRole("button", { name: /Оплатить 3.?970/ })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator(".order-totals")).toContainText(/Доставка\s*1.?000/);
});
