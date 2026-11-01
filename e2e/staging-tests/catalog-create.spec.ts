import { expect, test } from "@playwright/test";

test("real staging: draft, dimensions, seasonal boxes and outbound isolation", async ({
  page,
  context,
  request,
}, testInfo) => {
  const health = await request.get("/api/v1/ready");
  expect(health.headers()["x-ficusin-environment"]).toBe("staging");
  await page.goto("/admin?section=products");
  await page
    .getByRole("button", { name: "Добавить товар", exact: true })
    .click();
  let dialog = page.getByRole("dialog");
  const name = `Проверка стейджинга ${testInfo.project.name} ${Date.now()}`;
  await dialog.getByLabel("Название товара", { exact: true }).fill(name);
  await dialog.getByLabel("Диаметр горшка, см", { exact: true }).fill("11.3");
  await dialog.getByLabel("Высота растения, см", { exact: true }).fill("40");
  await dialog.getByLabel("Цена на сайте, ₽", { exact: true }).fill("1490.99");
  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/admin/products",
  );
  await dialog
    .getByRole("button", { name: "Создать карточку", exact: true })
    .click();
  const response = await createdResponse;
  expect(response.ok()).toBeTruthy();
  const { product } = await response.json();
  expect(product.status).toBe("draft");
  dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("button", { name: "Размеры и цены", exact: true }),
  ).toBeVisible();
  await dialog
    .getByRole("button", { name: "Размеры и цены", exact: true })
    .click();
  await dialog.getByLabel("Длина упаковки, см", { exact: true }).fill("40");
  await dialog.getByLabel("Ширина упаковки, см", { exact: true }).fill("15");
  await dialog.getByLabel("Высота упаковки, см", { exact: true }).fill("15");
  await dialog
    .getByLabel("Длина зимней упаковки, см", { exact: true })
    .fill("40");
  await dialog
    .getByLabel("Ширина зимней упаковки, см", { exact: true })
    .fill("20");
  await dialog
    .getByLabel("Высота зимней упаковки, см", { exact: true })
    .fill("20");
  await dialog
    .getByRole("button", { name: "Сохранить изменения", exact: true })
    .click();
  await expect(dialog.getByRole("status")).toContainText(
    "Товар и размеры сохранены",
  );
  await page.reload();
  dialog = page.getByRole("dialog");
  await expect(
    dialog.getByLabel("Ширина упаковки, см", { exact: true }),
  ).toHaveValue("15");
  await expect(
    dialog.getByLabel("Ширина зимней упаковки, см", { exact: true }),
  ).toHaveValue("20");
  const panel = dialog.getByRole("region", { name: /^СБИС: артикул / });
  await expect(panel).toContainText("Тестовый контур.");
  await expect(
    panel.getByRole("button", { name: "Создать в СБИС (тест)", exact: true }),
  ).toBeEnabled();
  await expect(panel).toContainText("11.3");
  await expect(panel).toContainText("40");
  const variantsResponse = await context.request.get(
    `/api/v1/admin/products/${product.id}/variants`,
  );
  expect(variantsResponse.ok()).toBeTruthy();
  const { variants } = await variantsResponse.json();
  expect(variants).toHaveLength(1);
  expect(variants[0].attributes).toMatchObject({
    pot_diameter_cm: 11.3,
    height_cm: 40,
    package_length_cm: 40,
    package_width_cm: 15,
    package_height_cm: 15,
    package_winter_length_cm: 40,
    package_winter_width_cm: 20,
    package_winter_height_cm: 20,
  });
  expect(Math.round(variants[0].price * 100)).toBe(149099);
  const endpoint = `/api/v1/admin/variants/${variants[0].id}/saby-export`;
  const before = await (await context.request.get(endpoint)).json();
  expect(before.configured).toBe(true);
  expect(before.simulation).toBe(true);
  await panel
    .getByRole("button", { name: "Создать в СБИС (тест)", exact: true })
    .click();
  await expect(panel).toContainText("Тестовый прогон завершён");
  const verified = await (await context.request.get(endpoint)).json();
  expect(verified.state).toBe("synced");
  expect(verified.snapshot.sabyId).toMatch(/^STG-/);
  expect(verified.dirty).toBe(false);
  const repeated = await context.request.post(endpoint, {
    data: { fingerprint: verified.fingerprint, checkOnly: false },
    headers: { Origin: process.env.FICUSIN_BASE_URL! },
  });
  expect(repeated.status()).toBe(200);
  expect((await repeated.json()).snapshot.sabyId).toBe(
    verified.snapshot.sabyId,
  );
  await expect(
    panel.getByRole("button", { name: "Обновить в СБИС (тест)", exact: true }),
  ).toBeDisabled();
  await panel.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: testInfo.outputPath("real-staging-catalog.png"),
    fullPage: false,
  });
  expect(
    await dialog.evaluate(
      (element) => element.scrollWidth <= element.clientWidth + 1,
    ),
  ).toBeTruthy();
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
