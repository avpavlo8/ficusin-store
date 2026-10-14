import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mockApi } from "./helpers";

test("@desktop блоки ухода совпадают с шириной навигации на большом экране", async ({ page }) => {
  await page.setViewportSize({ width: 1872, height: 1000 });
  await mockApi(page);
  await page.goto("/product/1#plant-passport");
  await expect(page.locator(".plant-arrival")).toBeVisible();
  const nav = await page.getByRole("tablist", { name: "Разделы товара" }).boundingBox();
  for (const selector of [".plant-arrival", ".plant-care-panel"]) {
    const panel = await page.locator(selector).first().boundingBox();
    expect(panel!.x).toBeCloseTo(nav!.x, 0);
    expect(panel!.width).toBeCloseTo(nav!.width, 0);
  }
});

test("@desktop @phone длинное описание и отзывы с фотографиями читаются без обрезки", async ({ page }) => {
  await mockApi(page);
  const description = Array.from({ length: 4 }, () => "Описание растения и его особенностей. ".repeat(8)).join("\n\n");
  const reviews = [
    { id: 1, rating: 5, text: "Растение приехало в хорошем состоянии. ".repeat(12), author: "Анна", date: "2026-09-20", verifiedPurchase: true, photos: Array(4).fill("/assets/hero-monstera.webp") },
    { id: 2, rating: 3, text: "Упаковка помялась, само растение целое.", author: "Ирина", date: "2026-09-21", verifiedPurchase: false, photos: [] },
  ];
  await page.route("**/api/v1/products/sections", route => route.fulfill({ json: { product: {
    id: "sections", name: "Аглаонема с длинным названием сорта", latin: "Aglaonema", description,
    images: ["/assets/hero-monstera.webp"], variants: [], recommendations: [], passport: {}, importantWarnings: [],
    rating: 4, reviewsCount: 2, reviews, catalogSection: "plants",
  } } }));
  await page.goto("/product/sections");
  await expect(page.locator(".plant-about-copy p")).toHaveCount(4);
  await expect(page.locator(".plant-about-photo")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  await page.getByRole("tab", { name: /Отзывы/ }).click();
  await expect(page.locator(".review-feed article")).toHaveCount(2);
  await expect(page.locator(".review-feed article").first()).toContainText("Ирина");
  await page.getByRole("combobox", { name: "Сортировка" }).selectOption("high");
  const review = page.locator(".review-feed article").first();
  await expect(review).toContainText("Анна");
  await expect(review).toContainText(reviews[0].text.trim());
  await expect(review.locator(".review-photos img")).toHaveCount(4);
  const mediaFit = await review.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    return [...element.querySelectorAll(".review-photos img")].every(img => {
      const photo = img.getBoundingClientRect();
      return photo.right <= bounds.right && photo.bottom <= bounds.bottom;
    });
  });
  expect(mediaFit).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const accessibility = await new AxeBuilder({ page }).include("#reviews").withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(accessibility.violations.filter(item => item.impact === "serious" || item.impact === "critical")).toEqual([]);
});
