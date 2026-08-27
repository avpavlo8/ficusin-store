import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

// Витрина — это главная страница магазина, и почти всё, что покупатель делает
// до корзины, происходит здесь. Раньше её не проверял никто.

test("@desktop главная использует компактную товарную структуру", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await expect(page.locator(".home-hero-visual img")).toHaveAttribute("src", /home-hero-4k\.webp/);
  await expect(page.getByRole("button", { name: "Видео о Фикусин" })).toHaveCount(0);
  await expect(page.locator(".home-stamp")).toHaveCount(1);
  await expect(page.locator(".home-collections")).toHaveCount(0);
  await expect(page.locator(".storefront-preset-carousel .preset")).toHaveCount(9);
  await expect(page.getByRole("button", { name: "Следующие подборки" })).toBeVisible();
  await expect(page.locator(".store-footer-menu")).toHaveCount(0);
  await expect(page.locator(".store-footer-connect")).toContainText("Давайте найдём");
  await expect(page.locator(".store-footer-socials a")).toHaveCount(4);
  await expect(page.locator(".store-footer-legal .store-footer-social-block")).toContainText("Мы в соцсетях");
  await expect(page.locator(".store-footer-connect .store-footer-social-block")).toHaveCount(0);
  const footerButtonLabels = await page.locator(".store-footer-contact-actions small").evaluateAll((labels) => labels.map((label) => ({ borderTop: getComputedStyle(label).borderTopWidth, paddingTop: getComputedStyle(label).paddingTop })));
  expect(footerButtonLabels).toEqual([{ borderTop: "0px", paddingTop: "0px" }, { borderTop: "0px", paddingTop: "0px" }]);
  const collectionRail = page.locator(".storefront-preset-carousel .storefront-presets");
  await expect(collectionRail).toHaveClass(/can-scroll-next/);
  const railGeometry = await collectionRail.evaluate((element) => {
    const cards = Array.from(element.querySelectorAll<HTMLElement>(".preset"));
    const railBox = element.getBoundingClientRect();
    const fourthBox = cards[3].getBoundingClientRect();
    return {
      overflows: element.scrollWidth > element.clientWidth,
      fourthStartsInside: fourthBox.left < railBox.right,
      fourthContinuesOutside: fourthBox.right > railBox.right,
    };
  });
  expect(railGeometry).toEqual({ overflows: true, fourthStartsInside: true, fourthContinuesOutside: true });
  await expect(page.locator(".home-catalog-toolbar")).toBeVisible();
  const headerMenus = page.locator(".header-dropdown");
  await headerMenus.first().locator(":scope > summary").click();
  await expect(headerMenus.first().getByRole("link", { name: /Растения/ })).toHaveAttribute("href", /\/catalog\//);
  await headerMenus.nth(1).locator(":scope > summary").click();
  await expect(headerMenus.first()).not.toHaveAttribute("open", "");
  await expect(headerMenus.nth(1).getByRole("link", { name: /Аглаонема/ })).toHaveAttribute("href", /\/catalog\//);
  await page.locator(".home-hero-copy").click();
  await expect(headerMenus.nth(1)).not.toHaveAttribute("open", "");
  await headerMenus.nth(2).locator(":scope > summary").click();
  await expect(headerMenus.nth(2).getByRole("link", { name: "Публичная оферта" })).toBeVisible();
  await expect(page.locator(".catalog-view-toggle")).toHaveCount(0);
  await expect(page.locator(".storefront-main").getByRole("heading", { name: "Каталог" })).toHaveCount(1);
});

// Значения характеристик хранятся кодами: low_light, easy, caution. Однажды
// они уезжали на витрину как есть, и покупатель читал «low_light» вместо
// «Полутень». Словарь один на карточку, фильтры и паспорт — проверяем его
// там, где ошибку видно первой.
test("@desktop характеристики подписаны по-русски, а не кодами", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const badges = page.locator(".storefront-card", { hasText: "Аглаонема Мария" })
    .locator(".storefront-attribute-badges span");
  await expect(badges.first()).toHaveText("Освещение: Полутень");
  await expect(badges.nth(1)).toHaveText("Сложность ухода: Лёгкий");

  // Ни один код не должен просочиться в сетку целиком.
  await expect(page.locator(".storefront-grid")).not.toContainText("low_light");
  await expect(page.locator(".storefront-grid")).not.toContainText("easy");
});

// Заголовок первого уровня — это то, что читает поисковик и произносит
// экранный диктор. Их должно быть ровно столько же, сколько страниц: одна.
test("@desktop у витрины ровно один заголовок первого уровня", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await expect(page.locator("h1")).toHaveCount(1);
  await expect(page.locator("h1")).toContainText("Растения");
});

test("@phone общий подвал остаётся устойчивым и полноширинным", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto("/");

  const footer = page.locator(".store-footer");
  await expect(footer.getByRole("link", { name: /Написать в чат/ })).toBeVisible();
  await expect.poll(async () => footer.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(390);
});

test("@desktop дерево раскрывается сразу до видов", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const tree = page.locator(".storefront-tree");
  await expect(tree.getByText("Растения", { exact: false }).first()).toBeVisible();
  // Свёрнуто по умолчанию: виды не мозолят глаза, пока их не попросили.
  await expect(tree.getByText("Аглаонема")).toHaveCount(0);

  await tree.getByText("Растения", { exact: false }).first().click();

  await expect(tree.getByText("Аглаонема")).toBeVisible();
  await expect(tree.getByText("Фикус", { exact: true })).toBeVisible();
  // Ступень с единственной веткой и без своих товаров ничего не решает —
  // её не показываем, иначе до видов пришлось бы нажимать дважды.
  await expect(tree.getByText("Комнатные растения")).toHaveCount(0);
});

test("@desktop пустые разделы остаются в дереве", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  await expect(page.locator(".storefront-tree").getByText("Кашпо и горшки")).toBeVisible();
});

test("@desktop выбор вида оставляет в сетке только его", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const tree = page.locator(".storefront-tree");
  await tree.getByText("Растения", { exact: false }).first().click();
  await tree.getByText("Аглаонема").click();

  const grid = page.locator(".storefront-grid");
  await expect(grid.getByText("Аглаонема Мария")).toBeVisible();
  await expect(grid.getByText("Фикус Бенджамина")).toHaveCount(0);
});

test("@desktop подборка фильтрует сетку, пустая не показывается", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await expect(page.getByRole("listitem", { name: /солнечное/ })).toHaveCount(0);
  await page.locator(".preset", { hasText: "Для ванной" }).click();

  const grid = page.locator(".storefront-grid");
  await expect(grid.getByText("Аглаонема Мария")).toBeVisible();
  await expect(grid.getByText("Фикус Бенджамина")).toHaveCount(0);
});

test("@desktop подборки переключаются только через канонические страницы", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await page.locator(".preset", { hasText: "Для ванной" }).click();
  await expect(page).toHaveURL(/\/collections\/bathroom$/);
  await expect(page.locator(".preset.active")).toHaveCount(1);
  await page.locator(".preset", { hasText: "Для офиса" }).click();
  await expect(page).toHaveURL(/\/collections\/office$/);
  await expect(page.locator(".preset.active")).toHaveCount(1);
  await expect(page.locator(".preset.active")).toContainText("Для офиса");
});

test("@desktop товар без остатка идёт как предзаказ", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Монстера Делициоза" });
  await expect(card).toHaveClass(/preorder/);
  await expect(card.locator(".storefront-price em")).toHaveText("Под заказ");
  await expect(card.locator(".storefront-preorder")).toHaveCount(0);
  await expect(card.getByRole("button", { name: "В корзину" })).toBeVisible();

  await page.getByLabel("Только в наличии").first().check();
  await expect(page.locator(".storefront-card", { hasText: "Монстера Делициоза" })).toHaveCount(0);
});

test("@desktop на карточке товара выбирается количество", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");

  await expect(page.locator(".pdp-quantity output")).toHaveText("1");
  await page.locator(".pdp-quantity button").last().click();
  await page.getByRole("button", { name: "В корзину" }).click();

  await expect(page.getByRole("button", { name: "В корзине — убрать" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Корзина, товаров: 2/ })).toBeVisible();
  await expect(page.getByText("Питомцы")).toBeVisible();
  await page.getByRole("button", { name: "Вопросы" }).click();
  await expect(page.locator("#questions")).toContainText("Когда пересаживать?");
  await page.getByRole("button", { name: /Отзывы/ }).click();
  await expect(page.locator("#reviews")).toContainText("Подтверждённая покупка");
});

test("@desktop PDP сохраняет коммерческую иерархию и навигацию", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");

  const purchase = page.locator(".pdp-summary");
  await expect(purchase.getByRole("heading", { level: 1 })).toHaveText("Аглаонема Мария");
  await expect(purchase.locator(".pdp-commerce-box")).toContainText("В наличии");
  await expect(purchase.getByRole("button", { name: "В корзину" })).toBeVisible();
  await expect(page.locator(".pdp-anchor-nav").getByRole("tab")).toHaveCount(5);
  await expect(page.locator(".pdp-anchor-nav")).not.toContainText("Паспорт");
  await expect(page.locator(".review-modal")).toHaveCount(0);
  await page.getByRole("radio", { name: "5 из 5" }).click();
  await expect(page.locator(".review-modal")).toBeVisible();
  await expect(page.getByLabel("Ваш отзыв")).toBeFocused();
  await expect(page.locator(".review-media-button input")).toHaveAttribute("accept", /video\/mp4/);
  await page.locator(".review-media-button input").setInputFiles([
    { name: "plant.png", mimeType: "image/png", buffer: Buffer.from("preview") },
    { name: "unboxing.mp4", mimeType: "video/mp4", buffer: Buffer.from("preview") },
  ]);
  await expect(page.locator(".review-media-preview figure")).toHaveCount(2);
  await page.route("**/api/v1/products/1/reviews", (route) => route.fulfill({ status: 201, json: { id: 17, status: "published" } }));
  await page.getByLabel("Ваш отзыв").fill("Растение приехало здоровым и хорошо упакованным.");
  await page.getByRole("button", { name: "Отправить отзыв" }).click();
  await expect(page.locator(".review-modal")).toHaveCount(0);
  await expect(page.locator(".review-submit-success")).toHaveText("Спасибо за отзыв.");
  await expect(page.getByText(/отправлен на модерацию/i)).toHaveCount(0);
});

test("@desktop вопросы открываются отдельной вкладкой", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  await page.getByRole("button", { name: "Вопросы" }).click();
  await expect(page.locator("#questions details")).toContainText("Когда пересаживать?");
});

test("@desktop ссылка на отзывы открывает содержимое вкладки", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/null-review", (route) => route.fulfill({ json: { product: {
    id: "null-review", name: "Аглаонема Мария", latin: "Aglaonema", shortDescription: "", description: "", careInstructions: "",
    images: ["/assets/product-pothos.png"], catalogSection: "plants", rating: 5, reviewsCount: 1, recommendations: [], passport: {}, importantWarnings: [], attributes: [], variants: [],
    reviews: [{ id: 1, rating: 5, text: "Отличное растение!", author: "Александр", date: "2026-08-23", verifiedPurchase: true, photos: null, media: null }],
  } } }));
  await page.goto("/product/null-review");
  await page.locator(".purchase-review-meta").click();
  await expect(page.locator("#reviews")).toBeVisible();
  await expect(page.locator("#reviews")).toContainText("Отличное растение!");
});

test("@desktop у кашпо нет растительного ухода и характеристик", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/pot", (route) => route.fulfill({ json: { product: {
    id: "pot", name: "Кашпо Арте", latin: "", shortDescription: "Керамическое кашпо", description: "", careInstructions: "ошибочный старый текст",
    images: ["/assets/product-pothos.png"], catalogSection: "pots", rating: 0, reviewsCount: 0, reviews: [], recommendations: [],
    passport: { lighting: "Рассеянный свет", faq: [{ question: "Как поливать?", answer: "Никак" }] }, importantWarnings: [],
    attributes: [{ code: "pot_material", name: "Материал", value: "Керамика", badge: true }, { code: "watering", name: "Полив", value: "moderate", badge: true }],
    variants: [{ id: 1, sku: "POT-1", label: "2,5 л", price: 990, stock: 3, wholesaleMinQty: 1, images: [], attributes: [] }],
  } } }));
  await page.goto("/product/pot");
  await expect(page.locator(".pdp-anchor-nav").getByRole("tab")).toHaveCount(3);
  await expect(page.locator(".pdp-anchor-nav")).not.toContainText("О растении");
  await expect(page.locator(".pdp-anchor-nav")).not.toContainText("Вопросы");
  await expect(page.locator(".pdp-key-characteristics")).toContainText("Материал");
  await expect(page.locator(".pdp-key-characteristics")).not.toContainText("Полив");
  await expect(page.locator("#care-guide")).toHaveCount(0);
});

test("@desktop инструкция по уходу ведёт от адаптации к персональным рекомендациям", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  await page.getByRole("tab", { name: "Уход", exact: true }).click();

  const guide = page.locator("#care-guide");
  await expect(guide).toContainText("Первые дни дома");
  await expect(guide).toContainText("Осмотрите");
  await expect(guide.getByRole("tab", { name: /Свет/ })).toHaveAttribute("aria-selected", "true");
  await guide.getByRole("tab", { name: /Полив/ }).click();
  await expect(guide.getByRole("tabpanel")).toContainText("Полив");
  await expect(guide.getByRole("tabpanel")).toContainText("После просыхания верхнего слоя");
  await expect(guide).toContainText("Спрашивают чаще всего");
  await expect(guide.getByText("Когда пересаживать?")).toBeVisible();
});

test("@desktop растение без ухода, характеристик и фото не получает пустые разделы", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/empty", (route) => route.fulfill({ json: { product: {
    id: "empty", name: "Растение без паспорта", latin: "", shortDescription: "", description: "", careInstructions: "",
    images: [], variants: [], recommendations: [], passport: {}, importantWarnings: [], rating: 0, reviewsCount: 0, reviews: [], catalogSection: "plants", attributes: [],
  } } }));
  await page.goto("/product/empty");
  await expect(page.locator(".pdp-image-placeholder")).toContainText("Фото скоро появится");
  await expect(page.getByRole("tab", { name: "Уход" })).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Вопросы" })).toHaveCount(0);
  await page.getByRole("tab", { name: "Характеристики" }).click();
  await expect(page.locator(".pdp-empty-section")).toContainText("скоро появятся");
  await page.getByRole("button", { name: "Отзывы" }).click();
  await expect(page.locator("#reviews")).toContainText("Здесь пока тихо");
  await expect(page.locator(".purchase-review-meta")).toContainText("Пока без отзывов");
});

test("@desktop вкладки PDP связаны с panel и переключаются стрелками", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  const about = page.getByRole("tab", { name: "О растении" });
  await about.focus();
  await about.press("ArrowRight");
  const characteristics = page.getByRole("tab", { name: "Характеристики" });
  await expect(characteristics).toBeFocused();
  await expect(characteristics).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", await characteristics.getAttribute("id") || "");
});

test("@desktop PDP не переполняет контрольные ширины", async ({ page }) => {
  await mockApi(page);
  for (const width of [768, 1024, 1440, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/product/1");
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), `${width}px шире экрана`).toBeLessThanOrEqual(1);
  }
});

test("@desktop фотография открывается в полноэкранной галерее", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  await page.getByRole("button", { name: "Открыть фотографию на весь экран" }).click();
  await expect(page.getByRole("dialog", { name: /Фотографии/ })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: /Фотографии/ })).toHaveCount(0);
});

test("@desktop сортировка по цене", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const sort = page.getByLabel("Сортировка");
  await sort.selectOption("cheap");
  await expect(page.locator(".storefront-name").first()).toHaveText("Кашпо Классик");

  await sort.selectOption("expensive");
  await expect(page.locator(".storefront-name").first()).toHaveText("Монстера Делициоза");
});

test("@desktop ссылка с запросом сразу показывает выдачу", async ({ page }) => {
  await mockApi(page);
  await page.goto("/?q=бенджамина");

  await expect(page.locator(".header-search input")).toHaveValue("бенджамина");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toHaveCount(0);
});

test("@desktop корзина открывается отдельной страницей", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();

  await expect(page).toHaveURL(/\/cart$/);
  const drawer = page.locator(".drawer.open");
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText("Аглаонема Мария")).toBeVisible();
  await expect(drawer.locator(".quantity span")).toHaveText("1");
});

test("@desktop корзина ведёт в отдельный пошаговый checkout", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

  await expect(page).toHaveURL(/\/checkout$/);
  const checkout = page.locator(".checkout-page-panel");
  await expect(checkout).toBeVisible();
  await expect(checkout.getByRole("heading", { name: "Оформление заказа" })).toBeVisible();
  await expect(checkout.getByLabel("Имя")).toBeVisible();
  await expect(checkout.getByLabel("Телефон")).toBeVisible();
  await expect(checkout.getByText("Способ доставки")).toBeHidden();
  await checkout.getByLabel("Имя").fill("Мария");
  await checkout.getByLabel("Телефон").fill("+7 900 123-45-67");
  await checkout.getByLabel("Email для чека").fill("maria@example.ru");
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await expect(checkout.getByText("Способ доставки")).toBeVisible();
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await expect(checkout.getByText("Способ оплаты")).toBeVisible();
  await expect(checkout.locator('input[name="consent"]')).toBeVisible();
});

test("@desktop оформление отправляет заказ с нормализованным телефоном", async ({ page }) => {
  await mockApi(page);
  let order: Record<string, unknown> | undefined;
  await page.route("**/api/v1/orders", async (route) => {
    order = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ json: { orderNumber: "WEB-1001" } });
  });
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

  const checkout = page.locator(".checkout-page-panel");
  await checkout.getByLabel("Имя").fill("Александр");
  await checkout.getByLabel("Телефон").fill("9151234567");
  await checkout.getByLabel("Email для чека").fill("buyer@example.com");
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await checkout.locator('input[name="consent"]').check();
  await checkout.getByRole("button", { name: /Продолжить/ }).click();

  await expect(checkout.getByRole("heading", { name: "Заказ принят" }).last()).toBeVisible();
  await expect(checkout.getByText("#WEB-1001")).toBeVisible();
  expect(order).toMatchObject({
    customer: { name: "Александр", phone: "+79151234567", email: "buyer@example.com" },
    delivery: "pickup",
    items: [{ id: "1", quantity: 1 }],
    consent: true,
  });
});

for (const delivery of [
  { id: "courier", label: "Курьер по Рязани", address: "Рязань, Почтовая, 1", price: 430, service: "Яндекс Доставка · Курьер" },
  { id: "post", label: "Почта России", address: "390000, Рязань, Почтовая, 1", price: 615, service: "Почта России" },
]) {
  test(`@desktop ${delivery.label}: адрес и оплата после подтверждения сохраняются`, async ({ page }) => {
    await mockApi(page);
    let order: Record<string, unknown> | undefined;
    await page.route("**/api/v1/delivery/providers", (route) => route.fulfill({ json: { courier: true, post: true } }));
    await page.route(`**/api/v1/delivery/${delivery.id}`, (route) => route.fulfill({ json: { quote: { price: delivery.price, service: delivery.service } } }));
    await page.route("**/api/v1/orders", async (route) => {
      order = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({ json: { orderNumber: "TEST-CHECKOUT-1" } });
    });
    await page.goto("/");
    const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
    await card.getByRole("button", { name: "В корзину" }).click();
    await card.getByRole("button", { name: /В корзине/ }).click();
    await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

    const checkout = page.locator(".checkout-page-panel");
    await checkout.getByLabel("Имя").fill("Тестовый заказ");
    await checkout.getByLabel("Телефон").fill("9151234567");
    await checkout.getByLabel("Email для чека").fill("test@example.com");
    await checkout.getByRole("button", { name: /Продолжить/ }).click();
    await checkout.locator(`input[name="delivery"][value="${delivery.id}"]`).check();
    await checkout.getByLabel("Адрес доставки").fill(delivery.address);
    await checkout.getByRole("button", { name: "Рассчитать доставку" }).click();
    await expect(checkout.locator(".cdek-quote > b")).toHaveText(`${delivery.price} ₽`);
    await checkout.getByRole("button", { name: /Продолжить/ }).click();
    await expect(checkout.getByText("Оплата после подтверждения заказа менеджером")).toBeVisible();
    await checkout.locator('input[name="consent"]').check();
    await checkout.getByRole("button", { name: /Продолжить/ }).click();

    expect(order).toMatchObject({ customer: { address: delivery.address }, delivery: delivery.id, paymentMethod: "manager_confirmation", consent: true });
  });
}

test("@desktop заказ с ручным расчётом не обещает немедленную отправку", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/delivery/providers", (route) => route.fulfill({ json: { courier: true, post: true } }));
  await page.route("**/api/v1/delivery/courier", (route) => route.fulfill({ json: { pending: true, message: "Тариф перевозчика недоступен" } }));
  await page.route("**/api/v1/orders", (route) => route.fulfill({ json: { orderNumber: "MANUAL-1001" } }));
  await page.goto("/");
  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();
  const checkout = page.locator(".checkout-page-panel");
  await checkout.getByLabel("Имя").fill("Ручной расчёт");
  await checkout.getByLabel("Телефон").fill("9151234567");
  await checkout.getByLabel("Email для чека").fill("manual@example.com");
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await checkout.locator('input[name="delivery"][value="courier"]').check();
  await checkout.getByLabel("Адрес доставки").fill("Рязань, Почтовая, 1");
  await checkout.getByRole("button", { name: "Рассчитать доставку" }).click();
  await expect(checkout.getByText("Стоимость уточнит менеджер")).toBeVisible();
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await checkout.locator('input[name="consent"]').check();
  await checkout.getByRole("button", { name: /Продолжить/ }).click();
  await expect(checkout.getByRole("heading", { name: "Заказ отправлен менеджеру" })).toBeVisible();
  await expect(checkout).toContainText("Менеджер свяжется с вами и согласует итоговую сумму");
  await expect(checkout).toContainText("После подтверждения пришлём ссылку на оплату");
  await expect(checkout).not.toContainText("Растения уже готовятся к встрече с вами");
});

test("@desktop оформление подставляет профиль авторизованного покупателя", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

  const checkout = page.locator(".checkout-page-panel");
  await expect(checkout.getByLabel("Имя")).toHaveValue("Александр");
  await expect(checkout.getByLabel("Телефон")).toHaveValue("+79150000000");
  await expect(checkout.getByLabel("Email для чека")).toHaveValue("owner@example.com");
  await expect(checkout.locator(".checkout-order-summary")).toContainText("1 490 ₽");
});

test("@desktop старая ссылка на корзину ведёт на отдельную страницу", async ({ page }) => {
  await mockApi(page);
  await page.goto("/?cart=1");

  await expect(page.locator(".drawer.open")).toBeVisible();
  await expect(page).toHaveURL(/\/cart$/);
});
