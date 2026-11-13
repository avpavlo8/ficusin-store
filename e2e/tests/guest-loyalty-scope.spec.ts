import { expect, test } from "@playwright/test";
import { catalogue, guest, mockApi, owner } from "./helpers";

for (const signedIn of [false, true]) {
  for (const path of ["/", "/cart", "/checkout"]) {
    test(`@desktop @phone loyalty notice ${signedIn ? "member" : "guest"} ${path}`, async ({
      page,
    }) => {
      await mockApi(page, signedIn ? owner : guest);
      await page.context().route("**/api/v1/cart", (route) =>
        route.fulfill({
          json: {
            items: { "1": 1 },
            lines: [{ ...catalogue[0], variantLabel: "D12", available: true }],
            missingSkus: [],
          },
        }),
      );
      await page.goto(path);
      await expect(page.locator(".store-header")).toBeVisible();
      if (path === "/")
        await expect(page.locator(".storefront-grid")).toBeVisible();
      else
        await expect(
          page.getByRole("heading", {
            name: path === "/cart" ? "Корзина" : "Оформление заказа",
            exact: true,
          }),
        ).toBeVisible();
      const notice = page.getByRole("region", { name: "Накопительная скидка" });
      const title = page.getByText("Больше растений — больше скидка", {
        exact: true,
      });
      if (!signedIn && path === "/cart") {
        await expect(notice).toBeVisible();
        await expect(title).toHaveCount(1);
        await expect(
          notice.getByRole("link", { name: "Создать аккаунт", exact: true }),
        ).toHaveAttribute("href", /\/register\?returnTo=%2Fcart$/);
      } else {
        // The closed homepage drawer must not leave a hidden banner mounted.
        await expect(title).toHaveCount(0);
        await expect(notice).toHaveCount(0);
      }
    });
  }
}
