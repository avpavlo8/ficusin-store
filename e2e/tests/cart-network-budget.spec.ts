import {test,expect} from '@playwright/test';
import {mockApi} from './helpers';
for(const path of ['/cart','/checkout'])test(`@desktop @phone ${path} shares categories and avoids full catalog`,async({page})=>{
 await mockApi(page);
 const calls:string[]=[];
 page.on('request',request=>{const url=new URL(request.url());if(url.pathname.startsWith('/api/'))calls.push(url.pathname)});
 await page.goto(path);
 await expect(page.locator('main')).toBeVisible();
 await expect.poll(()=>calls.filter(p=>p==='/api/v1/categories').length).toBe(1);
 // A loaded header and cart provide the readiness signal; no arbitrary sleeps.
 await expect(page.locator('.store-header')).toBeVisible();
 if(path==='/checkout'){
  await expect(page.locator('.checkout-load-state')).toBeVisible();
  await expect.poll(()=>calls.filter(p=>p==='/api/v1/payments/methods').length).toBe(1);
  await expect.poll(()=>calls.filter(p=>p==='/api/v1/delivery/cdek').length).toBe(1);
 }else{
  await expect(page.locator('.cart-page-panel')).toBeVisible();
  expect(calls.filter(p=>p==='/api/v1/payments/methods'||p==='/api/v1/delivery/cdek')).toHaveLength(0);
 }
 expect(calls.filter(p=>p==='/api/v1/catalog')).toHaveLength(0);
 expect(calls.filter(p=>p==='/api/v1/auth/me').length).toBeLessThanOrEqual(1);
});


test('@desktop @phone storefront cart write errors stay visible without mounting purchase panels',async({page})=>{
 await mockApi(page);
 await page.route('**/api/v1/cart',route=>route.request().method()==='PUT'?route.fulfill({status:503,json:{error:'Не удалось сохранить выбранные растения'}}):route.fallback());
 await page.goto('/');await page.locator('.storefront-card').first().getByRole('button',{name:'В корзину',exact:true}).click();
 const error=page.locator('.cart-error-toast');await expect(error).toContainText('Не удалось сохранить выбранные растения');await expect(error.getByRole('button',{name:'Повторить',exact:true})).toBeVisible();
 await expect(page.locator('.cart-checkout-host,.drawer,.checkout')).toHaveCount(0);
});
