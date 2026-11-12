import type { ChatReference } from "./chat";
export const chatOrderStatus = (value?: string) =>
  ({
    new: "Новый",
    confirmed: "Подтверждён",
    assembling: "Собирается",
    ready: "Готов к выдаче",
    shipped: "В доставке",
    completed: "Завершён",
    cancelled: "Отменён",
  })[value || ""] ||
  value ||
  "";
export function chatReferenceHref(reference: ChatReference, staff = false): string | undefined {
  if (!reference.available) return undefined;
  return reference.kind === "product"
    ? `/product/${encodeURIComponent(reference.id)}`
    : staff
      ? `/admin?section=orders&order=${encodeURIComponent(reference.id)}`
      : `/account/orders/${encodeURIComponent(reference.number || "")}`;
}
