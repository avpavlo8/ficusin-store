import assert from "node:assert/strict";
import test from "node:test";
import { netOrderPayment, paymentResultState, readPaymentResultOrder } from "../src/lib/payment-result.ts";

const order = {
  orderNumber: "ORDER-TEST", status: "confirmed", paymentStatus: "paid",
  deliveryMethod: "cdek", deliveryPayee: "shop", subtotal: 7450, total: 8040,
  paidAmount: 8040, refundedAmount: 0, amountDue: 0,
};

test("success requires server status and a settled balance", () => {
  assert.equal(paymentResultState(order), "success");
  assert.equal(paymentResultState({ ...order, paymentStatus: "pending" }), "pending");
  assert.equal(paymentResultState({ ...order, paidAmount: 0 }), "pending");
  assert.equal(paymentResultState({ ...order, paidAmount: 7450, amountDue: 590 }), "partial");
});

test("cancelled and refunded orders never display payment success", () => {
  assert.equal(paymentResultState({ ...order, status: "cancelled" }), "cancelled");
  assert.equal(paymentResultState({ ...order, status: "cancelled", paymentStatus: "refunded", refundedAmount: 8040 }), "refunded");
  assert.equal(paymentResultState({ ...order, paymentStatus: "failed", paidAmount: 0, amountDue: 8040 }), "failed");
});

test("a failed payment attempt is distinct from a cancelled order or a pending result", () => {
  const unpaid = { ...order, paidAmount: 0, amountDue: 8040 };
  for (const paymentStatus of ["failed", "cancelled", "canceled"]) {
    assert.equal(paymentResultState({ ...unpaid, paymentStatus }), "failed");
    assert.equal(paymentResultState({ ...unpaid, paymentStatus, status: "cancelled" }), "cancelled");
    assert.equal(paymentResultState({ ...unpaid, paymentStatus, paidAmount: 7450, amountDue: 590 }), "partial");
  }
  assert.equal(paymentResultState({ ...unpaid, paymentStatus: "pending" }), "pending");
  assert.equal(paymentResultState({ ...unpaid, paymentStatus: "on_delivery" }), "unconfirmed");
});

test("refunds and carrier delivery are included in the balance correctly", () => {
  assert.equal(paymentResultState({ ...order, refundedAmount: 590 }), "pending");
  assert.equal(paymentResultState({ ...order, deliveryPayee: "carrier", paidAmount: 7450 }), "success");
  assert.equal(netOrderPayment({ ...order, paidAmount: 0.3, refundedAmount: 0.1 }), 0.2);
});

test("the server attempt distinguishes refusal from an unpaid order without overriding paid money", () => {
  const unpaid = { ...order, paymentStatus: "pending", paidAmount: 0, amountDue: 8040 };
  assert.equal(paymentResultState({ ...unpaid, paymentAttemptStatus: "canceled" }), "failed");
  assert.equal(paymentResultState({ ...unpaid, paymentAttemptStatus: "pending" }), "pending");
  assert.equal(paymentResultState({ ...unpaid, paymentStatus: "cancelled", paymentAttemptStatus: "pending" }), "pending");
  assert.equal(paymentResultState({ ...order, paymentAttemptStatus: "canceled" }), "success");
  assert.equal(paymentResultState({ ...unpaid, paymentStatus: "on_delivery", paymentAttemptStatus: "canceled" }), "unconfirmed");
});

test("unrelated orders and malformed amounts are rejected", () => {
  assert.equal(readPaymentResultOrder(order, "OTHER-ORDER"), null);
  assert.equal(readPaymentResultOrder({ ...order, paidAmount: "8040" }, order.orderNumber), null);
  assert.equal(readPaymentResultOrder({ ...order, amountDue: -1 }, order.orderNumber), null);
  assert.equal(readPaymentResultOrder({ ...order, total: Infinity }, order.orderNumber), null);
  assert.equal(readPaymentResultOrder({ ...order, paymentAttemptStatus: 1 }, order.orderNumber), null);
  assert.equal(readPaymentResultOrder({ ...order, deliveryPayee: "unknown" }, order.orderNumber), null);
  assert.deepEqual(readPaymentResultOrder(order, order.orderNumber), order);
});
