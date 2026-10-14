import { expect, test } from "@playwright/test";

test("care instructions have live content and canonical URLs", async ({ page }) => {
  for (const [slug, title] of [
    ["mealybug", "Мучнистый червец"], ["spider-mite", "Паутинный клещ"],
    ["yellow-leaves", "Почему желтеют листья"], ["root-problems", "Проблемы с корнями"],
  ]) {
    const response = await page.goto(`/care/${slug}?plant=33`);
    expect(response?.status()).toBe(200);
    await expect(page.getByRole("heading", { level: 1, name: title, exact: true })).toBeVisible();
    await expect(page.locator(".care-story-section")).toHaveCount(9);
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", new RegExp(`/care/${slug}$`));
    await expect(page.getByRole("link", { name: "← К уходу за вашим растением", exact: true }).first()).toHaveAttribute("href", "/product/33#plant-passport");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
});
