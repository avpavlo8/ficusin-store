import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mockApi } from "./helpers";

const articles = [
  ["mealybug", "Мучнистый червец"],
  ["spider-mite", "Паутинный клещ"],
  ["yellow-leaves", "Почему желтеют листья"],
  ["root-problems", "Проблемы с корнями"],
];

for (const [slug, title] of articles) {
  test(`@desktop @phone статья ${slug}: прямой адрес, якоря и читаемость`, async ({ page }) => {
    await mockApi(page);
    await page.goto(`/care/${slug}`);
    await expect(page.getByRole("heading", { level: 1, name: title, exact: true })).toBeVisible();
    await expect(page.locator(".care-first-action")).toContainText("С чего начать");
    await expect(page.locator(".care-story-section")).toHaveCount(9);
    await expect(page.locator(".care-story-hero img")).toHaveJSProperty("naturalWidth", 1200);
    await expect(page.locator(".care-story-hero img")).toHaveAttribute("src", `/assets/care/articles-v1/${slug}.jpg`);
    await expect(page.locator(".care-story-hero img")).toHaveCSS("object-fit", "contain");
    const toc = page.getByRole("navigation", { name: "В этой статье" });
    await toc.getByRole("link").nth(1).click();
    const id = await toc.getByRole("link").nth(1).getAttribute("href");
    await expect.poll(async () => (await page.locator(id!).boundingBox())!.y).toBeGreaterThanOrEqual(0);
    await expect.poll(async () => (await page.locator(id!).boundingBox())!.y).toBeLessThan(160);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    const result = await new AxeBuilder({ page }).include(".care-reading-shell").withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(result.violations).toEqual([]);
  });
}

test("@desktop @phone из ухода в статью и обратно к тому же растению", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1#plant-passport");
  await page.locator(".plant-help").getByRole("link", { name: /Мучнистый червец/ }).click();
  await expect(page).toHaveURL(/\/care\/mealybug\?plant=1$/);
  await page.locator(".care-related").getByRole("link", { name: /Проблемы с корнями/ }).click();
  await expect(page).toHaveURL(/\/care\/root-problems\?plant=1$/);
  await page.getByRole("link", { name: "← К уходу за вашим растением", exact: true }).first().click();
  await expect(page).toHaveURL(/\/product\/1#plant-passport$/);
  await expect(page.getByRole("tab", { name: "Уход", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".plant-arrival")).toBeVisible();
});

test("@desktop библиотека показывает четыре инструкции, неизвестная статья — 404", async ({ page }) => {
  await mockApi(page);
  await page.goto("/care");
  await expect(page.locator(".care-related a")).toHaveCount(4);
  await page.goto("/care/does-not-exist");
  await expect(page.getByRole("heading", { name: /Здесь ничего/ })).toBeVisible();
});
