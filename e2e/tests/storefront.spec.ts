import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

// Витрина — это главная страница магазина, и почти всё, что покупатель делает
// до корзины, происходит здесь. Раньше её не проверял никто.

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

test("@desktop товар без остатка идёт как предзаказ", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");

  const card = page.locator(".storefront-card", { hasText: "Монстера Делициоза" });
  await expect(card).toHaveClass(/preorder/);
  // «Под заказ» на карточке написано дважды — подписью и на кнопке, поэтому
  // проверяем каждое место отдельно, а не по тексту вообще.
  await expect(card.locator(".storefront-preorder")).toContainText("срок уточнит менеджер");
  await expect(card.getByRole("button", { name: "Под заказ" })).toBeVisible();

  // Магазин, прячущий то, что кончилось, теряет продажу дважды: покупатель не
  // видит растения и никто не узнаёт, что его хотели.
  await page.getByLabel("Только в наличии").check();
  await expect(page.locator(".storefront-card", { hasText: "Монстера Делициоза" })).toHaveCount(0);
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

  await expect(page.locator(".storefront-search input")).toHaveValue("бенджамина");
  await expect(page.locator(".storefront-grid").getByText("Фикус Бенджамина")).toBeVisible();
  await expect(page.locator(".storefront-grid").getByText("Аглаонема Мария")).toHaveCount(0);
});
