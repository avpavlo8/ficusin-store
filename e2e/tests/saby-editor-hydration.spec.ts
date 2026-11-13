import { expect, test, type Page } from "@playwright/test";
import { owner } from "./helpers";

async function setup(page: Page, holdPlan = false, holdVariants = false) {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  if (!holdPlan) release();
  let releaseVariants!: () => void;
  const variantsGate = new Promise<void>((resolve) => {
    releaseVariants = resolve;
  });
  if (!holdVariants) releaseVariants();
  const empty = () => ({
    category: { id: "", name: "" },
    folder: { id: "", name: "" },
    mode: "create",
    existingCode: "",
  });
  const category = { id: "c-first", name: "Категория первого варианта" };
  const folder = { id: "f-first", name: "Раздел первого варианта" };
  const ownCategory = { id: "c-local", name: "Категория из черновика" };
  const product = {
    id: 1,
    slug: "test-saby-hydration",
    name: "Тестовое растение",
    latinName: "Plant",
    description: "Описание",
    careInstructions: "",
    status: "draft",
    catalogSection: "plants",
    categoryId: 1,
    price: 1490,
    stock: 0,
    sku: "10",
    image: "",
    variantLabel: "D11",
    wholesaleMinQty: 1,
    attributes: {},
    externalIds: [],
    sabyFields: [],
    overrideFields: [],
    importantWarnings: [],
    variantCount: 2,
    updatedAt: "2026-11-13T00:00:00Z",
    passport: { faq: [] },
  };
  const variants = [10, 11].map((id, index) => ({
    id,
    productId: 1,
    sku: String(id),
    label: index ? "D17" : "D11",
    price: 1490,
    stock: 0,
    wholesaleMinQty: 1,
    active: true,
    archived: false,
    attributes: { pot_diameter_cm: index ? 17 : 11, height_cm: 40 },
    externalIds: [
      { provider: "saby", type: "id", externalId: index ? "1102" : "1101" },
    ],
    images: [],
    managedFields: [],
  }));
  const mutations: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    const send = (body: unknown, status = 200) =>
      route.fulfill({ status, json: body });
    if (
      !["GET", "HEAD"].includes(request.method()) &&
      !path.endsWith("/pricing/preview")
    ) {
      mutations.push(`${request.method()} ${path}`);
      return send({ error: "Only local draft edits are expected" }, 409);
    }
    if (path === "/api/v1/auth/me")
      return send({ user: { ...owner.user, adminRole: "owner" } });
    if (path === "/api/v1/admin/dashboard")
      return send({
        user: { fullName: "Владелец" },
        role: "owner",
        permissions: [
          "dashboard.read",
          "products.read",
          "products.edit",
          "products.manage",
        ],
        dashboard: {
          products: 1,
          variants: 2,
          orders: 0,
          customers: 0,
          wholesalePending: 0,
          lastSync: null,
          recentOrders: [],
        },
      });
    if (path === "/api/v1/admin/products") return send({ products: [product] });
    if (path === "/api/v1/admin/categories")
      return send({
        categories: [
          {
            id: 1,
            name: "Растения",
            slug: "plants",
            parentId: null,
            sortOrder: 1,
          },
        ],
      });
    if (/\/categories\/\d+\/attributes$/.test(path))
      return send({ attributes: [] });
    if (path === "/api/v1/admin/products/1/variants") {
      await variantsGate;
      return send({ variants });
    }
    if (path.endsWith("/media")) return send({ media: [] });
    if (path === "/api/v1/admin/catalog-editor/references")
      return send({ categories: [category, ownCategory], folders: [folder] });
    if (path === "/api/v1/admin/products/1/editor-plan") {
      await gate;
      return send({
        config: {
          variants: variants.map((v) => ({ variantId: v.id, saby: empty() })),
        },
        publications: [],
        links: {},
        publicationMode: "disabled",
        barcodes: {},
        sabyLinks: {
          "10": {
            id: "1101",
            code: "X-FIRST",
            category,
            folder,
            mirrorFound: true,
          },
          "11": {
            id: "1102",
            code: "X-SECOND",
            category: { id: "", name: "" },
            folder: { id: "", name: "" },
            mirrorFound: false,
          },
        },
      });
    }
    if (path === "/api/v1/admin/settings")
      return send({ values: { "cdek.packaging_season": "summer" } });
    if (path.endsWith("/pricing/preview"))
      return send({
        quotes: {
          wb: { missing: ["Габариты"] },
          ozon: { missing: ["Габариты"] },
        },
      });
    if (path === "/api/v1/collections") return send({ collections: [] });
    return send({});
  });
  await page.goto("/admin?section=products&product=1");
  const editor = page.locator(".editor-page");
  if (holdVariants)
    await expect(
      editor.getByText("Загружаем данные связи СБИС…", { exact: true }),
    ).toBeVisible();
  else await expect(editor.getByRole("button", { name: /^D11/ })).toBeVisible();
  return { editor, release, releaseVariants, mutations };
}

test("@desktop existing Saby controls wait for variant identity while the rest of the draft stays editable", async ({
  page,
}) => {
  const { editor, releaseVariants, mutations } = await setup(page, false, true);
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveCount(0);
  await expect(
    editor.getByRole("button", { name: "Создать в СБИС", exact: true }),
  ).toHaveCount(0);
  await editor
    .getByLabel("Название товара *", { exact: true })
    .fill("Черновик во время загрузки связи");
  expect(
    await page.evaluate(
      () =>
        JSON.parse(
          localStorage.getItem("ficusin:product-edit-draft:v1:1") || "{}",
        ).name,
    ),
  ).toBe("Черновик во время загрузки связи");
  releaseVariants();
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("X-FIRST");
  await expect(
    editor.getByRole("button", { name: "Уже создан в СБИС", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("Категория первого варианта");
  await expect(
    editor.getByLabel("Название товара *", { exact: true }),
  ).toHaveValue("Черновик во время загрузки связи");
  expect(mutations).toEqual([]);
});

test("@desktop @phone Saby metadata resolves per variant and missing mirror never substitutes the internal ID", async ({
  page,
}) => {
  const { editor, mutations } = await setup(page);
  await expect(
    editor.getByRole("button", { name: "Уже создан в СБИС", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("X-FIRST");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("Категория первого варианта");
  await expect(
    editor.getByRole("combobox", { name: "Раздел в СБИС", exact: true }),
  ).toHaveValue("Раздел первого варианта");
  await expect(
    editor.getByText("Связь СБИС: X-FIRST", { exact: true }),
  ).toBeVisible();
  await editor.getByRole("button", { name: /^D17/ }).click();
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("X-SECOND");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("");
  await expect(
    editor.getByRole("combobox", { name: "Раздел в СБИС", exact: true }),
  ).toHaveValue("");
  await expect(
    editor.getByText("Связь СБИС: X-SECOND", { exact: true }),
  ).toBeVisible();
  await expect(editor.getByText(/Связь СБИС: 110[12]/)).toHaveCount(0);
  expect(mutations).toEqual([]);
});

test("@desktop @phone late Saby metadata preserves explicit create, edited code and cleared code across reload", async ({
  page,
}) => {
  const { editor, release, mutations } = await setup(page, true);
  await editor.getByLabel("Код товара в СБИС", { exact: true }).fill("X-USER");
  await editor.getByLabel("Код товара в СБИС", { exact: true }).fill("");
  await editor
    .getByRole("button", { name: "Создать в СБИС", exact: true })
    .click();
  await editor
    .getByRole("combobox", { name: "Категория в СБИС", exact: true })
    .click();
  await editor
    .locator(".editor-reference-options")
    .getByRole("button", { name: "Категория из черновика", exact: true })
    .click();
  await editor.getByRole("button", { name: /^D17/ }).click();
  await editor
    .getByLabel("Код товара в СБИС", { exact: true })
    .fill("X-REPLACEMENT");
  release();
  await expect(
    editor.getByText("Связь СБИС: X-SECOND", { exact: true }),
  ).toBeVisible();
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("X-REPLACEMENT");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("");
  await expect(
    editor.getByRole("combobox", { name: "Раздел в СБИС", exact: true }),
  ).toHaveValue("");
  await editor.getByRole("button", { name: /^D11/ }).click();
  await expect(
    editor.getByRole("button", { name: "Создать в СБИС", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("Категория из черновика");
  await editor
    .getByRole("button", { name: "Уже создан в СБИС", exact: true })
    .click();
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("");
  await page.reload();
  await expect(
    editor.getByText("Связь СБИС: X-FIRST", { exact: true }),
  ).toBeVisible();
  await expect(
    editor.getByLabel("Код товара в СБИС", { exact: true }),
  ).toHaveValue("");
  await expect(
    editor.getByRole("combobox", { name: "Категория в СБИС", exact: true }),
  ).toHaveValue("Категория из черновика");
  expect(mutations).toEqual([]);
});
