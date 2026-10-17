import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mockApi, setStoredCounts } from "./helpers";

const routes = ["/", "/product/1", "/cart", "/checkout", "/login", "/privacy"];

for (const route of routes) {
  test(`@desktop WCAG: ${route} has no serious accessibility violations`, async ({ page }) => {
    await setStoredCounts(page, ["1"], ["/cart", "/checkout"].includes(route) ? { "1": 1 } : {});
    await mockApi(page);
    await page.goto(route);
    await page.waitForLoadState("networkidle");
    const result = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
      .analyze();
    const blocking = result.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
    expect(blocking, blocking.map((item) => `${item.id}: ${item.help} (${item.nodes.length})`).join("\n")).toEqual([]);
    if (route === "/checkout") {
      await page.getByRole("button",{name:"К контактам"}).click();
      await page.getByLabel("Имя",{exact:true}).fill("Проверка доступности");
      await page.getByLabel("Телефон",{exact:true}).fill("9151234567");
      await page.getByLabel("Email для чека",{exact:true}).fill("accessibility@example.invalid");
      await page.getByRole("button",{name:"Проверить заказ"}).click();
      const reviewed = await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21a","wcag21aa","wcag22aa"]).analyze();
      expect(reviewed.violations.filter(item=>item.impact==="serious"||item.impact==="critical")).toEqual([]);
    }
  });
}
