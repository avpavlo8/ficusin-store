import { test, expect } from '@playwright/test';
import { mockApi, horizontalOverflow } from './helpers';

for (const device of ['@desktop','@phone']) {
  test(`chat keeps failed draft, retries idempotently and restores context ${device}`, async ({ page }) => {
    await mockApi(page);
    let thread = { id: 0, status: 'open', customerReadId: 0, staffReadId: 0, messages: [] as object[], hasMore: false };
    const attempts: Array<{ clientId: string; text: string; contextUrl: string }> = [];
    await page.route('**/api/v1/chat', route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat/messages', async route => {
      const body = route.request().postDataJSON(); attempts.push(body);
      if (attempts.length === 1) return route.fulfill({ status: 503, json: { error: 'Связь прервалась' } });
      thread = { ...thread, id: 7, messages: [{ id: 21, sender: 'customer', text: body.text, contextUrl: body.contextUrl, contextLabel: body.contextLabel, hasImage: false, createdAt: '2026-10-22T08:00:00Z' }] };
      await route.fulfill({ json: thread });
    });
    await page.goto('/chat?from=%2Fproduct%2Fficus&topic=Фикус');
    const input = page.getByRole('textbox', { name: 'Сообщение', exact: true });
    await input.fill('Как поливать фикус?');
    await expect(page.getByRole('button', { name: 'Отправить', exact: true })).toBeDisabled();
    await page.getByRole('checkbox').check();
    await page.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Текст сохранён');
    await expect(input).toHaveValue('Как поливать фикус?');
    await page.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect(page.getByText('Как поливать фикус?', { exact: true })).toBeVisible();
    expect(attempts).toHaveLength(2); expect(attempts[0].clientId).toBe(attempts[1].clientId); expect(attempts[1].contextUrl).toBe('/product/ficus');
    await expect(input).toHaveValue(''); await expect(page.getByText('Отправлено', { exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Фикус', exact: true })).toHaveAttribute('href', '/product/ficus');
    expect(await horizontalOverflow(page)).toBe(0);
  });
  test(`store support actions open internal chat ${device}`, async ({ page }) => {
    await mockApi(page); await page.goto('/contacts');
    await expect(page.getByRole('link', { name: 'чате с сотрудником магазина', exact: true })).toHaveAttribute('href', '/chat');
    await expect(page.locator('.store-footer-chat')).toHaveAttribute('href', '/chat');
    await expect(page.locator('.chat-launcher')).toHaveAttribute('href', '/chat');
    const floatingCart = page.locator('.floating-cart');
    if (await floatingCart.isVisible()) {
      const cart = await floatingCart.boundingBox(); const chat = await page.locator('.chat-launcher').boundingBox();
      expect(chat!.y + chat!.height).toBeLessThan(cart!.y);
    }
    await page.locator('.chat-launcher').click(); await expect(page).toHaveURL(/\/chat$/);
    await expect(page.getByRole('heading', { name: /На связи,\s*Фикусин/ })).toBeVisible();
  });
}
