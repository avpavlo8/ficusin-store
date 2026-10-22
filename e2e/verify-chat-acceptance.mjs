import { chromium, webkit } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const base=process.env.CHAT_BASE_URL || 'http://127.0.0.1:8080';
const artifacts=process.env.CHAT_ARTIFACT_DIR || new URL('../.impeccable/review',import.meta.url).pathname;
const staffToken=process.env.CHAT_STAFF_TOKEN || 'crm-acceptance-manager';
await mkdir(artifacts,{recursive:true});
for(const [engine,name,size] of [[chromium,'desktop',{width:1440,height:1100}],[webkit,'mobile',{width:390,height:844}]]){
 const browser=await engine.launch();
 try{
  const guest=await browser.newContext({viewport:size,serviceWorkers:'block'});
  const staff=await browser.newContext({viewport:size,serviceWorkers:'block'});
  await staff.addCookies([{name:'ficusin_session',value:staffToken,url:base}]);
  const page=await guest.newPage();const admin=await staff.newPage();page.setDefaultTimeout(12000);admin.setDefaultTimeout(12000);
  const failures=[];page.on('pageerror',e=>failures.push(String(e)));admin.on('pageerror',e=>failures.push(String(e)));
  assert.equal((await page.goto(base+'/chat?from=%2Fproduct%2Fficus&topic=Фикус%20Бенджамина')).status(),200);
  const message=`Добрый день! У фикуса начали желтеть листья. Стоит у восточного окна, поливаю раз в неделю. Подскажите, с чего начать? [${name}-${Date.now()}]`;
  await page.getByRole('textbox',{name:'Сообщение',exact:true}).fill(message);
  await page.getByRole('checkbox').check();await page.getByRole('button',{name:'Отправить',exact:true}).click();
  await page.getByText('Отправлено',{exact:true}).waitFor();
  const data=await (await guest.request.get(base+'/api/v1/chat')).json();assert.ok(data.id);
  await admin.goto(base+'/admin?section=chat');await admin.getByRole('button').filter({has:admin.getByText(message,{exact:true})}).click();
  const reply='Здравствуйте! Давайте посмотрим на растение. Пришлите, пожалуйста, фото листьев и грунта. Перед следующим поливом проверьте влажность на глубине нескольких сантиметров.';
  await admin.getByRole('textbox',{name:'Сообщение',exact:true}).fill(reply);await admin.getByRole('button',{name:'Отправить',exact:true}).click();
  await page.getByText(reply,{exact:true}).waitFor({timeout:12000});await page.getByText('Прочитано',{exact:true}).first().waitFor({timeout:12000});
  await page.screenshot({path:`${artifacts}/${name}.png`,fullPage:true});await admin.screenshot({path:`${artifacts}/admin-${name}.png`,fullPage:true});
  await page.reload(); await page.getByText(reply,{exact:true}).waitFor().catch(async error=>{console.log(await page.locator('body').innerText()); throw error;});
  const stranger=await browser.newContext();assert.equal((await (await stranger.request.get(base+'/api/v1/chat')).json()).messages.length,0);
  await page.getByLabel('Добавить фотографию',{exact:true}).setInputFiles({name:'plant.png',mimeType:'image/png',buffer:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=','base64')});
  await page.getByAltText('Фото к отправке').waitFor();await page.getByRole('button',{name:'Отправить',exact:true}).click();await page.getByAltText('Фотография в переписке — открыть целиком').waitFor();
  const updated=await (await guest.request.get(base+'/api/v1/chat')).json();const photoID=updated.messages.at(-1).id;
  assert.equal((await stranger.request.get(base+'/api/v1/chat/photos/'+photoID)).status(),404);
  assert.equal((await stranger.request.get(base+'/api/v1/admin/chat/'+data.id+'/photos/'+photoID)).status(),401);
  assert.equal((await staff.request.get(base+'/api/v1/admin/chat/'+data.id+'/photos/'+photoID)).status(),200);
  await admin.getByRole('button',{name:'Вопрос решён',exact:true}).click();await page.getByText('Вопрос решён. Новое сообщение снова откроет переписку.',{exact:true}).waitFor({timeout:12000});
  await page.getByRole('textbox',{name:'Сообщение',exact:true}).fill('Спасибо! Ещё вопрос.');await page.getByRole('button',{name:'Отправить',exact:true}).click();
  await page.getByText('Спасибо! Ещё вопрос.',{exact:true}).waitFor();assert.equal((await (await guest.request.get(base+'/api/v1/chat')).json()).status,'open');
  for(const p of [page,admin]){assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`overflow: ${p.url()}`);}
  assert.deepEqual(failures,[]);
  console.log(`${name}: customer message, staff reply, read receipt, refresh, photos, private access, close/reopen and layout passed`);
 }finally{await browser.close();}
}
