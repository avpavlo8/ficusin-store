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
  await expect(instructions.locator(".instruction-list > a")).toHaveCount(1);
  const directory = instructions.getByRole("navigation", {
    name: "Сервисы инструкций",
  });
  await expect(directory.getByRole("link")).toHaveCount(9);
  expect(
    await directory
      .getByRole("link")
      .evaluateAll((links) =>
        links.map((link) => link.getAttribute("aria-label")),
      ),
  ).toEqual([
    "Saby",
    "Wildberries",
    "Ozon",
    "СДЕК",
    "Сайт",
    "Flowwow",
    "Avito",
    "5Post",
    "Касса",
  ]);
  await expect
    .poll(() =>
      directory
        .locator("img")
        .evaluateAll((images: HTMLImageElement[]) =>
          images.every((image) => image.complete && image.naturalWidth > 0),
        ),
    )
    .toBe(true);

  const search = instructions.getByRole("searchbox", {
    name: "Поиск по инструкциям",
  });
  for (const service of [
    "Saby",
    "Wildberries",
    "Ozon",
    "Сайт",
    "Flowwow",
    "Avito",
    "5Post",
    "Касса",
  ]) {
    await directory.getByRole("link", { name: service, exact: true }).click();
    const section = instructions.getByRole("region", {
      name: service,
      exact: true,
    });
    await expect(
      section.getByText(`Инструкции по ${service} ещё готовятся`),
    ).toBeVisible();
    await expect(instructions.locator(".instruction-list > a")).toHaveCount(0);
    await expect(
      instructions.getByRole("region", { name: "Общие инструкции" }),
    ).toHaveCount(0);
    await instructions.getByRole("link", { name: "Все сервисы" }).click();
  }
  await directory.getByRole("link", { name: "СДЕК", exact: true }).click();
  const cdekSection = instructions.getByRole("region", {
    name: "СДЕК",
    exact: true,
  });
  await expect(cdekSection.locator(".instruction-list > a")).toHaveCount(1);
  await cdekSection
    .getByRole("link", { name: /Создать новый заказ СДЭК/ })
    .click();
  await expect(page).toHaveURL(/guide=cdek-create/);
  await page
    .getByRole("link", { name: "Инструкции СДЭК", exact: true })
    .click();
  await expect(page).toHaveURL(/section=instructions&service=cdek$/);
  await cdekSection
    .getByRole("link", { name: /Создать новый заказ СДЭК/ })
    .click();
  await page.getByRole("link", { name: "На главную", exact: true }).click();
  await expect(page).toHaveURL(/section=instructions$/);

  await directory.getByRole("link", { name: "Ozon", exact: true }).click();
  await search.fill("пенопласт");
  const matches = instructions.getByRole("region", {
    name: "Результаты поиска инструкций",
  });
  const foamGuide = matches.getByRole("link", { name: /Как резать пенопласт/ });
  await expect(matches.locator(".instruction-list > a")).toHaveCount(1);
  await expect(foamGuide).toContainText("12 карт раскроя");
  await foamGuide.click();
  await expect(page).toHaveURL(/guide=general-cut-foam/);
  const picker = page.getByRole("region", { name: "Выберите коробку" });
  await expect(picker.locator("summary")).toHaveCount(12);
  await expect(picker.locator("details[open]")).toHaveCount(0);
  await expect(page.locator(".instruction-step")).toHaveCount(0);
  await expect(page.locator(".instruction-pages > *")).toHaveCount(1);
  await picker.getByText("20 × 10 × 10 см", { exact: true }).click();
  const firstLayout = picker.getByRole("button", {
    name: /Увеличить раскрой для коробки 20 × 10 × 10/,
  });
  await expect
    .poll(() =>
      firstLayout
        .locator("img")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(3120);
  await firstLayout.click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page
    .getByRole("button", { name: "Закрыть изображение" })
    .press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const downloadPromise = page.waitForEvent("download");
  await picker.getByRole("link", { name: "Скачать раскрой PNG" }).click();
  expect((await downloadPromise).suggestedFilename()).toBe("box-20x10x10.png");
  await picker.getByText("20 × 20 × 20 см", { exact: true }).click();
  await expect(picker.locator("details[open]")).toHaveCount(1);
  await expect(firstLayout).toBeHidden();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await page.goBack();
  await expect(search).toHaveValue("пенопласт");
  await page.reload();
  await expect(search).toHaveValue("пенопласт");
  await search.fill("возврат озон");
  await expect(
    instructions.getByText("По этому запросу ничего не найдено"),
  ).toBeVisible();
  await instructions
    .getByRole("button", { name: "Сбросить поиск и фильтр" })
    .click();
  await expect(directory.getByRole("link")).toHaveCount(9);
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
  await page.route(
    "**/assets/instructions/v1/cdek-create/new-order-marker-v3.png",
    (route) => (fail ? route.abort() : route.continue()),
  );
  await page.goto("/admin?section=instructions&guide=cdek-create");
  await expect(page.getByText("Не удалось загрузить картинку.")).toBeVisible();
  await expect(
    page.locator(".instruction-step-description").first(),
  ).toContainText("На странице «Список заказов»");
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
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: "Печать / PDF" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-printed", "yes");
});

test("@desktop guide deep links, PNG assets and accessibility are valid", async ({
  page,
}) => {
  test.setTimeout(60_000);
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
    const files = [
      ...(guide.boxLayouts?.map((box) => box.imageFile) ?? []),
      ...guide.steps.flatMap((step) => [
        step.imageFile,
        step.example?.imageFile,
      ]),
    ].filter(Boolean);
    for (const name of files) {
      const response = await page.request.get(
        `/assets/instructions/v1/${guide.id}/${name}`,
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
  await expect(page.locator(".instruction-list > a")).toHaveCount(1);
  await page.screenshot({
    path: testInfo.outputPath("instructions-index.png"),
    fullPage: true,
  });
  await page.goto("/admin?section=instructions&guide=cdek-create");
  await expect
    .poll(() =>
      page
        .locator(".instruction-image-button img")
        .first()
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  await page.screenshot({
    path: testInfo.outputPath("instructions-guide.png"),
    fullPage: false,
  });
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});
