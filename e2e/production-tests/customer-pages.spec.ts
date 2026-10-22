import { expect, test } from "@playwright/test";

test("production wholesale directory finds a real organization and fills its details", async ({ page, request, isMobile }, info) => {
  const inn = "7707083893";
  const response = await request.get(`/api/v1/organizations/suggest?q=${inn}`);
  expect(response.ok()).toBeTruthy();
  const data = await response.json();
  expect(data.available).toBe(true);
  const organization = data.suggestions.find((item: { inn: string }) => item.inn === inn);
  expect(organization).toBeTruthy();
  expect(organization.name).toBeTruthy();
  expect(organization.legalAddress).toBeTruthy();

  await page.goto("/register");
  await page.getByRole("radio", { name: /Оптовый/ }).check();
  await expect(page.locator(".organization-details")).toHaveCount(0);
  const search = page.getByRole("combobox", { name: "Название организации или ИНН", exact: true });
  await search.fill(inn);
  const option = page.getByRole("option").filter({ hasText: organization.name }).filter({ hasText: organization.kpp }).first();
  await expect(option).toBeVisible();
  if (isMobile) await option.tap(); else await option.click();
  await expect(search).toHaveValue(organization.name);
  for (const [label, value] of [["ИНН", inn], ["КПП", organization.kpp], ["Юридический адрес", organization.legalAddress]]) {
    const field = page.getByLabel(label, { exact: true });
    await expect(field).toHaveValue(value);
    await expect(field).toHaveJSProperty("readOnly", true);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(0);
  await page.screenshot({ path: info.outputPath("real-organization.png"), fullPage: true });
  await search.fill("770");
  await expect(page.locator(".organization-details")).toHaveCount(0);
});

test("production information pages have navigation and the Yandex organization map", async ({ page }) => {
  for (const route of ["/delivery-and-returns", "/contacts", "/offer", "/privacy", "/requisites"]) {
    await page.goto(route);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Информация для покупателей" }).locator('a[aria-current="page"]')).toHaveCount(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(0);
    if (route === "/contacts") await expect(page.locator(".contacts-map iframe")).toHaveAttribute("src", /yandex\.ru\/map-widget\/v1\/\?.*oid=121536326951/);
  }
});
