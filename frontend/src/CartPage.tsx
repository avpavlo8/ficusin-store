import { useEffect, useMemo, useState } from "react";
import CheckoutHost from "./CheckoutHost";
import { StoreHeader, type HeaderMenuItem } from "./StoreHeader";
import { useSharedCart } from "./lib/cart";
import { sharedAPIJSON } from "./lib/api";
import { orderStorefrontCategories } from "./lib/category-order";
import "./styles/cart.css";
import "./styles/order-lamp.css";
import "./styles/checkout-page.css";

export default function CartPage({ checkout = false }: { checkout?: boolean }) {
  const [categories, setCategories] = useState<
    Array<{ id: number; parentId: number | null; name: string; slug: string; sortOrder: number }>
  >([]);
  const [cart, setCart, cartMeta] = useSharedCart();
  useEffect(() => {
    sharedAPIJSON<{ categories?: typeof categories }>("/api/v1/categories")
      .then((body: { categories?: typeof categories }) => setCategories(body.categories || []))
      .catch(() => setCategories([]));
  }, []);
  const headerMenus = useMemo(() => {
    const children = new Map<number | null, typeof categories>();
    categories.forEach((item) =>
      children.set(item.parentId, [...(children.get(item.parentId) || []), item]),
    );
    const order = orderStorefrontCategories<(typeof categories)[number]>;
    const branch = (item: (typeof categories)[number]): HeaderMenuItem => ({
      id: item.id,
      label: item.name,
      slug: item.slug,
      children: order(children.get(item.id) || []).map(branch),
    });
    return order(children.get(null) || []).map(branch);
  }, [categories]);
  return (
    <main
      className={checkout ? "cart-page checkout-page" : "cart-page"}
      aria-busy={cartMeta.status === "loading"}
    >
      <StoreHeader
        cartCount={Object.values(cart).reduce((sum, value) => sum + value, 0)}
        catalogMenuItems={headerMenus}
        onHomeCategoryPick={(id) => {
          window.location.assign(`/?category=${id}#catalog`);
        }}
      />
      <CheckoutHost
        cart={cart}
        products={cartMeta.lines}
        cartStatus={cartMeta.status}
        cartError={cartMeta.error}
        missingCartItems={cartMeta.missingSkus.length}
        onCartRetry={cartMeta.retry}
        checkoutPage={checkout}
        onCartChange={setCart}
      />
    </main>
  );
}
