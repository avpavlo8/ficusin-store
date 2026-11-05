import { expect, type APIRequestContext, type Page } from '@playwright/test';

export async function fillStagingCreationBase(page: Page, request: APIRequestContext, name: string) {
  const form = page.getByRole('dialog');
  await expect(form.locator('.creation-fields h2')).toHaveText('Основное');
  await form.getByLabel('Название товара', { exact: true }).fill(name);
  await form.getByLabel('Латинское название', { exact: true }).fill('Testa staging');
  await form.getByLabel('Описание', { exact: true }).fill('Тестовая карточка для проверки мастера. Не публиковать на рабочем сайте.');
  const { categories } = await (await request.get('/api/v1/admin/categories')).json();
  const category = categories.find((item: any) => item.slug === 'plants');
  await form.getByLabel('Категория сайта', { exact: true }).selectOption(String(category.id));
  const { attributes } = await (await request.get(`/api/v1/admin/categories/${category.id}/attributes`)).json();
  const known = new Set(['pot_diameter_cm', 'height_cm', 'package_length_cm', 'package_width_cm', 'package_height_cm', 'package_weight_grams']);
  for (const field of attributes.filter((field: any) => field.required && !known.has(field.code))) {
    const label = `${field.name}${field.unit ? `, ${field.unit}` : ''} *`;
    if (field.dataType === 'boolean') await form.getByLabel(label, { exact: true }).selectOption('false');
    else if (field.dataType === 'enum') await form.getByLabel(label, { exact: true }).selectOption(field.options[0]);
    else if (field.dataType === 'multi_enum') await form.getByRole('group', { name: label, exact: true }).getByRole('checkbox').first().check();
    else await form.getByLabel(label, { exact: true }).fill(field.dataType === 'number' ? '10' : 'Тестовое значение');
  }
  await form.getByRole('button', { name: 'Далее', exact: true }).click();
  for (const [label, value] of [['Цена на сайте, ₽', '1490.99'], ['Диаметр горшка, см', '11.3'], ['Высота растения, см', '40'], ['Длина упаковки, см', '20'], ['Ширина упаковки, см', '20'], ['Высота упаковки, см', '40'], ['Вес в упаковке, г', '700']]) await form.getByLabel(label, { exact: true }).fill(value);
  await form.getByRole('button', { name: 'Далее', exact: true }).click();
  await form.getByLabel('Добавить фотографии', { exact: true }).setInputFiles('../public/assets/care/guide-v2/arrival.jpg');
  await expect(form.getByAltText('Фото товара 1')).toBeVisible();
  await form.getByRole('button', { name: 'Далее', exact: true }).click();
  await form.getByLabel('Каталог СБИС', { exact: true }).selectOption('STG-CATALOG');
  await form.getByRole('button', { name: 'Далее', exact: true }).click();
}
