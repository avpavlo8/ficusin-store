import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { mockApi, owner } from './helpers';

// Vite preview does not emit the Go server's CSP. Exercise the deployed policy,
// otherwise a blocked blob preview looks healthy in every layout test.
const security = readFileSync(resolve(__dirname, '../../backend/internal/httpapi/security.go'), 'utf8');
const declaration = security.match(/const contentSecurityPolicy = ([\s\S]*?)\n\n/);
if (!declaration) throw new Error('Go Content-Security-Policy declaration not found');
const policy = [...declaration[1].matchAll(/"([^"]*)"/g)].map(match => match[1]).join('');
const webp = readFileSync(resolve(__dirname, '../../public/assets/hero-monstera.webp'));

async function fixture(page: Page) {
  await mockApi(page, owner);
  await page.route('**/*', async route => {
    if (route.request().resourceType() !== 'document') return route.fallback();
    const response = await route.fetch();
    return route.fulfill({ response, headers: { ...response.headers(), 'content-security-policy': policy } });
  });
  await page.addInitScript(() => {
    const violations: string[] = [];
    (window as unknown as { mediaPolicyViolations: string[] }).mediaPolicyViolations = violations;
    document.addEventListener('securitypolicyviolation', event => {
      if (event.effectiveDirective === 'img-src' || event.effectiveDirective === 'media-src') violations.push(event.effectiveDirective);
    });
  });
  let writes = 0;
  await page.route('**/api/v1/admin/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (!['GET', 'HEAD'].includes(request.method())) writes++;
    const json = path.endsWith('/dashboard')
      ? { user: { fullName: 'Проверка' }, role: 'owner', permissions: ['products.read', 'products.edit', 'products.manage'], dashboard: { products: 0, variants: 0, orders: 0, customers: 0, wholesalePending: 0, recentOrders: [] } }
      : path.endsWith('/products') ? { products: [] }
      : path.endsWith('/categories') ? { categories: [{ id: 1, name: 'Растения', slug: 'plants', sortOrder: 1, parentId: null, childrenCount: 0, productsCount: 0 }] }
      : path.endsWith('/collection-definitions') ? { collections: [] }
      : path.endsWith('/settings') ? { values: { 'cdek.packaging_season': 'summer' } }
      : path.endsWith('/references') ? { categories: [], folders: [] }
      : { attributes: [] };
    return route.fulfill({ json });
  });
  await page.goto('/admin?section=products&create=1');
  await expect(page.locator('.editor-page')).toBeVisible();
  // macOS's ephemeral WebKit datastore can reject every native Blob, even
  // independently of our editor. Probe a separate test database; supported
  // WebKit runtimes still run the full upload/reload assertions below.
  const capability = await page.evaluate(async () => {
    const name = 'ficusin-e2e-native-blob-probe';
    let db: IDBDatabase | undefined;
    try {
      db = await new Promise<IDBDatabase>((resolve, reject) => {
        const request = indexedDB.open(name, 1);
        request.onupgradeneeded = () => request.result.createObjectStore('probe');
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      await new Promise<void>((resolve, reject) => {
        const tx = db!.transaction('probe', 'readwrite');
        tx.objectStore('probe').put(new Blob(['probe']), 'blob');
        tx.oncomplete = () => resolve();
        tx.onabort = () => reject(tx.error);
      });
      return { name: '', message: '' };
    } catch (error) {
      return { name: (error as Error).name, message: (error as Error).message };
    } finally {
      db?.close();
      indexedDB.deleteDatabase(name);
    }
  });
  test.skip(capability.name === 'UnknownError' && /Error preparing Blob\/File data to be stored in object store/.test(capability.message),
    'Browser runtime rejects native IndexedDB Blob persistence in an independent probe (UnknownError: Error preparing Blob/File data to be stored in object store)');
  expect(capability).toEqual({ name: '', message: '' });
  return { writes: () => writes };
}

test('@desktop @phone uploaded WebP previews survive draft reload and cover changes under the Go CSP', async ({ page }, info) => {
  const f = await fixture(page);
  const gallery = page.getByRole('complementary', { name: 'Фото и видео товара' });
  const upload = gallery.locator('input[type="file"]');
  await upload.setInputFiles({ name: '4 (1).webp', mimeType: 'image/webp', buffer: webp });
  await expect(upload).toBeEnabled();
  const cover = gallery.locator('.editor-gallery-cover img');
  await expect(cover).toHaveAttribute('alt', '4 (1).webp');
  await expect.poll(() => cover.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
  await upload.setInputFiles({ name: 'Ещё одно.webp', mimeType: 'image/webp', buffer: webp });
  const images = gallery.locator('img');
  await expect(images).toHaveCount(2);
  const assertDecoded = async () => {
    await expect.poll(() => images.evaluateAll(nodes => nodes.every(node => (node as HTMLImageElement).complete && (node as HTMLImageElement).naturalWidth > 0))).toBe(true);
    expect(await page.evaluate(() => (window as unknown as { mediaPolicyViolations: string[] }).mediaPolicyViolations)).toEqual([]);
  };
  await assertDecoded();
  // The accessible reorder path remounts a thumbnail as the shared cover.
  await gallery.locator('.editor-photo-thumb').press('Alt+ArrowLeft');
  await expect(cover).toHaveAttribute('alt', 'Ещё одно.webp');
  await assertDecoded();
  await page.reload();
  await expect(images).toHaveCount(2);
  await expect(cover).toHaveAttribute('alt', 'Ещё одно.webp');
  await assertDecoded();
  await gallery.screenshot({ path: info.outputPath('restored-webp-gallery.png') });
  await page.getByRole('button', { name: 'Закрыть редактор', exact: true }).click();
  await page.getByRole('button', { name: 'Добавить товар', exact: true }).click();
  await expect(images).toHaveCount(2);
  await assertDecoded();
  expect(f.writes()).toBe(0);
});

test('@desktop @phone an unreadable image shows an explanation without discarding its draft', async ({ page }) => {
  const f = await fixture(page);
  const gallery = page.getByRole('complementary', { name: 'Фото и видео товара' });
  await gallery.locator('input[type="file"]').setInputFiles({ name: 'Повреждённое.webp', mimeType: 'image/webp', buffer: Buffer.from('not an image') });
  const cover = gallery.locator('.editor-gallery-cover');
  await expect(cover.getByRole('img', { name: 'Повреждённое.webp', exact: true })).toContainText('Не удалось открыть файл');
  await expect(cover.locator('img')).toHaveCount(0);
  await page.reload();
  await expect(cover.getByRole('img', { name: 'Повреждённое.webp', exact: true })).toContainText('Не удалось открыть файл');
  await expect(cover.locator('img')).toHaveCount(0);
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('ficusin:product-creation-draft:v1')!));
  expect(saved.cover.name).toBe('Повреждённое.webp');
  expect(f.writes()).toBe(0);
});
