import { expect, test } from "@playwright/test";

test("production homepage filters by budget and shows the live variant height and care scale", async ({page,request})=>{
  const response=await request.get("/api/v1/catalog");
  expect(response.ok()).toBeTruthy();
  const products=(await response.json()).products as Array<{id:string;price:number;name:string;catalogSection:string;heightCm?:number;careLevel?:string;stock:number;filterAttributes?:Array<{code:string;value:unknown}>}>;
  const positive=products.filter(p=>p.price>0);
  expect(positive.length).toBeGreaterThan(0);
  const budget=Math.min(...positive.map(p=>p.price));
  await page.goto("/");
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  const first=page.locator(".storefront-card").first();
  const href=await first.locator(".storefront-name").getAttribute("href");
  const current=products.find(product=>href===`/product/${product.id}`)!;
  expect(current).toBeTruthy();
  if(current.catalogSection==="plants") {
    await expect(first.locator(".storefront-attribute-badges")).toHaveCount(0);
    const specs=first.locator(".storefront-plant-specs");
    if(current.heightCm && current.heightCm>0) await expect(specs).toContainText(`Высота: ${current.heightCm} см`);
    else await expect(specs).toContainText("Высота: Не указана");
    const code=String(current.filterAttributes?.find(attribute=>attribute.code==="care_level")?.value||current.careLevel||"");
    const level=({easy:1,medium:2,demanding:3} as Record<string,number>)[code];
    if (level) await expect(first.locator(".care-scale")).toHaveAttribute("data-level", String(level));
    else await expect(first.locator(".care-scale")).toHaveCount(0);
  }
  if(current.stock>0) await expect(first.locator(".storefront-price em")).toHaveCount(0);
  else {
    await expect(first.locator(".storefront-price")).not.toContainText("≈");
    await expect(first.locator(".storefront-price")).toContainText("Под заказ · цена ориентировочная");
  }
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
    await expect(menu.locator(".mobile-menu-documents summary")).toHaveText("О магазине");
    expect(await menu.locator(".mobile-menu-links").evaluateAll(els=>els.every(el=>getComputedStyle(el).gridTemplateColumns.split(" ").length===1))).toBe(true);
    await expect(menu.getByRole("button",{name:"Закрыть меню",exact:true})).toBeFocused();
    const bounds=await menu.boundingBox(),viewport=page.viewportSize()!;
    expect(bounds!.width).toBeLessThan(viewport.width);
    expect(bounds!.height).toBeLessThan(viewport.height*.8);
    await expect(page.locator(".mobile-menu-backdrop")).toBeVisible();
    await expect(page.locator("body")).toHaveCSS("overflow","hidden");
    await page.keyboard.press("Escape");
    await expect(menu).not.toBeVisible();
    await expect(page.locator(".mobile-menu-backdrop")).toBeHidden();
    await expect(page.locator("body")).not.toHaveCSS("overflow","hidden");
    await expect(trigger).toBeFocused();
    await page.getByRole("button",{name:"Поиск",exact:true}).click();
    await expect(page.locator(".mobile-search")).toBeVisible();
    await expect(page.locator("body")).not.toHaveCSS("overflow","hidden");
    await page.getByRole("button",{name:"Закрыть поиск",exact:true}).click();
    const toolbar=page.locator(".home-catalog-toolbar");
    for(const control of [toolbar.locator(".home-filter-button"),toolbar.locator(".home-sort summary")]) {
      const box=await control.boundingBox(), icon=await control.locator("svg").boundingBox();
      expect(Math.abs(icon!.x+icon!.width/2-box!.x-box!.width/2)).toBeLessThan(1);
      expect(Math.abs(icon!.y+icon!.height/2-box!.y-box!.height/2)).toBeLessThan(1);
    }
    await expect(page.locator(".storefront-card .cart-button-label").first()).toBeHidden();
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
  const {categories}=await categoryResponse.json() as {categories:Array<{id:number;parentId:number|null;slug:string;name:string}>};
  const category=categories.find(item=>products.some(product=>product.categoryId===item.id)&&!categories.some(child=>child.parentId===item.id));
  expect(category).toBeTruthy();
  await page.goto("/#catalog");
  const picker=page.getByLabel("Категория",{exact:true});
  await expect(picker.locator(`option[value="${category!.slug}"]`)).toHaveCount(1);
  const phone=(await page.viewportSize())!.width<=850;
  const navigation=page.getByRole("navigation",{name:"Категории товаров"});
  if(phone) { await page.getByRole("button",{name:"Фильтры",exact:true}).click(); await picker.selectOption(category!.slug); }
  else {
    await expect(picker).toBeHidden();
    const side=page.locator(".storefront-side");
    await page.getByRole("link",{name:/Выбрать своё растение/}).click();
    await page.mouse.wheel(0,250);
    await expect(side).toBeVisible();
    const stickyTop=await side.evaluate(el=>parseFloat(getComputedStyle(el).top));
    await expect.poll(async()=>Math.round((await side.boundingBox())!.y)).toBe(Math.round(stickyTop));
    const initial=await side.boundingBox();
    const main=await page.locator(".storefront-main").boundingBox();
    expect(initial!.x+initial!.width).toBeLessThan(main!.x);
    await page.mouse.wheel(0,500);
    await expect.poll(async()=>Math.round((await side.boundingBox())!.y)).toBe(Math.round(stickyTop));
    await navigation.locator(`a[href="/catalog/${category!.slug}"]`).click();
  }
  await expect(page).toHaveURL(new RegExp(`/\\?category=${category!.id}#catalog$`));
  await expect(page.locator(".storefront-head h2")).toHaveText(category!.name);
  await expect(page.locator(".home-hero")).toBeAttached();
  await expect(page.getByRole("region",{name:"Подборки растений"})).toBeAttached();
  await expect(picker).toHaveValue(category!.slug);
  if((await page.viewportSize())!.width<=850) await expect(page.locator(".storefront-side")).toBeHidden();
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  const links=await page.locator(".storefront-card .storefront-name").evaluateAll(els=>els.map(el=>el.getAttribute("href")!));
  for(const link of links) expect(products.find(product=>link===`/product/${product.id}`)?.categoryId).toBe(category!.id);
  if(phone) await picker.selectOption("");
  else await navigation.getByRole("link",{name:/Весь каталог/}).click();
  await expect(page).toHaveURL(/\/#catalog$/);
  await expect(picker).toHaveValue("");
});
