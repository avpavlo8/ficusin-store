import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

for (const empty of [false, true]) {
  test(`@phone @desktop явная кнопка отзыва ${empty ? "без отзывов" : "с отзывами"}`, async ({ page }, testInfo) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    await mockApi(page);
    const responsePromise = page.waitForResponse("**/api/v1/products/1");
    await page.goto("/product/1");
    const body = await (await responsePromise).json();
    if (empty) {
      await page.route("**/api/v1/products/1", async (route) => {
        body.product.reviews = [];
        body.product.reviewsCount = 0;
        body.product.rating = 0;
        await route.fulfill({ json: body });
      });
      await page.reload();
    }
    const purchaseButton = page.locator(".pdp-summary").getByRole("button", { name: "Написать отзыв", exact: true });
    await expect(purchaseButton).toBeVisible();
    expect((await purchaseButton.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    await purchaseButton.scrollIntoViewIfNeeded();
    await expect(purchaseButton).toBeInViewport();
    await page.locator(".pdp-summary").screenshot({ path: `test-results/review-cta-${testInfo.project.name}-${empty}-purchase.png` });
    await purchaseButton.click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByLabel("Ваш отзыв")).toBeFocused();
    await page.getByRole("button", { name: "Закрыть", exact: true }).click();
    await page.getByRole("tab", { name: /Отзывы/ }).click();
    const feedButton = page.locator("#reviews").getByRole("button", { name: "Написать отзыв", exact: true });
    await feedButton.scrollIntoViewIfNeeded();
    await expect(feedButton).toBeInViewport();
    await page.locator("#reviews").screenshot({ path: `test-results/review-cta-${testInfo.project.name}-${empty}-feed.png` });
    await feedButton.click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByLabel("Ваш отзыв")).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}
