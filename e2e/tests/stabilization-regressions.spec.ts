import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

test("@desktop legacy backend placeholder is not shown as the product photo", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/legacy-placeholder", (route) => route.fulfill({ json: { product: {
    id: "legacy-placeholder",
    name: "Товар без фотографии",
    latin: "",
    shortDescription: "",
    description: "",
    careInstructions: "",
    images: ["/assets/hero-monstera.webp"],
    variants: [],
    recommendations: [],
    passport: {},
    importantWarnings: [],
    rating: 0,
    reviewsCount: 0,
    reviews: [],
    catalogSection: "pots",
    attributes: [],
  } } }));

  await page.goto("/product/legacy-placeholder");

  await expect(page.locator(".pdp-image-placeholder")).toContainText("Фото скоро появится");
  await expect(page.locator('.pdp-gallery img[src="/assets/hero-monstera.webp"]')).toHaveCount(0);
});
