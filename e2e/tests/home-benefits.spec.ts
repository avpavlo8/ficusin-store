import { expect, test } from '@playwright/test';
import { mockApi, owner } from './helpers';

test('@desktop native install prompt is used once and installed app hides invitation', async ({ page }) => {
  await mockApi(page,undefined,{showInstallOffer:true});
  await page.goto('/');
  await expect(page.getByRole('dialog',{name:'Фикусин на экране Домой'})).toBeVisible();
  await page.evaluate(() => {
    const event = new Event('beforeinstallprompt', { cancelable: true });
    Object.assign(event, { prompt: async () => { document.body.dataset.installRequested = 'yes'; }, userChoice: Promise.resolve({ outcome: 'accepted' }) });
    window.dispatchEvent(event);
  });
  await page.locator('.home-install').getByRole('button', { name: 'Установить', exact: true }).click();
  await expect(page.locator('body')).toHaveAttribute('data-install-requested', 'yes');
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
 const offer=page.getByRole('dialog',{name:'Фикусин на экране Домой'});
 await expect(offer).toBeVisible();await offer.getByRole('button',{name:'Закрыть предложение установки'}).click();
 await page.reload();await expect(page.locator('.storefront-grid')).toBeVisible();await expect(offer).toHaveCount(0);
 await page.goto('/account/profile');await expect(page.locator('.account-app-settings .home-install')).toBeVisible();
});
