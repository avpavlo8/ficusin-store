import { expect, test } from '@playwright/test';
import { horizontalOverflow, mockApi, owner } from './helpers';

test('@desktop @phone homepage explains loyalty and links directly to registration', async ({ page }, info) => {
  await mockApi(page);
  await page.goto('/');
  const benefits = page.locator('.home-benefits');
  await expect(benefits.getByRole('link', { name: 'Зарегистрироваться', exact: true })).toHaveAttribute('href', '/register');
  await expect(benefits).toContainText('до 10%');
  await expect(benefits).toContainText('10 000 ₽ выполненных заказов');
  const install = benefits.locator('.home-install');
  await expect(install).toBeVisible();
  await install.getByRole('button', { name: 'Как установить', exact: true }).click();
  await expect(install.locator('ol')).toBeVisible();
  expect(await horizontalOverflow(page)).toBe(0);
  await benefits.scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath('home-benefits.png') });
  await benefits.getByRole('link', { name: 'Зарегистрироваться', exact: true }).click();
  await expect(page).toHaveURL(/\/register$/);
});

test('@desktop @phone signed-in customers see their account instead of registration', async ({ page }) => {
  await mockApi(page, owner);
  await page.goto('/');
  await expect(page.locator('.home-benefits').getByRole('link', { name: 'Моя скидка' })).toHaveAttribute('href', '/account');
  await expect(page.locator('.home-benefits')).not.toContainText('Зарегистрируйтесь');
});

test('@desktop native install prompt is used once and installed app hides invitation', async ({ page }) => {
  await mockApi(page);
  await page.goto('/');
  await expect(page.locator('.home-install')).toBeVisible();
  await page.evaluate(() => {
    const event = new Event('beforeinstallprompt', { cancelable: true });
    Object.assign(event, { prompt: async () => { document.body.dataset.installRequested = 'yes'; }, userChoice: Promise.resolve({ outcome: 'accepted' }) });
    window.dispatchEvent(event);
  });
  await page.locator('.home-install').getByRole('button', { name: 'Установить', exact: true }).click();
  await expect(page.locator('body')).toHaveAttribute('data-install-requested', 'yes');
  await expect(page.locator('.home-install')).toHaveCount(0);
});

test('@desktop installed app never asks for notification permission on page load', async ({ page }) => {
  await mockApi(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'standalone', { value: true });
    Object.defineProperty(window, 'Notification', { value: { permission: 'default', requestPermission: () => { document.body.dataset.permissionRequested = 'yes'; return Promise.resolve('denied'); } } });
    Object.defineProperty(window, 'PushManager', { value: function () {} });
  });
  await page.route('**/api/v1/push/key', route => route.fulfill({ json: { enabled: true, publicKey: 'test' } }));
  await page.goto('/');
  await expect(page.locator('.home-install').getByRole('button', { name: 'Включить', exact: true })).toBeVisible({ timeout: 12000 });
  await expect(page.locator('body')).not.toHaveAttribute('data-permission-requested', 'yes');
  await page.locator('.home-install').getByRole('button', { name: 'Включить', exact: true }).click();
  await expect(page.locator('.home-install').getByRole('alert')).toContainText('запрещены');
});
