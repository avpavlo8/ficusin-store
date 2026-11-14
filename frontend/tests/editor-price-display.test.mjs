import { strict as assert } from "node:assert";
import { test } from "node:test";
import { wbPricePresentation } from "../src/editorPriceDisplay.ts";

test("WB integer discounts expose the effective price rather than promising the calculated target", () => {
  assert.deepEqual(wbPricePresentation(150025, 200000), {
    baseRub: 2000,
    discountPercent: 24,
    effectiveMinor: 152000,
  });
  assert.deepEqual(wbPricePresentation(150000, 200000), {
    baseRub: 2000,
    discountPercent: 25,
    effectiveMinor: 150000,
  });
  assert.deepEqual(wbPricePresentation(150025, null), {
    baseRub: 1500,
    discountPercent: 0,
    effectiveMinor: 150000,
  });
  assert.deepEqual(wbPricePresentation(150025, 150025), {
    baseRub: 1500,
    discountPercent: 0,
    effectiveMinor: 150000,
  });
});
