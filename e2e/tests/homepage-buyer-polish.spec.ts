import { expect, test } from "@playwright/test";
import { catalogue, mockApi, setStoredCounts } from "./helpers";

test("@desktop @phone a plants-only homepage has useful filters and a budget", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/catalog", route => route.fulfill({json:{products:catalogue.filter(p=>p.catalogSection==="plants")}}));
  await page.goto("/");
  await page.getByRole("button",{name:/Фильтры/}).click();
  const panel=page.locator(".home-filter-panel");
  await expect(panel).toContainText("Освещение");
  await panel.getByLabel("Цена: до",{exact:true}).fill("2000");
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await expect(page.locator(".storefront-card")).toContainText("Аглаонема");
  await expect(page).toHaveURL(/priceMax=2000/);
  await page.reload();
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await page.getByRole("button",{name:/Фильтры/}).click();
  await panel.getByRole("button",{name:"Сбросить фильтры"}).click();
  await expect(page.locator(".storefront-card")).toHaveCount(3);
});

test("@desktop @phone legacy numeric favorites remain visible and unavailable entries are explained", async ({ page }) => {
  await page.addInitScript(()=>localStorage.setItem("ficusin-favorites",JSON.stringify([1,999,1])));
  await mockApi(page);
  await page.goto("/favorites");
  await expect(page.locator(".storefront-card")).toHaveCount(1);
  await expect(page.locator(".favorites-unavailable")).toContainText("1 сохранённый товар недоступен");
  await expect(page.locator(".favorites-page")).not.toContainText("В избранном пока пусто");
  await page.locator(".storefront-card").getByRole("button",{name:"Убрать из избранного"}).click();
  await expect(page.locator(".storefront-card")).toHaveCount(0);
  await expect(page.locator(".favorites-unavailable")).toBeVisible();
});

test("@desktop @phone delivery is not promised free before a delivery choice", async ({ page }) => {
  await setStoredCounts(page,[],{"1":1});
  await mockApi(page);
  await page.goto("/checkout");
  const summary=page.locator(".checkout-order-summary");
  await expect(summary).toContainText("Выберите способ получения");
  await expect(summary).toContainText("Растения, без доставки");
  await expect(summary).not.toContainText("Итого к оплате");
});

test("@desktop search closes a catalogue menu instead of covering its result", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  const menu=page.locator(".desktop-nav > .header-dropdown").first();
  await menu.locator(":scope > summary").click();
  const search=page.locator(".header-search input");
  await search.fill("фикус");
  await search.press("Enter");
  await expect(menu).not.toHaveAttribute("open","");
  await expect(page.getByRole("heading",{name:"Результаты поиска"})).toBeVisible();
});

test("@phone mobile menu contains keyboard focus, closes on Escape and restores its trigger", async ({ page }) => {
  await mockApi(page);
  await page.goto("/");
  const trigger=page.getByRole("button",{name:"Открыть меню"});
  await trigger.click();
  const menu=page.getByRole("dialog",{name:"Меню магазина"});
  await expect(menu.getByRole("button",{name:"Закрыть меню"})).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(menu.getByRole("link",{name:"Реквизиты"})).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(menu.getByRole("button",{name:"Закрыть меню"})).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).not.toBeVisible();
  await expect(trigger).toBeFocused();
});

test("@phone footer contacts can be scrolled above bottom navigation on phone and tablet", async ({ page }) => {
  await mockApi(page);
  for(const width of [390,768]) {
    await page.setViewportSize({width,height:844});
    await page.goto("/");
    const address=page.locator(".store-footer-address");
    await address.scrollIntoViewIfNeeded();
    await page.evaluate(()=>window.scrollTo(0,document.documentElement.scrollHeight));
    const geometry=await page.evaluate(()=>({address:document.querySelector(".store-footer-address")!.getBoundingClientRect().bottom,nav:document.querySelector(".tab-bar")!.getBoundingClientRect().top}));
    expect(geometry.address).toBeLessThanOrEqual(geometry.nav);
  }
});

test("@desktop @phone reduced motion keeps content visible without decorative animation", async ({ page }) => {
  await page.emulateMedia({reducedMotion:"reduce"});
  await mockApi(page);
  await page.goto("/");
  await expect(page.locator(".home-hero-visual > img")).toBeVisible();
  await expect(page.locator(".storefront-card").first()).toBeVisible();
  expect(await page.locator(".home-hero-visual > img").evaluate(el=>getComputedStyle(el).animationName)).toBe("none");
  await expect(page.locator(".storefront-size").first()).toContainText("Ø 12 см");
  await expect(page.getByRole("link",{name:/Как упаковываем/})).toHaveAttribute("href","/delivery-and-returns");
});

test("@desktop @phone purchase icon and label do not overlap", async ({page})=>{
  await mockApi(page);
  await page.goto("/");
  const card=page.locator(".storefront-card").filter({hasText:"Аглаонема Мария"});
  const button=card.getByRole("button",{name:"В корзину",exact:true});
  await button.scrollIntoViewIfNeeded();
  const geometry=await button.evaluate(el=>({icon:el.querySelector("svg")!.getBoundingClientRect().right,label:el.querySelector(".cart-button-label")!.getBoundingClientRect().left,right:el.getBoundingClientRect().right,card:el.closest("article")!.getBoundingClientRect().right}));
  expect(geometry.icon).toBeLessThanOrEqual(geometry.label);
  expect(geometry.right).toBeLessThanOrEqual(geometry.card);
});


test("@desktop @phone clearing a submitted search restores the catalogue and URL", async ({page})=>{
  await mockApi(page);
  await page.goto("/?q=фикус");
  if((await page.viewportSize())!.width<=850) await page.getByRole("button",{name:"Поиск",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Результаты поиска"})).toBeVisible();
  await page.getByRole("button",{name:"Очистить поиск",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Каталог",exact:true})).toBeVisible();
  await expect(page).not.toHaveURL(/q=/);
  await expect(page.locator(".storefront-card")).toHaveCount(catalogue.length);
});


test("@desktop @phone categories remain visible with one root and nested plant kinds", async ({page})=>{
  await mockApi(page);
  await page.route("**/api/v1/categories",route=>route.fulfill({json:{categories:[
    {id:1,parentId:null,name:"Растения",slug:"plants",sortOrder:0},
    {id:3,parentId:1,name:"Комнатные растения",slug:"indoor-plants",sortOrder:0},
    {id:4,parentId:3,name:"Аглаонема",slug:"aglaonema",sortOrder:0},
    {id:5,parentId:3,name:"Фикус",slug:"ficus",sortOrder:1},
    {id:99,parentId:3,name:"Пустая категория",slug:"empty-category",sortOrder:2},
  ]}}));
  await page.goto("/");
  const picker=page.getByLabel("Категория",{exact:true});
  await expect(picker).toBeVisible();
  await expect(picker.locator('option[value="ficus"]')).toContainText("Фикус · 2");
  await expect(picker.locator('option[value="empty-category"]')).toHaveCount(0);
  await page.goto("/catalog/indoor-plants");
  await expect(picker).toHaveValue("indoor-plants");
  await expect(picker.locator('option[value="indoor-plants"]')).toContainText("Комнатные растения · 3");
  await picker.selectOption("ficus");
  await expect(page).toHaveURL(/\/catalog\/ficus$/);
  await expect(picker).toHaveValue("ficus");
  if((await page.viewportSize())!.width<=850) await expect(page.locator(".storefront-side")).toBeHidden();
  await expect(page.locator(".storefront-card")).toHaveCount(2);
  await expect(page.locator(".storefront-card").filter({hasText:"Аглаонема"})).toHaveCount(0);
  await picker.selectOption("");
  await expect(page).toHaveURL(/\/#catalog$/);
  await expect(page.locator(".storefront-card")).toHaveCount(catalogue.length);
});
