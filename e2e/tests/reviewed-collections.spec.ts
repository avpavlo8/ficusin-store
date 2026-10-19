import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

const titles = ["На рабочий стол", "Цитрусы и плодовые", "Безопасно для питомцев", "Неприхотливые", "Переносят полутень", "Редкий полив", "Цветущие растения", "Для солнечного окна", "Ампельные — для полок и подвесов", "Для ванной комнаты", "Крупные — от 80 см", "Небольшие — до 40 см", "Бонсай без зимовки", "В подарок", "Необычные растения", "Пёстрые листья", "Зелёная вертикаль", "Деревце для дома", "Бонсай"];
const small = { id: "1", sku: "small-40", name: "Арека", latin: "Chrysalidocarpus lutescens", category: "Растения", categoryId: 4, catalogSection: "plants", size: "D10", heightCm: 40, price: 1490, stock: 2, image: "/assets/hero-monstera.webp", careLevel: "medium", collections: ["tall"], filterAttributes: [] };
const tall = { ...small, sku: "tall-150", size: "D24", heightCm: 150, price: 8990 };

test("@desktop @phone 19 подборок сохраняют порядок и скрывают старые только с главной", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [
    ...titles.map((title, index) => ({ slug: `reviewed-${index}`, title, note: "Условия подбора", count: 1, coverUrl: "/assets/collections/2026-10-19/bonsai.webp", coverPosition: "center 30%", showOnHome: true })),
    { slug: "office", title: "Для офиса", count: 1, showOnHome: false },
  ] } }));
  await page.goto("/");
  await expect(page.locator(".preset")).toHaveCount(19);
  await expect(page.locator(".preset-title")).toHaveText(titles);
  await expect(page.locator(".preset[href='/collections/office']")).toHaveCount(0);
  await expect(page.locator(".preset-cover").first()).toHaveCSS("object-position", "50% 30%");
  await expect(page.locator(".preset-cover").first()).toHaveCSS("object-fit", "cover");
  await expect(page.locator(".preset").first()).not.toHaveAttribute("title");

  await expect(page.locator(".preset-cover").last()).toHaveAttribute("loading", "lazy");
  await expect.poll(() => page.locator(".preset-cover").first().evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await page.goto("/collections/office");
  await expect(page.getByRole("heading", { name: "Для офиса", exact: true }).first()).toBeVisible();
});

test("@desktop @phone подборка по высоте сохраняет подходящий размер, цену и SKU в ссылке", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [{ slug: "tall", title: "Крупные — от 80 см", note: "Высота выбранного варианта", count: 1, selectsVariant: true }] } }));
  await page.context().route("**/api/v1/catalog*", route => route.fulfill({ json: { products: [new URL(route.request().url()).searchParams.get("collection") === "tall" ? tall : small] } }));
  await page.goto("/collections/tall");
  const card = page.locator(".storefront-card");
  await expect(card).toHaveCount(1);
  await expect(card).toContainText("150 см");
  await expect(card).toContainText("D24");
  await expect(card.locator(".storefront-price")).toContainText(/8\s?990/);
  await expect(card.locator(".storefront-name")).toHaveAttribute("href", "/product/1?sku=tall-150");
  const saved = page.waitForRequest(request => request.url().endsWith("/api/v1/cart") && request.method() === "PUT");
  await card.getByRole("button", { name: "В корзину", exact: true }).click();
  expect((await saved).postDataJSON().items).toEqual({ "tall-150": 1 });

});

test("@desktop ошибка загрузки размера не подменяет его маленьким вариантом", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [{ slug: "tall", title: "Крупные", count: 1, selectsVariant: true }] } }));
  await page.context().route("**/api/v1/catalog*", route => new URL(route.request().url()).searchParams.has("collection") ? route.fulfill({ status: 503, json: { error: "Unavailable" } }) : route.fulfill({ json: { products: [small] } }));
  await page.goto("/collections/tall");
  await expect(page.getByRole("alert")).toContainText("Не удалось загрузить размеры подборки");
  await expect(page.locator(".storefront-card")).toHaveCount(0);
  await expect(page.getByText("Здесь пока пусто")).toHaveCount(0);
});

const reviewedCovers = [
  {
    "slug": "easy",
    "title": "Неприхотливые",
    "count": 60,
    "coverUrl": "/assets/redesign/collection-easy-4k.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "dark",
    "title": "Переносят полутень",
    "count": 32,
    "coverUrl": "/assets/collections/2026-10-19/partial-shade.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "rare",
    "title": "Редкий полив",
    "count": 22,
    "coverUrl": "/assets/redesign/filters/rare-water-wall-v2.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "pets",
    "title": "Безопасно для питомцев",
    "count": 33,
    "coverUrl": "/assets/collections/2026-10-19/pet-safe.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "flowering",
    "title": "Цветущие растения",
    "count": 34,
    "coverUrl": "/assets/collections/2026-10-19/flowering.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "sunny-window",
    "title": "Для солнечного окна",
    "count": 37,
    "coverUrl": "/assets/collections/2026-10-19/sunny-window.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "trailing",
    "title": "Ампельные — для полок и подвесов",
    "count": 20,
    "coverUrl": "/assets/collections/2026-10-19/trailing.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "bathroom",
    "title": "Для ванной комнаты",
    "count": 76,
    "coverUrl": "/assets/redesign/filters/bathroom-wall-v2.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "tall",
    "title": "Крупные — от 80 см",
    "count": 2,
    "coverUrl": "/assets/redesign/filters/tall-wall-v2.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "compact",
    "title": "Небольшие — до 40 см",
    "count": 6,
    "coverUrl": "/assets/redesign/filters/compact-wall-v2.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "indoor-bonsai",
    "title": "Бонсай без зимовки",
    "count": 6,
    "coverUrl": "/assets/collections/2026-10-19/indoor-bonsai.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "desk",
    "title": "На рабочий стол",
    "count": 13,
    "coverUrl": "/assets/collections/2026-10-19/desk.webp",
    "coverPosition": "center",
    "showOnHome": true
  },
  {
    "slug": "gifts",
    "title": "В подарок",
    "count": 16,
    "coverUrl": "/assets/collections/2026-10-19/gifts.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "unusual",
    "title": "Необычные растения",
    "count": 20,
    "coverUrl": "/assets/collections/2026-10-19/unusual.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "variegated",
    "title": "Пёстрые листья",
    "count": 50,
    "coverUrl": "/assets/collections/2026-10-19/variegated.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "vertical",
    "title": "Зелёная вертикаль",
    "count": 11,
    "coverUrl": "/assets/collections/2026-10-19/vertical.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "home-trees",
    "title": "Деревце для дома",
    "count": 47,
    "coverUrl": "/assets/collections/2026-10-19/home-trees.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "fruiting",
    "title": "Цитрусы и плодовые",
    "count": 21,
    "coverUrl": "/assets/collections/2026-10-19/fruiting.webp",
    "coverPosition": "center top",
    "showOnHome": true
  },
  {
    "slug": "bonsai",
    "title": "Бонсай",
    "count": 34,
    "coverUrl": "/assets/collections/2026-10-19/bonsai.webp",
    "coverPosition": "center 30%",
    "showOnHome": true
  }
];

test("@desktop @phone экран выбирает свою обложку и сохраняет пользовательскую из PIM", async ({ page }) => {
  await mockApi(page);
  let custom = false;
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: reviewedCovers.map(c => custom && c.slug === "easy" ? { ...c, coverUrl: "/assets/hero-monstera.webp" } : c) } }));
  await page.goto("/");
  const desktop = (page.viewportSize()?.width ?? 0) >= 851;
  for (const collection of reviewedCovers) {
    const card = page.locator(`.preset[href='/collections/${collection.slug}']`);
    await card.scrollIntoViewIfNeeded();
    const cover = card.locator("img");
    const expected = desktop ? `/assets/collections/2026-10-19-panoramic/${collection.slug}.webp` : collection.coverUrl;
    await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0 ? new URL(img.currentSrc).pathname : "")).toBe(expected);
    if (desktop) await expect(cover).toHaveCSS("object-position", "100% 50%");
  }
  custom = true;
  await page.reload();
  const customCover = page.locator(".preset[href='/collections/easy'] img");
  await customCover.scrollIntoViewIfNeeded();
  await expect.poll(() => customCover.evaluate((img: HTMLImageElement) => new URL(img.currentSrc).pathname)).toBe("/assets/hero-monstera.webp");
  await expect(page.locator(".preset[href='/collections/easy'] source")).toHaveCount(0);
});

test("@desktop смена ширины переключает обложку без изменения мобильного кадрирования", async ({ page }) => {
  await mockApi(page);
  await page.context().route("**/api/v1/collections", route => route.fulfill({ json: { collections: [reviewedCovers.find(c => c.slug === "fruiting")] } }));
  await page.goto("/");
  for (const width of [390, 768, 850, 851, 1024, 1440, 2560, 390]) {
    await page.setViewportSize({ width, height: 1100 });
    const cover = page.locator(".preset-cover");
    await cover.scrollIntoViewIfNeeded();
    const expected = width >= 851 ? "/assets/collections/2026-10-19-panoramic/fruiting.webp" : "/assets/collections/2026-10-19/fruiting.webp";
    await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.complete ? new URL(img.currentSrc).pathname : "")).toBe(expected);
    await expect(cover).toHaveCSS("object-position", width >= 851 ? "100% 50%" : "50% 0%");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  }
});
