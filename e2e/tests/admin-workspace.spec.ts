import { expect, test, type Page } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

const dashboard = {
  user: { fullName: "Александр" }, role: "owner",
  permissions: ["dashboard.read", "analytics.read", "orders.read", "products.read", "procurement.read", "customers.read", "integrations.edit"],
  dashboard: {
    products: 643, variants: 711, orders: 38, customers: 4, wholesalePending: 2,
    lastSync: { status: "success", itemsUpdated: 1000 },
    recentOrders: [
      { orderNumber: "TEST-NEW", customerName: "Покупатель", total: 3000, status: "new" },
      { orderNumber: "TEST-CANCELLED", customerName: "Покупатель", total: 2000, status: "cancelled" },
    ],
  },
};
const analytics = { revenue: 3000, orders: 1, daily: [{ date: "2026-09-08", revenue: 1000, orders: 0 }, { date: "2026-09-09", revenue: 2000, orders: 1 }] };

async function setup(page: Page) {
  await mockApi(page, owner);
  await page.route("**/api/v1/admin/dashboard", route => route.fulfill({ json: dashboard }));
  await page.route("**/api/v1/admin/analytics?days=*", route => route.fulfill({ json: analytics }));
}

// Приветствие зависит от времени суток.
const greeting = /^(Доброе утро|Добрый день|Добрый вечер|Доброй ночи), /;

test("@desktop @phone workspace preserves real navigation and filters recent orders", async ({ page }) => {
  await setup(page);
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: greeting })).toBeVisible();
  // На телефоне все разделы — в листе «Ещё», на компьютере — в боковом меню.
  const phone = (page.viewportSize()?.width ?? 1280) <= 760;
  if (phone) await page.getByRole("button", { name: "Ещё", exact: true }).click();
  const menu = page.getByRole("navigation", { name: phone ? "Все разделы управления" : "Разделы управления" });
  // Девять разделов: «Финансы» удалены владельцем.
  await expect(menu.getByRole("link")).toHaveCount(9);
  await expect(menu.getByRole("link", { name: "Маркетплейсы", exact: true })).toHaveCount(1);
  await expect(menu.getByRole("link", { name: "Склад", exact: true })).toHaveCount(0);
  await expect(menu.getByRole("link", { name: "Финансы", exact: true })).toHaveCount(0);
  if (phone) await page.getByRole("button", { name: "Закрыть", exact: true }).click();
  await page.getByRole("button", { name: "В работе", exact: true }).click();
  await expect(page.getByText("TEST-NEW", { exact: true })).toBeVisible();
  await expect(page.getByText("TEST-CANCELLED", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Отменённые", exact: true }).click();
  await expect(page.getByText("TEST-CANCELLED", { exact: true })).toBeVisible();
  await expect(page.getByText("TEST-NEW", { exact: true })).toHaveCount(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone Avito operations view surfaces risks and gates activation", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/avito", route => route.fulfill({ json: {
    state: { configured: true, publicationEnabled: false, feedUrl: "https://ficusin.ru/feeds/avito/test.xml", lastImportAt: "2026-10-04T08:00:00Z", lastWorkerAt: "2026-10-04T08:01:00Z", lastSuccessAt: "2026-10-04T08:01:00Z", lastError: "" },
    items: [
      { itemId: "101", title: "Дипсис Арека", status: "active", url: "https://www.avito.ru/items/101", remotePrice: 2490, desiredPublished: false, lastReconciledAt: "2026-10-04T08:01:00Z", lastError: "", products: [{ id: 1, name: "Дипсис Арека D12", slug: "areca", image: "", price: 2490, stock: 0 }] },
      { itemId: "102", title: "Фикус Бенджамина", status: "active", url: "https://www.avito.ru/items/102", remotePrice: 1990, desiredPublished: true, lastReconciledAt: "2026-10-04T08:01:00Z", lastError: "", products: [] },
    ],
  } }));
  await page.goto("/admin?section=avito");
  await expect(page.getByRole("heading", { name: "Авито", exact: true })).toBeVisible();
  await expect(page.getByText("Требуется внимание", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: /Не применилось · 1/ })).toBeVisible();
  await page.getByRole("button", { name: "Проверить готовность и включить" }).click();
  await expect(page.getByRole("heading", { name: "Проверка готовности" })).toBeVisible();
  await expect(page.getByText(/Требуют привязки: 1/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Включить управление" })).toBeDisabled();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone Avito separates prices and preserves an explicit pause", async ({ page }) => {
  await setup(page);
  const listing = { itemId: "201", title: "Олива европейская", status: "active", url: "", remotePrice: 2500, manualPaused: false, desiredPublished: true as boolean | null, lastReconciledAt: "2026-10-18T10:00:00Z", lastError: "", products: [{ id: 159, name: "Олива европейская", slug: "olive", image: "", price: 1950, stock: 24 }] };
  await page.route("**/api/v1/admin/avito", route => route.fulfill({ json: {
    state: { configured: true, publicationEnabled: false, feedUrl: "https://ficusin.ru/feeds/avito/test.xml", lastImportAt: "2026-10-18T10:00:00Z", lastWorkerAt: "2026-10-18T10:00:00Z", lastSuccessAt: "2026-10-18T10:00:00Z", lastError: "" }, items: [listing],
  } }));
  await page.route("**/api/v1/admin/avito/201/pause", async route => {
    expect(route.request().method()).toBe("PUT");
    listing.manualPaused = route.request().postDataJSON().paused;
    listing.desiredPublished = !listing.manualPaused;
    await route.fulfill({ json: { paused: listing.manualPaused } });
  });
  await page.goto("/admin?section=avito");
  await expect(page.getByRole("columnheader", { name: "Цена Авито", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Минимум сайта", exact: true })).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: "Олива европейская" });
  await expect(row.getByRole("cell").nth(4)).toHaveText(/2\s*500/);
  await expect(row.getByRole("cell").nth(5)).toContainText(/1\s*950/);
  await expect(row).toContainText(/Разница:.*550/);
  await row.getByRole("button", { name: "Приостановить", exact: true }).click();
  await expect(row).toContainText("Ручная пауза");
  await page.getByRole("button", { name: /На паузе · 1/ }).click();
  await expect(row.getByRole("button", { name: "Возобновить", exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: /На паузе · 1/ }).click();
  await expect(row).toContainText("Ручная пауза");
  await row.getByRole("button", { name: "Возобновить", exact: true }).click();
  await expect(row).toHaveCount(0);
  await page.getByRole("button", { name: /Разница цен · 1/ }).click();
  await expect(row.getByRole("button", { name: "Приостановить", exact: true })).toBeVisible();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone analytics failure does not trap the dashboard and can be retried", async ({ page }) => {
  await setup(page);
  let calls = 0;
  await page.route("**/api/v1/admin/analytics?days=*", route => ++calls === 1
    ? route.fulfill({ status: 503, json: { error: "Unavailable" } })
    : route.fulfill({ json: analytics }));
  await page.goto("/admin");
  await expect(page.getByText("Аналитика временно недоступна")).toBeVisible();
  await expect(page.getByText("TEST-NEW", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Попробовать снова" }).click();
  await expect(page.getByRole("img", { name: /Выручка всех каналов за 7 дней/ })).toBeVisible();
  await expect(page.getByText("Аналитика временно недоступна")).toHaveCount(0);
});

test("@desktop search opens permitted sections by keyboard", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/customers?*", route => route.fulfill({ json: { customers: [], total: 0, hasMore: false } }));
  await page.goto("/admin");
  await page.getByRole("searchbox", { name: "Найти раздел" }).fill("Клиенты");
  await page.getByRole("searchbox", { name: "Найти раздел" }).press("Enter");
  await expect(page.getByRole("heading", { name: "Клиенты", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Разделы управления" }).getByRole("link", { name: "Клиенты", exact: true })).toHaveAttribute("aria-current", "page");
});

test("@desktop restricted role has no inaccessible shortcuts or analytics requests", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/admin/dashboard", route => route.fulfill({ json: { ...dashboard, role: "manager", permissions: ["dashboard.read", "products.read"] } }));
  let analyticsRequests = 0;
  page.on("request", request => { if (request.url().includes("/admin/analytics")) analyticsRequests++; });
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: greeting })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Разделы управления" }).getByRole("link")).toHaveCount(2);
  await expect(page.getByRole("link", { name: "Настройки", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Открыть заказы" })).toHaveCount(0);
  await page.getByRole("searchbox", { name: "Найти раздел" }).fill("Настройки");
  await expect(page.getByText("Раздел не найден")).toBeVisible();
  expect(analyticsRequests).toBe(0);
});

for (const width of [390, 768, 1024, 1280]) {
  test(`@desktop переписка читаема на ${width} px`, async ({page}) => {
    await page.setViewportSize({width,height:1024});
    await setup(page);
    await page.route("**/api/v1/admin/dashboard",route=>route.fulfill({json:{...dashboard,permissions:[...dashboard.permissions,"chat.manage"]}}));
    await page.route("**/api/v1/admin/chat?*",route=>route.fulfill({json:{items:[{id:1,name:"Александр",status:"open",preview:"Нужна помощь",unread:0,updatedAt:"2026-11-04T19:00:00Z",order:{id:1,number:"0002-1",status:"new"}}],hasMore:false}}));
    await page.route("**/api/v1/admin/chat/1**",route=>route.fulfill({json:{id:1,status:"open",customerReadId:0,staffReadId:0,hasMore:false,consentRequired:false,messages:[]}}));
    await page.goto("/admin?section=chat");
    await page.getByRole("button",{name:/Александр.*Нужна помощь/}).click();
    const heading=page.getByRole("heading",{name:"Александр",exact:true});
    await expect(heading).toBeVisible();
    expect((await heading.boundingBox())!.height).toBeLessThan(45);
    const input=await page.getByRole("textbox",{name:"Сообщение",exact:true}).boundingBox();
    expect(input!.width).toBeGreaterThan(180);
    expect(input!.x+input!.width).toBeLessThanOrEqual(width);
    if(width<=1200){
      await page.getByRole("button",{name:"К перепискам",exact:true}).click();
      await expect(page.getByRole("searchbox",{name:"Найти переписку",exact:true})).toBeVisible();
    }
  });
}

test('@phone chat composer stays above menu through scroll and keyboard pan', async ({ page }, info) => {
  await page.addInitScript(() => {
    const viewport = new EventTarget(); Object.assign(viewport, { height: innerHeight, offsetTop: 0 });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
    (window as unknown as { panAdmin: (height: number, top: number) => void }).panAdmin = (height, top) => {
      Object.assign(viewport, { height, offsetTop: top });
      viewport.dispatchEvent(new Event('resize')); viewport.dispatchEvent(new Event('scroll'));
    };
  });
  await setup(page);
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({ json: { ...dashboard, permissions: [...dashboard.permissions, 'chat.manage'] } }));
  await page.route('**/api/v1/admin/chat?*', route => route.fulfill({ json: { items: [{ id: 1, name: 'Посетитель №3', status: 'closed', preview: 'Вопрос', unread: 0, updatedAt: '2026-11-04T19:00:00Z' }], hasMore: false } }));
  await page.route('**/api/v1/admin/chat/1**', route => route.fulfill({ json: { id: 1, status: 'closed', customerReadId: 0, staffReadId: 0, hasMore: false, consentRequired: false, messages: Array.from({ length: 25 }, (_, i) => ({ id: i + 1, sender: 'customer', text: 'Длинное сообщение покупателя о растении. '.repeat(6), createdAt: '2026-11-04T19:00:00Z', attachments: [] })) } }));
  await page.goto('/admin?section=chat');
  await page.getByRole('button', { name: /Посетитель №3.*Вопрос/ }).click();
  const composer = page.locator('.ws-chat .chat-compose');
  const menu = page.locator('.ws-tabbar');
  const assertDocked = async () => {
    const box = (await composer.boundingBox())!;
    const nav = (await menu.boundingBox())!;
    expect(Math.abs(box.y + box.height - nav.y)).toBeLessThanOrEqual(2);
    expect(box.y).toBeGreaterThan(0);
    const bottom = await page.evaluate(() => (visualViewport?.offsetTop || 0) + (visualViewport?.height || innerHeight));
    expect(nav.y + nav.height).toBeLessThanOrEqual(bottom + 2);
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  };
  await expect(composer).toBeVisible();
  await page.evaluate(() => (window as unknown as { panAdmin: (height: number, top: number) => void }).panAdmin(innerHeight, 0));
  await assertDocked();
  await page.locator('.chat-messages').evaluate(el => { el.scrollTop = 0; });
  await assertDocked();
  await page.screenshot({ path: info.outputPath('chat-docked.png') });
  await page.getByRole('textbox', { name: 'Сообщение', exact: true }).fill('Ответ покупателю\n'.repeat(8));
  await page.evaluate(() => (window as unknown as { panAdmin: (height: number, top: number) => void }).panAdmin(460, 90));
  await assertDocked();
  await page.screenshot({ path: info.outputPath('chat-keyboard.png') });
  await page.evaluate(() => (window as unknown as { panAdmin: (height: number, top: number) => void }).panAdmin(innerHeight, 0));
  await assertDocked();
  await menu.getByRole('link', { name: 'Заказы', exact: true }).click();
  await expect(page.locator('body')).not.toHaveClass(/admin-chat-mobile/);
});
