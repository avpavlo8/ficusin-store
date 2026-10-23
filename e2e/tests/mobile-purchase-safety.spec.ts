import { expect, test } from "@playwright/test";
import { horizontalOverflow, mockApi } from "./helpers";

test("@phone быстрая покупка показывает название и сохраняет товар при переходе в корзину", async ({ page }, info) => {
  await mockApi(page);
  await page.goto("/product/1");
  const purchase = page.locator(".pdp-mobile-purchase");
  const title = page.locator(".pdp-summary h1");
  await expect(title).toHaveText("Аглаонема Мария");
  for (const width of [320, 375, 390]) {
    await page.setViewportSize({width, height: 664});
    await expect(purchase.locator(".pdp-mobile-name")).toHaveText(await title.innerText());
    await expect(purchase.locator(".pdp-mobile-name")).toBeInViewport();
    await expect(purchase.getByRole("button", {name:"В корзину",exact:true})).toBeInViewport();
    const buy=(await purchase.boundingBox())!;
    const nav=(await page.locator(".tab-bar").boundingBox())!;
    expect(buy.y+buy.height).toBeLessThanOrEqual(nav.y+1);
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(1);
  }
  await page.screenshot({path:info.outputPath("mobile-purchase-after.png")});
  await purchase.getByRole("button",{name:"В корзину",exact:true}).click();
  await purchase.getByRole("link",{name:"В корзину →",exact:true}).click();
  await expect(page).toHaveURL(/\/cart$/);
  await expect(page.locator(".cart-line")).toHaveCount(1);
  await expect(page.locator(".pdp-mobile-purchase")).toHaveCount(0);
});
