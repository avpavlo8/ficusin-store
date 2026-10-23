import assert from "node:assert/strict";
import test from "node:test";
import { toKopecks, sumMoney, addMoney } from "../src/lib/money.ts";
import { discountedPrice } from "../src/lib/checkoutPrice.ts";
test("decimal rounding and integer cart sums", () => {
  assert.equal(toKopecks(1.005), 101);
  assert.equal(toKopecks(2.675), 268);
  assert.equal(toKopecks(-1.005), -101);
  assert.equal(
    sumMoney([
      { price: 0.1, quantity: 3 },
      { price: 0.2, quantity: 1 },
    ]),
    0.5,
  );
  assert.equal(addMoney(0.1, 0.2), 0.3);
  assert.equal(addMoney(100.01, -100), 0.01);
  assert.equal(sumMoney([{ price: discountedPrice(1, 3333), quantity: 3 }]), 2.01);
  for (const value of [Infinity, NaN, 1e14]) assert.throws(() => toKopecks(value));
});

test("discount keeps large and half-kopeck boundaries exact", () => {
  assert.equal(discountedPrice(0.01, 5000), 0.01);
  assert.equal(discountedPrice(0.03, 5000), 0.02);
  assert.equal(discountedPrice(999999999999.99, 1), 999899999999.99);
  assert.equal(discountedPrice(999999999999.95, 8999), 100099999999.99);
  assert.equal(discountedPrice(100, 9999), 10);
  assert.equal(
    addMoney(sumMoney([{ price: discountedPrice(0.03, 5000), quantity: 3 }]), 0.29),
    0.35,
  );
});
