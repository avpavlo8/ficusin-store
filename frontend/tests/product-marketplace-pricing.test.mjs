import assert from "node:assert/strict";
import test from "node:test";
import { marketplacePricingInput } from "../src/productMarketplacePricing.ts";
const dimensions = {
  package_length_cm: 20,
  package_width_cm: 15,
  package_height_cm: 40,
  package_winter_length_cm: 25,
  package_winter_width_cm: 20,
  package_winter_height_cm: 45,
};
test("retail quote uses the selected seasonal dimensions without altering site price", () => {
  const summer = marketplacePricingInput("4300", dimensions, "summer");
  const winter = marketplacePricingInput("4300", dimensions, "winter");
  assert.equal(summer.body.retailRub, 4300);
  assert.equal(winter.body.retailRub, 4300);
  assert.equal(summer.body.package.lengthCm, 20);
  assert.equal(winter.body.package.lengthCm, 25);
  assert.equal(winter.body.package.widthCm, 20);
  assert.equal(winter.body.package.heightCm, 45);
  assert.equal(winter.body.mode, "retail");
});
test("missing winter dimensions cannot fall back to summer", () => {
  const input = marketplacePricingInput(
    "4300",
    { ...dimensions, package_winter_width_cm: null },
    "winter",
  );
  assert.match(input.error, /зимней/);
  assert.equal(input.body, undefined);
});
test("incomplete or invalid values cannot produce a quote", () => {
  for (const price of ["", "0", "-1", "NaN"])
    assert.ok(marketplacePricingInput(price, dimensions, "summer").error);
  for (const size of [null, "", -5, 0, 1001, "NaN"])
    assert.ok(
      marketplacePricingInput("4300", { ...dimensions, package_length_cm: size }, "summer").error,
    );
});
