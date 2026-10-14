// Isolated CRM database only. No external Saby credentials or real documents.
import { chromium, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
const base = process.env.CRM_BASE_URL || 'http://127.0.0.1:8080';
const out = process.env.CRM_ARTIFACT_DIR || 'crm-artifacts';
await mkdir(out,{recursive:true});
const browser = await chromium.launch({headless:true});
const context = await browser.newContext({baseURL:base,serviceWorkers:'block'});
await context.addCookies([{name:'ficusin_session',value:'crm-acceptance-manager',url:base}]);
const page = await context.newPage();
const api=context.request;
const docID=randomUUID();
try {
 const products=(await (await api.get('/api/v1/admin/returns/products')).json()).items;
 const product=products.find(item=>item.name==='Фикус Stage 07');expect(product).toBeTruthy();
 const data={channel:'wb',channelComment:'',rows:[{key:'r',variantId:product.variantId,name:product.name,sku:product.sku,search:`${product.name} · ${product.label}`,quantity:2,condition:'restoring',comment:'Тестовый осмотр',discountPercent:30},{key:'ready',variantId:product.variantId,name:product.name,sku:product.sku,search:product.name,quantity:1,condition:'ready',comment:''}]};
 const draft=await api.put(`/api/v1/admin/returns/intakes/${docID}`,{data:{revision:0,data}});expect(draft.status()).toBe(200);
 const savedResponse=await api.post(`/api/v1/admin/returns/intakes/${docID}/save`,{data:{revision:1}});expect(savedResponse.status()).toBe(200);const saved=(await savedResponse.json()).intake;expect(saved.items).toHaveLength(3);
 const deny=await api.delete('/api/v1/admin/returns',{data:{documentId:docID}});expect(deny.status()).toBe(403);
 const item=saved.items[0];expect(item.article).toBeTruthy();expect(item.discountedPrice).toBe(Math.round(item.price*70)/100);
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:1050});
  await page.goto('/admin?section=returns');await page.getByRole('navigation',{name:'Состояния возвратов'}).getByRole('button',{name:/^Документы/}).click();
  await page.getByRole('button',{name:new RegExp(`Возврат №${saved.documentNumber} ·`)}).click();
  const dialog=page.getByRole('dialog',{name:new RegExp(`Возврат №${saved.documentNumber}`)});await expect(dialog).toBeVisible();await expect(dialog.getByRole('spinbutton',{name:'Количество 1',exact:true})).toHaveValue('2');await expect(dialog.getByRole('combobox',{name:'Состояние 1',exact:true})).toHaveValue('restoring');
  await expect(dialog.getByRole('button',{name:'Создать поступление',exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
  await page.screenshot({path:`${out}/return-document-${width}.png`,fullPage:true});
  await dialog.getByRole('button',{name:'Закрыть',exact:true}).click();
  await page.getByRole('navigation',{name:'Состояния возвратов'}).getByRole('button',{name:/^На восстановление/}).click();
  await page.locator('.return-unit').filter({hasText:item.article}).getByRole('button').click();
  await expect(page.getByRole('button',{name:'Продать',exact:true})).toBeVisible();await expect(page.getByRole('heading',{name:'Поступление в СБИС',exact:true})).toHaveCount(0);
  await expect(page.getByRole('button',{name:'Удалить все записи',exact:true})).toHaveCount(0);
  await page.getByRole('checkbox',{name:`Выбрать ${item.article}`,exact:true}).check();
  await expect(page.getByRole('button',{name:'Скачать этикетки',exact:true})).toBeEnabled();
  if(width===1440){
   const png=await page.screenshot();await page.locator('.return-photos input[type=file]').setInputFiles({name:'phone.png',mimeType:'',buffer:png});
   await expect(page.locator('.return-photos img')).toHaveCount(1);
  }
  await page.screenshot({path:`${out}/return-recovery-${width}.png`,fullPage:true});
  const discountPage=await page.goto('/discounts');expect(discountPage.status()).toBe(200);await expect(page.getByRole('heading',{name:'Растения со скидкой',exact:true})).toBeVisible();await expect(page.locator('.discount-plants-grid article').filter({hasText:item.article})).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);await page.screenshot({path:`${out}/discount-plants-${width}.png`,fullPage:true});
 }
 const labels=await api.post('/api/v1/admin/returns/labels',{data:{ids:saved.items.filter(item=>item.condition==='restoring').map(item=>item.id),includeDate:true}});
 // PDF fonts are supplied in this workflow; test actual download and media type.
 expect(labels.status()).toBe(200);expect(labels.headers()['content-type']).toBe('application/pdf');const pdf=await labels.body();expect(pdf.subarray(0,5).toString()).toBe('%PDF-');await writeFile(`${out}/return-labels-58x40.pdf`,pdf);
 const update=await api.patch(`/api/v1/admin/returns/${saved.items[2].id}`,{data:{condition:'restoring',comment:'Из продажи на восстановление',discountPercent:20}});expect(update.status()).toBe(200);expect((await update.json()).item.condition).toBe('restoring');
 await writeFile(`${out}/return-recovery-results.json`,JSON.stringify({savedDocument:true,managerDeleteDenied:true,photoUpload:true,labels:true,discounts:true,readyStatusChange:true,viewports:[1440,390]},null,2));
}finally{
 const owner=await browser.newContext({baseURL:base});await owner.addCookies([{name:'ficusin_session',value:'crm-acceptance-owner',url:base}]);await owner.request.delete('/api/v1/admin/returns',{data:{documentId:docID}});await owner.close();await context.close();await browser.close();
}
