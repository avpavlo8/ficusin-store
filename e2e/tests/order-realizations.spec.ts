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

const makePublication = (changes: Record<string, unknown> = {}) => ({
  id: 117, executionMode: 'mock', testMode: true, status: 'queued', message: 'Проверяем документ',
  documentId: '', documentUUID: '', stockVerified: false, canRetry: false, ...changes,
});

async function fixture(page: Page, { manager = false, stale = false, mode = 'disabled', enabled = true }: { manager?: boolean; stale?: boolean; mode?: 'disabled' | 'mock' | 'live'; enabled?: boolean } = {}) {
  const runtimeErrors: string[] = [];
  page.on('pageerror', error => runtimeErrors.push(error.message));
  await mockApi(page, owner);
  const events: Array<ReturnType<typeof makeEvent> & { publication?: ReturnType<typeof makePublication> }> = [
    makeEvent(17),
    makeEvent(18, { key: 'shipment:301', shipmentOfferId: 301, status: 'shipped', cdekStatus: 'IN_TRANSIT', intentId: 91, source: 'automatic', state: 'queued' }),
    makeEvent(19, { status: 'ready', cdekStatus: 'CREATED', canPrepare: false, blockers: ['Передача покупателю или СДЭК не подтверждена'] }),
  ];
  const prepares: any[] = [], reads: string[] = [], mutations: string[] = [], retries: number[] = [];
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
      return route.fulfill({ json: { events: events.filter(event => event.orderNumber.toLowerCase().includes(q)), warehouses: [{ id: 7, name: 'Основной склад Рязань', nativeMappingConfirmed: mode === 'live' }, { id: 8, name: 'Другой склад', nativeMappingConfirmed: false }], hasMore: false, executionMode: mode, dispatchMode: mode, dispatchEnabled: mode !== 'disabled' && enabled, autoDispatchEnabled: mode !== 'disabled' && enabled, externalWritesEnabled: mode === 'live' && enabled } });
    }
    const retry = path.match(/^\/api\/v1\/admin\/order-realizations\/(\d+)\/retry$/);
    if (retry) {
      retries.push(Number(retry[1]));
      const publication = events.find(event => event.publication?.id === Number(retry[1]))?.publication;
      if (!publication?.canRetry) return route.fulfill({ status: 409, json: { error: 'Нужна ручная сверка. Новый документ не создавался.' } });
      Object.assign(publication, { status: 'queued', canRetry: false, message: 'Повторная проверка состояния' });
      return route.fulfill({ json: { ok: true } });
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
        if (mode !== 'disabled' && enabled) event.publication = makePublication({ executionMode: mode, testMode: mode === 'mock' });
        return event;
      });
      return route.fulfill({ json: { events: prepared, externalWritesEnabled: false } });
    }
    return route.fulfill({ json: {} });
  });
  return { events, prepares, reads, mutations, retries, runtimeErrors };
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

async function mockCompletion(page: Page, info: { outputPath: (name: string) => string }, width?: number) {
  if (width) await page.setViewportSize({ width, height: 950 });
  const f = await fixture(page, { mode: 'mock' });
  await page.goto('/admin?section=orders&view=realizations');
  const panel = page.getByRole('region', { name: 'Реализация по заказам', exact: true });
  const row = panel.locator('[data-event="order:17"]');
  await expect(panel.locator('.order-realizations-capability')).toContainText('Тестовый обмен');
  await row.getByRole('checkbox').check();
  await panel.getByRole('combobox', { name: 'Склад реализации' }).selectOption('7');
  await panel.getByRole('button', { name: 'Создать тестовую реализацию (1)', exact: true }).click();
  await expect(row).toContainText('Тест · В очереди обмена');
  await expect(panel.getByRole('status')).toContainText('Реальные документы и остатки СБИС не меняются');
  await expect(row.getByRole('checkbox')).toBeDisabled();
  Object.assign(f.events[0].publication!, { status: 'processing', documentId: 'STG-SALE-117', documentUUID: '00000000-0000-4000-8000-000000000117' });
  await expect(row).toContainText('Тест · Обрабатывается', { timeout: 7000 });
  Object.assign(f.events[0].publication!, { status: 'posted', stockVerified: true });
  await expect(row).toContainText('Тестовая реализация завершена', { timeout: 7000 });
  await expect(panel.getByRole('status')).toHaveCount(0);
  await expect(row).toContainText('Тестовый документ: STG-SALE-117');
  await expect(row).toContainText('Документ СБИС и реальные списания не создавались.');
  await expect(panel.getByText('Проведено в СБИС', { exact: true })).toHaveCount(0);
  await expect(panel.getByText('Складское движение подтверждено.', { exact: true })).toHaveCount(0);
  expect(f.prepares).toHaveLength(1); expect(f.retries).toHaveLength(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await panel.screenshot({ path: info.outputPath('mock-realization-complete.png') });
  await page.reload(); await expect(row).toContainText('Тестовая реализация завершена');
  expect(f.prepares).toHaveLength(1); expect(f.runtimeErrors).toEqual([]);
}

for (const width of [390, 768, 1440]) {
  test(`@desktop mock realization is labelled as a test throughout dispatch at ${width}px`, async ({ page }, info) => mockCompletion(page, info, width));
}
test('@phone mock realization is labelled as a test throughout dispatch on touch devices', async ({ page }, info) => mockCompletion(page, info));

test('@desktop @phone live publication retries only an explicitly safe receipt and confirms stock proof', async ({ page }) => {
  const f = await fixture(page, { mode: 'live' });
  f.events[0].publication = makePublication({ executionMode: 'live', testMode: false, status: 'needs_attention', documentId: '2357,0', documentUUID: '00000000-0000-4000-8000-000000002357', canRetry: true, message: 'Ответ проведения потерян. Проверяем сохранённый документ.' });
  f.events[1].publication = makePublication({ id: 118, executionMode: 'live', testMode: false, status: 'needs_attention', message: 'Неизвестный результат создания. Нужна ручная сверка.', canRetry: false });
  f.events[2].publication = makePublication({ id: 119, executionMode: 'live', testMode: false, status: 'failed', message: 'Склад недоступен. Проверьте настройку.', canRetry: false });
  await page.goto('/admin?section=orders&view=realizations');
  const panel = page.getByRole('region', { name: 'Реализация по заказам', exact: true });
  const safe = panel.locator('[data-event="order:17"]'), unknown = panel.locator('[data-event="shipment:301"]');
  await expect(panel.getByRole('button', { name: 'Создать реализацию', exact: true })).toBeDisabled();
  await expect(panel.getByRole('combobox', { name: 'Склад реализации' }).locator('option[value="8"]')).toHaveJSProperty('disabled', true);
  await expect(unknown).toContainText('Нужна ручная сверка');
  await expect(unknown.getByRole('button', { name: 'Повторить проверку' })).toHaveCount(0);
  await expect(panel.locator('[data-event="order:19"]')).toContainText('Ошибка обмена');
  await safe.getByRole('button', { name: 'Повторить проверку', exact: true }).click();
  await expect(safe).toContainText('В очереди обмена');
  await expect(safe.getByRole('button', { name: 'Повторить проверку' })).toHaveCount(0);
  expect(f.retries).toEqual([117]); expect(f.prepares).toHaveLength(0);
  Object.assign(f.events[0].publication!, { status: 'posted', stockVerified: true });
  await expect(safe).toContainText('Проведено в СБИС', { timeout: 7000 });
  await expect(safe).toContainText('Складское движение подтверждено.');
  await expect(safe).toContainText('Документ СБИС: 2357,0');
  expect(f.mutations).toEqual(['/api/v1/admin/order-realizations/117/retry']);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  expect(f.runtimeErrors).toEqual([]);
});

test('@desktop a posted label without native stock proof never claims a real write', async ({ page }) => {
  const f = await fixture(page, { mode: 'live' });
  f.events[0].publication = makePublication({ executionMode: 'live', testMode: false, status: 'posted', documentId: '2357,0', documentUUID: '00000000-0000-4000-8000-000000002357', stockVerified: false, message: 'Проведено в СБИС' });
  f.events[1].publication = makePublication({ id: 118, executionMode: 'live', testMode: false, status: 'posted', documentId: 'STG-SALE-118', documentUUID: '00000000-0000-4000-8000-000000000118', stockVerified: true });
  await page.goto('/admin?section=orders&view=realizations');
  const panel = page.getByRole('region', { name: 'Реализация по заказам', exact: true });
  await expect(panel.locator('[data-event="order:17"]')).toContainText('Проведение не подтверждено');
  await expect(panel.locator('[data-event="shipment:301"]')).toContainText('Тестовая реализация завершена');
  await expect(panel.getByText('Проведено в СБИС', { exact: true })).toHaveCount(0);
  await expect(panel.getByText('Складское движение подтверждено.', { exact: true })).toHaveCount(0);
  expect(f.mutations).toEqual([]); expect(f.runtimeErrors).toEqual([]);
});

test('@desktop a paused mock dispatcher keeps preparation local', async ({ page }) => {
  await fixture(page, { mode: 'mock', enabled: false });
  await page.goto('/admin?section=orders&view=realizations');
  await expect(page.getByRole('button', { name: 'Подготовить на сайте', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Создать тестовую реализацию/ })).toHaveCount(0);
});
