import { expect, test } from '@playwright/test';
import { mockApi, setStoredCounts, horizontalOverflow } from './helpers';

test('@desktop @phone checkout validates after interaction and explains disabled submission', async ({page}) => {
  await mockApi(page); await setStoredCounts(page, [], {'1':1});
  await page.goto('/checkout', {waitUntil:'domcontentloaded'});
  await expect(page.locator('.checkout-contact-error')).toHaveCount(0);
  await expect(page.locator('.checkout-submit-hint')).toHaveText('Укажите имя');
  await expect(page.locator('.checkout-summary-items')).toContainText('Аглаонема Мария');
  await expect(page.locator('.checkout-payment-section')).toHaveCount(0);
  const phone=page.getByLabel('Телефон',{exact:true});
  await phone.fill('900'); await phone.press('Tab');
  await expect(page.locator('#phone-error')).toHaveText('Укажите номер телефона');
  await phone.fill('9000000000'); await expect(page.locator('#phone-error')).toHaveCount(0);
  await expect(page.locator('.checkout-footer')).toBeVisible();
  await expect(page.locator('.store-footer-connect')).toHaveCount(0);
  await expect(page.locator('.chat-launcher')).toBeHidden();
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test('@desktop @phone cart total includes only merchandise and survives quantity edits', async ({page}) => {
  await mockApi(page); await setStoredCounts(page, [], {'1':1});
  await page.goto('/cart',{waitUntil:'domcontentloaded'});
  const total=page.locator('.order-lamp-total strong'); await expect(total).toHaveText(/1.?490/);
  await expect(page.locator('.order-lamp-scene')).not.toContainText('500');
  await page.getByRole('button',{name:'Увеличить количество Аглаонема Мария',exact:true}).click();
  await expect(total).toHaveText(/2.?980/);
  await page.reload({waitUntil:'domcontentloaded'}); await expect(total).toHaveText(/2.?980/);
});

test('@desktop @phone product keeps care available in tabs, sorted sizes and complete recommendations', async ({page}) => {
  await mockApi(page);
  await page.route('**/api/v1/products/review',route=>route.fulfill({json:{product:{
    id:'review',name:'Алоказия Регал Шилдс',latin:'Alocasia',catalogSection:'plants',categoryId:4,
    description:'Описание растения',images:['/assets/missing-review-photo.jpg'],attributes:[],passport:{lighting:'Рассеянный свет',watering:'После просыхания'},
    variants:[32,10,23].map(n=>({id:n,sku:String(n),label:`D${n}`,potDiameterCm:n,price:1490,stock:5,attributes:[],images:[]})),
    recommendations:[1,2,3,4].map(n=>({id:String(n),sku:String(n),name:`Растение ${n}`,price:1490,image:'/assets/hero-monstera.webp',size:'D12',stock:5,latin:'Plant'})),
    importantWarnings:[],reviews:[],rating:0,reviewsCount:0,
  }}}));
  await page.goto('/product/review',{waitUntil:'domcontentloaded'});
  await expect(page.locator('.variant-picker button strong')).toHaveText(['Ø 10 см','Ø 23 см','Ø 32 см']);
  await expect(page.locator('#plant-passport')).toBeAttached();
  await expect(page.locator('#pdp-panel-care')).toBeHidden();
  await expect(page.locator('.purchase-review-stars')).toHaveCount(0);
  await expect(page.locator('.pdp-photo-track img')).toHaveAttribute('src','/assets/product-photo-pending-graphite-v1.svg');
  const rail=await page.locator('.pdp-related-track').boundingBox();
  const card=await page.locator('.pdp-related-track a').first().boundingBox();
  expect(card!.x).toBeGreaterThanOrEqual(rail!.x-1);
  await expect(page.locator('.pdp-related-side.prev')).toBeDisabled();
  await expect(page.locator('.floating-cart')).toHaveCount(0);
  if(page.viewportSize()!.width<=850) {
    await expect(page.locator('.pdp-mobile-purchase')).toBeVisible();
    const buy=await page.locator('.pdp-mobile-purchase').boundingBox();
    const tabs=await page.locator('.tab-bar').boundingBox();
    expect(buy!.y+buy!.height).toBeLessThanOrEqual(tabs!.y+1);
    await page.getByRole('button',{name:'Открыть меню',exact:true}).click();
    await expect(page.locator('.mobile-menu-backdrop')).toBeVisible();
    await expect(page.locator('.pdp-mobile-purchase')).toBeHidden();
    expect(await page.locator('.mobile-menu').evaluate(el=>getComputedStyle(el).backgroundColor)).toBe('rgb(250, 247, 239)');
  }
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test('@desktop search catalogue loads on demand and shares guest profile reads',async({page})=>{
 await mockApi(page); await setStoredCounts(page,[],{'1':1}); const calls={catalog:0,profile:0};
 page.on('request',req=>{if(req.url().endsWith('/api/v1/catalog'))calls.catalog++;if(req.url().endsWith('/api/v1/auth/me'))calls.profile++;});
 await page.goto('/checkout',{waitUntil:'domcontentloaded'}); await expect(page.locator('.checkout-submit-hint')).toBeVisible();
 expect(calls.catalog).toBe(0); expect(calls.profile).toBe(1);
 await page.locator('.header-search input').fill('фикус');
 await expect(page.getByRole('option',{name:/Фикус Бенджамина/})).toBeVisible(); expect(calls.catalog).toBe(1);
});
