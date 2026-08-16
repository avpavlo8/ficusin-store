import { useCallback, useEffect, useState } from "react";
import { STORAGE_EVENT } from "../StoreHeader";

type Cart = Record<string, number>;
const CART_KEY = "ficusin-cart";

function readCart(): Cart {
  try { return JSON.parse(localStorage.getItem(CART_KEY) || "{}") as Cart; }
  catch { return {}; }
}

function storeCart(cart: Cart) {
  localStorage.setItem(CART_KEY, JSON.stringify(cart));
  window.dispatchEvent(new Event(STORAGE_EVENT));
}

export function useSharedCart() {
  const [cart, setCartState] = useState<Cart>(readCart);
  useEffect(() => {
    const sync = () => setCartState(readCart());
    window.addEventListener("storage", sync);
    window.addEventListener(STORAGE_EVENT, sync);
    return () => {
      window.removeEventListener("storage", sync);
      window.removeEventListener(STORAGE_EVENT, sync);
    };
  }, []);
  const setCart = useCallback((next: Cart | ((current: Cart) => Cart)) => setCartState((current) => {
    const value = typeof next === "function" ? next(current) : next;
    storeCart(value);
    return value;
  }), []);
  return [cart, setCart] as const;
}
