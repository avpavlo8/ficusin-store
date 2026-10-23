import { toKopecks } from "./money.ts";
// Match backend/internal/order.discountedMinor: the discount is rounded for
// each variant in kopecks before multiplying by its quantity.
export function discountedPrice(price: number, discountBps: number): number {
  const priceMinor = toKopecks(price);
  const bps = Math.max(0, Math.min(9000, Math.trunc(discountBps)));
  return Number((BigInt(priceMinor) * BigInt(10000 - bps) + 5000n) / 10000n) / 100;
}
