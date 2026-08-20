import { useCallback, useEffect, useState } from "react";

export type Cart = Record<string, number>;
type CartUpdate = Cart | ((current: Cart) => Cart);

let snapshot: Cart = {};
let loaded = false;
let loading: Promise<void> | null = null;
let writes: Promise<void> = Promise.resolve();
const listeners = new Set<(cart: Cart) => void>();

// Зеркало последнего показанного состава корзины.
//
// Корзина живёт только на сервере, а страницы магазина открываются по
// настоящим адресам: каждый переход — новая загрузка и новый запрос. Если
// он не дошёл, показывать пустую корзину нельзя: это не факт, а домысел.
// Пустая корзина вместо собранной выглядит как потеря, и покупатель уходит.
//
// Пять минут — это страховка на переход и на пропавшую связь, а не вторая
// вечная копия. Прежнюю вечную копию убрали именно потому, что она умела
// расходиться с сервером и жить своей жизнью.
const mirrorKey = "ficusin-cart-mirror";
const mirrorLifetimeMs = 5 * 60 * 1000;

function clean(cart: Cart): Cart {
  return Object.fromEntries(Object.entries(cart).filter(([id, quantity]) => id && Number.isInteger(quantity) && quantity > 0));
}

function rememberMirror(cart: Cart) {
  try {
    window.localStorage.setItem(mirrorKey, JSON.stringify({ at: Date.now(), items: cart }));
  } catch {
    // Приватный режим Safari умеет запрещать запись. Магазин от этого
    // теряет страховку, но работать не перестаёт.
  }
}

function readMirror(): Cart | null {
  try {
    const raw = window.localStorage.getItem(mirrorKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { at?: number; items?: Cart };
    if (!parsed.items || typeof parsed.at !== "number") return null;
    if (Date.now() - parsed.at > mirrorLifetimeMs) {
      window.localStorage.removeItem(mirrorKey);
      return null;
    }
    return clean(parsed.items);
  } catch {
    return null;
  }
}

function publish(cart: Cart) {
  snapshot = clean(cart);
  rememberMirror(snapshot);
  listeners.forEach((listener) => listener(snapshot));
}

function loadCart(force = false): Promise<void> {
  if (loaded && !force) return Promise.resolve();
  if (loading && !force) return loading;
  loading = fetch("/api/v1/cart", { credentials: "same-origin", cache: "no-store" })
    .then(async (response) => {
      if (!response.ok) throw new Error("Не удалось загрузить корзину");
      const body = await response.json() as { items?: Cart };
      return clean(body.items || {});
    })
    .catch(() => null)
    .then((server) => {
      if (server === null) {
        // Спросить не получилось. Показываем то, что покупатель видел
        // последним, и не делаем вид, что корзина пуста.
        const mirror = readMirror();
        if (mirror) publish(mirror);
        return;
      }
      loaded = true;
      publish(server);
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
    // Корзина, которая не загрузилась, не повод не дать положить растение:
    // решение покупателя сохранится в зеркале и уедет следующей попыткой.
    if (loaded) apply(); else void loadCart().catch(() => undefined).then(apply);
  }, []);
  return [cart, setCart] as const;
}
