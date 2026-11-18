import assert from "node:assert/strict";
import test from "node:test";
import picomatch from "picomatch";

test("complex single branches in repeated extglobs are treated as literals", () => {
  for (const operator of ["+", "*"]) {
    const pattern = operator + "(asset[0-9])";
    for (const maxExtglobRecursion of [undefined, false]) {
      const matcher = picomatch.makeRe(pattern, { maxExtglobRecursion });
      assert.equal(matcher.test(pattern), true);
      assert.equal(matcher.test("asset1asset2"), false);
    }
  }
});

test("fixed literal repetitions and ordinary Vite file globs still match", () => {
  assert.equal(picomatch("+(asset)")("assetasset"), true);
  assert.equal(picomatch("src/**/*.{ts,tsx}")("src/catalog/Product.tsx"), true);
  assert.equal(picomatch("src/**/*.{ts,tsx}")("src/catalog/Product.css"), false);
});
