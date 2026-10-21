import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

const order = {
  orderNumber: "PAYMENT-TEST", status: "confirmed", paymentMethod: "online",
  paymentStatus: "pending", paymentAttemptStatus: "pending", deliveryMethod: "cdek",
  deliveryPayee: "shop", subtotal: 7450, total: 8040, paidAmount: 0,
  refundedAmount: 0, amountDue: 8040,
};

test("@desktop @phone return URL does not claim payment before sign in", async ({ page }) => {
  await mockApi(page);
  await page.goto("/?paid=PAYMENT-TEST");
  const login = page.getByRole("link", { name: "Войти и проверить" });
  await expect(login).toBeVisible();
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toHaveCount(0);
  await login.click();
  await page.getByRole("link", { name: "Зарегистрироваться", exact: true }).click();
  await expect(page).toHaveURL(/register\?returnTo=%2F%3Fpaid%3DPAYMENT-TEST/);
  await expect(page.locator(".auth-switch a")).toHaveAttribute("href", "/login?returnTo=%2F%3Fpaid%3DPAYMENT-TEST");
});

test("@desktop @phone cancelled attempt preserves the unpaid order", async ({ page }) => {
  await mockApi(page, owner);
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", (route) => route.fulfill({ json: {
    order: { ...order, paymentAttemptStatus: "canceled" },
  } }));
  await page.goto("/?paid=PAYMENT-TEST");
  await expect(page.getByRole("heading", { name: "Оплата не прошла", exact: true })).toBeVisible();
  await expect(page.locator(".payment-result-facts")).toContainText(/8.?040/);
  await expect(page.getByRole("link", { name: "Вернуться к заказу" })).toHaveAttribute("href", "/account/orders/PAYMENT-TEST");
  await expect(page.getByRole("button", { name: /Оплатить/ })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBe(0);
});

test("@desktop @phone pending payment refreshes to a verified success", async ({ page }) => {
  await mockApi(page, owner);
  let paid = false;
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", (route) => route.fulfill({ json: {
    order: paid ? { ...order, paymentStatus: "paid", paymentAttemptStatus: "paid", paidAmount: 8040, amountDue: 0 } : order,
  } }));
  await page.goto("/?paid=PAYMENT-TEST");
  await expect(page.getByRole("heading", { name: "Проверяем оплату", exact: true })).toBeVisible();
  await expect(page.getByText("Проверяем статус автоматически.", { exact: true })).toBeVisible();
  paid = true;
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole("link", { name: "Следить за заказом" })).toHaveAttribute("href", "/account/orders/PAYMENT-TEST");
  await expect(page.locator(".payment-result-amount")).toContainText(/8.?040/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBe(0);
});

test("@desktop @phone failed status read retries without creating another payment", async ({ page }) => {
  await mockApi(page, owner);
  let reads = 0;
  let paymentWrites = 0;
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", (route) => {
    reads++;
    return reads === 1 ? route.fulfill({ status: 503, json: { error: "Unavailable" } }) : route.fulfill({ json: {
      order: { ...order, paymentStatus: "paid", paidAmount: 8040, amountDue: 0 },
    } });
  });
  await page.route("**/api/v1/payments/orders/**", (route) => { paymentWrites++; return route.fulfill({ status: 409, json: {} }); });
  await page.goto("/?paid=PAYMENT-TEST");
  await expect(page.getByRole("heading", { name: "Не удалось проверить оплату", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Проверить ещё раз" }).click();
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toBeVisible();
  expect(paymentWrites).toBe(0);
});
