import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

test("@phone bottom catalogue opens searchable categories and restores focus", async ({page}) => {
  await mockApi(page);
  await page.goto("/");
  const trigger=page.locator('.tab-bar').getByRole('button',{name:'Каталог',exact:true});
  await trigger.click();
  const dialog=page.getByRole('dialog',{name:'Категории каталога'});
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Найти категорию').fill('фикус');
  await expect(dialog.getByRole('link',{name:/Фикус/})).toHaveCount(1);
  await expect(dialog.getByRole('link',{name:/Аглаонема/})).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await dialog.getByRole('link',{name:/Фикус/}).click();
  await expect(page).toHaveURL(/\/\?category=5$/);
  await expect(page.locator(".home-hero")).toBeAttached();
  await expect(page.locator('.storefront-card')).toHaveCount(2);
});

test("@phone toolbar uses icons and search stays at viewport top after scrolling",async({page})=>{
  await mockApi(page);
  await page.goto('/#catalog');
  const toolbar=page.locator('.home-catalog-toolbar');
  await expect(toolbar.locator('.catalog-tool-label')).toBeHidden();
  await expect(page.locator('.drawer:not(.open)')).toHaveCSS('box-shadow','none');
  await expect(toolbar.locator('.home-sort summary > span')).toBeHidden();
  for (const control of [toolbar.locator('.home-filter-button'), toolbar.locator('.home-sort summary')]) {
    const box = await control.boundingBox();
    const icon = await control.locator('svg').boundingBox();
    expect(Math.abs(icon!.x + icon!.width / 2 - box!.x - box!.width / 2)).toBeLessThan(1);
    expect(Math.abs(icon!.y + icon!.height / 2 - box!.y - box!.height / 2)).toBeLessThan(1);
  }
  const dimensions=await toolbar.evaluate(el=>({width:el.clientWidth,scroll:el.scrollWidth}));
  expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.width);
  await toolbar.getByRole('button',{name:'Фильтры',exact:true}).click();
  await expect(page.getByLabel('Категория',{exact:true})).toBeVisible();
  expect(await page.locator('.home-filter-panel > .storefront-attribute-filters').evaluate(el=>getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(1);
  await page.locator('.tab-bar').getByRole('button',{name:'Поиск',exact:true}).click();
  const field=page.locator('.mobile-search');
  await expect(field.getByRole('combobox')).toBeFocused();
  const box=await field.boundingBox();
  expect(box!.y).toBe(0);
  expect(box!.x).toBe(0);
  expect(box!.width).toBe((await page.viewportSize())!.width);
  await field.getByRole('combobox').fill('фикус');
  await field.getByRole('combobox').press('Enter');
  await expect(field).toHaveCount(0);
  await expect(page.getByRole('heading',{name:'Результаты поиска'})).toBeVisible();
});

test("@desktop @phone favorite counts match visible cards and hidden IDs survive",async({page})=>{
  await mockApi(page);
  await page.addInitScript(()=>localStorage.setItem('ficusin-favorites',JSON.stringify(['1','2','999'])));
  await page.goto('/favorites');
  await expect(page.locator('.storefront-card')).toHaveCount(2);
  await expect(page.locator('.favorites-heading')).toContainText('2 товара');
  await expect(page.locator('.favorites-button')).toHaveAttribute('aria-label','Избранное, товаров: 2');
  expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('ficusin-favorites')!))).toEqual(['1','2','999']);
  await expect(page.locator('.favorites-unavailable')).toContainText('1 сохранённый товар недоступен');
  const card=page.locator('.storefront-card').first();
  await card.getByRole('button',{name:'В корзину',exact:true}).click();
  await expect(card.getByRole('button',{name:'Перейти в корзину, 1 шт.'}).locator('.catalog-cart-icon')).toBeVisible();
  expect(await card.getByRole('button',{name:'Перейти в корзину, 1 шт.'}).evaluate(el=>getComputedStyle(el).backgroundImage)).toContain('linear-gradient');
});


test("@desktop header cart opens the shared cart without covering content", async ({page})=>{
  await mockApi(page);await page.goto('/');
  await expect(page.locator('.floating-cart')).toHaveCount(0);
  await page.locator('.storefront-card').first().getByRole('button',{name:'В корзину',exact:true}).click();
  await page.getByRole('button',{name:'Корзина, товаров: 1',exact:true}).click();
  await expect(page.locator('.drawer.open .cart-line')).toHaveCount(1);
});


test("@phone catalogue purchase buttons contain a cart icon without visible text",async({page})=>{
  await mockApi(page);await page.goto('/catalog/plants');
  const card=page.locator('.storefront-card').first();
  await expect(card.locator('.cart-button-label')).toBeHidden();
  await expect(card.locator('.catalog-cart-icon')).toBeVisible();
  await card.getByRole('button',{name:'В корзину',exact:true}).click();
  await expect(card.locator('.cart-item-count')).toHaveText('1');await expect(card.locator('.catalog-cart-icon')).toBeVisible();
  await expect(page.locator('.floating-cart')).toBeHidden();
});


test("@desktop homepage category names update cards while retaining the hero and collections", async ({page}) => {
  await mockApi(page); await page.goto('/#catalog');
  const hero = await page.locator('.home-hero').elementHandle();
  const tree = page.locator('.storefront-tree');
  await tree.getByRole('link', {name:/^Растения/}).click();
  await tree.getByRole('link', {name:/Фикус/}).click();
  await expect(page).toHaveURL(/\/\?category=5#catalog$/);
  await expect(page.locator('.storefront-head h2')).toHaveText('Фикус');
  await expect(page.locator('.storefront-card')).toHaveCount(2);
  expect(await hero!.evaluate(el => el.isConnected)).toBe(true);
  await expect(page.getByRole('region', {name:'Подборки растений'})).toBeAttached();
  await tree.getByRole('link', {name:/Аглаонема/}).click();
  await expect(page.locator('.storefront-head h2')).toHaveText('Аглаонема');
  await page.goBack();
  await expect(page.locator('.storefront-head h2')).toHaveText('Фикус');
  await expect(page.locator('.storefront-card')).toHaveCount(2);
  await page.reload();
  await expect(page.locator('.storefront-head h2')).toHaveText('Фикус');
  await tree.getByRole('link', {name:/Весь каталог/}).click();
  await expect(page.locator('.storefront-head h2')).toHaveText('Каталог');
  await expect(page.locator('.storefront-card')).toHaveCount(4);
});


test("@phone top menu is compact and search leaves the page usable", async ({page}) => {
  await mockApi(page); await page.goto('/#catalog');
  const menuTrigger = page.getByRole('button', {name:'Открыть меню',exact:true});
  await menuTrigger.click();
  const menu = page.getByRole('dialog', {name:'Меню магазина'});
  await expect(menu).toBeVisible();
  await expect(menu.locator('.mobile-menu-documents summary')).toHaveText('О магазине');
  expect(await menu.locator('.mobile-menu-links').evaluateAll(els=>els.every(el=>getComputedStyle(el).gridTemplateColumns.split(' ').length===1))).toBe(true);
  const box = await menu.boundingBox(), viewport = page.viewportSize()!;
  expect(box!.height).toBeLessThan(viewport.height * .8);
  expect(box!.width).toBeLessThan(viewport.width);
  await expect(page.locator('.mobile-menu-backdrop')).toBeVisible();
  await expect(page.locator('body')).toHaveCSS('overflow','hidden');
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0); await expect(menuTrigger).toBeFocused();
  await page.locator('.tab-bar').getByRole('button',{name:'Поиск',exact:true}).click();
  await expect(page.locator('.mobile-search')).toBeVisible();
  await expect(page.locator('body')).not.toHaveCSS('overflow','hidden');
  await page.locator('.storefront-card').first().getByRole('button',{name:'В избранное',exact:true}).click();
  await expect(page.locator('.storefront-card').first().getByRole('button',{name:'Убрать из избранного',exact:true})).toBeVisible();
});


test("@desktop @phone card quantity persists, respects stock and removes the last unit",async({page})=>{
  await mockApi(page); await page.goto('/');
  const card=page.locator('.storefront-card').filter({hasText:'Аглаонема Мария'});
  await card.getByRole('button',{name:'В корзину',exact:true}).click();
  const quantityBounds=await card.locator('.catalog-card-quantity').boundingBox();
  const cartBounds=await card.locator('.catalog-cart-open').boundingBox();
  expect(quantityBounds!.x+quantityBounds!.width).toBeLessThanOrEqual(cartBounds!.x);
  expect(cartBounds!.width).toBeGreaterThanOrEqual(44);
  expect(quantityBounds!.width/cartBounds!.width).toBeLessThan(1.5);
  for(const button of await card.locator('.catalog-card-quantity button').all()) {
    const bounds=await button.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(quantityBounds!.x);
    expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(quantityBounds!.x+quantityBounds!.width);
  }
  const plus=card.getByRole('button',{name:'Увеличить количество: Аглаонема Мария',exact:true});
  const minus=card.getByRole('button',{name:'Уменьшить количество: Аглаонема Мария',exact:true});
  await plus.click();await expect(card.locator('.cart-item-count')).toHaveText('2');
  await page.reload();await expect(card.locator('.cart-item-count')).toHaveText('2');
  await page.getByRole('button',{name:'Список',exact:true}).click();
  const cardBounds=await card.boundingBox();
  for(const control of await card.locator('.catalog-cart-controls button').all()) {
    const bounds=await control.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(cardBounds!.x);
    expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(cardBounds!.x+cardBounds!.width+1);
  }

  await minus.click();await expect(card.locator('.cart-item-count')).toHaveText('1');
  await card.getByRole('button',{name:'В избранное',exact:true}).click();
  await page.goto('/favorites');
  const favorite=page.locator('.storefront-card');
  await expect(favorite.locator('.cart-item-count')).toHaveText('1');
  await favorite.getByRole('button',{name:'Увеличить количество: Аглаонема Мария',exact:true}).click();
  await expect(favorite.locator('.cart-item-count')).toHaveText('2');
  await favorite.getByRole('button',{name:'Уменьшить количество: Аглаонема Мария',exact:true}).click();
  const removed=page.waitForResponse(response=>response.url().endsWith('/api/v1/cart') && response.request().method()==='PUT' && Object.keys(response.request().postDataJSON().items).length===0);
  await favorite.getByRole('button',{name:'Уменьшить количество: Аглаонема Мария',exact:true}).click();
  await removed;
  await expect(favorite.getByRole('button',{name:'В корзину',exact:true})).toBeVisible();
  await page.reload();await expect(favorite.getByRole('button',{name:'В корзину',exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test("@desktop @phone card plus disables at the available stock",async({page})=>{
  await mockApi(page);
  const {catalogue}=await import('./helpers');
  await page.route('**/api/v1/catalog',route=>route.fulfill({json:{products:[{...catalogue[0],stock:2}]}}));
  await page.goto('/');const card=page.locator('.storefront-card');
  await card.getByRole('button',{name:'В корзину',exact:true}).click();
  const plus=card.getByRole('button',{name:/Увеличить количество/});
  await plus.click();await expect(plus).toBeDisabled();
  await expect(card.locator('.cart-item-count')).toHaveText('2');
  await card.getByRole('button',{name:/Уменьшить количество/}).click();await expect(plus).toBeEnabled();
});

test('@desktop @phone delayed cart responses do not rewind rapid quantity clicks',async({page})=>{
  await mockApi(page);
  let items:Record<string,number>={};
  let writes=0;
  let releaseNavigation!:()=>void;
  const navigationHeld=new Promise<void>(resolve=>{releaseNavigation=resolve;});
  let releaseFirst!:()=>void;
  const held=new Promise<void>(resolve=>{releaseFirst=resolve;});
  await page.route('**/api/v1/cart',async route=>{
    if(route.request().method()==='PUT'){
      const next=route.request().postDataJSON().items;
      writes++;
      if(writes===1)await held;
      if(writes===4)await navigationHeld;
      items=next;
    }
    await route.fulfill({json:{items,lines:[],missingSkus:[]}});
  });
  await page.goto('/');
  const card=page.locator('.storefront-card').filter({hasText:'Аглаонема Мария'});
  await card.getByRole('button',{name:'В корзину',exact:true}).click();
  const plus=card.getByRole('button',{name:/Увеличить количество/});
  await plus.click();await plus.click();
  const count=card.locator('.cart-item-count');await expect(count).toHaveText('3');
  await count.evaluate(el=>{
    const values:string[]=[];
    (window as typeof window & {quantityValues:string[]}).quantityValues=values;
    new MutationObserver(()=>values.push(el.textContent||'')).observe(el,{subtree:true,characterData:true,childList:true});
  });
  const saved=page.waitForResponse(response=>response.url().endsWith('/api/v1/cart')&&response.request().method()==='PUT'&&response.request().postDataJSON().items['1']===3);
  releaseFirst();await saved;
  await expect(count).toHaveText('3');
  const values=await page.evaluate(()=>(window as typeof window & {quantityValues:string[]}).quantityValues);
  expect(values.every(value=>value==='3')).toBe(true);
  await page.reload();await expect(count).toHaveText('3');
  await card.getByRole('button',{name:/Увеличить количество/}).click();
  await expect(count).toHaveText('4');
  await card.getByRole('button',{name:'Перейти в корзину, 4 шт.',exact:true}).click();
  await expect(page).not.toHaveURL(/\/cart$/);
  releaseNavigation();await expect(page).toHaveURL(/\/cart$/);
  if(page.viewportSize()!.width<=850)await expect(page.locator('.tab-bar a[href="/cart"] b')).toHaveText('4');
  else await expect(page.getByLabel('Корзина, товаров: 4',{exact:true})).toBeVisible();
});
