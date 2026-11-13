export type CustomerOrderView = "work" | "completed" | "cancelled";

type DatedOrder = { orderNumber: string; createdAt: string };

// Offset pages can overlap when a new order arrives while history is open.
// Keep one current row per order and preserve newest-first ordering.
export function mergeCustomerOrderPage<T extends DatedOrder>(previous: T[], incoming: T[]): T[] {
  const unique = new Map(previous.map((order) => [order.orderNumber, order]));
  for (const order of incoming) unique.set(order.orderNumber, order);
  return [...unique.values()].sort((a, b) => {
    const aTime = Date.parse(a.createdAt) || 0;
    const bTime = Date.parse(b.createdAt) || 0;
    return bTime - aTime;
  });
}
