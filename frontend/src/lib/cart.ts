import { useCallback, useEffect, useState } from "react";
import { apiJSON } from "./api";

export type Cart = Record<string, number>;
type CartUpdate = Cart | ((current: Cart) => Cart);

export type CartProduct = {
  id: string;
  sku: string;
  name: string;
  variantLabel: string;
  price: number;
  image: string;
  stock: number;
  available: boolean;
};

type CartResponse = { items?: Cart; lines?: CartProduct[]; missingSkus?: string[] };
export type CartMeta = {
  status: "loading" | "ready" | "saving" | "error";
  error: string;
  lines: CartProduct[];
  missingSkus: string[];
  retry: () => void;
};

type State = Omit<CartMeta, "retry"> & { cart: Cart };

let state: State = { cart: {}, status: "loading", error: "", lines: [], missingSkus: [] };
let loaded = false;
let loading: Promise<void> | null = null;
let writes: Promise<void> = Promise.resolve();
const listeners = new Set<(next: State) => void>();

function clean(cart: Cart): Cart {
  return Object.fromEntries(Object.entries(cart).filter(([id, quantity]) => id && Number.isInteger(quantity) && quantity > 0));
}

function publish(next: Partial<State>) {
  state = { ...state, ...next, cart: next.cart ? clean(next.cart) : state.cart };
  listeners.forEach((listener) => listener(state));
}

function loadCart(force = false): Promise<void> {
  if (loaded && !force) return Promise.resolve();
  if (loading && !force) return loading;
  if (!loaded) publish({ status: "loading", error: "" });
  loading = apiJSON<CartResponse>("/api/v1/cart", { credentials: "same-origin", cache: "no-store" })
    .then((body) => {
      publish({ cart: body.items || {}, lines: body.lines || [], missingSkus: body.missingSkus || [], status: "ready", error: "" });
      loaded = true;
    })
    .catch((error: Error) => {
      publish({ status: "error", error: error.message || "Не удалось загрузить корзину" });
      throw error;
    })
    .finally(() => { loading = null; });
  return loading;
}

function saveCart(cart: Cart): Promise<void> {
  const expected = clean(cart);
  writes = writes.then(async () => {
    publish({ status: "saving", error: "" });
    const body = await apiJSON<CartResponse>("/api/v1/cart", {
      method: "PUT",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items: expected }),
    });
    publish({ cart: body.items || expected, lines: body.lines || [], missingSkus: body.missingSkus || [], status: "ready", error: "" });
  }).catch(async (error: Error) => {
    publish({ status: "error", error: error.message || "Не удалось сохранить корзину" });
  });
  return writes;
}

export function useSharedCart() {
  const [view, setView] = useState<State>(state);
  useEffect(() => {
    // Remove the obsolete durable browser copy left by earlier releases.
    window.localStorage.removeItem("ficusin-cart");
    const sync = (next: State) => setView(next);
    listeners.add(sync);
    void loadCart().catch(() => undefined);
    return () => { listeners.delete(sync); };
  }, []);
  const setCart = useCallback((update: CartUpdate) => {
    const apply = () => {
      const next = clean(typeof update === "function" ? update(state.cart) : update);
      publish({ cart: next });
      void saveCart(next);
    };
    if (loaded) apply(); else void loadCart().then(apply).catch(() => undefined);
  }, []);
  const retry = useCallback(() => { void loadCart(true).catch(() => undefined); }, []);
  return [view.cart, setCart, { status: view.status, error: view.error, lines: view.lines, missingSkus: view.missingSkus, retry }] as const;
}
