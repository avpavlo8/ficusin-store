import { useEffect, useState } from "react";
import CheckoutHost, { CartProduct } from "./CheckoutHost";
import { StoreHeader } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";

export default function CartPage() {
  const [products, setProducts] = useState<CartProduct[]>([]);
  const [cart, setCart] = useSharedCart();
  useEffect(() => {
    fetch("/api/v1/catalog", { cache: "no-store" }).then((response) => response.json())
      .then((data: { products?: CartProduct[] }) => setProducts(data.products || [])).catch(() => setProducts([]));
  }, []);
  return <main className="cart-page">
    <StoreHeader cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)} />
    <CheckoutHost cart={cart} products={products} cartOpen={true}
      cartPage onCartOpenChange={(open) => { if (!open) window.location.assign("/#catalog"); }} onCartChange={setCart} />
  </main>;
}
