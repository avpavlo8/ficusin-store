import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mockApi, setStoredCounts } from "./helpers";

const routes = ["/", "/product/1", "/cart", "/login", "/privacy"];

for (const route of routes) {
  test(`@desktop WCAG: ${route} has no serious accessibility violations`, async ({ page }) => {
    await setStoredCounts(page, ["1"], route === "/cart" ? { "1": 1 } : {});
    await mockApi(page);
    await page.goto(route);
    await page.waitForLoadState("networkidle");
    const result = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
      .analyze();
    const blocking = result.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
    expect(blocking, blocking.map((item) => `${item.id}: ${item.help} (${item.nodes.length})`).join("\n")).toEqual([]);
  });
}
