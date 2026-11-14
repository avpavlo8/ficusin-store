import { expect, test, type Page } from '@playwright/test';
import { mockApi, owner } from './helpers';

async function chat(page: Page, staff: boolean, consentRequired = false) {
  await mockApi(page, owner);
  let thread = { id: consentRequired ? 0 : 1, status: 'open', customerReadId: 0, staffReadId: 0,
    consentRequired, messages: [] as object[], hasMore: false };
  const sent: Array<{ text: string; consent: boolean }> = [];
  let finish!: () => void;
  const response = new Promise<void>(resolve => { finish = resolve; });
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({ json: {
    user: { fullName: 'Менеджер' }, role: 'manager', permissions: ['chat.manage'],
    dashboard: { products: 0, variants: 0, orders: 0, customers: 0, wholesalePending: 0, recentOrders: [] },
  } }));
  await page.route(/\/api\/v1\/admin\/chat\?/, route => route.fulfill({ json: {
    items: [{ id: 1, name: 'Проверка клавиатуры', status: 'open', preview: 'Переписка', unread: 0, updatedAt: '2026-11-14T09:00:00Z' }], hasMore: false,
  } }));
  const base = staff ? '/api/v1/admin/chat/1' : '/api/v1/chat';
  await page.route(url => url.pathname === base, route => route.fulfill({ json: thread }));
  await page.route(url => url.pathname === `${base}/read`, route => route.fulfill({ json: { ok: true } }));
  await page.route(url => url.pathname === `${base}/messages`, async route => {
    const body = route.request().postDataJSON();
    sent.push(body);
    await response;
    thread = { ...thread, id: 1, consentRequired: false, messages: [{
      id: 1, sender: staff ? 'staff' : 'customer', text: body.text, createdAt: '2026-11-14T09:01:00Z',
    }] };
    await route.fulfill({ json: thread });
  });
  await page.goto(staff ? '/admin?section=chat' : '/chat');
  if (staff) await page.getByRole('button', { name: /Проверка клавиатуры.*Переписка/ }).click();
  const input = page.getByRole('textbox', { name: 'Сообщение', exact: true });
  await expect(input).toBeEnabled();
  return { sent, finish, input };
}

for (const staff of [false, true]) {
  test(`@desktop @phone ${staff ? 'manager' : 'customer'} sends on Enter and keeps Shift+Enter as a newline`, async ({ page }) => {
    const { sent, finish, input } = await chat(page, staff);
    await input.fill('Первая строка');
    await input.press('Shift+Enter');
    await input.pressSequentially('Вторая строка');
    await expect(input).toHaveValue('Первая строка\nВторая строка');
    await expect(page.getByRole('button', { name: 'Отправить', exact: true })).toBeEnabled();
    expect(sent).toHaveLength(0);
    await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true, keyCode: 229 });
    await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', repeat: true });
    expect(sent).toHaveLength(0);
    await input.press('Enter');
    await expect.poll(() => sent.length).toBe(1);
    expect(sent[0].text).toBe('Первая строка\nВторая строка');
    await expect(input).toBeDisabled();
    finish();
    await expect(input).toBeEnabled();
    await expect(input).toHaveValue('');
    await expect(page.locator('.chat-message p')).toHaveText('Первая строка\nВторая строка');
  });
}

test('@desktop @phone Enter keeps a first customer message local until consent is checked', async ({ page }) => {
  const { sent, finish, input } = await chat(page, false, true);
  await input.fill('Первое сообщение');
  await input.press('Enter');
  expect(sent).toHaveLength(0);
  await expect(input).toHaveValue('Первое сообщение');
  await page.locator('.chat-consent input').check();
  await expect(page.getByRole('button', { name: 'Отправить', exact: true })).toBeEnabled();
  await input.press('Enter');
  await expect.poll(() => sent.length).toBe(1);
  expect(sent[0].consent).toBe(true);
  finish();
  await expect(input).toHaveValue('');
});
