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
  await expect(card.getByRole('button',{name:'Перейти в корзину, 1 шт.'})).toContainText('В корзину');
  expect(await card.getByRole('button',{name:'Перейти в корзину, 1 шт.'}).evaluate(el=>getComputedStyle(el).backgroundImage)).toContain('linear-gradient');
});


test("@desktop floating cart stays in the lower right and opens the shared cart", async ({page})=>{
  await mockApi(page);await page.goto('/');
  const floating=page.getByRole('link',{name:'Открыть корзину, товаров: 0',exact:true});
  await expect(floating).toBeVisible();await expect(floating).toHaveCSS('position','fixed');
  const box=await floating.boundingBox(),viewport=page.viewportSize()!;
  expect(viewport.width-box!.x-box!.width).toBe(24);expect(viewport.height-box!.y-box!.height).toBe(24);
  await page.locator('.storefront-card').first().getByRole('button',{name:'В корзину',exact:true}).click();
  await expect(page.locator('.floating-cart b')).toHaveText('1');
  await page.getByRole('link',{name:'Открыть корзину, товаров: 1',exact:true}).click();await expect(page).toHaveURL(/\/cart$/);
  await expect(page.locator('.floating-cart')).toHaveCount(0);
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
  const box = await menu.boundingBox(), viewport = page.viewportSize()!;
  expect(box!.height).toBeLessThan(viewport.height * .8);
  expect(box!.width).toBeLessThan(viewport.width);
  await expect(page.locator('.mobile-menu-backdrop')).toBeHidden();
  await expect(page.locator('body')).not.toHaveCSS('overflow','hidden');
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0); await expect(menuTrigger).toBeFocused();
  await page.locator('.tab-bar').getByRole('button',{name:'Поиск',exact:true}).click();
  await expect(page.locator('.mobile-search')).toBeVisible();
  await expect(page.locator('body')).not.toHaveCSS('overflow','hidden');
  await page.locator('.storefront-card').first().getByRole('button',{name:'В избранное',exact:true}).click();
  await expect(page.locator('.storefront-card').first().getByRole('button',{name:'Убрать из избранного',exact:true})).toBeVisible();
});
