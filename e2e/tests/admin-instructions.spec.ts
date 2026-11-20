import { expect, test, type Page } from "@playwright/test";
import { mockApi, owner, horizontalOverflow } from "./helpers";
import content from "../../frontend/src/instructions/content.json";
import AxeBuilder from "@axe-core/playwright";

async function setup(page: Page) {
  await mockApi(page, owner);
  await page.route("**/api/v1/admin/dashboard", (route) =>
    route.fulfill({
      json: {
        user: { fullName: "Сотрудник" },
        role: "manager",
        permissions: ["dashboard.read", "products.read", "orders.read"],
        dashboard: {
          products: 0,
          variants: 0,
          orders: 0,
          customers: 0,
          wholesalePending: 0,
          lastSync: null,
          recentOrders: [],
        },
      },
    }),
  );
}

test("@desktop @phone instructions are available to staff, searchable and preserve navigation", async ({
  page,
}) => {
  await setup(page);
  await page.goto("/admin?section=instructions");
  const instructions = page.getByRole("region", {
    name: "Инструкции",
    exact: true,
  });
  await expect(
    instructions.getByRole("heading", { name: "Инструкции", exact: true }),
  ).toBeVisible();
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(30);
  await instructions
    .getByRole("searchbox", { name: "Поиск по инструкциям" })
    .fill("возврат озон");
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(1);
  await instructions
    .getByRole("link", { name: /Посмотреть и принять возвраты/ })
    .click();
  await expect(page).toHaveURL(/guide=ozon-returns/);
  await expect(
    page.getByRole("heading", {
      name: "Посмотреть и принять возвраты",
      exact: true,
    }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator(".instruction-image-button img")
        .first()
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(1200);
  await page.getByRole("button", { name: /^Увеличить шаг 1/ }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await page
    .getByRole("button", { name: "Закрыть изображение" })
    .press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const downloadPromise = page.waitForEvent("download");
  await page
    .getByRole("link", { name: "Скачать PNG", exact: true })
    .first()
    .click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("ozon-returns-1.png");
  await page.goBack();
  await expect(instructions.getByRole("searchbox")).toHaveValue("возврат озон");
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(1);
  await page.reload();
  await expect(instructions.getByRole("searchbox")).toHaveValue("возврат озон");
  await instructions.getByRole("searchbox").fill("ничего-не-найдётся");
  await expect(
    instructions.getByText("По этому запросу ничего не найдено"),
  ).toBeVisible();
  await instructions
    .getByRole("button", { name: "Сбросить поиск и фильтр" })
    .click();
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(30);
  await instructions.getByRole("button", { name: "СДЭК", exact: true }).click();
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(2);
  await expect(page).toHaveURL(/service=cdek/);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone instruction image failures retain the step and can be retried", async ({
  page,
}) => {
  await setup(page);
  await page.addInitScript(() => {
    window.print = () => {
      document.documentElement.dataset.printed = "yes";
    };
  });
  let fail = true;
  await page.route("**/assets/instructions/v1/saby-sale/1.png", (route) =>
    fail ? route.abort() : route.continue(),
  );
  await page.goto("/admin?section=instructions&guide=saby-sale");
  await expect(
    page.getByText("Не удалось загрузить картинку. Текст шага доступен ниже."),
  ).toBeVisible();
  await expect(page.locator("figcaption").first()).toContainText(
    "Продажу оформляют на рабочем месте кассира",
  );
  await page.getByRole("button", { name: "Печать / PDF" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Не удалось подготовить все картинки к печати",
  );
  await expect(page.locator("html")).not.toHaveAttribute("data-printed", "yes");
  fail = false;
  await page.getByRole("button", { name: "Загрузить снова" }).click();
  await expect
    .poll(() =>
      page
        .locator(".instruction-image-button img")
        .first()
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(1200);
  await page.getByRole("button", { name: "Печать / PDF" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-printed", "yes");
});

test("@desktop guide deep links, PNG assets and accessibility are valid", async ({
  page,
}) => {
  await setup(page);
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (
      request.url().includes("/api/") &&
      !["GET", "HEAD"].includes(request.method())
    )
      mutations.push(request.url());
  });
  for (const guide of content.guides) {
    await page.goto(`/admin?section=instructions&guide=${guide.id}`);
    await expect(
      page.getByRole("heading", { level: 2, name: guide.title, exact: true }),
    ).toBeVisible();
    for (const [index] of guide.steps.entries()) {
      const response = await page.request.get(
        `/assets/instructions/v1/${guide.id}/${index + 1}.png`,
      );
      expect(response.ok()).toBe(true);
      expect((await response.body()).subarray(0, 8).toString("hex")).toBe(
        "89504e470d0a1a0a",
      );
    }
  }
  expect(mutations).toEqual([]);
  const results = await new AxeBuilder({ page })
    .include(".instructions")
    .analyze();
  expect(results.violations).toEqual([]);
  await page.goto("/admin?section=instructions&guide=unknown");
  await expect(
    page.getByText("Инструкция не найдена. Выберите её из списка."),
  ).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).include(".instructions").analyze())
      .violations,
  ).toEqual([]);
});

test("@desktop @phone instruction layouts", async ({ page }, testInfo) => {
  await setup(page);
  await page.goto("/admin?section=instructions");
  await expect(page.locator(".instruction-list > a")).toHaveCount(30);
  await page.screenshot({
    path: testInfo.outputPath("instructions-index.png"),
    fullPage: true,
  });
  await page.goto("/admin?section=instructions&guide=saby-writeoff");
  await expect
    .poll(() =>
      page
        .locator(".instruction-image-button img")
        .first()
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(1200);
  await page.screenshot({
    path: testInfo.outputPath("instructions-guide.png"),
    fullPage: false,
  });
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});
