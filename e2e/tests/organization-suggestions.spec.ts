import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mkdir } from 'node:fs/promises';
import { mockApi, horizontalOverflow } from './helpers';

const organization = { name: 'ООО «Тест Сад»', inn: '7707083893', kpp: '773601001', legalAddress: 'г Рязань, Тестовая улица, д 1' };
async function openWholesale(page: Page) {
  await mockApi(page);
  await page.goto('/register');
  await page.getByRole('radio', { name: /Оптовый/ }).check();
}

test('@desktop @phone organization name and INN suggestions fill readonly details and survive confirmation', async ({ page }, info) => {
  await openWholesale(page);
  await page.route('**/api/v1/organizations/suggest?*', route => route.fulfill({ json: { available: true, suggestions: [organization, { ...organization, kpp: '773601002', legalAddress: 'г Рязань, Тестовая улица, д 2' }] } }));
  const search = page.getByRole('combobox', { name: 'Название организации или ИНН', exact: true });
  await expect(page.locator('.organization-details')).toHaveCount(0);
  await search.fill('Тест Сад');
  await expect(page.getByRole('option')).toHaveCount(2);
  expect((await new AxeBuilder({ page }).include('.wholesale-fields').withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([]);
  if (process.env.ORG_REVIEW_OUTPUT) { await mkdir(process.env.ORG_REVIEW_OUTPUT, { recursive: true }); await page.screenshot({ path: `${process.env.ORG_REVIEW_OUTPUT}/${info.project.name}-suggestions.png`, fullPage: true }); }
  await search.press('ArrowDown'); await search.press('Enter');
  await expect(search).toHaveValue(organization.name);
  await expect(page.getByLabel('ИНН', { exact: true })).toHaveValue(organization.inn);
  await expect(page.getByLabel('КПП', { exact: true })).toHaveValue(organization.kpp);
  await expect(page.getByLabel('Юридический адрес', { exact: true })).toHaveValue(organization.legalAddress);
  await search.fill(organization.inn);
  await expect(page.locator('.organization-details')).toHaveCount(0);
  await expect(page.getByRole('option')).toHaveCount(2);
  await page.getByRole('option', { name: /773601002/ }).click();
  await expect(page.getByLabel('КПП', { exact: true })).toHaveValue('773601002');
  if (process.env.ORG_REVIEW_OUTPUT) await page.screenshot({ path: `${process.env.ORG_REVIEW_OUTPUT}/${info.project.name}-filled.png`, fullPage: true });
  for (const label of ['ИНН', 'КПП', 'Юридический адрес']) await expect(page.getByLabel(label, { exact: true })).toHaveJSProperty('readOnly', true);
  expect((await new AxeBuilder({ page }).include('.wholesale-fields').withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([]);
  await page.getByRole('radio', { name: /Розничный/ }).check();
  await page.getByRole('radio', { name: /Оптовый/ }).check();
  await expect(page.getByLabel('Юридический адрес', { exact: true })).toHaveValue('г Рязань, Тестовая улица, д 2');
  let payload: Record<string, unknown> = {};
  await page.route('**/api/v1/auth/request-code', route => route.fulfill({ json: { checkId: 'local-company', callPhonePretty: '+7 800 000-00-00' } }));
  await page.route('**/api/v1/auth/verify-code', route => { payload = route.request().postDataJSON(); return route.fulfill({ status: 202, json: {} }); });
  await page.getByLabel('Фамилия', { exact: true }).fill('Тестовая');
  await page.getByLabel('Имя', { exact: true }).fill('Анна');
  await page.getByLabel('Телефон', { exact: true }).fill('9000000000');
  await page.getByLabel('Email', { exact: true }).fill('test@example.com');
  await page.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Зарегистрироваться', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Подтвердите номер звонком' })).toBeVisible();
  await expect.poll(() => payload.companyName).toBe(organization.name);
  await page.getByRole('button', { name: 'Изменить данные' }).click();
  await expect(page.getByLabel('КПП', { exact: true })).toHaveValue('773601002');
  await expect(page.getByLabel('Юридический адрес', { exact: true })).toHaveValue('г Рязань, Тестовая улица, д 2');
  expect(payload.companyName).toBe(organization.name);
  expect(payload.inn).toBe(organization.inn);
  expect(payload.kpp).toBe('773601002');
  expect(await horizontalOverflow(page)).toBe(0);
});

test('@desktop @phone directory failures keep requisites hidden and block unselected organization', async ({ page }) => {
  await openWholesale(page);
  const search = page.getByRole('combobox', { name: 'Название организации или ИНН', exact: true });
  const lookup = page.locator('.organization-lookup').filter({ has: search });
  let mode = 'error';
  await page.route('**/api/v1/organizations/suggest?*', route => mode === 'error'
    ? route.fulfill({ status: 503, json: { error: 'Справочник временно недоступен. Попробуйте позже.' } })
    : route.fulfill({ json: { available: mode !== 'off', suggestions: [] } }));
  await search.fill('770');
  await expect(lookup.locator('.organization-status')).toContainText('временно недоступен');
  mode = 'off'; await search.fill('771');
  await expect(lookup.locator('.organization-status')).toContainText('пока не подключён');
  mode = 'empty'; await search.fill('772');
  await expect(lookup.locator('.organization-status')).toContainText('не найдена');
  await expect(page.locator('.organization-details')).toHaveCount(0);
  await page.getByLabel('Фамилия', { exact: true }).fill('Тестовая');
  await page.getByLabel('Имя', { exact: true }).fill('Анна');
  await page.getByLabel('Телефон', { exact: true }).fill('9000000000');
  await page.getByLabel('Email', { exact: true }).fill('test@example.com');
  await page.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Зарегистрироваться', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Выберите организацию из справочника');
  await expect(page.getByRole('option')).toHaveCount(0);
});

test('@desktop @phone stale organization response cannot replace newer suggestions', async ({ page }) => {
  await openWholesale(page);
  let release: (() => Promise<void>) | undefined;
  let started!: () => void;
  const firstStarted = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/api/v1/organizations/suggest?*', route => {
    if (new URL(route.request().url()).searchParams.get('q') === 'старое') {
      release = () => route.fulfill({ json: { available: true, suggestions: [{ ...organization, name: 'Старый результат' }] } }).catch(() => {});
      started(); return;
    }
    return route.fulfill({ json: { available: true, suggestions: [{ ...organization, name: 'Новый результат' }] } });
  });
  const search = page.getByRole('combobox', { name: 'Название организации или ИНН', exact: true });
  await search.fill('старое'); await firstStarted;
  await search.fill('новое');
  await expect(page.getByRole('option', { name: /Новый результат/ })).toBeVisible();
  await release?.();
  await expect(page.getByRole('option', { name: /Старый результат/ })).toHaveCount(0);
  await page.getByRole('option', { name: /Новый результат/ }).click();
  await expect(search).toHaveValue('Новый результат');
});
