import { expect, test } from '@playwright/test';
import { mockApi, owner, horizontalOverflow } from './helpers';

test('@desktop @phone native install prompt is used once and installed app hides invitation', async ({ page }) => {
  await mockApi(page,undefined,{showInstallOffer:true});
  await page.goto('/');
  await expect(page.getByRole('complementary',{name:'Фикусин на экране Домой',exact:true})).toBeVisible();
  await page.evaluate(() => {
    const event = new Event('beforeinstallprompt', { cancelable: true });
    Object.assign(event, { prompt: async () => { document.body.dataset.installRequested = String(Number(document.body.dataset.installRequested || 0) + 1); }, userChoice: Promise.resolve({ outcome: 'accepted' }) });
    window.dispatchEvent(event);
  });
  await page.locator('.home-install').getByRole('button', { name: 'Установить приложение', exact: true }).click();
  await expect(page.locator('body')).toHaveAttribute('data-install-requested', '1');
  await expect(page.locator('.home-install')).toHaveCount(0);
});

test('@desktop installed app offers notification switches in account without prompting on load', async ({ page }) => {
  await mockApi(page,owner);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'standalone', { value: true });
    Object.defineProperty(window, 'Notification', { value: { permission: 'default', requestPermission: () => { document.body.dataset.permissionRequested = 'yes'; return Promise.resolve('denied'); } } });
    Object.defineProperty(window, 'PushManager', { value: function () {} });
  });
  await page.route('**/api/v1/push/key', route => route.fulfill({ json: { enabled: true, publicKey: 'test' } }));
  await page.goto('/account/profile');
  await expect(page.getByRole('switch',{name:'Уведомления о заказах'})).toBeVisible();
  await expect(page.getByRole('switch',{name:'Уведомления о сообщениях'})).toBeVisible();
  await expect(page.locator('body')).not.toHaveAttribute('data-permission-requested', 'yes');
  await page.getByRole('switch',{name:'Уведомления о заказах'}).click();
  await expect(page.locator('.customer-push-settings').getByRole('alert')).toContainText('Разрешите уведомления');
});

test('@desktop @phone first homepage install offer closes once and stays available in account',async({page})=>{
 await mockApi(page,owner,{showInstallOffer:true});await page.goto('/');
 const offer=page.getByRole('complementary',{name:'Фикусин на экране Домой',exact:true});
 await expect(offer).toBeVisible();await offer.getByRole('button',{name:'Закрыть предложение установки'}).click();
 await page.reload();await expect(page.locator('.storefront-grid')).toBeVisible();await expect(offer).toHaveCount(0);
 await page.goto('/account/profile');await expect(page.locator('.account-app-settings .home-install')).toBeVisible();
});


test('@desktop @phone homepage installation banner leaves page, navigation and chat usable', async ({ page, browserName, isMobile }) => {
  await mockApi(page, undefined, { showInstallOffer: true });
  await page.addInitScript(() => {
    Object.defineProperty(window, 'Notification', { configurable: true, value: { permission: 'default', requestPermission: () => { document.body.dataset.permissionRequested = 'yes'; return Promise.resolve('denied'); } } });
  });
  await page.goto('/');
  const offer = page.getByRole('complementary', { name: 'Фикусин на экране Домой', exact: true });
  await expect(offer).toBeVisible();
  await expect(page.locator('dialog[open], [aria-modal="true"]')).toHaveCount(0);
  expect(await page.locator('body').evaluate(body => getComputedStyle(body).overflow)).not.toBe('hidden');
  expect(await offer.evaluate(element => element.contains(document.activeElement))).toBe(false);
  const banner = await offer.boundingBox();
  const chat = await page.getByRole('button', { name: /^Написать нам/ }).boundingBox();
  expect(banner).not.toBeNull(); expect(chat).not.toBeNull();
  expect(banner!.y + banner!.height).toBeLessThanOrEqual(chat!.y - 8);
  const navigation = page.locator('.tab-bar');
  if (await navigation.isVisible()) {
    const bar = await navigation.boundingBox();
    expect(banner!.y + banner!.height).toBeLessThanOrEqual(bar!.y);
  }
  const before = await page.evaluate(() => window.scrollY);
  // Mobile WebKit does not implement wheel injection; use its native element scrolling.
  if (browserName === 'webkit' && isMobile) await page.locator('.storefront-grid').scrollIntoViewIfNeeded();
  else { await page.mouse.move(24, 200); await page.mouse.wheel(0, 350); }
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(before);
  await expect(offer).toBeVisible();
  await page.getByRole('button', { name: 'Список', exact: true }).click();
  await expect(page.locator('.storefront-grid')).toHaveClass(/list-view/);
  await page.getByRole('button', { name: /^Написать нам/ }).click();
  await expect(page.locator('body')).toHaveClass(/quick-chat-open/);
  await expect(offer).toBeHidden();
  expect(await horizontalOverflow(page)).toBe(0);
  await expect(page.locator('body')).not.toHaveAttribute('data-permission-requested', 'yes');
});
