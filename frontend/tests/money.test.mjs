import assert from "node:assert/strict";
import test from "node:test";
import { toKopecks, sumMoney, addMoney } from "../src/lib/money.ts";
import { discountedPrice } from "../src/lib/checkoutPrice.ts";
test("decimal rounding and integer cart sums", () => {
  assert.equal(toKopecks(1.005), 101);
  assert.equal(toKopecks(2.675), 268);
  assert.equal(toKopecks(-1.005), -101);
  assert.equal(sumMoney([{price: 0.1,quantity: 3},{price: 0.2,quantity: 1}]), 0.5);
  assert.equal(addMoney(0.1,0.2), 0.3);
  assert.equal(addMoney(100.01,-100),0.01);
  assert.equal(sumMoney([{price:discountedPrice(1,3333),quantity:3}]),2.01);
  for (const value of [Infinity,NaN,1e14]) assert.throws(()=>toKopecks(value));
});
