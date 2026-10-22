import { expect, test } from '@playwright/test';
import { mockApi, owner, horizontalOverflow } from './helpers';

test('account chat stays above a panned iPhone keyboard and restores navigation @phone', async ({page},info) => {
  await page.addInitScript(() => {
    const viewport = new EventTarget(); Object.assign(viewport,{height:innerHeight,offsetTop:0});
    Object.defineProperty(window,'visualViewport',{value:viewport});
    (window as unknown as {panAccount:(height:number,top:number)=>void}).panAccount=(height,top)=>{Object.assign(viewport,{height,offsetTop:top});viewport.dispatchEvent(new Event('resize'));viewport.dispatchEvent(new Event('scroll'));};
  });
  await mockApi(page,owner); await page.goto('/account/chat');
  const input=page.getByRole('textbox',{name:'Сообщение',exact:true}); await expect(input).toBeVisible();
  await input.fill('Строка сообщения\n'.repeat(30));
  for(const viewport of [{height:330,top:400},{height:230,top:160},{height:page.viewportSize()!.height,top:0}]) {
    await page.evaluate(({height,top})=>(window as unknown as {panAccount:(height:number,top:number)=>void}).panAccount(height,top),viewport);
    await expect.poll(async()=>{const box=await input.boundingBox();return !!box&&box.y>=viewport.top&&box.y+box.height<=viewport.top+viewport.height+1;}).toBe(true);
    const send=await page.getByRole('button',{name:'Отправить',exact:true}).boundingBox();expect(send!.y+send!.height).toBeLessThanOrEqual(viewport.top+viewport.height+1);
    // Focus scrolling must not pan a hidden wrapper and leave a gap below the composer.
    const wrappers = ['.account-chat-view','.account-messenger','.chat-thread'];
    for (const selector of wrappers) {
      const wrapper = page.locator(selector);
      await wrapper.evaluate(element => { element.scrollTop = 80; });
      expect(await wrapper.evaluate(element => element.scrollTop)).toBe(0);
    }
    const composer = await page.locator('.chat-compose').boundingBox();
    expect(Math.abs(composer!.y + composer!.height - viewport.top - viewport.height)).toBeLessThanOrEqual(1);
    await expect(page.locator('.store-footer')).toBeHidden(); expect(await horizontalOverflow(page)).toBe(0);
  }
  await expect(page.locator('body')).toHaveClass(/account-chat-mobile/);
  await page.evaluate(()=>(window as unknown as {panAccount:(height:number,top:number)=>void}).panAccount(330,0));
  await expect(input).toBeInViewport();
  await page.locator('.account-chat-view').screenshot({path:info.outputPath('account-messenger.png')});
  await page.getByRole('link',{name:'Вернуться в личный кабинет',exact:true}).click();
  await expect(page.locator('body')).not.toHaveClass(/account-chat-mobile/);
  await expect(page.getByRole('navigation',{name:'Разделы личного кабинета'})).toBeVisible();
});

for (const device of ['@desktop','@phone']) {
  test(`granted notifications reconnect automatically and explicit chat opt-out survives reload ${device}`, async ({page})=>{
    await mockApi(page);
    await page.addInitScript(()=>{
      const serialized={endpoint:'https://web.push.apple.com/test-default',expirationTime:null,keys:{p256dh:'test-key',auth:'test-auth'}};
      const subscription={endpoint:serialized.endpoint,toJSON:()=>serialized,unsubscribe:()=>{throw new Error('shared subscription must survive');}};
      Object.defineProperty(window,'PushManager',{configurable:true,value:class{}});
      Object.defineProperty(window,'Notification',{configurable:true,value:{permission:'granted',requestPermission:()=>{throw new Error('must not request permission again');}}});
      const ready=Promise.resolve({pushManager:{getSubscription:async()=>subscription,subscribe:async()=>subscription}});
      Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{register:async()=>ready,ready}});
      Object.defineProperty(navigator,'standalone',{configurable:true,value:true});
    });
    let subscribed=false;let posts=0;let payload:object={};
    await page.route('**/api/v1/push/key',route=>route.fulfill({json:{enabled:true,publicKey:'BAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'}}));
    await page.route('**/api/v1/chat/notifications/state',route=>route.fulfill({json:{subscribed}}));
    await page.route('**/api/v1/chat/notifications',route=>{if(route.request().method()==='POST'){posts++;payload=route.request().postDataJSON();subscribed=true;}else subscribed=false;return route.fulfill({json:{ok:true}});});
    await page.goto('/chat');
    const off=page.getByRole('button',{name:'Выключить уведомления о сообщениях',exact:true});await expect(off).toBeVisible();
    expect(posts).toBeGreaterThan(0);expect(payload).toHaveProperty('expirationTime',null);
    await off.click();await expect(page.getByRole('button',{name:'Включить уведомления о сообщениях',exact:true})).toBeVisible();
    const before=posts;await page.reload();await expect(page.getByRole('button',{name:'Включить уведомления о сообщениях',exact:true})).toBeVisible();expect(posts).toBe(before);
  });
}

test('iPhone offers installation with actionable steps and respects later dismissal @phone',async({page},info)=>{
  await mockApi(page);await page.addInitScript(()=>{Object.defineProperty(navigator,'userAgent',{value:'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1'});Object.defineProperty(navigator,'standalone',{value:false});});
  await page.clock.install();await page.goto('/');await page.clock.fastForward(8100);
  const invitation=page.getByRole('complementary',{name:'Установить приложение Фикусин',exact:true});await expect(invitation).toBeVisible();
  await invitation.getByRole('button',{name:'Как установить',exact:true}).click();await expect(invitation).toContainText('Откройте меню «Поделиться»');await expect(invitation).toContainText('На экран Домой');expect(await horizontalOverflow(page)).toBe(0);
  await page.screenshot({path:info.outputPath('install-invitation.png')});
  await invitation.getByRole('button',{name:'Напомнить об установке позже',exact:true}).click();await page.reload();await page.clock.fastForward(10000);await expect(invitation).toHaveCount(0);
});
