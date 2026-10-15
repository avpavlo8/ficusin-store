// Real API + built UI, with owner permissions and an isolated database.
import {chromium,webkit,devices,expect} from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import {randomUUID} from 'node:crypto';
import {mkdir,writeFile} from 'node:fs/promises';
const base=process.env.CRM_BASE_URL||'http://127.0.0.1:8080';
const out=process.env.CRM_ARTIFACT_DIR||'crm-artifacts';await mkdir(out,{recursive:true});
const results=[];
for(const [name,engine,options] of [['desktop',chromium,{viewport:{width:1440,height:1000}}],['iphone',webkit,{...devices['iPhone 13']}],['android',chromium,{...devices['Pixel 5']}]]){
 const browser=await engine.launch({headless:true});const context=await browser.newContext({baseURL:base,serviceWorkers:'block',...options});await context.addCookies([{name:'ficusin_session',value:'crm-acceptance-owner',url:base}]);const api=context.request;const page=await context.newPage();const id=randomUUID();
 try{
  const products=(await(await api.get('/api/v1/admin/returns/products')).json()).items;const product=products.find(item=>item.sku==='99999199');expect(product).toBeTruthy();
  const data={channel:'wb',channelComment:'',rows:[{key:'ready',variantId:product.variantId,name:product.name,sku:product.sku,search:product.name,quantity:2,condition:'ready',comment:'Без скидки',discountPercent:0},{key:'recover',variantId:product.variantId,name:product.name,sku:product.sku,search:product.name,quantity:1,condition:'restoring',comment:'После возврата',discountPercent:0}]};
  expect((await api.put(`/api/v1/admin/returns/intakes/${id}`,{data:{revision:0,data}})).status()).toBe(200);const response=await api.post(`/api/v1/admin/returns/intakes/${id}/save`,{data:{revision:1}});expect(response.status()).toBe(200);const saved=(await response.json()).intake;
  const read=async()=> (await(await api.get(`/api/v1/admin/returns/intakes/${id}`)).json()).intake;
  const open=async()=>{await page.goto('/admin?section=returns');await page.getByRole('navigation',{name:'Журналы возвратов'}).getByRole('button',{name:/^Документы/}).click();await page.getByRole('button',{name:new RegExp(`Документ приёмки №${saved.documentNumber} `)}).click();return page.getByRole('dialog',{name:`Документ приёмки №${saved.documentNumber}`});};
  let dialog=await open();await dialog.getByRole('button',{name:'Редактировать документ',exact:true}).click();await expect(dialog.getByRole('spinbutton',{name:'Количество 1',exact:true})).toHaveValue('2');
  await dialog.getByRole('combobox',{name:'Канал',exact:true}).selectOption('other');await dialog.getByRole('textbox',{name:'Комментарий к каналу',exact:true}).fill('Проверка редактирования');
  await dialog.getByRole('spinbutton',{name:'Количество 1',exact:true}).fill('3');await dialog.getByRole('textbox',{name:'Комментарий 1',exact:true}).fill('Исправленный комментарий');await dialog.getByRole('spinbutton',{name:'Скидка 2',exact:true}).fill('30');
  await dialog.getByRole('button',{name:'Сохранить изменения',exact:true}).click();await expect(dialog.getByRole('button',{name:'Редактировать документ',exact:true})).toBeVisible();
  let changed=await read();expect(changed.items).toHaveLength(4);for(const item of saved.items)expect(changed.items.some(next=>next.id===item.id)).toBeTruthy();expect(changed.items.filter(item=>item.condition==='restoring')[0].discountPercent).toBe(30);expect(changed.items[0].comment).toBe('Исправленный комментарий');
  const download=page.waitForEvent('download');await dialog.getByRole('button',{name:'Скачать этикетки',exact:true}).click();await(await download).saveAs(`${out}/return-edited-labels-${name}.pdf`);
  await dialog.getByRole('button',{name:'Закрыть документ',exact:true}).click();dialog=await open();await dialog.getByRole('button',{name:'Редактировать документ',exact:true}).click();await expect(dialog.getByRole('spinbutton',{name:'Количество 1',exact:true})).toHaveValue('3');
  await dialog.getByRole('textbox',{name:'Комментарий 1',exact:true}).fill('Несохранённый комментарий');await dialog.getByRole('button',{name:'Закрыть',exact:true}).click();await expect(dialog.getByRole('alert')).toContainText('Сохраните изменения');await dialog.getByRole('button',{name:'Отменить изменения',exact:true}).click();expect((await read()).items[0].comment).toBe('Исправленный комментарий');
  await dialog.getByRole('button',{name:'Редактировать документ',exact:true}).click();await dialog.getByRole('button',{name:'Добавить растение',exact:false}).click();const search=dialog.getByRole('combobox',{name:'Название растения 3',exact:true});await search.fill('');await search.click();const list=dialog.getByRole('listbox',{name:'Каталог растений'});await expect(list).toBeVisible();await list.scrollIntoViewIfNeeded();const lastIndex=Math.min(7,(await list.getByRole('option').count())-1);for(let index=0;index<=lastIndex;index++)await search.press('ArrowDown');const activeBox=await list.getByRole('option').nth(lastIndex).boundingBox();const listBox=await list.boundingBox();expect(activeBox.y).toBeGreaterThanOrEqual(listBox.y-2);expect(activeBox.y+activeBox.height).toBeLessThanOrEqual(listBox.y+listBox.height+2);const box=await list.boundingBox();const footer=await dialog.locator('footer').boundingBox();expect(box.y+box.height).toBeLessThanOrEqual(footer.y+2);expect(await search.evaluate(el=>getComputedStyle(el).outlineStyle)).toBe('none');expect(await search.evaluate(el=>getComputedStyle(el.parentElement).outlineStyle)).toBe('solid');
  const a11y=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();expect(a11y.violations.filter(v=>['serious','critical'].includes(v.impact||''))).toEqual([]);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);await page.screenshot({path:`${out}/return-editor-${name}.png`,fullPage:true});
  if(name!=='desktop'){
   // A short viewport models the space left above a phone keyboard.
   await page.setViewportSize({width:390,height:480});await search.click();await list.scrollIntoViewIfNeeded();
   const inputBox=await search.boundingBox(),headerBox=await dialog.locator('header').boundingBox(),shortList=await list.boundingBox(),shortFooter=await dialog.locator('footer').boundingBox();
   expect(inputBox.y).toBeGreaterThanOrEqual(headerBox.y+headerBox.height-2);expect(shortList.y+shortList.height).toBeLessThanOrEqual(shortFooter.y+2);
   await page.screenshot({path:`${out}/return-editor-${name}-short.png`,fullPage:true});
  }
  await dialog.getByRole('button',{name:'Отменить изменения',exact:true}).click();await dialog.getByRole('button',{name:'Закрыть документ',exact:true}).click();
  results.push({name,savedEdits:true,stableUnitIDs:true,reloaded:true,unsavedCloseProtected:true,cancelRestores:true,zeroAndDiscountLabels:true,searchInsideDialog:true,singleFocusFrame:true});
 }catch(error){await page.screenshot({path:`${out}/return-edits-failure-${name}.png`,fullPage:true});throw error;}
 finally{await api.delete('/api/v1/admin/returns',{data:{documentId:id}});await context.close();await browser.close();}
}
await writeFile(`${out}/return-edits-results.json`,JSON.stringify(results,null,2));
