import { expect, test, type Page } from '@playwright/test';
import { horizontalOverflow, mockApi, owner } from './helpers';

const makeEvent = (orderId: number, changes: Record<string, unknown> = {}) => ({
  key: `order:${orderId}`, orderId, orderNumber: `TEST-HISTORY-${orderId}`, shipmentOfferId: 0,
  deliveryMethod: 'cdek', status: 'completed', cdekStatus: 'DELIVERED',
  intentId: 0, state: 'not_prepared', source: '', warehouseId: 0,
  fingerprint: String(orderId).padStart(64, '0'), blockers: [], canPrepare: true,
  lines: [{ orderItemId: orderId * 10, variantId: 11, name: 'Бонсай Лжелиственница прекрасная с длинным названием', sku: 'D13-TEST-HISTORY', sabyId: '1101', quantity: 2, unitPrice: '3490.50' }],
  ...changes,
});

async function fixture(page: Page, { manager = false, stale = false } = {}) {
  const runtimeErrors: string[] = [];
  page.on('pageerror', error => runtimeErrors.push(error.message));
  await mockApi(page, owner);
  const events = [
    makeEvent(17),
    makeEvent(18, { key: 'shipment:301', shipmentOfferId: 301, status: 'shipped', cdekStatus: 'IN_TRANSIT', intentId: 91, source: 'automatic', state: 'queued' }),
    makeEvent(19, { status: 'ready', cdekStatus: 'CREATED', canPrepare: false, blockers: ['Передача покупателю или СДЭК не подтверждена'] }),
  ];
  const prepares: any[] = [], reads: string[] = [], mutations: string[] = [];
  await page.route('**/api/v1/admin/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (!['GET', 'HEAD'].includes(request.method())) mutations.push(path);
    if (path === '/api/v1/admin/dashboard') return route.fulfill({ json: {
      user: { fullName: 'Проверка' }, role: manager ? 'manager' : 'owner',
      permissions: manager ? ['orders.read'] : ['orders.read', 'orders.edit', 'payments.refund'],
      dashboard: { products: 0, variants: 0, orders: 3, customers: 1, wholesalePending: 0, recentOrders: [] },
    } });
    if (path === '/api/v1/admin/orders') return route.fulfill({ json: { orders: [] } });
    if (path === '/api/v1/admin/order-realizations') {
      reads.push(url.search);
      if (manager) return route.fulfill({ status: 403, json: { error: 'Реализации доступны владельцу' } });
      const q = (url.searchParams.get('q') || '').toLowerCase();
      return route.fulfill({ json: { events: events.filter(event => event.orderNumber.toLowerCase().includes(q)), warehouses: [{ id: 7, name: 'Основной склад Рязань', nativeMappingConfirmed: false }], hasMore: false, externalWritesEnabled: false } });
    }
    if (path === '/api/v1/admin/order-realizations/prepare') {
      const body = request.postDataJSON(); prepares.push(body);
      if (stale && prepares.length === 1) {
        events[0].fingerprint = 'b'.repeat(64);
        return route.fulfill({ status: 409, json: { error: 'Состав отправки изменился; обновите список' } });
      }
      const prepared = body.events.map((selection: { orderId: number; shipmentOfferId: number; fingerprint: string }) => {
        const event = events.find(event => event.orderId === selection.orderId && event.shipmentOfferId === selection.shipmentOfferId)!;
        expect(selection.fingerprint).toBe(event.fingerprint);
        Object.assign(event, { state: 'prepared', source: 'manual', warehouseId: body.warehouseId, intentId: event.intentId || 117 });
        return event;
      });
      return route.fulfill({ json: { events: prepared, externalWritesEnabled: false } });
    }
    return route.fulfill({ json: {} });
  });
  return { events, prepares, reads, mutations, runtimeErrors };
}

async function prepareHistorical(page: Page, info: { outputPath: (name: string) => string }, width?: number) {
  if (width) await page.setViewportSize({ width, height: 950 });
  const f = await fixture(page);
  await page.goto('/admin?section=orders');
  await page.getByRole('link', { name: 'Реализации', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Реализация по заказам', exact: true });
  await expect(panel).toBeVisible();
  await expect(panel.getByText(/Создание реализации и изменение остатков в СБИС ещё не подключены/)).toBeVisible();
  await expect(panel.getByRole('checkbox', { name: 'Выбрать заказ TEST-HISTORY-19', exact: true })).toBeDisabled();
  await expect(panel.locator('[data-event="order:19"]')).toContainText('Передача покупателю или СДЭК не подтверждена');
  const prepare = panel.getByRole('button', { name: /^Подготовить на сайте/ });
  await expect(prepare).toBeDisabled(); expect(f.prepares).toHaveLength(0);
  await panel.screenshot({ path: info.outputPath('realizations-before-preparation.png') });
  await panel.getByRole('textbox', { name: 'Номер заказа', exact: true }).fill('TEST-HISTORY-17');
  await panel.getByRole('button', { name: 'Найти', exact: true }).click();
  await expect(panel.locator('[data-event]')).toHaveCount(1);
  await panel.getByRole('checkbox', { name: 'Выбрать заказ TEST-HISTORY-17', exact: true }).check();
  await expect(prepare).toBeDisabled();
  await panel.getByRole('combobox', { name: 'Склад реализации', exact: true }).selectOption('7');
  await expect(prepare).toHaveText('Подготовить на сайте (1)'); await expect(prepare).toBeEnabled();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await prepare.click();
  await expect(panel.getByRole('status')).toHaveText('Подготовлено на сайте. Документы в СБИС не создавались, остатки не изменялись.');
  const historical = panel.locator('[data-event="order:17"]');
  await expect(historical.locator('.order-realizations-lines')).toContainText(/2 шт\. × 3\s490,50\s₽/);
  await expect(historical).toContainText('Подготовка №117');
  await expect(historical).toContainText('Основной склад Рязань');
  await expect(historical.getByRole('checkbox')).toBeDisabled();
  expect(f.prepares).toEqual([{ warehouseId: 7, events: [{ orderId: 17, shipmentOfferId: 0, fingerprint: String(17).padStart(64, '0') }] }]);
  await page.reload();
  await expect(historical).toContainText('Подготовлено на сайте');
  await expect(historical).toContainText('Подготовка №117');
  await expect(historical.getByRole('checkbox')).toBeDisabled();
  await panel.getByRole('button', { name: 'Обновить', exact: true }).click();
  await expect(historical).toContainText('Подготовка №117');
  await expect(prepare).toBeDisabled();
  expect(f.prepares).toHaveLength(1);
  expect(f.mutations).toEqual(['/api/v1/admin/order-realizations/prepare']);
  expect(f.runtimeErrors).toEqual([]);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await historical.screenshot({ path: info.outputPath('prepared-historical-order.png') });
}

for (const width of [390, 768, 1440]) {
  test(`@desktop historical realization keeps its preparation after reload at ${width}px`, async ({ page }, info) => prepareHistorical(page, info, width));
}
test('@phone historical realization keeps its preparation after reload on touch devices', async ({ page }, info) => prepareHistorical(page, info));

test('@desktop @phone realization route is restricted to the owner before loading any events', async ({ page }) => {
  const f = await fixture(page, { manager: true });
  await page.goto('/admin?section=orders');
  await expect(page.getByRole('heading', { name: 'Заказы', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Реализации', exact: true })).toHaveCount(0);
  await page.goto('/admin?section=orders&view=realizations');
  await expect(page.getByRole('status').getByText('Реализации доступны владельцу', { exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Склад реализации' })).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: /Выбрать заказ/ })).toHaveCount(0);
  expect(f.reads).toHaveLength(0); expect(f.mutations).toHaveLength(0);
  expect(f.runtimeErrors).toEqual([]);
});

test('@desktop a stale selection is explained and requires refreshed data before preparation', async ({ page }) => {
  const f = await fixture(page, { stale: true });
  await page.goto('/admin?section=orders&view=realizations');
  const panel = page.getByRole('region', { name: 'Реализация по заказам', exact: true });
  const checkbox = panel.getByRole('checkbox', { name: 'Выбрать заказ TEST-HISTORY-17', exact: true });
  const prepare = panel.getByRole('button', { name: /^Подготовить на сайте/ });
  await checkbox.check(); await panel.getByRole('combobox', { name: 'Склад реализации' }).selectOption('7');
  await prepare.click();
  await expect(panel.getByRole('alert')).toContainText('Состав отправки изменился; обновите список');
  expect(f.prepares).toHaveLength(1);
  await expect(panel.locator('[data-event="order:17"]')).toContainText('Не подготовлено');
  await panel.getByRole('button', { name: 'Обновить', exact: true }).click();
  await expect(checkbox).not.toBeChecked(); await expect(prepare).toBeDisabled();
  await checkbox.check(); await prepare.click();
  await expect(panel.locator('[data-event="order:17"]')).toContainText('Подготовка №117');
  expect(f.prepares).toHaveLength(2);
  expect(f.prepares[1].events[0].fingerprint).toBe('b'.repeat(64));
  expect(f.runtimeErrors).toEqual([]);
});
