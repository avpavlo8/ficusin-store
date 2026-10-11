// Match backend/internal/order.discountedMinor: the discount is rounded for
// each variant in kopecks before multiplying by its quantity.
export function discountedPrice(price: number, discountBps: number): number {
  const priceMinor = Math.round(price * 100);
  const bps = Math.max(0, Math.min(9000, Math.trunc(discountBps)));
  return Math.floor((priceMinor * (10000 - bps) + 5000) / 10000) / 100;
}
