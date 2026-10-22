import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi } from "./helpers";

test("@desktop @phone discount plants show an instance price and a shop visit instead of ordinary checkout", async ({ page }) => {
 await mockApi(page);
 await page.route("**/api/v1/discount-plants",route => route.fulfill({json:{items:[{id:1,article:"RET-000001",name:"Аглаонема Вайт Ленс",label:"D12",price:1000,discountPercent:30,discountedPrice:700,image:"/assets/hero-monstera.webp"}]}}));
 await page.goto("/discounts");
 await expect(page.getByRole("heading",{name:"Растения со скидкой",level:1,exact:true})).toBeVisible();
 const card=page.locator(".discount-plants-grid article");await expect(card).toContainText("RET-000001");await expect(card).toContainText("700 ₽");await expect(card.locator("del")).toHaveText("1 000 ₽");await expect(card).toContainText("На восстановлении");await expect(card).toContainText("Фото из каталога");await expect(card.getByRole("link",{name:"Посмотреть в магазине ↗"})).toHaveAttribute("href","/contacts");await expect(page.getByRole("button",{name:/Купить|В корзину/})).toHaveCount(0);expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
});

test("@desktop discount page distinguishes an empty list from a failed request", async ({page})=>{
 await mockApi(page);await page.route("**/api/v1/discount-plants",route=>route.fulfill({json:{items:[]}}));await page.goto("/discounts");await expect(page.getByRole("heading",{name:"Сейчас таких растений нет"})).toBeVisible();await page.unroute("**/api/v1/discount-plants");await page.route("**/api/v1/discount-plants",route=>route.fulfill({status:503,json:{error:"unavailable"}}));await page.reload();await expect(page.getByRole("alert")).toContainText("Не удалось загрузить растения");
});
