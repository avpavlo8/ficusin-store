import { test, expect } from '@playwright/test';
import { mockApi, horizontalOverflow, owner } from './helpers';
const initial = { id: 0, status: 'open', customerReadId: 0, staffReadId: 0, messages: [] as object[], hasMore: false };
for (const device of ['@desktop','@phone']) {
  test(`quick chat retries safely and stays on the current page ${device}`, async ({ page }) => {
    await mockApi(page); let thread = { ...initial }; const attempts: Array<{ clientId: string; text: string; contextUrl: string }> = [];
    await page.route('**/api/v1/chat?*', route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat', route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat/messages', async route => {
      const body = route.request().postDataJSON(); attempts.push(body);
      if (attempts.length === 1) return route.fulfill({ status: 503, json: { error: 'Связь прервалась' } });
      thread = { ...thread, id: 7, messages: [{ id: 21, sender: 'customer', text: body.text, contextUrl: body.contextUrl, contextLabel: body.contextLabel, hasImage: false, createdAt: '2026-10-22T08:00:00Z' }] };
      await route.fulfill({ json: thread });
    });
    await page.goto('/chat?from=%2Fproduct%2Fficus&topic=Фикус');
    const dialog = page.getByRole('dialog', { name: 'Быстрый чат с Фикусином' });
    const input = dialog.getByRole('textbox', { name: 'Сообщение', exact: true });
    await input.fill('Как поливать фикус?'); await expect(dialog.getByRole('button', { name: 'Отправить', exact: true })).toBeDisabled();
    await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect(dialog.getByRole('alert')).toContainText('Текст сохранён'); await expect(input).toHaveValue('Как поливать фикус?');
    await dialog.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect(dialog.getByText('Как поливать фикус?', { exact: true })).toBeVisible();
    expect(attempts).toHaveLength(2); expect(attempts[0].clientId).toBe(attempts[1].clientId); expect(attempts[1].contextUrl).toBe('/product/ficus');
    await expect(input).toHaveValue(''); await expect(dialog.getByLabel('Отправлено', { exact: true })).toBeVisible();
    await input.fill('Черновик'); await dialog.getByRole('button', { name: 'Свернуть чат', exact: true }).click();
    await page.getByRole('button', { name: 'Написать нам', exact: true }).click(); await expect(input).toHaveValue('Черновик');
    expect(await horizontalOverflow(page)).toBe(0);
    const box = await dialog.getByRole('button', { name: 'Отправить', exact: true }).boundingBox(); expect(box!.y + box!.height).toBeLessThanOrEqual((page.viewportSize()?.height || 0) + 1);
  });
  test(`support actions open a quick window and account retains history ${device}`, async ({ page }) => {
    await mockApi(page, owner); await page.goto('/contacts');
    await expect(page.locator('.store-footer-chat')).toHaveAttribute('href', '/chat');
    await page.locator('.chat-launcher').click(); await expect(page).toHaveURL(/\/contacts$/);
    const dialog = page.getByRole('dialog', { name: 'Быстрый чат с Фикусином' }); await expect(dialog).toBeVisible();
    await dialog.getByRole('link', { name: 'Вся переписка в личном кабинете' }).click(); await expect(page).toHaveURL(/\/account\/chat$/);
    await expect(page.getByRole('navigation', { name: 'Разделы личного кабинета' }).getByRole('link', { name: /Сообщения/ })).toHaveClass('active');
    await expect(page.getByRole('heading', { name: 'Сообщения', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Сообщение', exact: true })).toBeVisible();
    expect(await horizontalOverflow(page)).toBe(0);
  });
}
test('account does not mark offscreen replies read and catches up every message @desktop', async ({ page }) => {
  await mockApi(page, owner); let fresh = false; const read: number[] = [];
  const first = { id: 1, sender: 'customer', text: 'Первый вопрос', hasImage: false, createdAt: '2026-10-22T08:00:00Z' };
  const replies = Array.from({ length: 125 }, (_, index) => ({ id: index + 2, sender: 'staff', text: `Ответ ${index + 1}`, hasImage: false, createdAt: '2026-10-22T08:01:00Z' }));
  await page.route(/\/api\/v1\/chat(?:\?.*)?$/, route => {
    const after = Number(new URL(route.request().url()).searchParams.get('after'));
    const all = fresh ? [first, ...replies] : [first]; const eligible = after ? all.filter(message => message.id > after) : all;
    return route.fulfill({ json: { ...initial, id: 1, messages: eligible.slice(0, 50), hasMore: eligible.length > 50 } });
  });
  await page.route('**/api/v1/chat/read', route => { read.push(route.request().postDataJSON().through); return route.fulfill({ json: { ok: true } }); });
  await page.goto('/account/chat'); await expect(page.getByText('Первый вопрос', { exact: true })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight)); fresh = true;
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.locator('.chat-message')).toHaveCount(126);
  expect(read).toEqual([]);
  await page.locator('.chat-message').last().scrollIntoViewIfNeeded();
  await expect.poll(() => Math.max(...read)).toBe(126);
});
