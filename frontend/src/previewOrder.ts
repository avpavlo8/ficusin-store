import type { OrderDetail } from "./AccountOrderPage";

// Loaded only by Vite's DEV branch for the local confirmation walkthrough.
const previewOrder: OrderDetail = {
  orderNumber: "LOCAL-1024",
  deliveryMethod: "pickup",
  address: "Магазин Фикусин, Рязань",
  comment: "",
  customerName: "Анна",
  phone: "+7 900 000-00-00",
  email: "anna@example.com",
  status: "new",
  paymentStatus: "manager_confirmation",
  paymentMethod: "on_delivery",
  deliveryFee: 0,
  hasPreorder: true,
  deliveryFeePending: false,
  subtotal: 7450,
  total: 7450,
  paidAmount: 0,
  refundedAmount: 0,
  amountDue: 7450,
  paymentReady: false,
  createdAt: "2026-10-21T12:00:00+03:00",
  items: [
    { productName: "Аглаонема Вайт Ленс, D12", unitPrice: 1490, quantity: 4 },
    { productName: "Аглаонема Мария Кристина, D12", unitPrice: 1490, quantity: 1 },
  ],
  shipmentOffers: [],
};

export function getPreviewOrder(state: string | null): OrderDetail {
  if (state === "cancelled") return { ...previewOrder, status: "cancelled" };
  if (state === "confirmed" || state === "shipped" || state === "paid" || state === "payment-failed") {
    const paid = state === "shipped" || state === "paid";
    return {
      ...previewOrder, deliveryMethod: "cdek", address: "Москва, пункт выдачи СДЭК",
      status: state === "shipped" ? "shipped" : "confirmed", hasPreorder: false,
      paymentMethod: "online", paymentStatus: paid ? "paid" : "pending",
      paymentAttemptStatus: paid ? "paid" : state === "payment-failed" ? "canceled" : "pending",
      paymentReady: true, deliveryFee: 590, total: 8040,
      amountDue: paid ? 0 : 8040, paidAmount: paid ? 8040 : 0,
      trackNumber: state === "shipped" ? "LOCAL-CDEK-1024" : undefined,
    };
  }
  return previewOrder;
}
