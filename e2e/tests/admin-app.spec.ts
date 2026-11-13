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

test('@desktop @phone admin keeps app identity and links its logo to the store', async ({ page, request }) => {
  await setup(page);
  await page.goto('/admin');
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/admin.webmanifest?v=admin-v1');
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/assets/brand/admin-v1/app-180.png');
  const response = await request.get('/admin.webmanifest');
  const admin = await response.json();
  const store = await (await request.get('/manifest.webmanifest')).json();
  expect(admin.id).not.toBe(store.id);
  expect(admin.start_url).toBe('/admin?section=orders');
  expect(admin.scope).toBe('/admin');
  expect(admin.icons[0].src).not.toBe(store.icons[0].src);
  expect(admin.shortcuts.map((item: {url: string}) => item.url)).toEqual(['/admin?section=orders', '/admin?section=procurement', '/admin?section=products']);
  for (const icon of admin.icons) expect((await request.get(icon.src)).ok()).toBeTruthy();
  const phone = (page.viewportSize()?.width ?? 1280) <= 760;
  await expect(page.locator('.ws-account-access')).toHaveCount(0);
  await expect(page.locator('.ws-topbar').getByRole('link',{name:'Личный кабинет',exact:true})).toHaveCount(0);
  await expect(page.locator('.ws-topbar').getByRole('link',{name:'Панель управления',exact:true})).toHaveCount(0);
  await expect(page.locator('.admin-app-install')).toHaveCount(0);
  await expect(page.getByRole('link',{name:'Открыть магазин'})).toHaveCount(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  const brand=page.locator(phone ? '.ws-topbar-brand' : '.ws-brand');
  await expect(brand.getByRole('link',{name:'Фикусин — магазин',exact:true})).toHaveAttribute('href','/');
  await expect(brand.getByRole('link',{name:'Управление',exact:true})).toHaveAttribute('href','/admin');
  await brand.getByRole('link',{name:'Фикусин — магазин',exact:true}).click();
  await expect(page).toHaveURL('/');
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/manifest.webmanifest?v=app-logo-v2');
});

test('@desktop admin retains install manifest without displaying an install promotion', async ({ page }) => {
 await setup(page);await page.goto('/admin');
 const prevented=await page.evaluate(()=>{const event=new Event('beforeinstallprompt',{cancelable:true});Object.assign(event,{prompt:async()=>{},userChoice:Promise.resolve({outcome:'dismissed'})});window.dispatchEvent(event);return event.defaultPrevented;});
 expect(prevented).toBe(true);await expect(page.locator('.admin-app-install')).toHaveCount(0);
 await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href','/admin.webmanifest?v=admin-v1');
});

test('@desktop admin session expiry preserves the selected section after login', async ({ page }) => {
  await mockApi(page);
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({status: 401, json: {error: 'Требуется вход'}}));
  await page.goto('/admin?section=procurement');
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fadmin%3Fsection%3Dprocurement/);
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/admin.webmanifest?v=admin-v1');
});

test('@desktop @phone existing installed admin starts with new orders and preserves explicit routes', async ({ page }) => {
  await setup(page);
  await page.addInitScript(() => Object.defineProperty(navigator, 'standalone', { value: true }));
  await page.route('**/api/v1/admin/orders', route => route.fulfill({ json: { orders: [] } }));
  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: 'Заказы', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Новые/ })).toHaveAttribute('aria-current', 'page');
  await page.goto('/admin?section=dashboard');
  await expect(page.locator('.admin-page')).toHaveAttribute('data-section', 'dashboard');
});
