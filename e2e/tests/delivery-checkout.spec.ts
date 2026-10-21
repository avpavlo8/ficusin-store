import { expect, test, type Page } from "@playwright/test";
import { mockApi, owner, setStoredCounts } from "./helpers";

async function openDeliveryStep(page: Page) {
  await setStoredCounts(page, [], { "1": 1 });
  await page.goto("/checkout");
  const contact = page.locator(".checkout-contact-section");
  await contact.getByLabel("Имя").fill("Александр");
  await contact.getByLabel("Телефон").fill("9151234567");
  await contact.getByLabel("Email для чека").fill("client@example.com");
  await expect(page.locator(".checkout-delivery-section")).toBeVisible();
}

test("@desktop покупателю доступны только самовывоз и СДЭК", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/cdek?action=status", (route) =>
    route.fulfill({ json: { available: true } }));
  await openDeliveryStep(page);

  const methods = page.locator('.checkout-delivery-section input[name="delivery"]');
  await expect(methods).toHaveCount(2);
  await expect(methods.nth(0)).toHaveValue("pickup");
  await expect(methods.nth(1)).toHaveValue("cdek");
  await expect(page.getByLabel("Курьер по Рязани")).toHaveCount(0);
  await expect(page.getByLabel("Почта России")).toHaveCount(0);
});

test("@desktop оформление показывает персональную скидку до отправки", async ({ page }) => {
  await mockApi(page, owner);
  await page.route("**/api/v1/auth/me", (route) => route.fulfill({
    json: { user: { ...owner.user, retailDiscountBps: 100 } },
  }));
  await setStoredCounts(page, [], { "1": 1 });
  await page.goto("/checkout");

  const summary = page.locator(".checkout-order-summary");
  await expect(summary).toContainText(/Растения без скидки\s*1.?490/);
  await expect(summary).toContainText(/Персональная скидка 1%\s*−14,90/);
  await expect(summary).toContainText(/Растения, без доставки\s*1.?475,10/);
  await expect(summary).toContainText("Выберите способ получения");
});

test("@desktop стоимость СДЭК входит в сумму онлайн-заказа", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/cdek?action=status", (route) =>
    route.fulfill({ json: { available: true } }));
  await page.route("**/api/v1/delivery/cdek?action=cities&city=*", (route) =>
    route.fulfill({ json: { cities: [{ code: 44, city: "Москва", region: "Москва" }] } }));
  await page.route("**/api/v1/delivery/cdek?action=offices&cityCode=*", (route) =>
    route.fulfill({ json: { offices: [{ code: "MSK1", name: "ПВЗ", location: { city: "Москва", address: "Тверская, 1" } }] } }));
  await page.route("**/api/v1/delivery/cdek", (route) =>
    route.fulfill({ json: { quotes: [{ tariffCode: 136, tariffName: "Склад — склад", price: 590, daysMin: 2, daysMax: 3 }] } }));
  await openDeliveryStep(page);

  const submit = page.locator('.checkout-navigation button.primary-button');
  await expect(submit).toBeDisabled();
  await page.locator('input[name="delivery"][value="cdek"]').check();
  const summary = page.locator(".checkout-order-summary");
  await expect(summary).toContainText("Выберите город");
  await expect(summary).not.toContainText("Уточнит менеджер");
  await expect(page.getByLabel("Город получения")).toHaveAttribute("required", "");
  await page.getByLabel("Город получения").fill("Моск");
  await expect(submit).toBeDisabled();
  await page.getByRole("listbox", { name: "Найденные города" }).getByRole("button", { name: /Москва/ }).click();
  await expect(summary).toContainText("Выберите пункт выдачи");
  await expect(submit).toBeDisabled();
  await page.getByLabel("Пункт выдачи").click();
  await page.getByRole("listbox", { name: "Пункты выдачи" }).getByRole("button", { name: /Тверская, 1/ }).click();

  await expect(summary).toContainText(/Растения\s*1.?490/);
  await expect(summary).toContainText(/Доставка\s*590/);
  await expect(summary).toContainText(/Итого к оплате\s*2.?080/);
  await expect(submit).toBeDisabled();
  await page.locator('input[name="consent"]').check();
  await expect(submit).toBeEnabled();
  await page.getByLabel("Email для чека").fill("invalid-email");
  await expect(submit).toBeDisabled();
  await page.getByLabel("Email для чека").fill("client@example.com");
  await expect(submit).toBeEnabled();
  await page.getByLabel("Город получения").fill("Моск");
  await expect(summary).toContainText("Выберите город");
  await expect(summary).not.toContainText("Уточнит менеджер");
  await expect(submit).toBeDisabled();
});

test("@desktop растение под заказ оформляется без выбора оплаты", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/payments/methods?*", (route) =>
    route.fulfill({ json: { methods: [] } }));
  let submittedPayment: string | undefined;
  await page.route("**/api/v1/orders", (route) => {
    submittedPayment = (route.request().postDataJSON() as { paymentMethod?: string }).paymentMethod;
    return route.fulfill({ json: { orderNumber: "PRE-TEST", hasPreorder: true, paymentStatus: "pending" } });
  });
  await setStoredCounts(page, [], { "3": 1 });
  await page.goto("/checkout");
  await page.locator(".checkout-contact-section").getByLabel("Имя").fill("Александр");
  await page.locator(".checkout-contact-section").getByLabel("Телефон").fill("9151234567");
  await page.locator(".checkout-contact-section").getByLabel("Email для чека").fill("client@example.com");
  await page.locator('input[name="delivery"][value="pickup"]').check();
  await expect(page.locator(".checkout-order-summary")).toContainText("Сейчас платить не нужно");
  await expect(page.getByText("Способ оплаты заказа")).toHaveCount(0);
  await page.locator('.checkout-submit-section input[name="consent"]').check();
  await page.getByRole("button", { name: "Отправить заказ" }).click();
  await expect(page.getByText("Заказ отправлен менеджеру")).toBeVisible();
  expect(submittedPayment).toBe("");
});

test("@desktop самовывоз сохраняет оплату наличными для растения под заказ", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/payments/methods?*", (route) => route.fulfill({ json: { methods: [
    { id: "online", title: "Картой на сайте", note: "После оформления" },
    { id: "on_delivery", title: "Наличными при получении", note: "В магазине при самовывозе" },
  ] } }));
  let submittedPayment: string | undefined;
  let paymentAttempts = 0;
  await page.route("**/api/v1/orders", (route) => {
    submittedPayment = route.request().postDataJSON().paymentMethod;
    return route.fulfill({ json: { orderNumber: "PRE-CASH", hasPreorder: true, paymentStatus: "on_delivery" } });
  });
  await page.route("**/api/v1/payments/orders/**", (route) => {
    paymentAttempts++;
    return route.fulfill({ status: 400, json: {} });
  });
  await setStoredCounts(page, [], { "3": 1 });
  await page.goto("/checkout");
  await page.getByLabel("Имя", { exact: true }).fill("Тест");
  await page.getByLabel("Телефон", { exact: true }).fill("9000000000");
  await page.getByLabel("Email для чека", { exact: true }).fill("checkout-test@example.test");
  await page.locator('input[name="delivery"][value="pickup"]').check();
  await page.locator('input[name="paymentMethod"][value="on_delivery"]').check();
  await expect(page.getByText("Растения и доставка оплачиваются на сайте.", { exact: false })).toHaveCount(0);
  await page.locator('input[name="consent"]').check();
  await page.getByRole("button", { name: "Отправить заказ" }).click();
  await expect(page.getByText("Заказ отправлен менеджеру")).toBeVisible();
  expect(submittedPayment).toBe("on_delivery");
  expect(paymentAttempts).toBe(0);
});
