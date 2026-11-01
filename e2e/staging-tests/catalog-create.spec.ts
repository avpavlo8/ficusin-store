import { createHmac } from "node:crypto";
import { expect, test } from "@playwright/test";

test("real staging: draft, dimensions, seasonal boxes and outbound isolation", async ({
  page,
  context,
  request,
}, testInfo) => {
  const health = await request.get("/api/v1/ready");
  expect(health.headers()["x-ficusin-environment"]).toBe("staging");
  const session = createHmac("sha256", process.env.STAGING_ACCESS_TOKEN!)
    .update("ficusin-staging-owner-session")
    .digest("hex");
  await context.addCookies([
    {
      name: "ficusin_session",
      value: session,
      url: process.env.FICUSIN_BASE_URL!,
      secure: true,
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
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
  await expect(panel).toContainText("Подключение СБИС не настроено");
  await expect(
    panel.getByRole("button", { name: "Создать в СБИС", exact: true }),
  ).toBeDisabled();
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
  expect(before.configured).toBe(false);
  const blocked = await context.request.post(endpoint, {
    data: { fingerprint: before.fingerprint, checkOnly: false },
    headers: { Origin: process.env.FICUSIN_BASE_URL! },
  });
  expect(blocked.status()).toBe(400);
  expect((await blocked.json()).error).toContain(
    "подключение СБИС не настроено",
  );
  expect((await (await context.request.get(endpoint)).json()).state).toBe(
    "idle",
  );
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
