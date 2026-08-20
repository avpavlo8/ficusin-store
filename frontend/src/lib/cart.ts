import { useCallback, useEffect, useState } from "react";

export type Cart = Record<string, number>;
type CartUpdate = Cart | ((current: Cart) => Cart);

let snapshot: Cart = {};
let loaded = false;
let loading: Promise<void> | null = null;
let writes: Promise<void> = Promise.resolve();
const listeners = new Set<(cart: Cart) => void>();

function clean(cart: Cart): Cart {
  return Object.fromEntries(Object.entries(cart).filter(([id, quantity]) => id && Number.isInteger(quantity) && quantity > 0));
}

function publish(cart: Cart) {
  snapshot = clean(cart);
  listeners.forEach((listener) => listener(snapshot));
}

function loadCart(force = false): Promise<void> {
  if (loaded && !force) return Promise.resolve();
  if (loading && !force) return loading;
  loading = fetch("/api/v1/cart", { credentials: "same-origin", cache: "no-store" })
    .then(async (response) => {
      if (!response.ok) throw new Error("Не удалось загрузить корзину");
      const body = await response.json() as { items?: Cart };
      publish(body.items || {});
      loaded = true;
    })
    .finally(() => { loading = null; });
  return loading;
}

function saveCart(cart: Cart): Promise<void> {
  const expected = clean(cart);
  writes = writes.then(async () => {
    const response = await fetch("/api/v1/cart", {
      method: "PUT",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items: expected }),
    });
    if (!response.ok) throw new Error("Не удалось сохранить корзину");
  }).catch(async () => {
    await loadCart(true).catch(() => undefined);
  });
  return writes;
}

export function useSharedCart() {
  const [cart, setCartState] = useState<Cart>(snapshot);
  useEffect(() => {
    // Remove the obsolete durable browser copy left by earlier releases.
    window.localStorage.removeItem("ficusin-cart");
    const sync = (next: Cart) => setCartState(next);
    listeners.add(sync);
    void loadCart().catch(() => undefined);
    return () => { listeners.delete(sync); };
  }, []);
  const setCart = useCallback((update: CartUpdate) => {
    const apply = () => {
      const next = clean(typeof update === "function" ? update(snapshot) : update);
      publish(next);
      void saveCart(next);
    };
    if (loaded) apply(); else void loadCart().then(apply).catch(() => undefined);
  }, []);
  return [cart, setCart] as const;
}
