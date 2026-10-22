import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi, horizontalOverflow, owner } from './helpers';
const initial = { id: 0, consentRequired: true, status: 'open', customerReadId: 0, staffReadId: 0, messages: [] as object[], hasMore: false };
test('a failed chat download preserves the storefront and offers recovery @desktop', async ({ page }) => {
  await mockApi(page);
  await page.route(/\/assets\/QuickChat-[^/]+\.js$/, route => route.abort());
  await page.goto('/'); await page.locator('.chat-launcher').click();
  await expect(page.getByRole('alert')).toContainText('Не удалось открыть чат');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await page.getByRole('button', { name: 'Закрыть', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
});
test('quick chat follows the panned keyboard viewport and restores the page @phone', async ({ page }) => {
  // Resizing a Playwright viewport alone does not reproduce Safari's keyboard pan.
  await page.addInitScript(() => {
    const viewport = new EventTarget();
    Object.assign(viewport, { height: innerHeight, offsetTop: 0 });
    Object.defineProperty(window, 'visualViewport', { value: viewport });
    (window as unknown as { panChatViewport: () => void }).panChatViewport = () => {
      Object.assign(viewport, { height: 330, offsetTop: 400 });
      viewport.dispatchEvent(new Event('resize'));
      viewport.dispatchEvent(new Event('scroll'));
    };
  });
  await mockApi(page); await page.goto('/');
  await page.locator('.chat-launcher').scrollIntoViewIfNeeded();
  const originalScroll = await page.evaluate(() => scrollY);
  await page.locator('.chat-launcher').click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox', { name: 'Сообщение', exact: true }).fill('Вопрос с телефона');
  await page.evaluate(() => (window as unknown as { panChatViewport: () => void }).panChatViewport());
  await expect.poll(async () => (await dialog.boundingBox())!.y).toBeGreaterThanOrEqual(400);
  const send = await dialog.getByRole('button', { name: 'Отправить', exact: true }).boundingBox();
  expect(send!.y).toBeGreaterThanOrEqual(400);
  expect(send!.y + send!.height).toBeLessThanOrEqual(730);
  expect(await page.locator('body').evaluate(element => getComputedStyle(element).position)).toBe('fixed');
  await dialog.getByRole('button', { name: 'Свернуть чат', exact: true }).click();
  await expect.poll(() => page.evaluate(() => scrollY)).toBeCloseTo(originalScroll, 0);
  expect(await page.locator('body').evaluate(element => getComputedStyle(element).position)).not.toBe('fixed');
});
test('landscape touch chat keeps attachment and emoji chooser inside the panel @phone', async ({ page }) => {
  await mockApi(page); await page.setViewportSize({ width: 844, height: 390 }); await page.goto('/chat');
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox', { name: 'Сообщение', exact: true }).fill('Строка\n'.repeat(100));
  await dialog.getByLabel('Добавить фото, видео или файл', { exact: true }).setInputFiles({ name: 'памятка.txt', mimeType: 'text/plain', buffer: Buffer.from('test') });
  await expect(dialog.locator('.chat-preview')).toBeVisible();
  await dialog.getByRole('button', { name: 'Смайлики', exact: true }).tap();
  await expect(dialog).toHaveAttribute('aria-modal', 'true');
  const panel = await dialog.boundingBox();
  const heading = await dialog.locator('.chat-panel-heading').boundingBox();
  const preview = await dialog.locator('.chat-preview').boundingBox();
  const emoji = await dialog.locator('.chat-emoji-panel').boundingBox();
  expect(preview!.y).toBeGreaterThanOrEqual(heading!.y + heading!.height);
  expect(emoji!.y).toBeGreaterThanOrEqual(heading!.y + heading!.height);
  expect(emoji!.y + emoji!.height).toBeLessThanOrEqual(panel!.y + panel!.height);
  await dialog.getByRole('button', { name: 'Вставить 🤗', exact: true }).tap();
  await expect(dialog.getByRole('textbox', { name: 'Сообщение', exact: true })).toHaveValue(/🤗/);
});
for (const device of ['@desktop','@phone']) {
  test(`chat notifications require opt-in and can be disabled without affecting orders ${device}`, async ({ page }) => {
    await mockApi(page);
    await page.addInitScript(() => {
      let permission = 'default'; let subscription: object | null = null;
      const serialized = { endpoint: 'https://fcm.googleapis.com/fcm/send/test-chat', expirationTime: null, keys: { p256dh: 'test-key', auth: 'test-auth' } };
      const mock = { endpoint: serialized.endpoint, toJSON: () => serialized, unsubscribe: () => { throw new Error('must not disable order subscriptions'); } };
      Object.defineProperty(window, 'PushManager', { configurable: true, value: class {} });
      Object.defineProperty(window, 'Notification', { configurable: true, value: { get permission() { return permission; }, requestPermission: async () => { permission = 'granted'; return permission; } } });
      const ready = Promise.resolve({ pushManager: { getSubscription: async () => subscription, subscribe: async () => { subscription = mock; return mock; } } });
      Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { register: async () => ready, ready } });
      Object.defineProperty(navigator, 'standalone', { configurable: true, value: true });
    });
    let enabled = false; const methods: string[] = [];
    await page.route('**/api/v1/push/key', route => route.fulfill({ json: { enabled: true, publicKey: 'BAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' } }));
    await page.route('**/api/v1/chat/notifications/state', route => route.fulfill({ json: { subscribed: enabled } }));
    await page.route('**/api/v1/chat/notifications', route => { methods.push(route.request().method()); enabled = route.request().method() === 'POST'; return route.fulfill({ json: { ok: true } }); });
    await page.goto('/chat');
    const bell = page.getByRole('button', { name: 'Включить уведомления о сообщениях', exact: true });
    await expect(bell).toBeVisible(); expect(methods).toEqual([]);
    await bell.click(); await expect(page.getByRole('button', { name: 'Выключить уведомления о сообщениях', exact: true })).toBeVisible();
    await expect(page.getByRole('status')).toContainText('Покажем текст новых сообщений');
    await page.getByRole('button', { name: 'Закрыть подсказку об уведомлениях', exact: true }).click();
    await page.getByRole('button', { name: 'Выключить уведомления о сообщениях', exact: true }).click();
    await expect(bell).toBeVisible(); expect(methods).toEqual(['POST', 'DELETE']);
  });
  test(`chat photo opens over the thread and closes back to its trigger ${device}`, async ({ page }) => {
    await mockApi(page);
    await page.route(/\/api\/v1\/chat(?:\?.*)?$/, route => route.fulfill({ json: { ...initial, id: 1, consentRequired: false, messages: [{ id: 21, sender: 'customer', text: 'Фото растения', hasImage: true, createdAt: '2026-10-22T08:00:00Z' }] } }));
    await page.route('**/api/v1/chat/photos/21', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="400"><rect width="100" height="400" fill="#173f32"/></svg>' }));
    await page.goto('/chat');
    const photo = page.getByRole('button', { name: 'Открыть фотографию', exact: true });
    await photo.click();
    const viewer = page.getByRole('dialog', { name: 'Просмотр фотографии', exact: true });
    await expect(viewer).toBeVisible(); await expect(page).toHaveURL(/\/chat$/);
    const close = viewer.getByRole('button', { name: 'Закрыть фотографию', exact: true });
    await expect(close).toBeFocused();
    for (let i = 0; i < 4; i++) { await page.keyboard.press('Tab'); expect(await viewer.evaluate(node => node.contains(document.activeElement))).toBe(true); }
    await close.click(); await expect(viewer).toHaveCount(0); await expect(photo).toBeFocused();
    await photo.click(); await page.keyboard.press('Escape'); await expect(viewer).toHaveCount(0);
    await expect(page.getByRole('dialog', { name: 'Быстрый чат с Фикусином', exact: true })).toBeVisible();
  });
  test(`quick chat retries safely and stays on the current page ${device}`, async ({ page }) => {
    await mockApi(page); let thread = { ...initial }; const attempts: Array<{ clientId: string; text: string; contextUrl: string }> = [];
    await page.route('**/api/v1/chat?*', route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat', route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat/messages', async route => {
      const body = route.request().postDataJSON(); attempts.push(body);
      if (attempts.length === 1) return route.fulfill({ status: 503, json: { error: 'Связь прервалась' } });
      thread = { ...thread, id: 7, consentRequired: false, messages: [{ id: 21, sender: 'customer', text: body.text, contextUrl: body.contextUrl, contextLabel: body.contextLabel, hasImage: false, createdAt: '2026-10-22T08:00:00Z' }] };
      await route.fulfill({ json: thread });
    });
    await page.goto('/chat?from=%2Fproduct%2Fficus&topic=Фикус');
    const dialog = page.getByRole('dialog', { name: 'Быстрый чат с Фикусином' });
    const input = dialog.getByRole('textbox', { name: 'Сообщение', exact: true });
    await expect(input).toBeVisible();
    await expect(dialog.getByText('История переписки — в', { exact: false })).toHaveCount(0);
    expect((await new AxeBuilder({ page }).include('#quick-chat').withTags(['wcag2a','wcag2aa']).analyze()).violations).toEqual([]);
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
    await page.goto('/account/chat'); await expect(page).toHaveURL(/\/account\/chat$/);
    if (device === '@desktop') {
      await expect(page.getByRole('navigation', { name: 'Разделы личного кабинета' }).getByRole('link', { name: /Сообщения/ })).toHaveClass('active');
      await expect(page.getByRole('heading', { name: 'Сообщения', exact: true })).toBeVisible();
    } else await expect(page.getByRole('link',{name:'Вернуться в личный кабинет',exact:true})).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Сообщение', exact: true })).toBeVisible();
    expect(await horizontalOverflow(page)).toBe(0);
  });
}
test('account does not mark offscreen replies read and catches up every message @desktop', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 300 });
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
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  // The shorter account menu must not leave the conversation partly visible.
  const panel = await page.locator('.chat-messages').boundingBox();
  expect(panel!.y + panel!.height).toBeLessThanOrEqual(0);
  fresh = true;
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.locator('.chat-message')).toHaveCount(126);
  expect(read).toEqual([]);
  await page.locator('.chat-message').last().scrollIntoViewIfNeeded();
  await expect.poll(() => Math.max(...read)).toBe(126);
});

for (const device of ['@desktop', '@phone']) {
  test(`account sends an attachment without focusing or typing in the composer ${device}`, async ({ page }) => {
    await mockApi(page, owner);
    await page.route(/\/api\/v1\/chat(?:\?.*)?$/, route => route.fulfill({ json: { ...initial, id: 4, consentRequired: false } }));
    let posted = false;
    await page.route('**/api/v1/chat/messages', route => {
      posted = true;
      return route.fulfill({ json: { ...initial, id: 4, consentRequired: false } });
    });
    await page.goto('/account/chat');
    const account = page.locator('.account-chat-conversation');
    await account.getByLabel('Добавить фото, видео или файл', { exact: true }).setInputFiles({ name: 'инструкция.txt', mimeType: 'text/plain', buffer: Buffer.from('private file') });
    await expect(account.locator('.chat-preview')).toBeVisible();
    await account.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect.poll(() => posted).toBe(true);
  });
  test(`registered user has one-row composer, emoji and attachments without repeated consent ${device}`, async ({ page }) => {
    await mockApi(page, owner);
    let thread = { ...initial, consentRequired: false }; let payload: Record<string, unknown> = {};
    await page.route(/\/api\/v1\/chat(?:\?.*)?$/, route => route.fulfill({ json: thread }));
    await page.route('**/api/v1/chat/messages', route => {
      payload = route.request().postDataJSON();
      const attachment = payload.attachment as { name: string } | undefined;
      thread = { ...thread, id: 4, messages: [{ id: 5, sender: 'customer', text: payload.text, attachmentKind: 'file', attachmentName: attachment?.name, attachmentSize: 12, hasImage: false, createdAt: '2026-10-22T08:00:00Z' }] };
      return route.fulfill({ json: thread });
    });
    await page.goto('/account/chat');
    const account = page.locator('.account-chat-conversation');
    const input = account.getByRole('textbox', { name: 'Сообщение', exact: true });
    await expect(input).toBeVisible(); await expect(account.getByRole('checkbox')).toHaveCount(0);
    await input.fill('Спасибо '); await account.getByRole('button', { name: 'Смайлики', exact: true }).click();
    await account.getByRole('button', { name: 'Вставить 💚', exact: true }).click(); await expect(input).toHaveValue('Спасибо 💚');
    await account.getByLabel('Добавить фото, видео или файл', { exact: true }).setInputFiles({ name: 'инструкция.txt', mimeType: 'text/plain', buffer: Buffer.from('private file') });
    await expect(account.locator('.chat-preview')).toContainText('инструкция.txt');
    await account.getByRole('button', { name: 'Отправить', exact: true }).click();
    await expect(account.getByRole('link', { name: /инструкция.txt/ })).toBeVisible();
    expect((await new AxeBuilder({ page }).include('.account-chat-conversation').withTags(['wcag2a','wcag2aa']).analyze()).violations).toEqual([]);
    expect(payload.consent).toBe(false); expect(payload.text).toBe('Спасибо 💚'); expect((payload.attachment as { name: string }).name).toBe('инструкция.txt');
    const accountInputId = await input.getAttribute('id');
    if (device === '@phone') await page.getByRole('link',{name:'Вернуться в личный кабинет',exact:true}).click();
    else await page.locator('.account-chat-conversation .chat-panel-heading').click();
    await page.locator('.chat-launcher').click();
    const dialog = page.getByRole('dialog', { name: 'Быстрый чат с Фикусином' });
    const quickInput = dialog.getByRole('textbox', { name: 'Сообщение', exact: true }); await expect(quickInput).toBeVisible();
    if (device === '@desktop') expect(accountInputId).not.toBe(await quickInput.getAttribute('id')); else await expect(quickInput).toHaveAttribute('id', /.+/);
    if (device === '@phone') {
      await expect(dialog).toHaveAttribute('aria-modal', 'true');
      expect(await page.locator('.account-page').evaluate(node => (node as HTMLElement).inert)).toBe(true);
      for (let i = 0; i < 12; i++) { await page.keyboard.press('Tab'); expect(await dialog.evaluate(node => node.contains(document.activeElement))).toBe(true); }
    }
    await dialog.getByRole('button', { name: 'Свернуть чат', exact: true }).click(); await expect(dialog).toBeHidden();
    await expect(page.locator('.chat-launcher')).toBeFocused();
  });
}
for (const device of ['@desktop', '@phone']) {
  test(`quick chat loads older history in pages without losing recent messages ${device}`, async ({ page }) => {
    await mockApi(page);
    const messages = Array.from({ length: 100 }, (_, index) => ({ id: index + 1, sender: index % 2 ? 'staff' : 'customer', text: `Сообщение ${index + 1}`, hasImage: false, createdAt: '2026-10-22T08:00:00Z' }));
    const cursors: string[] = [];
    await page.route(/\/api\/v1\/chat(?:\?.*)?$/, route => {
      const url = new URL(route.request().url());
      const before = url.searchParams.get('before');
      if (before) cursors.push(before);
      const result = before ? messages.slice(0, 50) : url.searchParams.has('after') ? [] : messages.slice(50);
      return route.fulfill({ json: { ...initial, id: 4, consentRequired: false, messages: result, hasMore: !before && !url.searchParams.has('after') } });
    });
    await page.goto('/chat');
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByText('Сообщение 100', { exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: 'Предыдущие сообщения', exact: true }).click();
    await expect(dialog.locator('[data-message-id]')).toHaveCount(100);
    await expect(dialog.getByRole('button', { name: 'Предыдущие сообщения', exact: true })).toHaveCount(0);
    expect(cursors).toEqual(['51']);
    await expect(dialog.getByText('Сообщение 1', { exact: true })).toHaveCount(1);
    await expect(dialog.getByText('Сообщение 100', { exact: true })).toHaveCount(1);
  });
}
