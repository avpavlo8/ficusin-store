import { expect, test, type Page } from "@playwright/test";
import { mockApi, setStoredCounts } from "./helpers";

async function open(page: Page, items = { "1": 1 }) {
  await mockApi(page);
  await setStoredCounts(page, [], items);
  await page.goto("/checkout");
  await expect(page.getByRole("group", { name: "Способ доставки" })).toBeVisible();
}
async function contact(page: Page) {
  await page.getByRole("button", { name: "К контактам" }).click();
  await page.getByLabel("Имя", {exact:true}).fill("Мария Тест");
  await page.getByLabel("Телефон", {exact:true}).fill("9151234567");
  await page.getByLabel("Email для чека", {exact:true}).fill("test@example.invalid");
}
async function cdek(page: Page, quote: () => number) {
  await page.route("**/api/v1/delivery/cdek?action=status", r => r.fulfill({json:{available:true}}));
  await page.route("**/api/v1/delivery/cdek?action=cities&*", r => r.fulfill({json:{cities:[{code:44,city:"Москва"}]}}));
  await page.route("**/api/v1/delivery/cdek?action=offices&*", r => r.fulfill({json:{offices:[{code:"MSK1",name:"ПВЗ",location:{city:"Москва",address:"Тверская, 1"}}]}}));
  await page.route("**/api/v1/delivery/cdek", r => r.fulfill({json:{quotes:[
    {tariffCode:136,tariffName:"Склад-склад",price:quote(),daysMin:2,daysMax:3},
    {tariffCode:138,tariffName:"Дверь-склад",price:1200,daysMin:2,daysMax:3},
  ]}}));
}
async function chooseOffice(page: Page) {
  await page.getByRole("radio",{name:/СДЭК по России/}).check();
  await page.getByLabel("Город получения").fill("Мос");
  await page.getByRole("group",{name:"Найденные города"}).getByRole("button").click();
  await page.getByLabel("Пункт выдачи",{exact:true}).click();
  await page.getByRole("group",{name:"Пункты выдачи"}).getByRole("button").click();
}

test("@phone @desktop purchase starts with delivery and a usable order summary", async ({page}, info) => {
  await open(page);
  const summary=page.locator(".checkout-order-summary");
  await expect(summary).toContainText("Выберите способ получения");
  if ((page.viewportSize()?.width ?? 0)<900) await summary.locator("summary").click();
  await expect(summary).toContainText("Горшок Ø 12 см");
  if ((page.viewportSize()?.width ?? 0)<900) {
    await expect(page.locator(".tab-bar")).toHaveCount(0);
    const a=(await summary.boundingBox())!;const b=(await page.locator(".checkout-layout>form").boundingBox())!;
    expect(a.y+a.height).toBeLessThanOrEqual(b.y);
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  await page.screenshot({path:info.outputPath("delivery.png"),fullPage:true});
});

test("@phone @desktop invalid phone is stopped before review; reload restores draft but not consent", async ({page}, info) => {
  await open(page);
  await contact(page);
  await page.getByLabel("Телефон",{exact:true}).fill("123");
  await page.getByRole("button",{name:"Проверить заказ",exact:false}).click();
  await expect(page.getByLabel("Телефон",{exact:true})).toBeFocused();
  await expect(page.locator("#phone-error")).toContainText("полный российский номер");
  await page.getByLabel("Телефон",{exact:true}).fill("9151234567");
  await page.getByRole("button",{name:"Проверить заказ",exact:false}).click();
  await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Мария Тест");
  await page.locator('input[name="consent"]').check();
  await page.reload();
  await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Мария Тест");
  await expect(page.locator('input[name="consent"]')).not.toBeChecked();
  await page.getByRole("button",{name:"Изменить контакты"}).click();
  await expect(page.getByLabel("Email для чека")).toHaveValue("test@example.invalid");
  await page.screenshot({path:info.outputPath("contact.png"),fullPage:true});
});

test("@phone @desktop restored CDEK office gets a fresh quotation, duplicate service windows are not choices", async ({page}) => {
  await mockApi(page); await setStoredCounts(page,[],{"1":1});
  let price=590; await cdek(page,()=>price);
  await page.goto("/checkout"); await chooseOffice(page);
  await expect(page.locator(".cdek-tariff")).toHaveCount(0);
  await expect(page.locator(".cdek-quote")).toContainText("590");
  await expect(page.locator(".checkout-page-panel")).not.toContainText("Склад-склад");
  await contact(page); await page.getByRole("button",{name:"Проверить заказ"}).click();
  price=650; await page.reload();
  await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Тверская, 1");
  await expect(page.locator(".checkout-order-summary")).toContainText(/Доставка\s*650/);
  await expect(page.locator(".checkout-order-summary")).not.toContainText("590");
});

test("@phone @desktop pickup payment explanation follows the selected method",async ({page},info)=>{
  await mockApi(page); await setStoredCounts(page,[],{"1":1});
  await page.route("**/api/v1/payments/methods?*",r=>r.fulfill({json:{methods:[{id:"online",title:"Картой на сайте",note:"Защищённая оплата"},{id:"on_delivery",title:"При получении",note:"В магазине"}]}}));
  await page.goto("/checkout");await contact(page);await page.getByRole("button",{name:"Проверить заказ"}).click();
  await expect(page.getByRole("button",{name:/Перейти к оплате/})).toBeVisible();
  await page.getByRole("radio",{name:/При получении/}).check();
  await expect(page.getByText("Оплатите заказ в магазине, когда будете забирать растения. Сейчас платить не нужно.")).toBeVisible();
  await expect(page.getByRole("button",{name:/Оформить самовывоз/})).toBeVisible();
  await page.screenshot({path:info.outputPath("review.png"),fullPage:true});
});

test("@phone @desktop submit failure keeps the reviewed draft and offers safe retry",async({page})=>{
  await open(page); await contact(page); await page.getByRole("button",{name:"Проверить заказ"}).click();
  let calls=0;
  await page.route("**/api/v1/orders",async r=>{calls++;await r.fulfill(calls===1?{status:503,json:{error:"Сервис временно недоступен"}}:{json:{orderNumber:"TEST-RETRY",paymentStatus:"pay_on_pickup"}})});
  await page.locator('input[name="consent"]').check(); await page.getByRole("button",{name:/Оформить самовывоз/}).click();
  await expect(page.getByRole("alert")).toContainText("Данные сохранены");
  await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Мария Тест");
  await page.getByRole("button",{name:/Оформить самовывоз/}).click();
  await expect(page.getByText("#TEST-RETRY")).toBeVisible();expect(calls).toBe(2);
  expect(await page.evaluate(()=>sessionStorage.getItem("ficusin-checkout-session-v1"))).toBeNull();
});

test("@phone @desktop a mixed order asks for confirmation instead of payment",async({page})=>{
  await open(page,{"1":1,"3":1}); await contact(page); await page.getByRole("button",{name:"Проверить заказ"}).click();
  await expect(page.getByText("Оформить без оплаты",{exact:true})).toBeVisible();
  await expect(page.locator(".payment-note")).toContainText("Срок поставки пока не подтверждён");
  await expect(page.getByRole("radio",{name:/Картой на сайте/})).toHaveCount(0);
});

test("@phone product has a reachable mobile purchase and cart actions are comfortable",async({page},info)=>{
  await mockApi(page);await page.goto("/product/1");
  await expect(page.locator(".pdp-mobile-buy")).toBeVisible();
  const action=page.locator(".pdp-mobile-buy button");await expect(action).toBeInViewport();
  const geometry=await page.evaluate(()=>({action:document.querySelector('.pdp-mobile-buy')!.getBoundingClientRect().bottom,nav:document.querySelector('.tab-bar')!.getBoundingClientRect().top}));
  expect(geometry.action).toBeLessThanOrEqual(geometry.nav+1);
  await page.screenshot({path:info.outputPath("pdp.png")});
  await action.click();await expect(action).toHaveText("Перейти в корзину");await action.click();
  await expect(page).toHaveURL(/\/cart$/);
  const plus=page.getByRole("button",{name:/Увеличить количество/});expect((await plus.boundingBox())!.width).toBeGreaterThanOrEqual(44);
  const total=(await page.locator(".cart-summary-total").boundingBox())!;const next=(await page.getByRole("button",{name:/Оформить заказ/}).boundingBox())!;expect(total.y+total.height).toBeLessThanOrEqual(next.y);
  await page.screenshot({path:info.outputPath("cart.png"),fullPage:true});
});

test("@phone @desktop an unavailable quotation keeps the PVZ and offers manager confirmation without payment",async({page})=>{
  await mockApi(page);await setStoredCounts(page,[],{"1":1});await cdek(page,()=>590);
  await page.route("**/api/v1/delivery/cdek",r=>r.abort("failed"));
  await page.goto("/checkout");await chooseOffice(page);
  await expect(page.locator(".cdek-quote")).toContainText("Рассчитает менеджер");
  await contact(page);await page.getByRole("button",{name:"Проверить заказ"}).click();
  await expect(page.getByRole("button",{name:"Отправить заказ"})).toBeVisible();
  await expect(page.getByRole("button",{name:/Перейти к оплате/})).toHaveCount(0);
});

test("@phone @desktop a draft from a different signed-in customer cannot prefill guest contacts",async({page})=>{
  await page.addInitScript(()=>sessionStorage.setItem("ficusin-checkout-session-v1",JSON.stringify({savedAt:Date.now(),owner:"customer:other",step:3,profile:{name:"Другой покупатель",phone:"+79150000000",email:"other@example.invalid",address:""},delivery:"pickup",deliveryConfirmed:true,paymentMethod:"on_delivery",city:null,officeCode:"",repack:false})));
  await open(page); await page.getByRole("button",{name:"К контактам"}).click();
  await expect(page.getByLabel("Имя",{exact:true})).toHaveValue("");
  await expect(page.getByLabel("Телефон",{exact:true})).toHaveValue("");
});
