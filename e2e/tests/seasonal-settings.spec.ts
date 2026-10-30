import { expect, test } from '@playwright/test';
import { mockApi, owner, horizontalOverflow } from './helpers';

test('@desktop @phone owner selects and saves the packaging season',async({page})=>{
 await mockApi(page,owner);
 await page.route('**/api/v1/admin/dashboard',route=>route.fulfill({json:{user:{fullName:'Владелец'},role:'owner',permissions:['dashboard.read','integrations.edit'],dashboard:{products:0,variants:0,orders:0,customers:0,wholesalePending:0,lastSync:null,recentOrders:[]}}}));
 let season='summer';
 const writes:string[]=[];
 await page.route('**/api/v1/admin/settings',route=>{
   if(route.request().method()==='PUT'){season=route.request().postDataJSON().values['cdek.packaging_season'];writes.push(season)}
   return route.fulfill({json:{definitions:[{key:'cdek.packaging_season',title:'Упаковка для расчёта СДЭК',kind:'select',note:'Выберите летние или зимние габариты для новых расчётов. Если выбранная упаковка не заполнена, доставку рассчитает менеджер. Вес общий для обоих вариантов. Сохранённые коробки заказов не меняются.',options:[{value:'summer',title:'Летняя упаковка'},{value:'winter',title:'Зимняя упаковка'}]}],values:{'cdek.packaging_season':season}}});
 });
 await page.goto('/admin?section=settings');
 const select=page.getByRole('combobox',{name:/Упаковка для расчёта СДЭК/});
 await expect(select).toHaveValue('summer');
 await select.selectOption('winter');
 expect(writes).toEqual([]);
 await page.getByRole('button',{name:'Сохранить',exact:true}).click();
 await expect(page.getByText('Настройки сохранены и уже действуют')).toBeVisible();
 expect(writes).toEqual(['winter']);
 await page.reload();await expect(select).toHaveValue('winter');
 expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
 if(process.env.SEASONAL_VISUAL_DIR)await page.screenshot({path:`${process.env.SEASONAL_VISUAL_DIR}/settings-${test.info().project.name}.png`});
});
