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
    await expect(page.locator(".pdp-summary").getByRole("button", { name: "Написать отзыв", exact: true })).toHaveCount(0);
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

for (const status of ["completed", "confirmed", "cancelled"]) {
  test(`@phone @desktop отзыв из заказа ${status}`, async ({ page }, testInfo) => {
    const { owner } = await import("./helpers");
    await page.emulateMedia({ reducedMotion: "reduce" });
    await mockApi(page, owner);
    await page.route("**/api/v1/account/orders/REVIEW-ORDER", route => route.fulfill({ json: {
      order: {
        orderNumber: "REVIEW-ORDER", status, deliveryMethod: "pickup", address: "Магазин", comment: "",
        customerName: "Покупатель", phone: "+79990000000", email: "test@example.invalid",
        paymentMethod: "online", paymentStatus: "paid", deliveryFee: 0, subtotal: 2980, total: 2980,
        paidAmount: 2980, refundedAmount: 0, amountDue: 0, paymentReady: false, createdAt: "2026-11-01T12:00:00Z",
        items: [
          { productName: "Аглаонема Мария", productSlug: "1", unitPrice: 1490, quantity: 1 },
          { productName: "Снятый с публикации товар", unitPrice: 1490, quantity: 1 },
        ], shipmentOffers: [],
      },
    } }));
    await page.goto("/account/orders/REVIEW-ORDER");
    const review = page.getByRole("link", { name: /Оставить отзыв о товаре/ });
    if (status !== "completed") {
      await expect(page.getByRole("heading", { name: /Ваши растения/ })).toBeVisible();
      await expect(review).toHaveCount(0);
      return;
    }
    await expect(review).toHaveCount(1);
    await expect(review).toHaveAttribute("href", "/product/1#write-review");
    expect((await review.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    await page.locator(".order-detail-plants").screenshot({ path: `test-results/order-reviews-${testInfo.project.name}.png` });
    await review.click();
    await expect(page).toHaveURL(/\/product\/1#write-review$/);
    await expect(page.getByRole("tab", { name: /Отзывы/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByLabel("Ваш отзыв")).toBeFocused();
    await page.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(page.locator(".pdp-summary").getByRole("button", { name: "Написать отзыв", exact: true })).toHaveCount(0);
    await page.locator("#reviews").getByRole("button", { name: "Написать отзыв", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });
}
