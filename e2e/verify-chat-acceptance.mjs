import { chromium, webkit } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const base=process.env.CHAT_BASE_URL || 'http://127.0.0.1:8080';
const artifacts=process.env.CHAT_ARTIFACT_DIR || new URL('../.impeccable/review',import.meta.url).pathname;
const staffToken=process.env.CHAT_STAFF_TOKEN || 'crm-acceptance-manager';
const customerToken=process.env.CHAT_CUSTOMER_TOKEN || 'crm-acceptance-owner';
await mkdir(artifacts,{recursive:true});
for(const [engine,name,size] of [[chromium,'desktop',{width:1440,height:1000}],[webkit,'mobile',{width:390,height:844}]]){
 const browser=await engine.launch();
 try{
  const guest=await browser.newContext({viewport:size,serviceWorkers:'block'});const staff=await browser.newContext({viewport:size,serviceWorkers:'block'});
  await staff.addCookies([{name:'ficusin_session',value:staffToken,url:base}]);
  const page=await guest.newPage();const admin=await staff.newPage();page.setDefaultTimeout(15000);admin.setDefaultTimeout(15000);
  const failures=[];page.on('pageerror',e=>failures.push(String(e)));admin.on('pageerror',e=>failures.push(String(e)));
  assert.equal((await page.goto(base+'/chat?from=%2Fproduct%2Fficus&topic=Фикус%20Бенджамина')).status(),200);
  let dialog=page.getByRole('dialog',{name:'Быстрый чат с Фикусином'});await dialog.waitFor();
  let box=await dialog.getByRole('button',{name:'Отправить',exact:true}).boundingBox();assert.ok(box.y+box.height<=size.height,'composer below viewport');
  const message=`Добрый день! У фикуса начали желтеть листья. Стоит у восточного окна, поливаю раз в неделю. Подскажите, с чего начать? [${name}-${Date.now()}]`;
  await dialog.getByRole('textbox',{name:'Сообщение',exact:true}).fill(message);await dialog.getByRole('checkbox').check();await dialog.getByRole('button',{name:'Отправить',exact:true}).click();await dialog.getByLabel('Отправлено',{exact:true}).last().waitFor();
  const data=await (await guest.request.get(base+'/api/v1/chat')).json();assert.ok(data.id);
  await admin.goto(base+'/admin?section=chat');await admin.getByRole('button').filter({has:admin.getByText(message,{exact:true})}).click();
  const reply='Здравствуйте! Пришлите, пожалуйста, фото листьев и грунта. Посмотрим на растение вместе.';
  await admin.getByRole('textbox',{name:'Сообщение',exact:true}).fill(reply);await admin.getByRole('button',{name:'Отправить',exact:true}).click();
  await dialog.getByText(reply,{exact:true}).waitFor({timeout:15000});await dialog.getByLabel('Прочитано',{exact:true}).first().waitFor({timeout:15000});
  await page.screenshot({path:`${artifacts}/quick-${name}.png`,fullPage:false});await admin.screenshot({path:`${artifacts}/admin-${name}.png`,fullPage:true});
  await page.reload();dialog=page.getByRole('dialog',{name:'Быстрый чат с Фикусином'});await dialog.getByText(reply,{exact:true}).waitFor();
  const stranger=await browser.newContext();assert.equal((await (await stranger.request.get(base+'/api/v1/chat')).json()).messages.length,0);
  await dialog.getByLabel('Добавить фотографию',{exact:true}).setInputFiles({name:'plant.webp',mimeType:'image/webp',buffer:await (await guest.request.get(base+'/assets/hero-monstera.webp')).body()});
  await dialog.getByAltText('Фото к отправке').waitFor();box=await dialog.getByRole('button',{name:'Отправить',exact:true}).boundingBox();assert.ok(box.y+box.height<=size.height,'photo composer below viewport');
  await dialog.getByRole('button',{name:'Отправить',exact:true}).click();await dialog.getByAltText('Фотография в переписке — открыть целиком').waitFor();
  const updated=await (await guest.request.get(base+'/api/v1/chat')).json();const photoID=updated.messages.at(-1).id;
  assert.equal((await stranger.request.get(base+'/api/v1/chat/photos/'+photoID)).status(),404);assert.equal((await stranger.request.get(base+'/api/v1/admin/chat/'+data.id+'/photos/'+photoID)).status(),401);assert.equal((await staff.request.get(base+'/api/v1/admin/chat/'+data.id+'/photos/'+photoID)).status(),200);
  if(name==='mobile') {await page.setViewportSize({width:390,height:430});box=await dialog.getByRole('button',{name:'Отправить',exact:true}).boundingBox();assert.ok(box.y+box.height<=431,'short viewport/keyboard composer');await page.setViewportSize(size);}
  // Logging in binds the guest history to the account, including on another device.
  await guest.addCookies([{name:'ficusin_session',value:customerToken,url:base}]);await dialog.getByRole('link',{name:'Вся переписка в личном кабинете'}).click();
  await page.getByRole('heading',{name:'Сообщения',exact:true}).waitFor();await page.getByText(message,{exact:true}).waitFor();
  await page.screenshot({path:`${artifacts}/account-${name}.png`,fullPage:true});
  const another=await browser.newContext({viewport:size,serviceWorkers:'block'});await another.addCookies([{name:'ficusin_session',value:customerToken,url:base}]);const account=await another.newPage();await account.goto(base+'/account/chat');await account.getByText(message,{exact:true}).waitFor();
  for(const p of [page,admin])assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`overflow: ${p.url()}`);
  assert.deepEqual(failures,[]);console.log(`${name}: quick chat, visible composer, send/reply/read, photos/private access, login binding, account history and second device passed`);
 }finally{await browser.close();}
}
