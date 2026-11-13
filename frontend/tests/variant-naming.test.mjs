import assert from "node:assert/strict";
import test from "node:test";
import { categoryVariantName } from "../src/variantNaming.ts";
const variant = (diameter, label = "Основной размер") => ({
  label,
  attributes: { pot_diameter_cm: diameter },
});

test("plant names follow pot diameter, including later attribute changes", () => {
  const item = variant(12);
  assert.equal(categoryVariantName(item, "plants"), "D12");
  item.attributes.pot_diameter_cm = 24;
  assert.equal(categoryVariantName(item, "plants"), "D24");
  assert.equal(item.label, "Основной размер");
  assert.equal(categoryVariantName(variant("12,5"), "plants"), "D12.5");
});
test("missing or invalid diameters never become misleading names", () => {
  for (const value of [undefined, null, "", " ", 0, -1, NaN, Infinity, "unknown", false, []]) {
    assert.equal(categoryVariantName(variant(value), "plants"), "");
  }
});
test("other categories do not inherit the plant naming rule", () => {
  assert.equal(categoryVariantName(variant(12, "Пакет 5 л"), "soil"), "Пакет 5 л");
  assert.equal(categoryVariantName(variant(12), undefined), "Основной размер");
});
