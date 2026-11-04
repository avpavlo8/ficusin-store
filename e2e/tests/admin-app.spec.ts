import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi, owner } from "./helpers";

async function setup(page: import('@playwright/test').Page) {
  await mockApi(page, owner);
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({ json: {
    user: { fullName: 'Александр' }, role: 'manager',
    permissions: ['orders.read', 'products.read', 'procurement.read'],
    dashboard: { products: 10, variants: 10, orders: 0, customers: 0, wholesalePending: 0, recentOrders: [] },
  } }));
}

test('@desktop @phone admin has independent app identity, install help and store link', async ({ page, request }) => {
  await setup(page);
  await page.goto('/admin');
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/admin.webmanifest?v=admin-v1');
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/assets/brand/admin-v1/app-180.png');
  const response = await request.get('/admin.webmanifest');
  const admin = await response.json();
  const store = await (await request.get('/manifest.webmanifest')).json();
  expect(admin.id).not.toBe(store.id);
  expect(admin.start_url).toBe('/admin');
  expect(admin.scope).toBe('/admin');
  expect(admin.icons[0].src).not.toBe(store.icons[0].src);
  expect(admin.shortcuts.map((item: {url: string}) => item.url)).toEqual(['/admin?section=orders', '/admin?section=procurement', '/admin?section=products']);
  for (const icon of admin.icons) expect((await request.get(icon.src)).ok()).toBeTruthy();
  // Установка — внизу меню: на телефоне в листе «Ещё», на компьютере в боковой панели.
  const phone = (page.viewportSize()?.width ?? 1280) <= 760;
  if (phone) await page.getByRole('button', { name: 'Ещё', exact: true }).click();
  const menu = page.locator(phone ? '.ws-more' : '.ws-sidebar');
  await menu.locator('.admin-app-install summary').click();
  await expect(menu.locator('.admin-app-install-panel')).toBeVisible();
  await expect(menu.locator('.admin-app-install-panel')).toContainText('Фикусин — управление');
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await menu.locator('.admin-app-install summary').click();
  await menu.getByRole('link', { name: 'Открыть магазин' }).click();
  await expect(page).toHaveURL('/');
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/manifest.webmanifest?v=app-logo-v2');
});

test('@desktop install prompt is captured before the lazy admin workspace opens', async ({ page }) => {
  await setup(page);
  await page.goto('/admin');
  await page.evaluate(() => {
    const event = new Event('beforeinstallprompt', { cancelable: true });
    Object.assign(event, { prompt: async () => { (window as unknown as { prompted: boolean }).prompted = true; }, userChoice: Promise.resolve({outcome: 'accepted'}) });
    window.dispatchEvent(event);
  });
  await page.locator('.ws-sidebar .admin-app-install summary').click();
  await page.locator('.ws-sidebar').getByRole('button', {name: 'Установить', exact: true}).click();
  await expect(page.locator('.admin-app-install')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as {prompted: boolean}).prompted)).toBe(true);
});

test('@desktop admin session expiry preserves the selected section after login', async ({ page }) => {
  await mockApi(page);
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({status: 401, json: {error: 'Требуется вход'}}));
  await page.goto('/admin?section=procurement');
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fadmin%3Fsection%3Dprocurement/);
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/admin.webmanifest?v=admin-v1');
});
