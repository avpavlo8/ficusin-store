import assert from "node:assert/strict";
import test from "node:test";
import {
  gramsToKilograms,
  kilogramsToGrams,
  packagingWeightInput,
} from "../src/packagingWeight.ts";

test("kilogram entry is persisted in canonical grams", () => {
  for (const [input, grams] of [
    ["1", 1000],
    ["1.25", 1250],
    ["1,25", 1250],
    ["0.001", 1],
    [".5", 500],
    ["0", 0],
  ]) {
    assert.equal(kilogramsToGrams(input), grams);
    assert.equal(kilogramsToGrams(gramsToKilograms(grams)), grams);
  }
});

test("loading existing grams never reinterprets or changes historical weights", () => {
  assert.equal(gramsToKilograms(1), "0.001");
  assert.equal(gramsToKilograms(1000), "1");
  assert.equal(gramsToKilograms("1250"), "1.25");
  assert.equal(gramsToKilograms(0), "0");
  assert.deepEqual(packagingWeightInput, { min: "0.001", step: "0.001" });
});

test("clearing weight preserves an empty draft and invalid input cannot become NaN", () => {
  for (const input of ["", " ", "-1", "NaN", "Infinity", "1e3", "1.2.3", "9007199254740991"])
    assert.equal(kilogramsToGrams(input), null);
  for (const grams of [null, undefined, "", " ", "NaN", Infinity, -1, {}, true])
    assert.equal(gramsToKilograms(grams), "");
});
