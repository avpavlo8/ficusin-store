// Real built app + PostgreSQL, with isolated synthetic CI rows; no API mocks.
import { chromium, expect } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
const base = process.env.CRM_BASE_URL || 'http://127.0.0.1:8080';
const out = process.env.CRM_ARTIFACT_DIR || 'crm-artifacts';
await mkdir(out, {recursive:true});
const browser = await chromium.launch({headless:true});
const results = [];
try {
 for (const role of ['owner','manager']) {
  const context = await browser.newContext({baseURL:base,serviceWorkers:'block'});
  await context.addCookies([{name:'ficusin_session',value:`crm-acceptance-${role}`,url:base}]);
  const page = await context.newPage();
  const api = context.request;
  const procurementDraft = JSON.stringify({supplierId:77,exchangeRate:"91.25",items:[{sabyId:"unsaved-ci",quantity:3}]});
  if(role==='owner') {
   await page.goto('/admin');
   await page.evaluate(([key,value])=>localStorage.setItem(key,value),['ficusin:procurement-plan-draft:v1',procurementDraft]);
  }
  const response = await api.get('/api/v1/admin/dashboard');
  expect(response.status()).toBe(200);
  const data = await response.json();expect(data.role).toBe(role);
  const products = (await (await api.get('/api/v1/admin/products')).json()).products;
  const product = products.find(p=>p.slug==='crm-acceptance-ficus');expect(product).toBeTruthy();
  const variants=(await (await api.get(`/api/v1/admin/products/${product.id}/variants`)).json()).variants;
  const variant=variants[0];expect(variant).toBeTruthy();
  const relationshipsResponse=await api.get(`/api/v1/admin/products/${product.id}/relationships`);expect(relationshipsResponse.status()).toBe(200);
  const relationships=(await relationshipsResponse.json()).relationships;expect(Array.isArray(relationships.mappings)).toBeTruthy();expect(Array.isArray(relationships.suppliers)).toBeTruthy();
  const beforeMedia=(await (await api.get(`/api/v1/admin/products/${product.id}/media`)).json()).media;
  expect(beforeMedia.length).toBe(2);
  if(role==='manager') {
   for(const path of ['/admin?section=procurement','/admin?section=marketplaces','/admin/settings','/api/v1/admin/analytics','/api/v1/admin/procurement/orders/18/saby-prices.xlsx']) expect((await api.get(path)).status()).toBe(403);
   for(const body of [{name:'Forbidden',priceMinor:1},{name:'Forbidden',stock:null},{name:'Forbidden',externalIds:[]}]) expect((await api.patch(`/api/v1/admin/products/${product.id}`,{data:body})).status()).toBe(403);
   expect((await api.patch(`/api/v1/admin/variants/${variant.id}`,{data:{label:'Forbidden',priceMinor:1}})).status()).toBe(403);
   expect((await api.patch(`/api/v1/admin/variants/${variant.id}`,{data:{label:'Forbidden',attributes:{external_price:1}}})).status()).toBe(403);
   const ok=await api.patch(`/api/v1/admin/variants/${variant.id}`,{data:{label:'D12 проверено',attributes:{package_weight_grams:1200}}});
   expect(ok.status(),await ok.text()).toBe(200);
   const saved=(await ok.json()).variant;expect(saved.price).toBe(variant.price);expect(saved.stock).toBe(variant.stock);expect(saved.externalIds).toEqual(variant.externalIds);
   expect(saved.attributes.package_weight_grams).toBe(1200);
  }
  const edit=await api.patch(`/api/v1/admin/products/${product.id}`,{data:{description:`Контент ${role}`,...(role==='owner'?{image:product.image}:{})}});
  expect(edit.status(),await edit.text()).toBe(200);
  const afterMedia=(await (await api.get(`/api/v1/admin/products/${product.id}/media`)).json()).media;
  expect(afterMedia).toEqual(beforeMedia);
  const emptyCollection=await api.post('/api/v1/admin/collection-definitions',{data:{slug:`crm-empty-${role}`,title:'Пустая',coverUrl:'',mode:'manual',rules:[]}});expect(emptyCollection.status()).toBe(400);
  await page.goto('/admin?section=collections');await expect(page.getByRole('heading',{name:'Подборки',exact:true})).toBeVisible();
  const collectionCountBefore=((await (await api.get('/api/v1/admin/collection-definitions')).json()).collections||[]).length;
  await page.getByRole('button',{name:'Новая подборка',exact:true}).click();
  expect(((await (await api.get('/api/v1/admin/collection-definitions')).json()).collections||[]).length).toBe(collectionCountBefore);
  const collectionTitle=`CRM подборка ${role}`;await page.getByLabel('Название',{exact:true}).fill(collectionTitle);await page.getByLabel('Slug',{exact:true}).fill(`crm-stage-03-${role}`);await page.getByLabel('Адрес изображения',{exact:true}).fill('/assets/hero-monstera.webp');await page.getByLabel('Показывать после создания',{exact:false}).check();const collectionCreateResponse=page.waitForResponse(response=>response.url().endsWith('/api/v1/admin/collection-definitions/with-cover')&&response.request().method()==='POST');await page.getByRole('button',{name:'Создать подборку',exact:true}).click();const savedCollectionResponse=await collectionCreateResponse;expect(savedCollectionResponse.status(),await savedCollectionResponse.text()).toBe(201);
  await expect(page.getByText(collectionTitle,{exact:true}).first()).toBeVisible();
  const createdCollections=(await (await api.get('/api/v1/admin/collection-definitions')).json()).collections;const createdCollection=createdCollections.find(item=>item.slug===`crm-stage-03-${role}`);expect(createdCollection.coverUrl).toBe('/assets/hero-monstera.webp');
  const memberResponse=await api.patch(`/api/v1/admin/collections/${createdCollection.id}`,{data:{products:[product.id]}});expect(memberResponse.status()).toBe(200);
  await page.reload();await expect(page.getByText(collectionTitle,{exact:true}).first()).toBeVisible();await page.screenshot({path:`${out}/${role}-collections-1440.png`,fullPage:true});
  await page.goto('/');await expect(page.getByText(collectionTitle,{exact:true}).first()).toBeVisible();
  for(const width of [1440,390]) {
   await page.setViewportSize({width,height:1000});
   await page.goto('/admin?section=orders');
   await expect(page.getByRole('heading',{name:'Заказы',exact:true})).toBeVisible();
   await expect(page.getByText('CRM-CHECK-01',{exact:true})).toBeVisible();
   expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
   await page.screenshot({path:`${out}/${role}-orders-${width}.png`,fullPage:true});
   await page.goto('/admin?section=customers');
   await expect(page.getByRole('heading',{name:'Клиенты',exact:true})).toBeVisible();
   await page.getByRole('button',{name:role==='owner'?'Изменить':'Открыть',exact:true}).first().click();
   await expect(page.getByRole('dialog')).toBeVisible();
   if(role==='manager') await expect(page.getByRole('button',{name:'Сохранить',exact:true})).toHaveCount(0);
   await page.screenshot({path:`${out}/${role}-customer-${width}.png`,fullPage:true});
   await page.goto('/admin/categories');
   await expect(page.getByRole('heading',{name:'Категории и атрибуты',exact:true})).toBeVisible();
   await page.screenshot({path:`${out}/${role}-categories-${width}.png`,fullPage:true});
   await page.reload();await expect(page.getByRole('heading',{name:'Категории и атрибуты',exact:true})).toBeVisible();
   if(role==='owner') {
    await page.goto('/admin?section=marketplaces');
    await expect(page.getByRole('heading',{name:'Маркетплейсы',exact:true})).toBeVisible();
    await expect(page.getByRole('heading',{name:'История обмена',exact:true})).toBeVisible();
    await expect(page.getByText('Ноль новых строк означает только', {exact:false})).toBeVisible();
    expect(await page.locator('tbody tr').count()).toBe(6);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
    await page.screenshot({path:`${out}/owner-marketplaces-${width}.png`,fullPage:true});
    const analyticsResponse=await api.get('/api/v1/admin/analytics?days=7&channel=all');expect(analyticsResponse.status()).toBe(200);const analytics=await analyticsResponse.json();expect(analytics.revenue).toBe(2490);expect(analytics.orders).toBe(1);expect(analytics.soldUnits).toBe(1);expect(analytics.returns).toBe(1);expect(analytics.dataQuality.duplicates).toBeGreaterThanOrEqual(1);expect(analytics.dataQuality.pending).toBeGreaterThanOrEqual(1);
    await page.goto('/admin?section=analytics');await expect(page.getByRole('heading',{name:'Аналитика продаж',exact:true})).toBeVisible();await expect(page.getByText('1 продаж ожидают сверки',{exact:true})).toBeVisible();await expect(page.getByText('Нет данных',{exact:true})).toBeVisible();await page.getByRole('button',{name:'Каналы',exact:true}).click();await expect(page.getByRole('cell',{name:'Avito',exact:true})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);await page.screenshot({path:`${out}/owner-analytics-${width}.png`,fullPage:true});
    await page.goto('/admin?section=procurement');await expect(page.getByRole('heading',{name:'Закупки',exact:true})).toBeVisible();await page.getByRole('button',{name:'Что заказать',exact:false}).click();await expect(page.getByRole('heading',{name:'Рекомендации к закупке',exact:true})).toBeVisible();await expect(page.getByText('Монстера Stage 05',{exact:true})).toBeVisible();await expect(page.getByText('25 шт.',{exact:true})).toBeVisible();await page.getByLabel('Выбрать Монстера Stage 05').check();await expect(page.getByRole('button',{name:'Сформировать заказ',exact:true})).toBeEnabled();expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);await page.screenshot({path:`${out}/owner-stage05-recommendations-${width}.png`,fullPage:true});
    await page.getByRole('button',{name:'Под заказ',exact:false}).click();await expect(page.getByRole('heading',{name:'Под заказ и идеи магазина',exact:true})).toBeVisible();await expect(page.getByText('распределено 0 · осталось 2',{exact:true})).toBeVisible();await page.screenshot({path:`${out}/owner-stage05-requests-${width}.png`,fullPage:true});
    await page.getByRole('button',{name:'Проверить наличие',exact:false}).first().click();await expect(page.getByRole('heading',{name:'Проверить наличие',exact:true})).toBeVisible();await expect(page.getByText('Фикус ожидание Stage 05',{exact:true})).toBeVisible();await expect(page.getByText('Фикус будущая проверка',{exact:true})).toHaveCount(0);await page.screenshot({path:`${out}/owner-stage05-availability-${width}.png`,fullPage:true});
    await page.locator('#workspace-content').getByRole('button',{name:'Закупки',exact:true}).click();await page.getByText('CRM-STAGE-06',{exact:true}).click();const invoiceDialog=page.getByRole('dialog',{name:'CRM-STAGE-06',exact:true});await expect(invoiceDialog).toBeVisible();await expect(invoiceDialog.getByText('Совпадает',{exact:true})).toBeVisible();await expect(invoiceDialog.getByText('Изменено',{exact:true})).toBeVisible();await expect(invoiceDialog.getByText('Нет в инвойсе',{exact:true})).toBeVisible();await expect(invoiceDialog.getByText('Добавлено поставщиком',{exact:true})).toBeVisible();await expect(invoiceDialog.getByText('NL-V-09',{exact:false})).toBeVisible();await page.screenshot({path:`${out}/owner-stage06-reconciliation-${width}.png`,fullPage:true});await invoiceDialog.getByRole('button',{name:'Закрыть',exact:true}).click();
    await page.getByText('CRM-STAGE-07',{exact:true}).click();const costDialog=page.getByRole('dialog',{name:'CRM-STAGE-07',exact:true});await expect(costDialog).toBeVisible();await expect(costDialog.getByText('Текущая: 945 ₽ · оценочная',{exact:true})).toBeVisible();await expect(costDialog.getByText('Черновик создан · ждёт проведения',{exact:true})).toBeVisible();await expect(costDialog.getByRole('button',{name:'Проведение подтверждено — закрыть',exact:true})).toHaveCount(0);await page.screenshot({path:`${out}/owner-stage07-cost-receipt-${width}.png`,fullPage:true});await costDialog.getByRole('button',{name:'Закрыть',exact:true}).click();
    expect(await page.evaluate(key=>localStorage.getItem(key),'ficusin:procurement-plan-draft:v1')).toBe(procurementDraft);
   }
   if(role==='manager') {
    const denied=await page.goto('/admin?section=finance');expect(denied.status()).toBe(403);
    await expect(page.getByRole('heading',{name:'Доступ к разделу ограничен'})).toBeVisible();
    await page.screenshot({path:`${out}/manager-denied-${width}.png`,fullPage:true});
   }
  }
  results.push({role,api:'passed',gallery:'preserved',viewports:[1440,390]});
  await context.close();
 }
 await writeFile(`${out}/results.json`,JSON.stringify({sha:process.env.GITHUB_SHA,environment:'ephemeral CI PostgreSQL; synthetic rows; real HTTP API',results},null,2));
} finally { await browser.close(); }
