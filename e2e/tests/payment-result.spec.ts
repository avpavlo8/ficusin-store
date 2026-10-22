import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

async function captureMetrika(page: import("@playwright/test").Page) {
  await page.route("**/api/v1/analytics/config", route => route.fulfill({ json: { yandexMetrikaId: 111907858 } }));
  await page.route("**/api/v1/analytics/events", route => route.fulfill({ status: 204 }));
  await page.addInitScript(() => {
    const target = window as unknown as { ym: (...args: unknown[]) => void; goalCalls: unknown[][] };
    target.goalCalls = [];
    target.ym = (...args) => { target.goalCalls.push(args); };
  });
}

async function paidGoals(page: import("@playwright/test").Page) {
  return page.evaluate(() => (window as unknown as { goalCalls: unknown[][] }).goalCalls.filter(args => args[2] === "paid_order"));
}

const order = {
  orderNumber: "PAYMENT-TEST", status: "confirmed", paymentMethod: "online",
  paymentStatus: "pending", paymentAttemptStatus: "pending", deliveryMethod: "cdek",
  deliveryPayee: "shop", subtotal: 7450, total: 8040, paidAmount: 0,
  refundedAmount: 0, amountDue: 8040,
};

for (const [returnTo, expectedPath] of [["/?paid=PAYMENT-TEST", "/?paid=PAYMENT-TEST"], ["/\\example.com", "/account"]]) {
  test(`@desktop @phone login safely returns to ${expectedPath}`, async ({ page }) => {
    await mockApi(page, owner);
    await page.route("**/api/v1/auth/request-code", route => route.fulfill({ json: { checkId: "LOCAL-CHECK", callPhonePretty: "+7 800 000-00-00" } }));
    await page.route("**/api/v1/auth/verify-code", route => route.fulfill({ json: { user: owner.user } }));
    await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`);
    const expectedURL = new URL(expectedPath, page.url()).href;
    await page.getByLabel("Телефон", { exact: true }).fill("9150000000");
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page).toHaveURL(expectedURL, { timeout: 10_000 });
  });
}

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
  await captureMetrika(page);
  let paid = false;
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", (route) => route.fulfill({ json: {
    order: paid ? { ...order, paymentStatus: "paid", paymentAttemptStatus: "paid", paidAmount: 8040, amountDue: 0 } : order,
  } }));
  await page.goto("/?paid=PAYMENT-TEST");
  await expect(page.getByRole("heading", { name: "Проверяем оплату", exact: true })).toBeVisible();
  await expect(page.getByText("Проверяем статус автоматически.", { exact: true })).toBeVisible();
  expect(await paidGoals(page)).toEqual([]);
  paid = true;
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByRole("link", { name: "Следить за заказом" })).toHaveAttribute("href", "/account/orders/PAYMENT-TEST");
  await expect(page.locator(".payment-result-amount")).toContainText(/8.?040/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBe(0);
  await expect.poll(() => paidGoals(page)).toEqual([[111907858, "reachGoal", "paid_order", { order_price: 8040, currency: "RUB" }]]);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toBeVisible();
  expect(await paidGoals(page)).toEqual([]);
});

for (const [label, changed] of [
  ["partial", { paymentStatus: "paid", paidAmount: 7450, amountDue: 590 }],
  ["cancelled", { status: "cancelled", paymentStatus: "paid", paidAmount: 8040, amountDue: 0 }],
  ["refunded", { paymentStatus: "refunded", paidAmount: 8040, refundedAmount: 8040, amountDue: 0 }],
] as const) {
  test(`@desktop @phone ${label} never reports a paid conversion`, async ({ page }) => {
    await mockApi(page, owner);
    await captureMetrika(page);
    await page.route("**/api/v1/account/orders/PAYMENT-TEST", route => route.fulfill({ json: { order: { ...order, ...changed } } }));
    await page.goto("/?paid=PAYMENT-TEST");
    await expect(page.locator(".payment-result-facts")).toBeVisible();
    expect(await paidGoals(page)).toEqual([]);
  });
}

test("@desktop @phone paid conversion excludes carrier delivery and order capabilities", async ({ page }) => {
  await mockApi(page, owner);
  await captureMetrika(page);
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", route => route.fulfill({ json: {
    order: { ...order, paymentStatus: "paid", deliveryPayee: "carrier", paidAmount: 7450, amountDue: 0 },
  } }));
  await page.goto("/?paid=PAYMENT-TEST");
  await expect.poll(() => paidGoals(page)).toEqual([[111907858, "reachGoal", "paid_order", { order_price: 7450, currency: "RUB" }]]);
});

test("@desktop @phone privacy preference disables payment conversion", async ({ page }) => {
  await mockApi(page, owner);
  await captureMetrika(page);
  await page.addInitScript(() => Object.defineProperty(navigator, "doNotTrack", { value: "1", configurable: true }));
  await page.route("**/api/v1/account/orders/PAYMENT-TEST", route => route.fulfill({ json: {
    order: { ...order, paymentStatus: "paid", paidAmount: 8040, amountDue: 0 },
  } }));
  await page.goto("/?paid=PAYMENT-TEST");
  await expect(page.getByRole("heading", { name: "Оплата прошла", exact: true })).toBeVisible();
  expect(await paidGoals(page)).toEqual([]);
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
