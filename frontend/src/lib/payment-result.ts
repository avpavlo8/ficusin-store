export type PaymentResultOrder = {
  metrikaServer?: boolean;
  orderNumber: string;
  status: string;
  paymentStatus: string;
  paymentAttemptStatus?: string;
  deliveryMethod: string;
  deliveryPayee?: "shop" | "carrier";
  subtotal: number;
  total: number;
  paidAmount: number;
  refundedAmount: number;
  amountDue: number;
};

export type PaymentResultState = "success" | "pending" | "failed" | "partial" | "cancelled" | "refunded" | "unconfirmed";

export function readPaymentResultOrder(value: unknown, expectedNumber: string): PaymentResultOrder | null {
  if (!value || typeof value !== "object") return null;
  const order = value as Record<string, unknown>;
  if (order.orderNumber !== expectedNumber) return null;
  if (!["status", "paymentStatus", "deliveryMethod"].every((key) => typeof order[key] === "string")) return null;
  if (order.paymentAttemptStatus != null && typeof order.paymentAttemptStatus !== "string") return null;
  if (!["subtotal", "total", "paidAmount", "refundedAmount", "amountDue"].every((key) => typeof order[key] === "number" && Number.isFinite(order[key]) && order[key] >= 0)) return null;
  if (order.deliveryPayee != null && order.deliveryPayee !== "shop" && order.deliveryPayee !== "carrier") return null;
  return order as PaymentResultOrder;
}

export function netOrderPayment(order: PaymentResultOrder): number {
  return Math.max(0, Math.round((order.paidAmount - order.refundedAmount) * 100)) / 100;
}

// A provider return URL is only a request to check. Success requires server
// payment status and the order balance, including refunds and carrier fees.
export function paymentResultState(order: PaymentResultOrder): PaymentResultState {
  const net = Math.round(netOrderPayment(order) * 100);
  if (order.paymentStatus === "refunded" && order.refundedAmount > 0 && net === 0) return "refunded";
  if (order.status === "cancelled") return "cancelled";
  const expected = Math.round((order.deliveryPayee === "carrier" ? order.subtotal : order.total) * 100);
  if (order.paymentStatus === "paid" && order.amountDue === 0 && expected > 0 && net >= expected) return "success";
  if (net > 0 && order.amountDue > 0) return "partial";
  if (["on_delivery", "invoice"].includes(order.paymentStatus)) return "unconfirmed";
  const attempt = order.paymentAttemptStatus;
  if (attempt) return ["failed", "cancelled", "canceled"].includes(attempt) ? "failed" : "pending";
  if (["failed", "cancelled", "canceled"].includes(order.paymentStatus)) return "failed";
  return "pending";
}
