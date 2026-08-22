import { expect, test, type Page } from "@playwright/test";
import { horizontalOverflow, owner } from "./helpers";

async function mockCatalogAdmin(page: Page) {
  await page.addInitScript((user) => {
    const originalFetch = window.fetch.bind(window);
    const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));
    window.fetch = async (input, init) => {
      const raw = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      const path = new URL(raw, window.location.origin).pathname;
      if (path === "/api/v1/auth/me") return json({ user });
      if (path === "/api/v1/admin/dashboard") return json({
        user: { fullName: "Александр" }, role: "owner",
        permissions: ["dashboard.read", "products.read", "products.edit"],
        dashboard: { products: 1, variants: 1, orders: 0, customers: 0, wholesalePending: 0, lastSync: null, recentOrders: [] },
      });
      if (path === "/api/v1/admin/categories") return json({ categories: [
        { id: 1, parentId: null, name: "Растения", slug: "plants", sortOrder: 10, productsCount: 1 },
        { id: 2, parentId: 1, name: "Для ванной", slug: "bathroom", sortOrder: 10, productsCount: 1 },
      ] });
      if (path === "/api/v1/admin/attributes") return json({ attributes: [
        { id: 1, code: "placement", name: "Размещение", description: "", dataType: "multi_enum", unit: "", audience: "customer", scope: "product", global: false, active: true, options: [{ code: "bathroom", label: "Ванная", sortOrder: 10, active: true }] },
        { id: 2, code: "package_weight_g", name: "Вес упаковки", description: "", dataType: "number", unit: "г", audience: "technical", scope: "variant", global: false, active: true, options: [] },
      ] });
      if (path.endsWith("/effective-attributes")) return json({ attributes: [
        { id: 1, code: "placement", name: "Размещение", description: "", dataType: "multi_enum", unit: "", audience: "customer", scope: "product", global: false, active: true, options: [{ code: "bathroom", label: "Ванная", active: true }], required: true, filterable: true, showOnPdp: true, keyCharacteristic: false, badge: false, sortOrder: 10, showInCharacteristics: true, excluded: false, inherited: true, sourceCategoryId: 1, sourceCategoryName: "Растения" },
      ] });
      if (path === "/api/v1/admin/catalog-filters") return json({ filters: [] });
      if (path === "/api/v1/admin/products") return json({ products: [{ id: 1, name: "Аглаонема Мария", latinName: "Aglaonema", sku: "FIC-1", stock: 5 }] });
      if (path === "/api/v1/admin/collection-definitions") return json({ collections: [{ id: 1, slug: "bathroom", title: "Для ванной", note: "Любят влажный воздух", coverUrl: "/assets/redesign/filters/bathroom-wall-v2.webp", sortOrder: 10, active: true, mode: "dynamic", rules: [{ attribute: "placement", operator: "contains", value: "bathroom" }], products: [1] }] });
      if (path.startsWith("/api/v1/")) return json({});
      return originalFetch(input, init);
    };
  }, owner.user);
  await page.goto("/admin");
}

for (const target of ["desktop", "phone"] as const) {
  test(`@${target} подборки и PIM не выходят за границы экрана`, async ({ page }) => {
    await mockCatalogAdmin(page);
    await page.locator(".account-sidebar").getByRole("button", { name: "Подборки" }).click();
    await expect(page.locator(".admin-collection-head", { hasText: "Для ванной" })).toBeVisible();
    const shellOverflow = await horizontalOverflow(page);
    await page.locator(".admin-collection-head", { hasText: "Для ванной" }).click();
    await expect(page.getByText("Обложка подборки")).toBeVisible();
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(Math.max(1, shellOverflow));

    await page.locator(".account-sidebar").getByRole("button", { name: "Категории" }).click();
    await expect(page.getByRole("heading", { name: "Атрибуты и схема категорий" })).toBeVisible();
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(Math.max(1, shellOverflow));
  });
}
