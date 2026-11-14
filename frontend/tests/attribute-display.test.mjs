import assert from "node:assert/strict";
import test from "node:test";
import { displayAttributeValue } from "../src/product/types.ts";

test("PIM display values override both machine codes and the seeded fallback without relocalizing labels", () => {
  assert.equal(
    displayAttributeValue({ value: "matte_ceramic_v2", displayValue: "Матовая керамика" }),
    "Матовая керамика",
  );
  assert.equal(displayAttributeValue({ value: "custom", displayValue: "warm" }), "warm");
  assert.equal(
    displayAttributeValue({
      value: ["custom_a", "custom_b"],
      displayValue: ["Авторское_название", "Второе название"],
    }),
    "Авторское_название, Второе название",
  );
  assert.equal(displayAttributeValue({ value: 99, displayValue: 0, unit: "см" }), "0 см");
});

test("option labels localize custom multi-values and preserve raw values for filtering", () => {
  const attribute = {
    value: ["velvet_custom", "smooth_custom"],
    optionLabels: { velvet_custom: "Бархатная", smooth_custom: "Гладкая" },
  };
  assert.equal(displayAttributeValue(attribute), "Бархатная, Гладкая");
  assert.deepEqual(attribute.value, ["velvet_custom", "smooth_custom"]);
});

test("metadata-free attributes keep boolean, numeric units, seeded and unknown-code fallbacks", () => {
  assert.equal(displayAttributeValue({ value: false }), "Нет");
  assert.equal(displayAttributeValue({ value: true }), "Есть");
  assert.equal(displayAttributeValue({ value: 1.5, unit: "л" }), "1.5 л");
  assert.equal(displayAttributeValue({ value: "diffused" }), "Рассеянный свет");
  assert.equal(
    displayAttributeValue({ value: ["custom_unknown", "easy"] }),
    "custom unknown, Лёгкий",
  );
});
