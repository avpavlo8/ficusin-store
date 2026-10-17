import { expect, test } from "@playwright/test";
import { mockApi } from "./helpers";

for (const url of ["/", "/product/1", "/care/mealybug", "/cart"]) {
  test(`@desktop вложенные категории открываются на ${url}`, async ({ page }) => {
    await mockApi(page);
    await page.goto(url);
    const catalog = page.locator(".header-dropdown").first();
    await catalog.locator(":scope > summary").click();
    await catalog.locator(".header-submenu > summary").filter({hasText:"Растения"}).first().click();
    await catalog.locator(".header-submenu > summary").filter({hasText:"Комнатные растения"}).first().click();
    await expect(catalog.getByRole("link", { name: /Фикус/ })).toBeVisible();
    await expect(catalog).toHaveAttribute("open", "");
    expect(await catalog.getByRole("link", { name: /Фикус/ }).evaluate(el => getComputedStyle(el, "::after").display)).toBe("none");
  });
}

test("@desktop покупка выше рекомендаций, рекомендации под обеими колонками", async ({ page }) => {
  await mockApi(page);
  await page.goto("/product/1");
  const size = page.getByRole("group", { name: "Размер горшка" });
  await expect(size).toBeVisible();
  const commerce = page.locator(".pdp-order-column .pdp-commerce-box");
  await expect(commerce.getByRole("button", { name: "В корзину", exact: true })).toBeVisible();
  expect((await commerce.boundingBox())!.y).toBeGreaterThan((await size.boundingBox())!.y);
  const details = (await page.locator(".pdp-key-characteristics").boundingBox())!;
  const rail = (await page.locator(".pdp-summary .pdp-related").boundingBox())!;
  expect(rail.y).toBeGreaterThanOrEqual(details.y + details.height);
  expect(rail.x + rail.width).toBeGreaterThanOrEqual(details.x + details.width - 1);
  await expect(page.locator(".pdp-related")).toHaveCount(1);
  const cards = page.locator(".pdp-summary .related-card");
  const first = (await cards.nth(0).boundingBox())!;
  const second = (await cards.nth(1).boundingBox())!;
  expect(second.y).toBeCloseTo(first.y,0);
  expect(second.x).toBeGreaterThan(first.x+first.width);
  await expect(page.locator(".pdp-lead")).toHaveCount(0);
  await expect(page.getByText("Артикул", { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

test("@phone галерея листается горизонтальным свайпом, вертикальный жест сохраняет фото", async ({ page }) => {
  await mockApi(page);
  await page.route("**/api/v1/products/1", route => route.fulfill({ json: { product: {
    id:"1",name:"Антуриум",latin:"Anthurium",catalogSection:"plants",description:"Описание",
    images:["/assets/care/guide-v4/lighting.jpg","/assets/care/guide-v4/watering.jpg"],
    variants:[{id:1,sku:"1",label:"D12",price:1390,stock:5,images:[],attributes:[]}],
    recommendations:[],attributes:[],passport:{},reviews:[],importantWarnings:[],rating:0,reviewsCount:0,
  } } }));
  await page.goto("/product/1");
  const image = page.getByRole("button", { name: "Открыть фотографию на весь экран" });
  await expect(image.locator(".pdp-photo-track")).toBeVisible();
  expect(await image.locator(".pdp-photo-track").evaluate(el=>getComputedStyle(el).transitionDuration)).toBe("0.55s");
  await expect(page.locator(".pdp-image-count")).toHaveText("1 / 2");
  const swipe = async (x: number, y: number) => {
    await image.dispatchEvent("touchstart", { touches:[{identifier:1,clientX:280,clientY:350}] });
    await image.dispatchEvent("touchend", { touches:[],changedTouches:[{identifier:1,clientX:x,clientY:y}] });
  };
  const track = image.locator(".pdp-photo-track");
  await image.dispatchEvent("touchstart", {touches:[{identifier:1,clientX:280,clientY:350}]});
  await image.dispatchEvent("touchmove", {touches:[{identifier:1,clientX:200,clientY:353}]});
  await expect(track).toHaveCSS("transition-duration","0s");
  expect(await track.evaluate(el=>new DOMMatrix(getComputedStyle(el).transform).m41)).toBeCloseTo(-80,0);
  await expect(page.locator(".pdp-image-count")).toHaveText("1 / 2");
  await image.dispatchEvent("touchcancel", {touches:[]});
  await expect(track).toHaveCSS("transition-duration","0.55s");
  await expect.poll(()=>track.evaluate(el=>Math.round(new DOMMatrix(getComputedStyle(el).transform).m41))).toBe(0);
  await image.dispatchEvent("touchstart", {touches:[{identifier:1,clientX:280,clientY:350}]});
  await image.dispatchEvent("touchmove", {touches:[{identifier:1,clientX:250,clientY:351}]});
  await image.dispatchEvent("touchend", {touches:[],changedTouches:[{identifier:1,clientX:250,clientY:351}]});
  await expect(page.locator(".pdp-image-count")).toHaveText("1 / 2");
  await expect.poll(()=>track.evaluate(el=>Math.round(new DOMMatrix(getComputedStyle(el).transform).m41))).toBe(0);
  await swipe(70,355);
  await expect(page.locator(".pdp-image-count")).toHaveText("2 / 2");
  await image.click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await swipe(270,550);
  await expect(page.locator(".pdp-image-count")).toHaveText("2 / 2");
  await page.waitForTimeout(450);
  await image.click();
  await expect(page.getByRole("dialog")).toBeVisible();
});

for (const device of ["@phone", "@desktop"]) {
  test(`${device} поиск показывает только результаты и позволяет очистить запрос`, async ({page}) => {
    await mockApi(page);
    await page.goto("/?q=фикус");
    await expect(page.getByRole("heading", {name:"Результаты поиска"})).toBeVisible();
    await expect(page.locator(".home-hero,.catalog-landing-hero,.storefront-tree")).toHaveCount(0);
    await expect(page.locator(".storefront-side")).toBeHidden();
    if (device === "@phone") await page.getByRole("button", {name:"Поиск по каталогу", exact:true}).first().click();
    const search = page.locator(device === "@phone" ? ".mobile-catalog-search" : ".header-search");
    await search.getByRole("combobox").fill("фику");
    await expect(search.getByRole("listbox")).toBeVisible();
    await expect(search.getByRole("option").first()).toBeVisible();
    await expect(page.locator(".storefront-shell")).toBeVisible();
    await expect(page.locator(".storefront-head")).toContainText("фикус");
    await expect(page).toHaveURL(/q=/);
    await search.getByRole("combobox").fill("несуществующее растение 987654");
    await expect(page.locator(".storefront-shell")).toBeVisible();
    await expect(page.locator(".storefront-head")).toContainText("фикус");
    await search.getByRole("combobox").press("Enter");
    await expect(page.getByText("Ничего не нашли", {exact:true})).toBeVisible();
    await search.getByRole("button",{name:"Очистить поиск"}).click();
    await expect(page.getByText("Ничего не нашли", {exact:true})).toHaveCount(0);
    await expect(page).not.toHaveURL(/q=/);
    await expect(page.locator(".home-hero")).toBeVisible();
  });
}

test("@desktop шаги первых дней одинаковой ширины и оставляют место фотографии", async ({page}) => {
  await mockApi(page);
  await page.goto("/product/1");
  await page.getByRole("tab",{name:"Уход",exact:true}).click();
  const steps = await page.locator(".plant-arrival li").evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {width:r.width,right:r.right}}));
  expect(steps).toHaveLength(5);
  expect(steps[3].width).toBeCloseTo(steps[0].width,0);
  const block = (await page.locator(".plant-arrival").boundingBox())!;
  expect(Math.max(...steps.map(s=>s.right))).toBeLessThan(block.x+block.width*.75);
});
