import { expect, test } from "@playwright/test";

// This is deliberately an unmocked UI + database/API test. The staging runtime
// must use the isolated database, mock file storage and mock MP publisher.
test("real staging: route editor draft, photos, seasonal boxes and outbound isolation", async ({ page, context, request }, testInfo) => {
  const health = await request.get("/api/v1/ready");
  expect(health.headers()["x-ficusin-environment"]).toBe("staging");
  const categoryResponse = await context.request.get("/api/v1/admin/categories");
  expect(categoryResponse.ok()).toBeTruthy();
  const { categories } = await categoryResponse.json();
  const plantRoot = categories.find((c: {slug:string}) => c.slug === "plants");
  expect(plantRoot, "staging has the plant catalog schema").toBeTruthy();
  const category = categories.find((c: {parentId:number;id:number}) => c.parentId === plantRoot.id && !categories.some((child: {parentId:number}) => child.parentId === c.id)) || plantRoot;
  const referenceResponse = await context.request.get("/api/v1/admin/catalog-editor/references?channel=saby");
  expect(referenceResponse.ok()).toBeTruthy();
  const refs = await referenceResponse.json();
  expect(refs.categories.length, "isolated staging Saby categories are prepared").toBeGreaterThan(0);
  expect(refs.folders.length, "isolated staging Saby folders are prepared").toBeGreaterThan(0);
  let creates = 0;
  page.on("request", r => { if (r.method() === "POST" && new URL(r.url()).pathname === "/api/v1/admin/products") creates++; });
  await page.goto("/admin?section=products");
  await page.getByRole("button", { name: "Добавить товар", exact: true }).click();
  await expect(page).toHaveURL(/create=1/);
  let editor = page.locator(".editor-page");
  await expect(editor).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const name = `Проверка стейджинга ${testInfo.project.name} ${Date.now()}`;
  await editor.getByLabel("Название товара *", { exact: true }).fill(name);
  await editor.getByRole("combobox", { name: "Найти категорию" }).fill(category.name);
  const categoryNames: string[] = [];
  for (let item = category; item; item = categories.find((c: {id:number}) => c.id === item.parentId)) categoryNames.unshift(item.name);
  await editor.locator(".category-picker-list").getByRole("button", { name: categoryNames.join(" / "), exact: true }).click();
  for (const [label, value] of [["Категория в СБИС", refs.categories[0]], ["Раздел в СБИС", refs.folders[0]]] as const) {
    await editor.getByRole("combobox", { name: label, exact: true }).click();
    await editor.locator(".editor-reference-options").getByRole("button", { name: value.name, exact: true }).click();
  }
  await editor.getByRole("textbox", { name: "О растении", exact: true }).fill("Изолированная проверка нового редактора.");
  await editor.getByLabel("Диаметр горшка", { exact: true }).fill("11.3");
  await editor.getByLabel("Высота", { exact: true }).fill("40");
  await editor.getByLabel("Цена на сайте, ₽ *", { exact: true }).fill("1490.99");
  await editor.getByLabel("Вес, г", { exact: true }).fill("1250");
  for (const [season, width] of [["летней упаковки", "15"], ["зимней коробки", "20"]]) {
    await editor.getByLabel(`Длина ${season}, см`, { exact: true }).fill("40");
    await editor.getByLabel(`Ширина ${season}, см`, { exact: true }).fill(width);
    await editor.getByLabel(`Высота ${season}, см`, { exact: true }).fill(width);
  }
  await editor.locator('input[type="file"]').setInputFiles({ name: "staging-cover.png", mimeType: "image/png", buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAE0lEQVR4nGOQy7MJmBbFAMRAFgAcLgQRjyuHXwAAAABJRU5ErkJggg==", "base64") });
  await expect(editor.getByRole("img", { name: "staging-cover.png", exact: true })).toBeVisible();
  await page.reload();
  editor = page.locator(".editor-page");
  await expect(editor.getByLabel("Название товара *", { exact: true })).toHaveValue(name);
  await expect(editor.getByLabel("Ширина летней упаковки, см", { exact: true })).toHaveValue("15");
  await expect(editor.getByLabel("Ширина зимней коробки, см", { exact: true })).toHaveValue("20");
  await expect(editor.getByRole("img", { name: "staging-cover.png", exact: true })).toBeVisible();
  expect(creates, "autosave/reload never creates a server product").toBe(0);
  await editor.getByRole("button", { name: "Закрыть редактор", exact: true }).click();
  await page.getByRole("button", { name: "Добавить товар", exact: true }).click();
  await expect(editor.getByLabel("Название товара *", { exact: true })).toHaveValue(name);
  const destinations = editor.getByRole("group", { name: "Где создать товар" });
  await expect(destinations.getByLabel("Сайт", { exact: true })).toBeChecked();
  await expect(destinations.getByLabel("СБИС", { exact: true })).toBeChecked();
  await expect(destinations.getByLabel("Wildberries", { exact: true })).not.toBeChecked();
  await expect(destinations.getByLabel("Ozon", { exact: true })).not.toBeChecked();
  const createdResponse = page.waitForResponse(r => r.request().method() === "POST" && new URL(r.url()).pathname === "/api/v1/admin/products");
  const submitted = page.waitForResponse(r => r.request().method() === "PUT" && /\/admin\/products\/\d+\/editor-plan$/.test(new URL(r.url()).pathname));
  await editor.getByRole("button", { name: "Создать товар", exact: true }).click();
  const response = await createdResponse;
  expect(response.ok()).toBeTruthy();
  const { product } = await response.json();
  expect(product.status).toBe("draft");
  const planResponse = await submitted;
  expect(planResponse.ok(), await planResponse.text()).toBeTruthy();
  await expect(page).toHaveURL(new RegExp(`product=${product.id}`));
  await expect(editor.getByRole("button", {name:"Сохранить", exact:true})).toBeEnabled();
  expect(creates).toBe(1);
  const variantsResponse = await context.request.get(`/api/v1/admin/products/${product.id}/variants`);
  expect(variantsResponse.ok()).toBeTruthy();
  const { variants } = await variantsResponse.json();
  expect(variants).toHaveLength(1);
  expect(variants[0].label).toBe("D11.3");
  expect(variants[0].attributes).toMatchObject({ pot_diameter_cm: 11.3, height_cm: 40, package_weight_grams: 1250, package_length_cm: 40, package_width_cm: 15, package_height_cm: 15, package_winter_length_cm: 40, package_winter_width_cm: 20, package_winter_height_cm: 20 });
  expect(Math.round(variants[0].price * 100)).toBe(149099);
  const mediaResponse = await context.request.get(`/api/v1/admin/products/${product.id}/media`);
  expect(mediaResponse.ok()).toBeTruthy();
  const { media } = await mediaResponse.json();
  expect(media).toHaveLength(1);
  expect(media[0].primary).toBe(true);
  const ownMediaResponse = await context.request.get(`/api/v1/admin/variants/${variants[0].id}/media`);
  expect(ownMediaResponse.ok()).toBeTruthy();
  expect((await ownMediaResponse.json()).media, "common cover is not duplicated as an owned variant photo").toEqual([]);
  const image = await context.request.get(media[0].url);
  expect(image.ok()).toBeTruthy();
  expect(image.headers()["content-type"]).toContain("image/");
  const endpoint = `/api/v1/admin/products/${product.id}/editor-plan`;
  await expect.poll(async () => (await (await context.request.get(endpoint)).json()).publications.find((p: {channel:string}) => p.channel === "saby")?.status, {timeout: 20000}).toBe("simulated");
  const plan = await (await context.request.get(endpoint)).json();
  expect(plan.publicationMode).toBe("mock");
  expect(plan.config.variants[0].saby).toMatchObject({mode:"create",category:refs.categories[0],folder:refs.folders[0]});
  expect(plan.publications).toHaveLength(1);
  expect(plan.publications[0].message).toContain("Рабочий СБИС не изменялся");
  expect(plan.barcodes[String(variants[0].id)]).toMatch(/^200\d{10}$/);
  const after = await (await context.request.get(`/api/v1/admin/products/${product.id}/variants`)).json();
  const sabyLink = after.variants[0].externalIds.find((link: {provider:string;type:string;externalId:string}) => link.provider === "saby" && link.type === "id");
  expect(sabyLink?.externalId).toMatch(/^STG-EDITOR-200/);
  await page.reload();
  await expect(page.locator(".editor-page").getByLabel("Название товара *", { exact: true })).toHaveValue(name);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
});

test("staging access stays open across guest 401 responses and reloads", async ({
  page,
  context,
}) => {
  await context.clearCookies();
  await page.goto("/login");
  await expect(
    page.getByRole("heading", { name: "Вход на стейджинг", exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Пароль", { exact: true })
    .fill(process.env.STAGING_ACCESS_TOKEN!);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  for (let attempt = 0; attempt < 3; attempt++) {
    await expect(page).toHaveURL(/\/login$/);
    const me = await context.request.get("/api/v1/auth/me");
    expect(me.status()).toBe(401);
    expect(me.headers()["www-authenticate"]).toBeUndefined();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Вход на стейджинг", exact: true }),
    ).toHaveCount(0);
  }
});

test("new customer can register without a real call and gets no admin access", async ({
  page,
  context,
}) => {
  await context.clearCookies({ name: "ficusin_session" });
  await page.goto("/register");
  const phone = `+79993${Date.now().toString().slice(-6)}`;
  await page.getByLabel("Фамилия", { exact: true }).fill("Регистрация");
  await page.getByLabel("Имя", { exact: true }).fill("Тест");
  await page.getByLabel("Телефон", { exact: true }).fill(phone);
  await page
    .getByLabel("Email", { exact: true })
    .fill(`test-${phone.slice(1)}@example.invalid`);
  await page.locator('input[name="consent"]').check();
  await page
    .getByRole("button", { name: "Зарегистрироваться", exact: true })
    .click();
  await expect(page).toHaveURL(/\/account(?:\?|$)/);
  expect((await context.request.get("/api/v1/auth/me")).status()).toBe(200);
  expect((await context.request.get("/api/v1/admin/products")).status()).toBe(
    403,
  );
});
