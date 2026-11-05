import { expect, test, type Page } from '@playwright/test';
import { horizontalOverflow, mockApi, owner } from './helpers';

const fields = [
  { code: 'brand', name: 'Бренд', type: 'text', required: true, shared: true },
  { code: 'plantKind', name: 'Вид растения', type: 'text', required: true, shared: true },
  { code: 'country', name: 'Страна производства', type: 'select', required: true, options: ['Россия', 'Нидерланды'], shared: true },
  { code: 'contents', name: 'Комплектация', type: 'text', required: true, shared: true },
];
const options = { enabled: true, simulation: true, sabyFolders: [{ id: 'STG-CATALOG', name: 'Тестовый каталог СБИС' }], wb: [{ id: 'STG-WB-PLANTS', name: 'Комнатные растения — тестовая категория', fields }], ozon: [{ id: 'STG-OZON-PLANTS', name: 'Комнатные растения — тестовая категория', fields: [...fields, { code: 'tnved', name: 'Код ТН ВЭД', type: 'text', required: true }, { code: 'vat', name: 'НДС', type: 'text', required: true }] }] };
const categories = [{ id: 1, parentId: null, name: 'Растения', slug: 'plants', sortOrder: 1 }];
async function setup(page: Page, timeoutAfterFinish = false) {
  await mockApi(page, owner);
  let creations = 0;
  let draft: any = { id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', revision: 0, simulation: true, data: {}, photos: [], results: [] };
  await page.route('**/api/v1/admin/dashboard', route => route.fulfill({ json: { user: { fullName: 'Александр' }, role: 'owner', permissions: ['products.read', 'products.manage', 'products.edit'], dashboard: { products: 0, variants: 0, orders: 0, customers: 0, wholesalePending: 0, recentOrders: [] } } }));
  await page.route('**/api/v1/admin/products', route => route.fulfill({ json: { products: [] } }));
  await page.route('**/api/v1/admin/categories', route => route.fulfill({ json: { categories } }));
  await page.route('**/api/v1/admin/categories/1/attributes', route => route.fulfill({ json: { attributes: [{ code: 'light', name: 'Освещение', dataType: 'enum', options: ['diffused'], optionLabels: { diffused: 'Рассеянный свет' }, scope: 'product', required: true }] } }));
  await page.route('**/api/v1/admin/product-creations**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname; const method = request.method();
    if (path.endsWith('/options')) return route.fulfill({ json: options });
    if (path.endsWith('/photos') && method === 'POST') { draft.photos.push({ id: `photo${draft.photos.length + 1}`, filename: 'plant.jpg', url: '/assets/hero-monstera.webp' }); return route.fulfill({ json: draft }); }
    if (path.includes('/photos/') && method === 'DELETE') { const id = path.split('/').at(-1); draft.photos = draft.photos.filter((photo: any) => photo.id !== id); draft.revision++; return route.fulfill({ json: draft }); }
    if (path.endsWith('/finish')) {
      if (!draft.product) { creations++; draft.product = { id: 77, productCode: '77', name: draft.data.name, status: 'draft', stock: 0 }; draft.results = ['saby', ...(draft.data.wb.enabled ? ['wb'] : []), ...(draft.data.ozon.enabled ? ['ozon'] : [])].map(channel => ({ channel, externalId: `STG-${channel}-77`, state: 'simulated' })); }
      if (timeoutAfterFinish) return route.abort('timedout');
      return route.fulfill({ json: draft });
    }
    if (method === 'PUT') { const body = request.postDataJSON(); if (draft.product || body.revision !== draft.revision) return route.fulfill({ status: 409, json: { error: 'Черновик изменился' } }); draft = { ...draft, data: body.data, revision: draft.revision + 1 }; }
    return route.fulfill({ json: draft });
  });
  return { creations: () => creations, read: () => draft };
}
async function open(page: Page) {
  await page.goto('/admin?section=products');
  await page.getByRole('button', { name: 'Добавить товар', exact: true }).click();
  await expect(page.locator('.creation-fields h2')).toHaveText('Основное');
}
const dialog = (page: Page) => page.getByRole('dialog');
async function next(page: Page) { await dialog(page).getByRole('button', { name: 'Далее', exact: true }).click(); }
async function baseSteps(page: Page) {
  const form = dialog(page);
  await form.getByLabel('Название товара', { exact: true }).fill('Аглаонема Мария');
  await form.getByLabel('Латинское название', { exact: true }).fill('Aglaonema');
  await form.getByLabel('Описание', { exact: true }).fill('Растение для рассеянного света.');
  await form.getByLabel('Категория сайта', { exact: true }).selectOption('1');
  await form.getByLabel('Освещение *', { exact: true }).selectOption('diffused');
  await next(page);
  await form.getByLabel('Цена на сайте, ₽', { exact: true }).fill('1490.99');
  await form.getByLabel('Диаметр горшка, см', { exact: true }).fill('11.3');
  await form.getByLabel('Высота растения, см', { exact: true }).fill('40');
  await next(page);
  await form.getByLabel('Добавить фотографии', { exact: true }).setInputFiles({ name: 'plant.png', mimeType: 'image/png', buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+rGioAAAAASUVORK5CYII=', 'base64') });
  await expect(form.getByAltText('Фото товара 1')).toBeVisible();
  await next(page);
  await form.getByLabel('Каталог СБИС', { exact: true }).selectOption('STG-CATALOG');
  await next(page);
}

test('@desktop @phone wizard blocks incomplete steps, saves on reload and permits channel skips', async ({ page }, info) => {
  const state = await setup(page); await open(page);
  await expect(dialog(page).getByRole('button', { name: 'Далее', exact: true })).toBeDisabled();
  await expect(dialog(page)).toContainText('Укажите название товара');
  await baseSteps(page);
  expect(state.creations()).toBe(0);
  await expect(page).toHaveURL(/creation=aaaaaaaa/);
  await page.reload();
  await expect(page.locator('.creation-fields h2')).toHaveText('Wildberries');
  await dialog(page).getByRole('button', { name: 'Пропустить Wildberries', exact: true }).click();
  await dialog(page).getByRole('button', { name: 'Пропустить Ozon', exact: true }).click();
  await expect(dialog(page)).toContainText('1 490,99');
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: info.outputPath('wizard-review.png') });
  await dialog(page).getByRole('button', { name: 'Создать', exact: true }).click();
  await expect(dialog(page)).toContainText('Карточки созданы в стейджинге');
  await expect(dialog(page)).toContainText('Wildberries пропущен');
  expect(state.creations()).toBe(1);
  expect(state.read().data.priceMinor).toBe(149099);
  expect(state.read().data.photos).toEqual(['photo1']);
});

test('@desktop @phone Ozon receives only compatible WB fields and requires its own attributes', async ({ page }, info) => {
  const state = await setup(page, true); await open(page); await baseSteps(page);
  const form = dialog(page);
  await form.getByRole('button', { name: 'Заполнить Wildberries', exact: true }).click();
  await form.getByLabel('Категория Wildberries', { exact: true }).selectOption('STG-WB-PLANTS');
  await expect(form.getByRole('button', { name: 'Далее', exact: true })).toBeDisabled();
  await form.getByLabel('Wildberries: Бренд', { exact: true }).fill('Тестовый бренд');
  await form.getByLabel('Wildberries: Вид растения', { exact: true }).fill('Аглаонема');
  await form.getByLabel('Wildberries: Страна производства', { exact: true }).selectOption('Нидерланды');
  await form.getByLabel('Wildberries: Комплектация', { exact: true }).fill('Растение в горшке');
  for (const [label, value] of [['Длина упаковки, см', '20'], ['Ширина упаковки, см', '20'], ['Высота упаковки, см', '40'], ['Вес в упаковке, г', '700']]) await form.getByLabel(label, { exact: true }).fill(value);
  await next(page);
  await form.getByRole('button', { name: 'Заполнить Ozon', exact: true }).click();
  await form.getByLabel('Категория Ozon', { exact: true }).selectOption('STG-OZON-PLANTS');
  await expect(form.getByLabel('Ozon: Бренд', { exact: true })).toHaveValue('Тестовый бренд');
  await expect(form.getByLabel('Ozon: Страна производства', { exact: true })).toHaveValue('Нидерланды');
  await expect(form.getByLabel('Ozon: Код ТН ВЭД', { exact: true })).toHaveValue('');
  await expect(form.getByLabel('Ozon: НДС', { exact: true })).toHaveValue('');
  await expect(form.getByRole('button', { name: 'Далее', exact: true })).toBeDisabled();
  await page.screenshot({ path: info.outputPath('wizard-ozon.png') });
  await form.getByLabel('Ozon: Код ТН ВЭД', { exact: true }).fill('0000000000');
  await form.getByLabel('Ozon: НДС', { exact: true }).fill('Тестовая ставка');
  await next(page);
  await form.getByRole('button', { name: 'Создать', exact: true }).click();
  // A lost POST response is recovered by reading the same immutable draft.
  await expect(form).toContainText('Карточки созданы в стейджинге');
  await expect(form.locator('.creation-complete li')).toHaveCount(3);
  expect(state.creations()).toBe(1);
});
