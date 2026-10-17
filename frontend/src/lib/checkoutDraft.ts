export type CheckoutDraft = {
  savedAt: number;
  owner: string;
  step: 1 | 2 | 3;
  profile: { name: string; phone: string; email: string; address: string };
  delivery: string;
  deliveryConfirmed: boolean;
  paymentMethod: string;
  city: { code: number; city: string; region?: string } | null;
  officeCode: string;
  repack: boolean;
};

const key = "ficusin-checkout-session-v1";
const maxAge = 2 * 60 * 60 * 1000;

// A tab-scoped, expiring draft. Never persist consent, prices, payment links
// or provider quotes: each of those must be confirmed afresh.
export function readCheckoutDraft(): CheckoutDraft | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) || "null") as CheckoutDraft | null;
    if (!value || !Number.isFinite(value.savedAt) || Date.now() - value.savedAt > maxAge || value.savedAt > Date.now()) return null;
    if (![1, 2, 3].includes(value.step) || !["pickup", "cdek"].includes(value.delivery)) return null;
    if (!value.profile || !["name", "phone", "email", "address"].every(key => typeof value.profile[key as keyof typeof value.profile] === "string" && value.profile[key as keyof typeof value.profile].length <= 1000)) return null;
    if (typeof value.owner !== "string" || typeof value.officeCode !== "string" || typeof value.paymentMethod !== "string" || typeof value.repack !== "boolean" || typeof value.deliveryConfirmed !== "boolean") return null;
    if (value.city && (!Number.isInteger(value.city.code) || typeof value.city.city !== "string")) return null;
    return value;
  } catch { return null; }
}

export function saveCheckoutDraft(value: Omit<CheckoutDraft, "savedAt">) {
  try { sessionStorage.setItem(key, JSON.stringify({ ...value, savedAt: Date.now() })); } catch { /* Checkout also works with storage disabled. */ }
}

export function clearCheckoutDraft() {
  try { sessionStorage.removeItem(key); } catch { /* Storage may be disabled. */ }
}
