import { expect, test, type Page } from '@playwright/test';
import { mockApi, owner, horizontalOverflow } from './helpers';

const thread = { id: 1, status: 'open', customerReadId: 0, staffReadId: 0, consentRequired: false,
  messages: [{ id: 1, sender: 'staff', text: 'Проверочная история переписки', createdAt: '2026-11-13T12:00:00Z' }], hasMore: false };

async function fixtures(page: Page) {
  await page.addInitScript(() => {
    const viewport = new EventTarget(); Object.assign(viewport, { height: innerHeight, offsetTop: 0 });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
    (window as unknown as { panReference: (height: number, top: number) => void }).panReference = (height, top) => {
      Object.assign(viewport, { height, offsetTop: top });
      viewport.dispatchEvent(new Event('resize')); viewport.dispatchEvent(new Event('scroll'));
    };
  });
  await mockApi(page, owner);
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({ json: { user: { fullName: 'Тест' }, role: 'owner', permissions: ['chat.manage', 'orders.read', 'products.read'], dashboard: { products: 1, variants: 1, orders: 0, customers: 1, wholesalePending: 0, recentOrders: [] } } }));
  await page.route(/\/api\/v1\/admin\/chat\?/, route => route.fulfill({ json: { items: [{ id: 1, name: 'Тестовая переписка', status: 'open', preview: 'История', unread: 0, updatedAt: '2026-11-13T12:00:00Z' }], hasMore: false } }));
  await page.route(/\/api\/v1\/(?:admin\/chat\/1|chat)(?:\?.*)?$/, route => route.fulfill({ json: thread }));
  await page.route(/\/api\/v1\/(?:admin\/chat\/1|chat)\/read$/, route => route.fulfill({ json: { ok: true } }));
  await page.route(/\/api\/v1\/(?:admin\/chat\/1|chat)\/references\?/, route => {
    const kind = new URL(route.request().url()).searchParams.get('kind');
    return route.fulfill({ json: { items: Array.from({ length: 24 }, (_, index) => ({ kind, available: true, id: String(index + 1), label: kind === 'order' ? `Заказ TEST-${index + 1}` : `Фикус для проверки ${index + 1}`, href: kind === 'order' ? `/account/orders/${index + 1}` : `/product/${index + 1}` })) } });
  });
}

for (const role of ['admin', 'customer'] as const) {
  for (const kind of ['product', 'order'] as const) {
    test(`@phone ${role} ${kind} picker docks below the chat header through keyboard pan`, async ({ page }, info) => {
      await fixtures(page);
      let sends = 0; page.on('request', request => { if (/\/chat(?:\/1)?\/messages$/.test(request.url())) sends++; });
      await page.goto(role === 'admin' ? '/admin?section=chat' : '/account/chat');
      if (role === 'admin') {
        await expect(page.locator('.ws-tabbar')).toBeVisible();
        await page.getByRole('button', { name: /Тестовая переписка.*История/ }).click();
        await expect(page.locator('.ws-tabbar')).toBeHidden();
      } else await expect(page.locator('.tab-bar')).toBeHidden();
      // The mock must follow the post-meta viewport, just as the real browser does.
      await page.evaluate(() => (window as unknown as { panReference: (height: number, top: number) => void }).panReference(innerHeight, 0));
      const draft = 'Черновик остаётся на месте\n'.repeat(8);
      await page.getByRole('textbox', { name: 'Сообщение', exact: true }).fill(draft);
      await page.evaluate(() => (window as unknown as { panReference: (height: number, top: number) => void }).panReference(330, 90));
      await page.getByRole('button', { name: 'Прикрепить вложение', exact: true }).click();
      await page.getByRole('button', { name: kind === 'product' ? 'Товар' : 'Заказ', exact: true }).click();
      const picker = page.getByRole('dialog', { name: kind === 'product' ? 'Прикрепить товар' : 'Прикрепить заказ', exact: true });
      const search = picker.getByRole('searchbox', { name: kind === 'product' ? 'Поиск товара' : 'Поиск заказа', exact: true });
      await expect(search).toBeFocused();
      if (role === 'admin') await expect(page.locator('.admin-page')).toHaveAttribute('data-chat-typing', '');
      await search.fill('Тест');
      await expect(picker.getByRole('button', { name: kind === 'product' ? 'Фикус для проверки 24' : 'Заказ TEST-24', exact: true })).toBeVisible();
      await page.screenshot({ path: info.outputPath(`${role}-${kind}-normal.png`) });
      expect(await search.evaluate(input => getComputedStyle(input).fontSize)).toBe('16px');
      await page.evaluate(() => (window as unknown as { panReference: (height: number, top: number) => void }).panReference(330, 90));
      const header = page.locator(role === 'admin' ? '.admin-chat-detail .chat-panel-heading' : '.account-chat-conversation .chat-panel-heading');
      const assertVisible = async () => {
        const h = (await header.boundingBox())!, p = (await picker.boundingBox())!, input = (await search.boundingBox())!;
        const composer = (await page.locator('.chat-compose').boundingBox())!, results = (await picker.locator('.chat-reference-options').boundingBox())!;
        expect(p.y).toBeGreaterThanOrEqual(h.y + h.height - 1);
        expect(input.y).toBeGreaterThanOrEqual(90);
        expect(input.y + input.height).toBeLessThanOrEqual(420);
        expect(p.y + p.height).toBeLessThanOrEqual(composer.y + 1);
        expect(composer.y + composer.height).toBeLessThanOrEqual(421);
        expect(results.height).toBeGreaterThanOrEqual(44);
        expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
      };
      await expect.poll(async () => (await search.boundingBox())!.y).toBeGreaterThanOrEqual(90);
      await assertVisible();
      await page.screenshot({ path: info.outputPath(`${role}-${kind}-keyboard.png`) });
      const last = picker.getByRole('button', { name: kind === 'product' ? 'Фикус для проверки 24' : 'Заказ TEST-24', exact: true });
      await last.scrollIntoViewIfNeeded();
      await last.click();
      await expect(picker).toHaveCount(0);
      await expect(page.locator('.chat-reference-preview')).toContainText(kind === 'product' ? 'Фикус для проверки 24' : 'Заказ TEST-24');
      await expect(page.getByRole('textbox', { name: 'Сообщение', exact: true })).toHaveValue(draft);
      expect(sends).toBe(0);
      if (role === 'admin') {
        await page.evaluate(() => (window as unknown as { panReference: (height: number, top: number) => void }).panReference(innerHeight, 0));
        await page.getByRole('button', { name: 'К перепискам', exact: true }).click();
        await expect(page.locator('.ws-tabbar')).toBeVisible();
      }
    });
  }
}

for (const role of ['admin', 'customer'] as const) {
  for (const width of [768, 1440]) {
    test(`@desktop ${role} reference picker preserves the shared layout at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 }); await fixtures(page);
      await page.goto(role === 'admin' ? '/admin?section=chat' : '/account/chat');
      if (role === 'admin') await page.getByRole('button', { name: /Тестовая переписка.*История/ }).click();
      await page.getByRole('textbox', { name: 'Сообщение', exact: true }).fill('Несохранённый текст');
      await page.getByRole('button', { name: 'Прикрепить вложение', exact: true }).click();
      await page.getByRole('button', { name: 'Товар', exact: true }).click();
      const picker = page.getByRole('dialog', { name: 'Прикрепить товар', exact: true });
      await expect(picker.getByRole('searchbox', { name: 'Поиск товара' })).toBeFocused();
      const h = (await page.locator(role === 'admin' ? '.admin-chat-detail .chat-panel-heading' : '.account-chat-conversation .chat-panel-heading').boundingBox())!;
      const box = (await picker.boundingBox())!, composer = (await page.locator('.chat-compose').boundingBox())!;
      expect(box.y).toBeGreaterThanOrEqual(h.y + h.height - 1);
      expect(box.y + box.height).toBeLessThanOrEqual(composer.y + 1);
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
      await picker.getByRole('button', { name: 'Закрыть выбор вложения' }).click();
      await expect(picker).toHaveCount(0);
      await expect(page.getByRole('textbox', { name: 'Сообщение', exact: true })).toHaveValue('Несохранённый текст');
      await expect(page.getByRole('log', { name: 'Переписка с магазином' })).toBeVisible();
    });
  }
}
