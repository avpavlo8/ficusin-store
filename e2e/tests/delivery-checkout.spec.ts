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
