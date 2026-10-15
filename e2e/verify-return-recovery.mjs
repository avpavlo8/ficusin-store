// The real built application and an isolated PostgreSQL database. No Saby
// credentials: receipt assertions exercise the durable queue, never real stock.
import {chromium,expect} from '@playwright/test';
import {randomUUID} from 'node:crypto';
import {mkdir,writeFile} from 'node:fs/promises';
const base=process.env.CRM_BASE_URL||'http://127.0.0.1:8080';
const out=process.env.CRM_ARTIFACT_DIR||'crm-artifacts';
await mkdir(out,{recursive:true});
const browser=await chromium.launch({headless:true});
const context=await browser.newContext({baseURL:base,serviceWorkers:'block'});
await context.addCookies([{name:'ficusin_session',value:'crm-acceptance-manager',url:base}]);
const owner=await browser.newContext({baseURL:base,serviceWorkers:'block'});
await owner.addCookies([{name:'ficusin_session',value:'crm-acceptance-owner',url:base}]);
const page=await context.newPage(),api=context.request,docID=randomUUID();
let saved;
try{
 const products=(await(await api.get('/api/v1/admin/returns/products')).json()).items;
 const product=products.find(item=>item.sku==='99999199');expect(product).toBeTruthy();
 const data={channel:'wb',channelComment:'',rows:[{key:'recovery',variantId:product.variantId,name:product.name,sku:product.sku,search:`${product.name} · ${product.label}`,quantity:2,condition:'restoring',comment:'Внутренний осмотр',discountPercent:30},{key:'ready',variantId:product.variantId,name:product.name,sku:product.sku,search:product.name,quantity:1,condition:'ready',comment:''},{key:'dead',variantId:product.variantId,name:product.name,sku:product.sku,search:product.name,quantity:1,condition:'dead',comment:'Повреждение'}]};
 expect((await api.put(`/api/v1/admin/returns/intakes/${docID}`,{data:{revision:0,data}})).status()).toBe(200);
 const accepted=await api.post(`/api/v1/admin/returns/intakes/${docID}/save`,{data:{revision:1}});expect(accepted.status()).toBe(200);saved=(await accepted.json()).intake;expect(saved.items).toHaveLength(4);
 expect((await api.delete('/api/v1/admin/returns',{data:{documentId:docID}})).status()).toBe(403);
 expect((await api.get('/api/v1/admin/returns/archive')).status()).toBe(403);
 expect((await api.post('/api/v1/admin/returns/restore',{data:{documentId:docID}})).status()).toBe(403);
 const item=saved.items.find(entry=>entry.condition==='restoring');const originalReady=saved.items.find(entry=>entry.condition==='ready');expect(item.discountedPrice).toBe(Math.round(item.price*70)/100);
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:1000});
  await page.goto('/admin?section=returns');await page.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Документы/}).click();
  await page.getByRole('button',{name:new RegExp(`Документ приёмки №${saved.documentNumber}`)}).click();
  const dialog=page.getByRole('dialog',{name:`Документ приёмки №${saved.documentNumber}`});await expect(dialog.getByRole('heading',{name:'Состав документа',exact:true})).toBeVisible();await expect(dialog.getByRole('spinbutton')).toHaveCount(0);await expect(dialog.getByRole('button',{name:'Сохранить',exact:true})).toHaveCount(0);
  await expect(dialog.getByRole('button',{name:'Создать поступление',exact:true})).toBeVisible();
  await page.screenshot({path:`${out}/return-document-${width}.png`,fullPage:true});
  const instance=dialog.locator('.return-document-units article').filter({hasText:item.article});await instance.getByRole('button',{name:'Открыть экземпляр'}).click();
  const detail=page.getByRole('complementary',{name:`Экземпляр ${item.article}`});await expect(detail).toBeVisible();
  if(width===390){const heading=await detail.getByRole('heading',{name:item.productName,exact:true}).boundingBox();expect(heading.y).toBeLessThan(400);await expect(page.locator('.returns-list')).not.toBeVisible();}
  await expect(detail.getByRole('button',{name:'Подготовить к продаже',exact:true})).toBeVisible();await expect(detail.getByRole('heading',{name:'Поступление в СБИС',exact:true})).toHaveCount(0);
  const discount=width===1440?35:40;
  await detail.getByRole('spinbutton',{name:'Скидка экземпляра'}).fill(String(discount));
  await detail.getByRole('textbox',{name:'Комментарий осмотра'}).fill(`Осмотр ${width} — сохранить при уходе `);
  await detail.getByRole('textbox',{name:'Описание для покупателя'}).fill('Повреждён один лист. Новый рост здоровый.');
  // Leave while edits are pending, not after waiting for the debounce.
  if(width===390)await detail.getByRole('button',{name:'К списку растений',exact:false}).click();
  await page.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Документы/}).click();
  const updated=(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item;expect(updated.discountPercent).toBe(discount);expect(updated.comment).toBe(`Осмотр ${width} — сохранить при уходе`);expect(updated.publicComment).toContain('Повреждён один лист');
  await page.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Растения/}).click();await page.getByRole('searchbox',{name:'Поиск возвратов'}).fill(item.article);await page.locator('.return-unit').filter({hasText:item.article}).getByRole('button').click();await expect(detail).toBeVisible();await expect(detail.getByRole('spinbutton',{name:'Скидка экземпляра'})).toHaveValue(String(discount));
  const current=(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item;await expect(detail.locator('.return-price strong')).toContainText(current.discountedPrice.toLocaleString('ru-RU',{maximumFractionDigits:2})+' ₽');
  if(width===1440){
   const buffer=await page.screenshot();await detail.locator('input[type=file]').setInputFiles({name:'phone.png',mimeType:'',buffer});await expect(detail.locator('.return-photo')).toHaveCount(1);
   await detail.getByRole('button',{name:'Увеличить фото 1'}).click();await expect(page.getByRole('dialog',{name:'Фото осмотра'}).locator('img')).toBeVisible();await page.getByRole('dialog',{name:'Фото осмотра'}).getByRole('button',{name:'Закрыть',exact:true}).click();
   await detail.getByRole('button',{name:'Удалить фото 1'}).click();await expect(detail.locator('.return-photo')).toHaveCount(0);await detail.getByRole('button',{name:'Восстановить фото',exact:true}).click();await expect(detail.locator('.return-photo')).toHaveCount(1);
   const old=(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item.photoIds[0];
   const upload=page.waitForEvent('filechooser');await detail.getByRole('button',{name:'Заменить фото 1'}).click();await(await upload).setFiles({name:'replace.png',mimeType:'image/png',buffer});await expect.poll(async()=>(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item.photoIds[0]).not.toBe(old);await expect(detail.locator('.return-photo')).toHaveCount(1);
  }
  await page.screenshot({path:`${out}/return-recovery-${width}.png`,fullPage:true});
  if(width===390)await detail.getByRole('button',{name:'К списку растений',exact:false}).click();
  await page.getByRole('checkbox',{name:`Выбрать ${item.article}`,exact:true}).check();await expect(page.getByRole('button',{name:/^Скачать этикетки/})).toBeEnabled();
  const download=page.waitForEvent('download');await page.getByRole('button',{name:/^Скачать этикетки/}).click();await(await download).saveAs(`${out}/return-label-${width}.pdf`);
  const response=await page.goto(`/discounts#${item.article}`);expect(response.status()).toBe(200);const offer=page.locator('.discount-plants-grid article').filter({hasText:item.article});await expect(offer).toContainText('Повреждён один лист');await expect(offer.locator('img')).toHaveAttribute('src',new RegExp(`^/api/v1/discount-plants/${item.id}/photo\\?v=\\d+$`));await expect(offer).not.toContainText('Внутренний осмотр');expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);await page.screenshot({path:`${out}/discount-plants-${width}.png`,fullPage:true});
 }
 const labels=await api.post('/api/v1/admin/returns/labels',{data:{ids:saved.items.filter(item=>item.condition==='restoring').map(item=>item.id),includeDate:true}});expect(labels.status()).toBe(200);expect(labels.headers()['content-type']).toBe('application/pdf');const pdf=await labels.body();expect(pdf.subarray(0,5).toString()).toBe('%PDF-');await writeFile(`${out}/return-labels-58x40.pdf`,pdf);
 // Preparation moves the instance to ready and the card adopts server state.
 await page.goto('/admin?section=returns');await page.getByRole('searchbox',{name:'Поиск возвратов'}).fill(item.article);await page.locator('.return-unit').getByRole('button').click();const detail=page.getByRole('complementary',{name:`Экземпляр ${item.article}`});await detail.getByRole('button',{name:'Подготовить к продаже',exact:true}).click();await expect.poll(async()=>(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item.condition).toBe('ready');await expect(detail.getByRole('heading',{name:'Поступление в СБИС'})).toBeVisible();
 if((await page.viewportSize()).width===390)await detail.getByRole('button',{name:'К списку растений',exact:false}).click();
 await page.getByRole('searchbox',{name:'Поиск возвратов'}).fill('');await page.getByRole('checkbox',{name:`Выбрать ${item.article}`,exact:true}).check();await page.getByRole('checkbox',{name:`Выбрать ${originalReady.article}`,exact:true}).check();await page.getByRole('button',{name:'Создать поступление · 1',exact:true}).click();const preview=page.getByRole('dialog',{name:'Состав нового поступления'});await expect(preview.locator('li')).toHaveCount(1);await preview.getByRole('button',{name:'Создать поступление',exact:true}).click();await expect(preview).toHaveCount(0);
 const first=(await(await api.get(`/api/v1/admin/returns/${item.id}`)).json()).item;expect(first.receiptStatus).not.toBe('none');expect((await api.post('/api/v1/admin/returns/receipt-batch',{data:{ids:[item.id,originalReady.id]}})).status()).toBe(202);
 // Owner can archive and restore the accepted document. Manager cannot.
 const ownerPage=await owner.newPage();await ownerPage.setViewportSize({width:390,height:844});await ownerPage.goto('/admin?section=returns');await ownerPage.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Документы/}).click();await ownerPage.getByRole('button',{name:`Удалить документ ${saved.documentNumber}`,exact:true}).click();const deletion=ownerPage.getByRole('dialog',{name:'Удалить записи возвратов?'});await expect(deletion).toContainText('4 растения');await expect(deletion).toContainText('можно восстановить');await deletion.getByRole('button',{name:'Удалить записи',exact:true}).click();await expect(deletion).toHaveCount(0);
 await ownerPage.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Удалённые/}).click();const restoreResponse=ownerPage.waitForResponse(response=>response.url().endsWith('/api/v1/admin/returns/restore')&&response.request().method()==='POST');await ownerPage.locator('.return-archive article').filter({hasText:`Документ приёмки №${saved.documentNumber}`}).getByRole('button',{name:'Восстановить документ'}).click();expect((await restoreResponse).status()).toBe(200);const reopened=await api.get(`/api/v1/admin/returns/intakes/${docID}`);expect(reopened.status()).toBe(200);const restored=(await reopened.json()).intake;expect(restored.items).toHaveLength(4);expect(restored.data.rows[0].quantity).toBe(2);expect(restored.items[0].discountPercent).toBe(40);expect(restored.items[0].photoIds).toHaveLength(1);expect(restored.items[0].id).toBe(item.id);if(first.receiptExternalUrl){expect(restored.items[0].receiptExternalUrl).toBe(first.receiptExternalUrl);expect(restored.items[0].receiptStatus).not.toBe('none');}
 // Archiving safely cancels a queued action before its first attempt. Its
 // restoration may therefore detach that unsent action and return to 'none'.
 // Completed external-document identity is also covered by the PostgreSQL test.

 await writeFile(`${out}/return-recovery-results.json`,JSON.stringify({savedDocument:true,managerArchiveDenied:true,photoUpload:true,photoZoom:true,photoDeleteRestore:true,photoReplace:true,autosaveOnLeave:true,labels:true,publicDescription:true,readyStatusChange:true,batchReceipt:true,archiveRestore:true,viewports:[1440,390]},null,2));
}catch(error){
 await page.screenshot({path:`${out}/return-failure.png`,fullPage:true});await writeFile(`${out}/return-failure.html`,await page.content());throw error;
}finally{
 await owner.request.delete('/api/v1/admin/returns',{data:{documentId:docID}});await context.close();await owner.close();await browser.close();
}
