import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

const baseProduct = {
  id: "care-layout", name: "Растение с подробным уходом", latin: "", shortDescription: "", description: "",
  careInstructions: "Индивидуальная заметка из карточки.", images: ["/assets/hero-monstera.webp"],
  variants: [], recommendations: [], importantWarnings: ["Берегите от холодного сквозняка."],
  rating: 0, reviewsCount: 0, reviews: [], catalogSection: "plants",
};

test("@desktop @phone длинный уход растягивает блок, сохраняя текст и пропорции фотографии", async ({ page }) => {
  await mockApi(page);
  let soil = "Рыхлая смесь с корой и перлитом.";
  await page.route("**/api/v1/products/care-layout", route => route.fulfill({ json: { product: {
    ...baseProduct, passport: { lighting: "Яркий рассеянный свет.", watering: "Проверьте грунт перед поливом.", humidity: "Поддерживайте влажность воздуха.", temperature: "Избегайте холодных сквозняков.", soil, repotting: "Оцените корневой ком.", fertilizer: "Следуйте инструкции к удобрению.", growthRate: "Зимой рост замедляется." },
  } } }));
  await page.goto("/product/care-layout#plant-soil");
  const panel = page.locator("#plant-soil");
  await expect(panel).toContainText(soil);
  await expect.poll(async () => (await panel.boundingBox())?.y).toBeLessThan(150);
  const shortPanel = await panel.boundingBox();
  const shortArt = await panel.locator(".plant-care-art").boundingBox();
  soil = Array.from({ length: 8 }, (_, index) => `Абзац ${index + 1}. Подберите состав под потребности растения. Можно использовать готовую смесь или добавить к подходящему грунту кору и перлит. Количество компонентов зависит от условий дома.`).join("\n\n");
  await page.reload();
  await expect(panel.locator(".plant-copy p")).toHaveCount(8);
  await expect(panel.locator(".plant-copy")).toHaveText(soil.replace(/\n\n/g, ""));
  // Read geometry in one frame: smooth anchor scrolling moves viewport coordinates.
  const { longPanel, longArt, copy } = await panel.evaluate(element => ({
    longPanel: element.getBoundingClientRect().toJSON(),
    longArt: element.querySelector(".plant-care-art")!.getBoundingClientRect().toJSON(),
    copy: element.querySelector(".plant-copy")!.getBoundingClientRect().toJSON(),
  }));
  expect(longPanel!.height).toBeGreaterThan(shortPanel!.height + 200);
  expect(longArt!.width).toBeCloseTo(shortArt!.width, 0);
  expect(longArt!.height).toBeCloseTo(shortArt!.height, 0);
  expect(copy!.y + copy!.height).toBeLessThan(longPanel!.y + longPanel!.height);
  expect(copy!.x + copy!.width).toBeLessThanOrEqual(longPanel!.x + longPanel!.width);
  expect(copy!.x + copy!.width <= longArt!.x + 1 || copy!.y >= longArt!.y + longArt!.height - 1).toBe(true);
  await expect(page.locator(".plant-care-panel")).toHaveCount(7);
  await expect(page.locator("#plant-humidity")).toContainText("Поддерживайте влажность воздуха.");
  await expect(page.locator("#plant-temperature")).toContainText("Избегайте холодных сквозняков.");
  await expect(page.locator(".plant-warning")).toContainText(baseProduct.importantWarnings[0]);
  await expect(page.locator(".plant-care-note")).toContainText(baseProduct.careInstructions);
  await expect(page.locator("#plant-passport").getByRole("tab")).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await expect(page.locator(".plant-arrival-photo")).toHaveJSProperty("naturalWidth", 1200);
  await page.goto("/product/care-layout#plant-watering");
  await expect.poll(async () => (await page.locator("#plant-watering").boundingBox())?.y).toBeLessThan(150);
  await expect.poll(async () => (await page.locator("#plant-watering").boundingBox())?.y).toBeGreaterThanOrEqual(0);
});

test("@desktop @phone отдельная температура доступна по прямой ссылке без пустой влажности", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/care-layout", route => route.fulfill({ json: { product: {
    ...baseProduct, passport: { temperature: "Текст только о температуре." },
  } } }));
  await page.goto("/product/care-layout#plant-temperature");
  await expect(page.locator("#plant-climate-title")).toHaveText("Температура");
  await expect(page.locator("#plant-humidity")).toHaveCount(0);
  await expect(page.locator(".plant-care-panel")).toHaveCount(1);
  await expect(page.locator("#plant-temperature")).toBeVisible();
  await expect.poll(async () => (await page.locator("#plant-temperature").boundingBox())?.y).toBeLessThan(150);
});
