import { expect, test } from "@playwright/test";
import { mockApi, setStoredCounts } from "./helpers";

async function openDeliveryStep(page: Parameters<typeof test>[0] extends never ? never : any) {
  await setStoredCounts(page, [], { "saby-1": 1 });
  await page.goto("/checkout");
  const contact = page.locator('[data-checkout-step="1"]');
  await contact.getByLabel("Имя").fill("Александр");
  await contact.getByLabel("Телефон").fill("9151234567");
  await contact.getByLabel("Email для чека").fill("client@example.com");
  await contact.getByRole("button", { name: /Продолжить/ }).click();
  await expect(page.locator('[data-checkout-step="2"]')).toBeVisible();
}

test("@desktop Почта России показывает договорной тариф и не пропускает без расчёта", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/providers", (route) =>
    route.fulfill({ json: { courier: false, post: true } }));
  await page.route("**/api/v1/delivery/post", async (route) => {
    const body = route.request().postDataJSON() as { address?: string; items?: Array<{ id: string; quantity: number }> };
    expect(body.address).toContain("Москва");
    expect(body.items).toEqual([{ id: "saby-1", quantity: 1 }]);
    await route.fulfill({ json: { quote: { price: 615, daysMin: 2, daysMax: 4, service: "Почта России" } } });
  });

  await openDeliveryStep(page);
  await page.getByLabel("Почта России").check();
  const step = page.locator('[data-checkout-step="2"]');
  await step.getByLabel("Адрес доставки").fill("Москва, Мясницкая улица, 1");
  await expect(step.getByRole("button", { name: "Продолжить →" })).toBeDisabled();
  await step.getByRole("button", { name: "Рассчитать доставку" }).click();
  await expect(step.getByText("615 ₽")).toBeVisible();
  await expect(step.getByText("2–4 дн.")).toBeVisible();
  await expect(step.getByRole("button", { name: "Продолжить →" })).toBeEnabled();
});

test("@desktop курьер по Рязани получает цену Яндекс Доставки", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/providers", (route) =>
    route.fulfill({ json: { courier: true, post: false } }));
  await page.route("**/api/v1/delivery/courier", (route) =>
    route.fulfill({ json: { quote: { price: 430, service: "Яндекс Доставка · Курьер" } } }));

  await openDeliveryStep(page);
  await page.getByLabel("Курьер по Рязани").check();
  const step = page.locator('[data-checkout-step="2"]');
  await step.getByLabel("Адрес доставки").fill("Рязань, улица Ленина, 1");
  await step.getByRole("button", { name: "Рассчитать доставку" }).click();
  await expect(step.getByText("430 ₽")).toBeVisible();
  await expect(step.getByText(/Яндекс Доставка · Курьер/)).toBeVisible();
});

test("@desktop неподключённые перевозчики не предлагаются покупателю", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/providers", (route) =>
    route.fulfill({ json: { courier: false, post: false } }));

  await openDeliveryStep(page);
  await expect(page.getByLabel("Курьер по Рязани")).toHaveCount(0);
  await expect(page.getByLabel("Почта России")).toHaveCount(0);
  await expect(page.getByLabel("Самовывоз в Рязани")).toBeVisible();
});
