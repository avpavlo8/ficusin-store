import { useCallback, useEffect, useState } from "react";

export type Cart = Record<string, number>;
type CartUpdate = Cart | ((current: Cart) => Cart);

let snapshot: Cart = {};
let loaded = false;
let loading: Promise<void> | null = null;
let writes: Promise<void> = Promise.resolve();
const listeners = new Set<(cart: Cart) => void>();

// Черновик последней незаписанной корзины. Ссылки шапки и нижней панели
// ведут на настоящий адрес, браузер уходит со страницы и обрывает
// незавершённые запросы — растение, только что положенное в корзину, до
// сервера не доезжает. Заметнее всего это в Safari.
//
// Пять минут — это про «не потерять по дороге». Вечная вторая копия
// корзины в браузере однажды уже расходилась с сервером, и её убрали.
const pendingKey = "ficusin-cart-pending";
const pendingLifetimeMs = 5 * 60 * 1000;

function clean(cart: Cart): Cart {
  return Object.fromEntries(Object.entries(cart).filter(([id, quantity]) => id && Number.isInteger(quantity) && quantity > 0));
}

function rememberPending(cart: Cart) {
  try {
    window.localStorage.setItem(pendingKey, JSON.stringify({ at: Date.now(), items: cart }));
  } catch {
    // Приватный режим Safari умеет запрещать запись. Это ухудшает надёжность,
    // но не должно ронять корзину.
  }
}

function forgetPending() {
  try {
    window.localStorage.removeItem(pendingKey);
  } catch {
    // см. rememberPending
  }
}

function readPending(): Cart | null {
  try {
    const raw = window.localStorage.getItem(pendingKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { at?: number; items?: Cart };
    if (!parsed.items || typeof parsed.at !== "number") return null;
    if (Date.now() - parsed.at > pendingLifetimeMs) {
      forgetPending();
      return null;
    }
    return clean(parsed.items);
  } catch {
    forgetPending();
    return null;
  }
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
      const server = body.items || {};
      const pending = readPending();
      loaded = true;
      // Черновик побеждает: он и есть последнее решение покупателя, просто
      // не доехавшее до сервера. Дописываем и стираем черновик.
      if (pending && JSON.stringify(pending) !== JSON.stringify(clean(server))) {
        publish(pending);
        void saveCart(pending);
        return;
      }
      forgetPending();
      publish(server);
    })
    .finally(() => { loading = null; });
  return loading;
}

function saveCart(cart: Cart): Promise<void> {
  const expected = clean(cart);
  rememberPending(expected);
  writes = writes.then(async () => {
    const response = await fetch("/api/v1/cart", {
      method: "PUT",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items: expected }),
    });
    if (!response.ok) throw new Error("Не удалось сохранить корзину");
    forgetPending();
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
