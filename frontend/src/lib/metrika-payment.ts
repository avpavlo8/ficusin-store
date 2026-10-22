import { paymentResultState, type PaymentResultOrder } from "./payment-result";

type Metrika = (...args: unknown[]) => void;
const reported = new Set<string>();

// This is a browser conversion, not the canonical sales ledger. Only the
// authenticated server response may establish payment success.
export async function reportPaidOrder(order: PaymentResultOrder): Promise<void> {
  const privacy = navigator as Navigator & { globalPrivacyControl?: boolean };
  if (order.metrikaServer === true || privacy.globalPrivacyControl || navigator.doNotTrack === "1" || paymentResultState(order) !== "success") return;
  const key = `ficusin-metrika-paid:${order.orderNumber}`;
  if (reported.has(key)) return;
  try { if (localStorage.getItem(key)) return; } catch { /* Memory still prevents repeated polling events. */ }
  reported.add(key);
  let sent = false;
  try {
    const response = await fetch("/api/v1/analytics/config", { cache: "no-store" });
    if (!response.ok) return;
    const config = await response.json() as { yandexMetrikaId?: number };
    const counter = Number(config.yandexMetrikaId);
    if (!Number.isSafeInteger(counter) || counter <= 0) return;
    // The async first-party loader may not have installed its queue yet.
    let ym = (window as unknown as { ym?: Metrika }).ym;
    for (let attempt = 0; !ym && attempt < 20; attempt++) {
      await new Promise<void>((resolve) => window.setTimeout(resolve, 250));
      ym = (window as unknown as { ym?: Metrika }).ym;
    }
    if (!ym) return;
    // Keep order identifiers and payment credentials out of external analytics.
    // Carrier delivery and accidental overpayments are not shop revenue.
    ym(counter, "reachGoal", "paid_order", {
      order_price: order.deliveryPayee === "carrier" ? order.subtotal : order.total,
      currency: "RUB",
    });
    sent = true;
    try { localStorage.setItem(key, "1"); } catch { /* Optional analytics cannot interrupt payment UI. */ }
  } catch { /* A blocked counter or unavailable config must not affect the order. */ }
  finally { if (!sent) reported.delete(key); }
}
