import { expect, test } from "@playwright/test";

test("production homepage filters the live catalogue by budget and shows a variant size", async ({page,request})=>{
  const response=await request.get("/api/v1/catalog");
  expect(response.ok()).toBeTruthy();
  const products=(await response.json()).products as Array<{id:string;price:number;name:string}>;
  const positive=products.filter(p=>p.price>0);
  expect(positive.length).toBeGreaterThan(0);
  const budget=Math.min(...positive.map(p=>p.price));
  await page.goto("/");
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  await expect(page.locator(".storefront-size").first()).not.toBeEmpty();
  await page.getByRole("button",{name:/Фильтры/}).click();
  await page.getByLabel("Цена: до",{exact:true}).fill(String(budget));
  await expect(page).toHaveURL(new RegExp(`priceMax=${budget}`));
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  const links=await page.locator(".storefront-card .storefront-name").evaluateAll(els=>els.map(el=>el.getAttribute("href")!));
  for(const link of links){
    const product=products.find(p=>link===`/product/${p.id}`);
    expect(product).toBeTruthy();
    expect(product!.price).toBeLessThanOrEqual(budget);
  }
});

test("production saved numeric IDs resolve to the live product instead of an empty favourites page", async ({page,request})=>{
  const response=await request.get("/api/v1/catalog");
  expect(response.ok()).toBeTruthy();
  const {products}=await response.json() as {products:Array<{id:string;name:string}>};
  const product=products.find(p=>/^\d+$/.test(p.id));
  expect(product).toBeTruthy();
  await page.addInitScript(id=>localStorage.setItem("ficusin-favorites",JSON.stringify([Number(id),"homepage-smoke-unavailable"])),product!.id);
  await page.goto("/favorites");
  await expect(page.locator(".storefront-card .storefront-name")).toHaveAttribute("href",`/product/${product!.id}`);
  await expect(page.locator(".favorites-unavailable")).toContainText("1 сохранённый товар недоступен");
  await expect(page.locator(".favorites-page")).not.toContainText("В избранном пока пусто");
});

test("production mobile footer and keyboard menu are reachable", async ({page,request})=>{
  const asset=await request.get("/assets/product-photo-pending.svg");
  expect(asset.ok()).toBeTruthy();
  expect(asset.headers()["content-type"]).toContain("image/svg+xml");
  await page.goto("/");
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  if((await page.viewportSize())!.width<=850){
    const trigger=page.getByRole("button",{name:"Открыть меню",exact:true});
    await trigger.click();
    const menu=page.getByRole("dialog",{name:"Меню магазина"});
    await expect(menu.getByRole("button",{name:"Закрыть меню",exact:true})).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(menu).not.toBeVisible();
    await expect(trigger).toBeFocused();
    const hint=page.getByRole("button",{name:"Скрыть подсказку",exact:true});
    if(await hint.isVisible())await hint.click();
    await page.locator(".store-footer-address").scrollIntoViewIfNeeded();
    await page.evaluate(()=>window.scrollTo({top:document.documentElement.scrollHeight,behavior:"instant"}));
    const rects=await page.evaluate(()=>({address:document.querySelector(".store-footer-address")!.getBoundingClientRect().bottom,nav:document.querySelector(".tab-bar")!.getBoundingClientRect().top}));
    expect(rects.address).toBeLessThanOrEqual(rects.nav);
  }
});


test("production clearing a search restores the catalogue without another Enter", async ({page})=>{
  await page.goto("/?q=фикус");
  if((await page.viewportSize())!.width<=850) await page.getByRole("button",{name:"Поиск",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Результаты поиска"})).toBeVisible();
  await page.getByRole("button",{name:"Очистить поиск",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Каталог",exact:true})).toBeVisible();
  await expect(page).not.toHaveURL(/q=/);
  await expect(page.locator(".storefront-card").first()).toBeVisible();
});


test("production categories are available beside the catalogue and open the chosen plant kind", async ({page,request})=>{
  const [catalogResponse,categoryResponse]=await Promise.all([request.get("/api/v1/catalog"),request.get("/api/v1/categories")]);
  expect(catalogResponse.ok()).toBeTruthy(); expect(categoryResponse.ok()).toBeTruthy();
  const {products}=await catalogResponse.json() as {products:Array<{id:string;categoryId:number}>};
  const {categories}=await categoryResponse.json() as {categories:Array<{id:number;parentId:number|null;slug:string}>};
  const category=categories.find(item=>products.some(product=>product.categoryId===item.id)&&!categories.some(child=>child.parentId===item.id));
  expect(category).toBeTruthy();
  await page.goto("/");
  const picker=page.getByLabel("Категория",{exact:true});
  await expect(picker.locator(`option[value="${category!.slug}"]`)).toHaveCount(1);
  await picker.selectOption(category!.slug);
  await expect(page).toHaveURL(new RegExp(`/catalog/${category!.slug}$`));
  await expect(picker).toHaveValue(category!.slug);
  if((await page.viewportSize())!.width<=850) await expect(page.locator(".storefront-side")).toBeHidden();
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  const links=await page.locator(".storefront-card .storefront-name").evaluateAll(els=>els.map(el=>el.getAttribute("href")!));
  for(const link of links) expect(products.find(product=>link===`/product/${product.id}`)?.categoryId).toBe(category!.id);
  await picker.selectOption("");
  await expect(page).toHaveURL(/\/#catalog$/);
  await expect(picker).toHaveValue("");
});
