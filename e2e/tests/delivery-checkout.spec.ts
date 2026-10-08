import { expect, test, type Page } from "@playwright/test";
import { mockApi, setStoredCounts } from "./helpers";

async function openDeliveryStep(page: Page) {
  await setStoredCounts(page, [], { "1": 1 });
  await page.goto("/checkout");
  const contact = page.locator('[data-checkout-step="1"]');
  await contact.getByLabel("Имя").fill("Александр");
  await contact.getByLabel("Телефон").fill("9151234567");
  await contact.getByLabel("Email для чека").fill("client@example.com");
  await contact.getByRole("button", { name: /Продолжить/ }).click();
  await expect(page.locator('[data-checkout-step="2"]')).toBeVisible();
}

test("@desktop покупателю доступны только самовывоз и СДЭК", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/cdek?action=status", (route) =>
    route.fulfill({ json: { available: true } }));
  await openDeliveryStep(page);

  const methods = page.locator('[data-checkout-step="2"] input[name="delivery"]');
  await expect(methods).toHaveCount(2);
  await expect(methods.nth(0)).toHaveValue("pickup");
  await expect(methods.nth(1)).toHaveValue("cdek");
  await expect(page.getByLabel("Курьер по Рязани")).toHaveCount(0);
  await expect(page.getByLabel("Почта России")).toHaveCount(0);
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

  await page.locator('input[name="delivery"][value="cdek"]').check();
  await page.getByLabel("Город получения").fill("Моск");
  await page.getByRole("listbox", { name: "Найденные города" }).getByRole("button", { name: /Москва/ }).click();
  await page.getByLabel("Пункт выдачи").click();
  await page.getByRole("listbox", { name: "Пункты выдачи" }).getByRole("button", { name: /Тверская, 1/ }).click();

  const summary = page.locator(".checkout-order-summary");
  await expect(summary).toContainText(/Растения\s*1.?490/);
  await expect(summary).toContainText(/Доставка\s*590/);
  await expect(summary).toContainText(/Итого к оплате\s*2.?080/);
});
