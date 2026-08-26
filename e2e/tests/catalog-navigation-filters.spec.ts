import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi } from "./helpers";

async function openPlantFilters(page: import("@playwright/test").Page) {
  await page.goto("/catalog/plants");
  await page.getByRole("button", { name: /Фильтры/ }).click();
}

test("@desktop Неприхотливые открывает каноническую страницу подборки", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  const easy = page.locator(".preset", { hasText: "Неприхотливые" });
  await expect(easy).toHaveAttribute("href", "/collections/easy");
  await easy.click();
  await expect(page).toHaveURL(/\/collections\/easy$/);
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toHaveCount(0);
});

test("@desktop у подборок отсутствует множественный выбор", async ({ page }) => {
  await mockApi(page);
  await page.goto("/collections/bathroom");
  await expect(page.locator(".preset.active")).toHaveCount(1);
  await page.locator(".preset", { hasText: "Для офиса" }).click();
  await expect(page).toHaveURL(/\/collections\/office$/);
  await expect(page.locator(".preset.active")).toHaveCount(1);
  await expect(page.locator(".preset.active")).toContainText("Для офиса");
});

test("@desktop сброс дополнительных фильтров сохраняет подборку", async ({ page }) => {
  await mockApi(page);
  await page.goto("/collections/tall");
  await page.getByRole("button", { name: /Фильтры/ }).click();
  await page.locator(".home-filter-panel").getByLabel("Только в наличии").check();
  await page.locator(".home-filter-panel").getByRole("button", { name: "Сбросить фильтры" }).click();
  await expect(page).toHaveURL(/\/collections\/tall$/);
  await expect(page.locator(".preset.active")).toContainText("Вырастает высоким");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Монстера Делициоза")).toBeVisible();
});

test("@desktop общий каталог не показывает смешанные атрибутные фильтры", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  await expect(page.locator(".storefront-attribute-filters")).toHaveCount(0);
  await expect(page.locator(".home-filter-group .catalog-dropdown")).toHaveCount(0);
  await expect(page.getByText("Тип кашпо", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Освещение", { exact: true })).toHaveCount(0);
});

test("@desktop категория растений показывает только растительные фильтры", async ({ page }) => {
  await mockApi(page);
  await openPlantFilters(page);
  const filters = page.locator(".storefront-attribute-filters");
  await expect(filters).toContainText("Освещение");
  await expect(filters).toContainText("Сложность ухода");
  await expect(filters).toContainText("Высота");
  await expect(filters).not.toContainText("Тип кашпо");
  await expect(filters).not.toContainText("Материал");
});

test("@desktop категория кашпо показывает только фильтры кашпо", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/pots");
  await page.getByRole("button", { name: /Фильтры/ }).click();
  const filters = page.locator(".storefront-attribute-filters");
  await expect(filters).toContainText("Тип кашпо");
  await expect(filters).toContainText("Материал");
  await expect(filters).toContainText("Дренажное отверстие");
  await expect(filters).not.toContainText("Освещение");
  await expect(filters).not.toContainText("Сложность ухода");
});

test("@desktop фильтры подборки вычисляются только по товарам подборки", async ({ page }) => {
  await mockApi(page);
  await page.goto("/collections/easy");
  await page.getByRole("button", { name: /Фильтры/ }).click();
  const filters = page.locator(".storefront-attribute-filters");
  await expect(filters).toContainText("Освещение");
  await expect(filters).not.toContainText("Тип кашпо");
  await expect(filters.getByRole("option", { name: /Рассеян/ })).toHaveCount(0);
});

test("@desktop прямой URL и Back Forward восстанавливают категорию", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/aglaonema");
  await expect(page.locator(".storefront-main").getByRole("heading", { name: "Аглаонема" })).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toBeVisible();
  await page.locator(".storefront-tree").getByRole("link", { name: /Фикус/ }).click();
  await expect(page).toHaveURL(/\/catalog\/ficus$/);
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/catalog\/aglaonema$/);
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toBeVisible();
  await page.goForward();
  await expect(page).toHaveURL(/\/catalog\/ficus$/);
});

test("@desktop неизвестные category и collection slug показывают 404", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/not-a-category");
  await expect(page.getByRole("heading", { name: /Здесь ничего не растёт/ })).toBeVisible();
  await page.goto("/collections/not-a-collection");
  await expect(page.getByRole("heading", { name: /Здесь ничего не растёт/ })).toBeVisible();
});

test("@desktop пустая выдача сохраняет контекст и предлагает сброс", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/plants?min.height_cm=999");
  await expect(page.locator(".storefront-empty")).toContainText("По текущим фильтрам товаров нет");
  await page.locator(".storefront-empty").getByRole("button", { name: "Сбросить фильтры" }).click();
  await expect(page).toHaveURL(/\/catalog\/plants$/);
  await expect(page.locator(".storefront-card")).toHaveCount(3);
});

test("@desktop range chips select и boolean имеют разные контролы", async ({ page }) => {
  await mockApi(page);
  await openPlantFilters(page);
  const filters = page.locator(".storefront-attribute-filters");
  await expect(filters.getByLabel("Высота: от")).toHaveAttribute("type", "number");
  await expect(filters.getByLabel("Высота: до")).toHaveAttribute("type", "number");
  const select = filters.locator("select");
  await expect(select).toHaveCount(1);
  await expect(select.locator("option", { hasText: "Полутень" })).toHaveCount(1);
  const careChips = filters.locator(".catalog-chip-filter", { hasText: "Сложность ухода" }).locator("button");
  await expect(careChips).toHaveCount(2);
  const flowering = filters.locator(".catalog-chip-filter", { hasText: "Цветёт" });
  await expect(flowering.locator("button", { hasText: "Да" })).toHaveCount(1);
  await expect(flowering.locator("button", { hasText: "Нет" })).toHaveCount(1);
  await expect(flowering).not.toContainText("true");
  await expect(flowering).not.toContainText("false");
});

test("@desktop смена категории очищает несовместимые значения", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/plants?filter.light_level=low_light");
  await expect(page).toHaveURL(/filter\.light_level=low_light/);
  await page.locator(".storefront-tree").getByRole("link", { name: /Кашпо и горшки/ }).click();
  await expect(page).toHaveURL(/\/catalog\/pots$/);
  await expect(page.url()).not.toContain("light_level");
  await page.getByRole("button", { name: /Фильтры/ }).click();
  await expect(page.locator(".storefront-attribute-filters")).not.toContainText("Освещение");
});

test("@desktop поиск из категории явно становится глобальным", async ({ page }) => {
  await mockApi(page);
  await page.goto("/catalog/aglaonema?q=фикус");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".catalog-search-scope")).toContainText("по всему каталогу");
});

test("@phone существующая вёрстка каталога не получает горизонтальный скролл", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto("/catalog/plants");
  await expect.poll(() => horizontalOverflow(page)).toBeLessThanOrEqual(1);
});
