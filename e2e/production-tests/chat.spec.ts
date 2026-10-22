import { expect, test } from '@playwright/test';

test('production support links open the private staff chat', async ({ page }) => {
  await page.goto('/contacts');
  await expect(page.locator('.store-footer-chat')).toHaveAttribute('href', '/chat');
  await expect(page.locator('.chat-launcher')).toHaveAttribute('href', '/chat');
  const response = await page.goto('/chat?from=%2Fcare%2Fyellow-leaves&topic=Почему%20желтеют%20листья');
  expect(response?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: /На связи,\s*Фикусин/ })).toBeVisible();
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', /noindex/);
  await expect(page.getByText('Загружаем переписку…', { exact: true })).toBeHidden();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.locator('.chat-topic a')).toHaveAttribute('href', '/care/yellow-leaves');
  await page.getByRole('textbox', { name: 'Сообщение', exact: true }).fill('Проверка формы без отправки');
  await expect(page.getByRole('button', { name: 'Отправить', exact: true })).toBeDisabled();
  await page.getByRole('checkbox').check();
  await expect(page.getByRole('button', { name: 'Отправить', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
  const thread = await page.request.get('/api/v1/chat');
  expect(thread.status()).toBe(200);
  expect(thread.headers()['cache-control']).toBe('no-store');
  expect((await thread.json()).messages).toEqual([]);
  // Do not submit synthetic support messages to production.
});
