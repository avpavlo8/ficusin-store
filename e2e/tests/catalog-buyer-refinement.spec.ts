import { expect, test } from "@playwright/test";
import { catalogue, horizontalOverflow, mockApi } from "./helpers";

test("@desktop @phone a small category keeps buyer criteria together and provides pot choices", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/catalog", route => route.fulfill({ json: { products: [10, 12].map((diameter, i) => ({
    ...catalogue[0], id: String(i + 1), sku: String(i + 1), filterAttributes: [
      { code: "light_level", name: "Освещённость", value: "diffused", filterable: true },
      { code: "watering", name: "Полив", value: "moderate", filterable: true },
      { code: "care_level", name: "Уход", value: "easy", filterable: true },
      { code: "pet_safety", name: "Для питомцев", value: "caution", filterable: true },
      { code: "pot_diameter_cm", name: "Размер горшка", unit: "см", value: diameter, displayMode: "select", filterable: true },
    ],
  })) } }));
  await page.goto("/catalog/aglaonema");
  await page.getByRole("button", { name: /Фильтры/ }).click();
  const panel = page.locator(".home-filter-panel");
  await expect(panel.getByLabel("Уровень ухода", { exact: true })).toBeVisible();
  await expect(panel.getByLabel("Уровень освещения", { exact: true })).toBeVisible();
  await expect(panel.getByLabel("Для питомцев", { exact: true })).toHaveCount(0);
  await expect(panel.getByLabel("Цена: до")).toBeVisible();
  await expect(page.locator(".home-catalog-toolbar select")).toHaveCount(0);
  if ((await page.viewportSize())!.width > 850) expect((await panel.boundingBox())!.height).toBeLessThan(300);
  await expect(panel.getByLabel("Горшок, см", { exact: true })).toBeVisible();
  const diameter = panel.getByLabel("Горшок, см", { exact: true });
  await expect(diameter.locator("option")).toHaveText(["Любое значение", "10", "12"]);
  await diameter.selectOption("12");
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await page.getByRole("button", { name: /^Фильтры/ }).click();
  await expect(page.getByRole("button", { name: /Убрать фильтр: Размер горшка/ })).toBeVisible();
  await page.getByRole("button", { name: /Убрать фильтр: Размер горшка/ }).click();
  await expect(page.locator(".storefront-card")).toHaveCount(2);
  await expect(page.getByText("Показан размер выбранного варианта", { exact: false })).toHaveCount(0);
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone plant cards show a care scale while PDP keeps a text level", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  const card = page.locator(".storefront-card").filter({ hasText: "Аглаонема Мария" });
  await expect(card.locator(".storefront-plant-specs")).toContainText("Высота: 35 см");
  await expect(card.getByRole("img", { name: "Сложность ухода: Лёгкий уход, 1 из 3", exact: true })).toBeVisible();
  await expect(card.locator(".care-scale")).toHaveAttribute("data-level", "1");
  await expect(card).not.toContainText("Освещение:");
  await expect(card).not.toContainText("Полив:");
  await expect(card.locator(".storefront-price")).not.toContainText("В наличии");
  const unavailable = page.locator(".storefront-card").filter({ hasText: "Монстера Делициоза" });
  await expect(unavailable.locator(".storefront-price")).toContainText("≈");
  await expect(unavailable.locator(".storefront-price")).toContainText("Нет в наличии");
  await expect(unavailable.getByRole("button", { name: "Заказать", exact: true })).toBeVisible();
  await card.getByRole("button", { name: "В корзину", exact: true }).click();
  const added = card.getByRole("button", { name: "Перейти в корзину, 1 шт.", exact: true });
  await expect(card.locator(".cart-item-count")).toHaveText("1");
  await expect(added.locator(".catalog-cart-icon")).toBeVisible();
  await page.goto("/product/1");
  await expect(page.locator(".pdp-key-characteristics")).toContainText("Лёгкий уход");
  await expect(page.locator(".pdp-key-characteristics .care-scale")).toHaveCount(0);
});

test("@desktop @phone height and flowering filter independently while secondary parameters are absent", async ({ page }) => {
  await mockApi(page);
  const products = [
    { height: 35, flowering: false, country: "Нидерланды" },
    { height: 120, flowering: true, country: "Россия" },
    {},
  ].map((values, index) => ({
    ...catalogue[0], id: String(index + 1), sku: String(index + 1),
    filterAttributes: [
      ...(values.height ? [{ code: "height_cm", name: "Высота растения", unit: "см", value: values.height, filterable: true, displayMode: "range" }] : []),
      ...(values.flowering !== undefined ? [{ code: "flowering", name: "Цветение", value: values.flowering, filterable: true, displayMode: "chips" }] : []),
      ...(values.country ? [{ code: "growing_country", name: "Страна выращивания", value: values.country, filterable: true }] : []),
    ],
  }));
  await page.route("**/api/v1/catalog", route => route.fulfill({ json: { products } }));
  await page.goto("/catalog/plants");
  await expect(page.locator(".storefront-card")).toHaveCount(3);
  await page.getByRole("button", { name: /Фильтры/ }).click();
  await page.getByLabel("Высота", { exact: true }).selectOption("100:150");
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await page.locator(".catalog-applied-filters").getByRole("button", { name: "Сбросить фильтры" }).click();
  await page.getByLabel("Цветение", { exact: true }).selectOption("false");
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await page.reload();
  await expect(page).toHaveURL(/filter.flowering=false/);
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await page.locator(".catalog-applied-filters").getByRole("button", { name: "Сбросить фильтры" }).click();
  await expect(page.locator(".storefront-card")).toHaveCount(3);
  await page.getByRole("button", { name: /Фильтры/ }).click();
  await expect(page.getByLabel("Страна выращивания", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Другие параметры", { exact: false })).toHaveCount(0);
});

test("@desktop @phone favorites sit beside the gradient cart action and preserve their red outline and filled states", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  const card = page.locator(".storefront-card").filter({ hasText: "Аглаонема Мария" });
  const favorite = card.getByRole("button", { name: "В избранное", exact: true });
  const cart = card.getByRole("button", { name: "В корзину", exact: true });
  await favorite.scrollIntoViewIfNeeded();
  const bounds = async () => ({ heart: (await favorite.boundingBox())!, cart: (await cart.boundingBox())! });
  const grid = await bounds();
  expect(grid.heart.x + grid.heart.width).toBeLessThanOrEqual(grid.cart.x);
  if ((await page.viewportSize())!.width > 500) expect(Math.abs(grid.heart.y - grid.cart.y)).toBeLessThanOrEqual(2);
  expect(grid.heart.width).toBeGreaterThanOrEqual(44);
  await expect(favorite.locator("svg")).toHaveCSS("fill", "none");
  await expect(favorite.locator("svg")).toHaveCSS("stroke", "rgb(237, 28, 36)");
  await expect(cart).toHaveCSS("background-image", /linear-gradient/);
  await favorite.click();
  const selected = card.getByRole("button", { name: "Убрать из избранного", exact: true });
  await expect(selected).toHaveAttribute("aria-pressed", "true");
  await expect(selected.locator("svg")).toHaveCSS("fill", "rgb(237, 28, 36)");
  await expect(selected.locator("svg")).toHaveCSS("stroke", "rgb(34, 27, 22)");
  await page.getByRole("button", { name: "Список", exact: true }).click();
  const heart = (await selected.boundingBox())!;
  const action = (await cart.boundingBox())!;
  expect(heart.x + heart.width).toBeLessThanOrEqual(action.x);
  if ((await page.viewportSize())!.width > 500) expect(Math.abs(heart.y - action.y)).toBeLessThanOrEqual(2);
  if ((await page.viewportSize())!.width > 850) {
    const badges = await card.locator(".storefront-plant-specs").evaluateAll(elements => elements.map(element => element.getBoundingClientRect().height));
    expect(badges.length).toBeGreaterThan(0);
    expect(Math.max(...badges)).toBeLessThan(90);
  }
  expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop @phone compact filters provide ordered choices, price shortcuts and an accessible stock switch", async ({page})=>{
  await mockApi(page);
  const prices=[999,1000,2000,3000,5000,5001];
  await page.route('**/api/v1/catalog',route=>route.fulfill({json:{products:prices.map((price,index)=>({...catalogue[0],id:String(index+1),sku:String(index+1),price,stock:index===0?0:5,filterAttributes:[...catalogue[0].filterAttributes,
    {code:'pot_diameter_cm',name:'Диаметр горшка',value:index%2?12:10,filterable:true,displayMode:'range'},
    {code:'watering',name:'Полив',value:index%2?'rare':'moderate',filterable:true},
  ]}))}}));
  await page.goto('/catalog/aglaonema');
  await page.getByRole('button',{name:'Фильтры',exact:true}).click();
  const panel=page.locator('.home-filter-panel');
  const labels=await panel.locator(':scope > .storefront-attribute-filters label:not(.catalog-stock-switch)').evaluateAll(elements=>elements.filter(el=>!el.classList.contains('catalog-category-picker')).map(el=>el.childNodes[0].textContent));
  expect(labels).toEqual(['Уровень освещения','Уровень ухода','Цветение','Горшок, см','Высота','Полив']);
  if ((await page.viewportSize())!.width>1100) expect((await panel.boundingBox())!.height).toBeLessThan(280);
  const fields=await panel.locator(':scope > .storefront-attribute-filters').boundingBox();
  const budget=await panel.locator('.catalog-budget-row').boundingBox();
  expect(budget!.y).toBeGreaterThanOrEqual(fields!.y+fields!.height);
  for (const [name,count] of [['До 1 000',2],['До 3 000',4],['До 5 000',5],['Больше 5 000',1]] as const) {
    const preset=panel.getByRole('button',{name,exact:true});await preset.click();
    await expect(preset).toHaveAttribute('aria-pressed','true');
    await expect(page.locator('.storefront-card')).toHaveCount(count);
  }
  await page.reload();
  await page.getByRole('button',{name:'Фильтры',exact:true}).click();
  await expect(panel.getByRole('button',{name:'Больше 5 000',exact:true})).toHaveAttribute('aria-pressed','true');
  await panel.getByRole('button',{name:'До 1 000',exact:true}).click();
  await panel.getByRole('switch',{name:'Только в наличии',exact:true}).check();
  await expect(page.locator('.storefront-card')).toHaveCount(1);
  await expect(page).toHaveURL(/stock=1/);
});

test("@desktop @phone care uses a gradient pointer and ratings sit next to the favorite with a larger right aligned price",async({page})=>{
  await mockApi(page);await page.goto('/');
  const card=page.locator('.storefront-card').filter({hasText:'Аглаонема Мария'});
  await card.scrollIntoViewIfNeeded();
  const scale=card.locator('.care-scale');
  await expect(scale).toHaveCSS('background-image',/linear-gradient/);
  await expect(scale).toHaveAttribute('data-level','1');
  const direction=await scale.evaluate(el=>getComputedStyle(el).getPropertyValue('--care-position').trim());expect(direction).toBe('88%');
  const heart=await card.locator('.storefront-fav svg').boundingBox();
  const star=await card.locator('.storefront-rating svg').boundingBox();
  expect(star!.width).toBe(heart!.width);expect(star!.height).toBe(heart!.height);
  if ((await page.viewportSize())!.width > 500) {
    expect(Math.abs(star!.y-heart!.y)).toBeLessThanOrEqual(1);
    expect(star!.x+star!.width).toBeLessThanOrEqual(heart!.x);
  }
  await expect(card.getByRole('img',{name:'Оценка 4.8 из 5, отзывов: 12'})).toBeVisible();
  const price=card.locator('.storefront-price');
  await expect(price).toHaveCSS('text-align','right');
  expect(await price.locator('strong').evaluate(el=>parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(18);
  const button=await card.getByRole('button',{name:'В корзину',exact:true}).boundingBox();
  expect(button!.width).toBeGreaterThan(50);
  const withoutReviews=page.locator('.storefront-card').filter({hasText:'Кашпо'}).locator('.storefront-rating');
  await expect(withoutReviews).toHaveAttribute('aria-hidden','true');
  await expect(withoutReviews.locator('svg')).toHaveCount(0);
});


test("@desktop @phone unrated products reserve rating space and keep purchase widths equal", async ({page})=>{
  await mockApi(page);
  await page.route('**/api/v1/catalog',route=>route.fulfill({json:{products:[catalogue[0],{...catalogue[0],id:'2',sku:'2',name:'Без отзывов',reviewsCount:0,rating:0}]}}));
  const widths=(await page.viewportSize())!.width>850?[1280]:[375,390];
  await page.goto('/');
  for(const width of widths){
    await page.setViewportSize({width,height:900});
    await expect(page.locator('.storefront-card')).toHaveCount(2);
    const cards=page.locator('.storefront-card');
    const empty=cards.nth(1).locator('.storefront-rating');
    await expect(empty).toHaveAttribute('aria-hidden','true');await expect(empty.locator('svg')).toHaveCount(0);
    const buttons=await cards.locator('.storefront-buy > button:not(.storefront-fav), .catalog-cart-controls').all();
    const boxes=await Promise.all(buttons.map(button=>button.boundingBox()));
    expect(Math.abs(boxes[0]!.width-boxes[1]!.width)).toBeLessThan(2);
    if (width === widths[0]) {
      const saved = page.waitForResponse(response => response.url().endsWith('/api/v1/cart') && response.request().method() === 'PUT');
      await cards.first().getByRole('button', { name: 'В корзину', exact: true }).click();
      expect((await saved).ok()).toBe(true);
    }
    const added=cards.first().getByRole('button',{name:'Перейти в корзину, 1 шт.'});
    await expect(added).toBeVisible();
    const content=await added.evaluate(el=>({width:el.clientWidth,scroll:el.scrollWidth}));expect(content.scroll).toBeLessThanOrEqual(content.width);
  }
  await page.reload();
  await expect(page.locator('.storefront-card').first().getByRole('button', { name: 'Перейти в корзину, 1 шт.', exact: true })).toBeVisible();
});
