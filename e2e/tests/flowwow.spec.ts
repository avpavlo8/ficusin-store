import {expect,test} from '@playwright/test';
import {mockApi,owner,horizontalOverflow} from './helpers';
test('@desktop @phone Flowwow shows links, errors and existing seller cards',async({page},testInfo)=>{
 await mockApi(page,owner);
 await page.route('**/api/v1/admin/dashboard',route=>route.fulfill({json:{user:{fullName:'Александр'},role:'owner',permissions:['dashboard.read','products.read','products.manage','integrations.edit'],dashboard:{}}}));
 await page.route('**/api/v1/admin/flowwow',route=>route.fulfill({json:{configured:true,shopId:'123',publicationMode:'mock',items:[{variantId:10,productId:1,name:'Фикус Эластика',sku:'FICUS-10',priceMinor:199900,stock:3,remoteId:'500',offerId:'FICUS-10',status:'synced',message:'Карточка обновлена',lastError:''},{variantId:11,productId:2,name:'Аглаонема Мария',sku:'AGL-11',priceMinor:250000,stock:0,remoteId:'501',offerId:'AGL-11',status:'failed',message:'Не принята карточка',lastError:'Flowwow: цена пока не подтверждена'}]}}));
 await page.route('**/api/v1/admin/flowwow/products?page=0',route=>route.fulfill({json:{items:[{productId:500,offerId:'FICUS-10',name:'Фикус Эластика',price:'1999.00',stock:3,type:1}],total:1}}));
 await page.goto('/admin?section=flowwow');
 await expect(page.getByRole('heading',{name:'Flowwow',exact:true})).toBeVisible();
 await expect(page.getByText('Карточка обновлена',{exact:true})).toBeVisible();
 await page.getByRole('combobox',{name:'Показать',exact:true}).selectOption('attention');
 await expect(page.getByRole('link',{name:'Фикус Эластика',exact:true})).toHaveCount(0);
 await expect(page.getByText('Flowwow: цена пока не подтверждена',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Карточки в Flowwow',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Карточки магазина Flowwow'})).toBeVisible();
 await expect(page.getByRole('cell',{name:'FICUS-10',exact:true})).toBeVisible();
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
 await page.screenshot({path:testInfo.outputPath('flowwow.png'),fullPage:true});
});
