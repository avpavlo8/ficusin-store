import { expect, test } from "@playwright/test";
import { mockApi, owner } from "./helpers";

// Витрина — это главная страница магазина, и почти всё, что покупатель делает
// до корзины, происходит здесь. Раньше её не проверял никто.

test("@desktop главная сохраняет утверждённую визуальную структуру", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await expect(page.locator(".home-hero-visual img")).toHaveAttribute("src", /home-hero-4k\.webp/);
  await expect(page.locator(".home-team img")).toBeVisible();
  await expect(page.locator(".home-collections")).toHaveCount(0);
  await expect(page.locator(".storefront-preset-carousel .preset")).toHaveCount(9);
  await expect(page.getByRole("button", { name: "Следующие подборки" })).toBeVisible();
  await expect(page.locator(".home-catalog-toolbar")).toBeVisible();
  const headerMenus = page.locator(".header-dropdown");
  await headerMenus.first().locator(":scope > summary").click();
  await expect(headerMenus.first().getByRole("button", { name: /Растения/ })).toBeVisible();
  await headerMenus.nth(1).locator(":scope > summary").click();
  await expect(headerMenus.first()).not.toHaveAttribute("open", "");
  await expect(headerMenus.nth(1).getByRole("button", { name: /Аглаонема/ })).toBeVisible();
  await page.locator(".home-hero-copy").click();
  await expect(headerMenus.nth(1)).not.toHaveAttribute("open", "");
  await page.getByRole("button", { name: "Список" }).click();
  await expect(page.locator(".storefront-grid")).toHaveClass(/list-view/);
  await expect(page.locator(".storefront-main").getByRole("heading", { name: "Каталог" })).toHaveCount(1);
});

test("@mobile нижний блок остаётся полноширинной кнопкой чата", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto("/");

  const footer = page.locator(".home-service");
  await expect(footer.getByRole("link", { name: /Написать в чат/ })).toBeVisible();
  await expect(footer.getByRole("heading", { name: /Не знаете/ })).toBeHidden();
  await expect(footer.locator(".home-service-card")).toBeHidden();
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

  // Товаров в кашпо пока нет, но раздел у магазина есть, и покупатель
  // должен видеть, что он существует.
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

  // «На солнечное окно» не подходит ни одному товару стенда: плитка, ведущая
  // в «ничего не нашли», хуже, чем её отсутствие.
  await expect(page.getByRole("listitem", { name: /солнечное/ })).toHaveCount(0);

  await page.locator(".preset", { hasText: "Для ванной" }).click();

  const grid = page.locator(".storefront-grid");
  await expect(grid.getByText("Аглаонема Мария")).toBeVisible();
  await expect(grid.getByText("Фикус Бенджамина")).toHaveCount(0);
});

test("@desktop быстрые фильтры поддерживают множественный выбор", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await page.locator(".preset", { hasText: "Для ванной" }).click();
  await page.locator(".preset", { hasText: "Для офиса" }).click();
  await expect(page.locator(".preset.active")).toHaveCount(2);
  await expect(page.locator(".storefront-card")).toHaveCount(0);

  await page.locator(".preset", { hasText: "Для ванной" }).click();
  await expect(page.locator(".preset.active")).toHaveCount(1);
});

test("@desktop товар без остатка идёт как предзаказ", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Монстера Делициоза" });
  await expect(card).toHaveClass(/preorder/);
  await expect(card.locator(".storefront-price em")).toHaveText("Под заказ");
  await expect(card.locator(".storefront-preorder")).toHaveCount(0);
  await expect(card.getByRole("button", { name: "В корзину" })).toBeVisible();

  // Магазин, прячущий то, что кончилось, теряет продажу дважды: покупатель не
  // видит растения и никто не узнаёт, что его хотели.
  await page.getByLabel("Только в наличии").check();
  await expect(page.locator(".storefront-card", { hasText: "Монстера Делициоза" })).toHaveCount(0);
});

test("@desktop на карточке товара выбирается количество", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/saby-1");

  await expect(page.locator(".pdp-quantity output")).toHaveText("1");
  await page.locator(".pdp-quantity button").last().click();
  await page.getByRole("button", { name: "В корзину" }).click();

  await expect(page.getByRole("button", { name: "Обновить корзину" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Корзина, товаров: 2/ })).toBeVisible();
  await expect(page.getByText("Безопасно для животных")).toBeVisible();
  await expect(page.locator("#plant-passport")).toContainText("Тропические леса Азии");
  await expect(page.locator("#reviews")).toContainText("Подтверждённая покупка");
});

test("@desktop PDP сохраняет коммерческую иерархию и навигацию", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/saby-1");

  const purchase = page.locator(".pdp-summary");
  await expect(purchase.getByRole("heading", { level: 1 })).toHaveText("Аглаонема Мария");
  await expect(purchase.locator(".pdp-commerce-box")).toContainText("В наличии");
  await expect(purchase.getByRole("button", { name: "В корзину" })).toBeVisible();
  await expect(page.locator(".pdp-anchor-nav").getByRole("link")).toHaveCount(3);
  await expect(page.locator(".passport-sections")).toContainText("Регулярный уход");
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
});

test("@desktop прямой QR-якорь открывает паспорт без потери контента", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/saby-1#plant-passport");
  await expect(page.locator("#plant-passport")).toBeVisible();
  await expect(page.locator("#plant-passport details")).toContainText("Когда пересаживать?");
});

test("@desktop пустой паспорт остаётся полезным и не рисует фиктивные данные", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/empty", (route) => route.fulfill({ json: { product: {
    id: "empty", name: "Растение без паспорта", latin: "", shortDescription: "", description: "", careInstructions: "",
    images: ["/assets/product-pothos.png"], variants: [], recommendations: [], passport: {}, importantWarnings: [], rating: 0, reviewsCount: 0, reviews: [], catalogSection: "plants",
  } } }));
  await page.goto("/product/empty#plant-passport");
  const singlePhoto = await page.locator(".pdp-gallery.single .pdp-image").boundingBox();
  expect(singlePhoto?.width || 0).toBeGreaterThan(400);
  await expect(page.locator("#plant-passport")).toContainText("Паспорт готовится");
  await expect(page.locator("#reviews")).toContainText("Здесь пока тихо");
  await expect(page.locator(".purchase-review-meta")).toContainText("Пока без отзывов");
});

test("@desktop фотография открывается в полноэкранной галерее", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/saby-1");
  await page.getByRole("button", { name: "Открыть фотографию на весь экран" }).click();
  await expect(page.getByRole("dialog", { name: /Фотографии/ })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: /Фотографии/ })).toHaveCount(0);
});

test("@desktop сортировка по цене", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  await page.getByLabel("Сортировка").selectOption("cheap");
  await expect(page.locator(".storefront-name").first()).toHaveText("Аглаонема Мария");

  await page.getByLabel("Сортировка").selectOption("expensive");
  await expect(page.locator(".storefront-name").first()).toHaveText("Монстера Делициоза");
});

test("@desktop ссылка с запросом сразу показывает выдачу", async ({ page }) => {
  await mockApi(page);
  // Поиск из шапки на других страницах ведёт сюда. Однажды витрина уступала
  // место старой странице при любом параметре в адресе, и такие ссылки
  // открывались по-старому.
  await page.goto("/?q=бенджамина");

  await expect(page.locator(".header-search input")).toHaveValue("бенджамина");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toHaveCount(0);
});

test("@desktop корзина открывается поверх витрины без навигации", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();

  await expect(page).toHaveURL(/\/$/);
  const drawer = page.locator(".drawer.open");
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText("Аглаонема Мария")).toBeVisible();
  await expect(drawer.locator(".quantity span")).toHaveText("1");
});

test("@desktop оформление открывается из вынесенной корзины", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

  const checkout = page.locator(".checkout.open");
  await expect(checkout).toBeVisible();
  await expect(checkout.getByRole("heading", { name: "Оформление заказа" })).toBeVisible();
  await expect(checkout.getByLabel("Имя")).toBeVisible();
  await expect(checkout.getByLabel("Телефон")).toBeVisible();
  const consent = checkout.locator('input[name="consent"]');
  const submit = checkout.getByRole("button", { name: "Подтвердить заказ" });
  await expect(consent).toBeVisible();
  await expect(submit).toBeVisible();
  expect(await consent.evaluate((node) => Boolean(node.compareDocumentPosition(document.querySelector('.checkout.open button.primary-button')!) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  await expect(page).toHaveURL(/\/$/);
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

  const checkout = page.locator(".checkout.open");
  await checkout.getByLabel("Имя").fill("Александр");
  await checkout.getByLabel("Телефон").fill("9151234567");
  await checkout.getByLabel("Email").fill("buyer@example.com");
  await checkout.locator('input[name="consent"]').check();
  await checkout.getByRole("button", { name: "Подтвердить заказ" }).click();

  await expect(checkout.getByRole("heading", { name: "Заказ принят" })).toBeVisible();
  expect(order).toMatchObject({
    customer: { name: "Александр", phone: "+79151234567", email: "buyer@example.com" },
    delivery: "pickup",
    items: [{ id: "saby-1", quantity: 1 }],
    consent: true,
  });
});

for (const delivery of [
  { id: "courier", label: "Курьер по Рязани", address: "Рязань, Почтовая, 1" },
  { id: "post", label: "Почта России", address: "390000, Рязань, Почтовая, 1" },
]) {
  test(`@desktop ${delivery.label}: адрес и оплата после подтверждения сохраняются`, async ({ page }) => {
    await mockApi(page);
    let order: Record<string, unknown> | undefined;
    await page.route("**/api/v1/orders", async (route) => {
      order = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({ json: { orderNumber: "TEST-CHECKOUT-1" } });
    });
    await page.goto("/");
    const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
    await card.getByRole("button", { name: "В корзину" }).click();
    await card.getByRole("button", { name: /В корзине/ }).click();
    await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

    const checkout = page.locator(".checkout.open");
    await checkout.locator(`input[name="delivery"][value="${delivery.id}"]`).check();
    await expect(checkout.getByText("Оплата после подтверждения заказа менеджером")).toBeVisible();
    await checkout.getByLabel("Имя").fill("Тестовый заказ");
    await checkout.getByLabel("Телефон").fill("9151234567");
    await checkout.getByLabel("Email").fill("test@example.com");
    await checkout.getByLabel("Адрес доставки").fill(delivery.address);
    await checkout.locator('input[name="consent"]').check();
    await checkout.getByRole("button", { name: "Подтвердить заказ" }).click();

    expect(order).toMatchObject({
      customer: { address: delivery.address },
      delivery: delivery.id,
      paymentMethod: "manager_confirmation",
      consent: true,
    });
  });
}

test("@desktop оформление подставляет профиль авторизованного покупателя", async ({ page }) => {
  await mockApi(page, owner);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Аглаонема Мария" });
  await card.getByRole("button", { name: "В корзину" }).click();
  await card.getByRole("button", { name: /В корзине/ }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "Оформить заказ" }).click();

  const checkout = page.locator(".checkout.open");
  await expect(checkout.getByLabel("Имя")).toHaveValue("Александр");
  await expect(checkout.getByLabel("Телефон")).toHaveValue("+79150000000");
  await expect(checkout.getByLabel("Email")).toHaveValue("owner@example.com");
  await expect(checkout.locator(".checkout-total > div").first().locator("span").last()).toHaveText("1 490 ₽");
});

test("@desktop старая ссылка на корзину открывает новую панель один раз", async ({ page }) => {
  await mockApi(page);
  await page.goto("/?cart=1");

  await expect(page.locator(".drawer.open")).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
});
