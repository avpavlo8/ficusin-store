import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mockApi } from "./helpers";

test("@desktop @phone вкладка ухода не сдвигает страницу, а прямая ссылка ведёт к теме", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  const tabs = page.getByRole("tablist", { name: "Разделы товара" });
  await expect(tabs).toBeVisible();
  await tabs.evaluate(element => window.scrollTo({ top: window.scrollY + element.getBoundingClientRect().top, behavior: "instant" }));
  const before = await page.evaluate(() => window.scrollY);
  for (const name of ["Уход", "О растении", "Уход"]) {
    await page.getByRole("tab", { name, exact: true }).click();
    // Include deferred effects and smooth scrolling, not just the first frame.
    await page.waitForTimeout(600);
    expect(await page.evaluate(() => window.scrollY)).toBeCloseTo(before, 0);
  }
  await page.getByRole("tab", { name: "О растении", exact: true }).click();
  await page.goto("/product/1#plant-watering");
  await expect(page.getByRole("tab", { name: "Уход", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect.poll(async () => (await page.locator("#plant-watering").boundingBox())?.y).toBeLessThan(150);
  await expect.poll(async () => (await page.locator("#plant-watering").boundingBox())?.y).toBeGreaterThanOrEqual(0);
});

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
  await expect(page.locator(".plant-about-photo")).toHaveAttribute("src", "/assets/care/guide-v4/about.jpg");
  await page.locator(".plant-about-photo").scrollIntoViewIfNeeded();
  await expect(page.locator(".plant-about-photo")).toHaveJSProperty("naturalWidth", 1200);
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


test("@desktop @phone все тексты загружены до кликов, видна только выбранная вкладка", async ({ page }) => {
  await mockApi(page);
  let productRequests = 0;
  page.on("request", request => { if (new URL(request.url()).pathname === "/api/v1/products/1") productRequests++; });
  await page.goto("/product/1");
  await expect(page.locator("#pdp-panel-about")).toBeVisible();
  // textContent reads the rendered DOM even when the native hidden panel is closed.
  await expect(page.locator("#pdp-panel-care")).toContainText("После просыхания верхнего слоя");
  await expect(page.locator("#pdp-panel-questions")).toContainText("Весной, когда корни заполнят горшок.");
  await expect(page.locator("#pdp-panel-reviews #reviews")).toBeAttached();
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  const loadedRequests = productRequests;
  await page.getByRole("tab", { name: "Вопросы", exact: true }).click();
  await page.locator("#questions summary").first().click();
  await page.getByRole("tab", { name: "Уход", exact: true }).click();
  await expect(page.locator("#pdp-panel-care")).toBeVisible();
  await expect(page.locator("#pdp-panel-questions")).toBeHidden();
  await page.getByRole("tab", { name: "Вопросы", exact: true }).click();
  await expect(page.locator("#questions details").first()).toHaveAttribute("open", "");
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  expect(productRequests).toBe(loadedRequests);
});

test("@desktop вкладки переключаются стрелками и не оставляют скрытый контент в фокусе", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  const about = page.getByRole("tab", { name: "О растении", exact: true });
  await about.focus();
  await about.press("ArrowRight");
  await expect(page.getByRole("tab", { name: "Уход", exact: true })).toBeFocused();
  await expect(page.locator("#pdp-panel-care")).toBeVisible();
  await page.keyboard.press("End");
  await expect(page.getByRole("tab", { name: "Вопросы", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(about).toBeFocused();
  await expect(page.locator("#pdp-panel-about")).toBeVisible();
  const accessibility = await new AxeBuilder({ page }).include(".pdp-tabs-shell").withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(accessibility.violations.filter(item => item.impact === "serious" || item.impact === "critical")).toEqual([]);
});
