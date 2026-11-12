import assert from "node:assert/strict";
import test from "node:test";
import { chatReferenceHref, chatOrderStatus } from "../src/lib/chat-reference.ts";
test("product uses canonical public code and unavailable snapshots have no link", () => {
  const card = { kind: "product", id: "9001", label: "Товар", available: true };
  assert.equal(chatReferenceHref(card), "/product/9001");
  assert.equal(chatReferenceHref({ ...card, available: false }), undefined);
});
test("orders use separate customer/staff destinations without capability tokens", () => {
  const card = { kind: "order", id: "201", number: "0000-201", label: "Заказ", available: true };
  assert.equal(chatReferenceHref(card), "/account/orders/0000-201");
  assert.equal(chatReferenceHref(card, true), "/admin?section=orders&order=201");
  assert.equal(chatOrderStatus("shipped"), "В доставке");
});
