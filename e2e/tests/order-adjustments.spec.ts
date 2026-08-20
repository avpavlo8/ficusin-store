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

  await expect(page.getByText("Оплачено")).toBeVisible();
  await expect(page.getByText("Возвращено")).toBeVisible();
  await expect(page.getByText("К доплате")).toBeVisible();
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

  await expect(page.getByText(/Менеджер подтвердит наличие и доставку/)).toBeVisible();
  await expect(page.getByRole("button", { name: /Оплатить/ })).toHaveCount(0);
});
