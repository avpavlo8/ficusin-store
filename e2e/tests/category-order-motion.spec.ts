import { expect, test } from "@playwright/test";
import { catalogue, mockApi } from "./helpers";

test("@desktop @phone plant categories use Russian alphabetical order despite old manual positions", async ({ page }) => {
  await mockApi(page);
  const names = ["Цитрусовые", "Мясоеды", "Ананас", "Туя", "Адениум", "Амариллис", "Традесканция", "Азалия"];
  const expected = ["Адениум", "Азалия", "Амариллис", "Ананас", "Мясоеды", "Традесканция", "Туя", "Цитрусовые"];
  const kinds = names.map((name, index) => ({ id: index + 10, parentId: 2, name, slug: `kind-${index}`, sortOrder: index }));
  await page.route("**/api/v1/categories", route => route.fulfill({ json: { categories: [
    { id: 1, parentId: null, name: "Растения", slug: "plants", sortOrder: 0 },
    { id: 2, parentId: 1, name: "Комнатные растения", slug: "indoor-plants", sortOrder: 0 },
    ...kinds,
  ] } }));
  await page.route("**/api/v1/catalog", route => route.fulfill({ json: { products: kinds.map(kind => ({
    ...catalogue[0], id: String(kind.id), sku: String(kind.id), name: kind.name, categoryId: kind.id,
  })) } }));
  await page.goto("/#catalog");
  const picker = page.getByLabel("Категория", { exact: true });
  await expect(picker.locator("option")).toHaveCount(10);
  expect((await picker.locator("option").allTextContents()).slice(2).map(label => label.trim().split(" · ")[0])).toEqual(expected);
  const tree = page.getByRole("navigation", { name: "Категории товаров", includeHidden: true });
  expect(await tree.locator('a[href^="/catalog/kind-"] > span').allTextContents()).toEqual(expected);
  expect((await page.locator('.desktop-nav a[href^="/catalog/kind-"]').allTextContents()).map(label => label.replace("→", ""))).toEqual(expected);
});

test("@desktop @phone category feedback preserves selection and stops moving with reduced motion", async ({ page }) => {
  await mockApi(page);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/catalog/ficus");
  const selected = page.locator('.storefront-tree a[aria-current="page"]');
  await expect(selected).toContainText("Фикус");
  await expect(selected).toHaveCSS("animation-name", "category-selection-fill");
  const picker = page.getByLabel("Категория", { exact: true });
  await expect(picker).toHaveValue("ficus");
  await expect(picker).toHaveCSS("animation-name", "category-selection-fill");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(selected).toHaveCSS("animation-name", "none");
  await expect(selected.locator(".category-icon")).toHaveCSS("transform", "none");
  await expect(picker).toHaveCSS("animation-name", "none");
  await expect(selected).toHaveCSS("background-color", "rgb(230, 231, 182)");
  await expect(picker).toHaveCSS("background-color", "rgb(230, 231, 182)");
  await expect(page.locator(".storefront-card")).toHaveCount(2);
});
