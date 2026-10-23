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
 expect(calls.filter(p=>p==='/api/v1/catalog')).toHaveLength(0);
 expect(calls.filter(p=>p==='/api/v1/auth/me').length).toBeLessThanOrEqual(1);
});
