import assert from "node:assert/strict";
import test from "node:test";
import { initialAttributeFilters, initialRangeFilters } from "../src/lib/catalog-query-filters.ts";

test("URL attribute filters retain values in a prototype-free dictionary", () => {
  const filters = initialAttributeFilters(
    "?filter.light_level=low_light&filter.__proto__=polluted&filter.constructor=untrusted&q=ignored",
  );
  assert.equal(Object.getPrototypeOf(filters), null);
  assert.equal(filters.light_level, "low_light");
  assert.equal(Object.hasOwn(filters, "__proto__"), true);
  assert.equal(filters.__proto__, "polluted");
  assert.equal(filters.constructor, "untrusted");
  assert.equal(Object.hasOwn(filters, "q"), false);
  assert.equal(Object.prototype.polluted, undefined);
});

test("range limits merge in either order without inheriting special object properties", () => {
  const ranges = initialRangeFilters(
    "?max.height_cm=50&min.height_cm=10&min.__proto__=2&max.__proto__=8&max.constructor=9",
  );
  assert.equal(Object.getPrototypeOf(ranges), null);
  assert.deepEqual(ranges.height_cm, { min: "10", max: "50" });
  assert.deepEqual(ranges.__proto__, { min: "2", max: "8" });
  assert.deepEqual(ranges.constructor, { min: "", max: "9" });
  assert.equal(Object.hasOwn(Object.prototype, "min"), false);
  assert.equal(Object.hasOwn(Object.prototype, "max"), false);
});
