import { expect, test } from "@playwright/test";

test("Purchase delivery, validation and draft recovery work on the deployed bundle",async({page},info)=>{
  const response=await page.request.get("/api/v1/catalog");expect(response.ok()).toBeTruthy();
  const data=await response.json() as {products:Array<{sku:string;name:string;stock:number}>};
  const product=data.products.find(item=>item.stock>0);expect(product).toBeTruthy();
  let orderRequests=0;page.on("request",r=>{if(r.method()==="POST"&&new URL(r.url()).pathname==="/api/v1/orders")orderRequests++});
  try {
    const saved=await page.request.put("/api/v1/cart",{data:{items:{[product!.sku]:1}}});expect(saved.ok()).toBeTruthy();
    await page.goto("/checkout",{waitUntil:"domcontentloaded"});
    const summary=page.locator(".checkout-order-summary");
    await expect(page.getByRole("group",{name:"Способ доставки"})).toBeVisible();
    await expect(summary).toContainText("Выберите способ получения");
    if ((page.viewportSize()?.width??0)<900) await expect(page.locator(".tab-bar")).toHaveCount(0);
    await page.getByRole("button",{name:"К контактам"}).click();
    await page.getByLabel("Имя",{exact:true}).fill("Проверка интерфейса");
    await page.getByLabel("Телефон",{exact:true}).fill("123");
    await page.getByLabel("Email для чека",{exact:true}).fill("browser-check@example.invalid");
    await page.getByRole("button",{name:"Проверить заказ"}).click();
    await expect(page.getByLabel("Телефон",{exact:true})).toBeFocused();
    await expect(page.locator("#phone-error")).toBeVisible();
    await page.getByLabel("Телефон",{exact:true}).fill("9001234567");
    await page.getByRole("button",{name:"Проверить заказ"}).click();
    await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Проверка интерфейса");
    await page.reload({waitUntil:"domcontentloaded"});
    await expect(page.getByRole("region",{name:"Проверка данных"})).toContainText("Проверка интерфейса");
    await expect(page.locator('input[name="consent"]')).not.toBeChecked();
    await expect(page.getByRole("button",{name:/Перейти к оплате|Оформить самовывоз|Отправить заказ/})).toBeVisible();
    await page.screenshot({path:info.outputPath("purchase-live.png"),fullPage:true});
    expect(orderRequests).toBe(0);
  } finally {
    const cleared=await page.request.put("/api/v1/cart",{data:{items:{}}});expect(cleared.ok()).toBeTruthy();
    await page.evaluate(()=>sessionStorage.removeItem("ficusin-checkout-session-v1")).catch(()=>undefined);
  }
});
