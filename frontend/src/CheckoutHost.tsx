import { sumMoney } from "./lib/money";
import { storeSession } from "./lib/store-session";
import { useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { CartPanel, CheckoutPanel, type CartLine } from "./CartCheckout";
import { useCheckout } from "./useCheckout";
import { track } from "./lib/analytics";

type Cart = Record<string, number>;

export type CartProduct = {
  id: string;
  sku: string;
  name: string;
  price: number;
  image: string;
  stock?: number;
  variantLabel?: string;
  available?: boolean;
};

type StoreUser = {
  email: string;
  phone: string;
  fullName: string;
  lastName: string;
  patronymic: string;
  deliveryAddress: string;
  retailDiscountBps: number;
};

type CheckoutHostProps = {
  cart: Cart;
  products: CartProduct[];
  onCartChange: Dispatch<SetStateAction<Cart>>;
  checkoutPage?: boolean;
  cartStatus?: "loading" | "ready" | "saving" | "error";
  cartError?: string;
  missingCartItems?: number;
  onCartRetry?: () => void;
};

type CheckoutPageContentProps = Pick<
  CheckoutHostProps,
  "cartStatus" | "cartError" | "missingCartItems" | "onCartRetry"
> & {
  cartLines: CartLine[];
  cartCount: number;
  setCart: Dispatch<SetStateAction<Cart>>;
  user: StoreUser | null;
  pricingReady: boolean;
  pricingError: boolean;
};

// Keep checkout providers and form state scoped to /checkout. The component
// stays mounted after submit clears the cart so the order receipt remains visible.
function CheckoutPageContent({
  cartLines,
  cartCount,
  setCart,
  user,
  pricingReady,
  pricingError,
  cartStatus = "ready",
  cartError = "",
  missingCartItems = 0,
  onCartRetry,
}: CheckoutPageContentProps) {
  const [notice, setNotice] = useState("");
  const checkout = useCheckout({
    cartLines,
    cartCount,
    setCart,
    setNotice,
    discountBps: user?.retailDiscountBps ?? 0,
  });
  const { setCheckoutProfile } = checkout;
  useEffect(() => {
    if (!user) return;
    setCheckoutProfile({
      name: [user.lastName, user.fullName, user.patronymic].filter(Boolean).join(" "),
      phone: user.phone,
      email: user.email,
      address: user.deliveryAddress,
    });
  }, [user, setCheckoutProfile]);
  return cartStatus === "loading" ? (
    <section className="checkout-load-state" role="status">
      <h1>Загружаем оформление…</h1>
      <p>Проверяем состав и актуальные цены корзины.</p>
    </section>
  ) : cartStatus === "error" ? (
    <section className="checkout-load-state error" role="alert">
      <h1>Не удалось открыть оформление</h1>
      <p>{cartError}</p>
      {onCartRetry && (
        <button type="button" className="primary-button" onClick={onCartRetry}>
          Повторить
        </button>
      )}
    </section>
  ) : missingCartItems > 0 || cartLines.some((item) => item.available === false) ? (
    <section className="checkout-load-state error" role="alert">
      <h1>Состав корзины изменился</h1>
      <p>Вернитесь в корзину и удалите недоступные товары.</p>
      <a className="primary-button" href="/cart">
        Вернуться в корзину
      </a>
    </section>
  ) : cartStatus === "ready" && !cartLines.length && !checkout.panelProps.orderNumber ? (
    <section className="checkout-load-state">
      <h1>Корзина пуста</h1>
      <p>Перед оформлением добавьте товары.</p>
      <a className="primary-button" href="/#catalog">
        Перейти в каталог
      </a>
    </section>
  ) : (
    <CheckoutPanel
      user={!!user}
      pricingReady={pricingReady}
      pricingError={pricingError}
      {...checkout.panelProps}
      feedbackMessage={notice}
    />
  );
}

// Cart keys are Ficusin SKUs, not product-card codes. That is the only way
// two sizes of one product can coexist in the same order without collapsing
// into one line.
export default function CheckoutHost({
  cart: externalCart,
  products,
  onCartChange,
  checkoutPage = false,
  cartStatus = "ready",
  cartError = "",
  missingCartItems = 0,
  onCartRetry,
}: CheckoutHostProps) {
  const cart = externalCart;
  const setCart = onCartChange;
  const [user, setUser] = useState<StoreUser | null>(null);
  const [pricingReady, setPricingReady] = useState(false);
  const [pricingError, setPricingError] = useState(false);

  const cartLines = products
    .filter((product) => cart[product.sku])
    .map((product) => ({ ...product, id: product.sku, quantity: cart[product.sku] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = sumMoney(cartLines);
  useEffect(() => {
    if (cartLines.length)
      track("view_cart", {
        value: subtotal,
        quantity: cartCount,
        properties: { items: cartLines.length },
      });
  }, [checkoutPage, cartLines.length, cartCount, subtotal]);

  useEffect(() => {
    let cancelled = false;
    storeSession()
      .then(async (response) => {
        if (response.status === 401) return null;
        if (!response.ok) throw new Error("Не удалось загрузить профиль");
        return (await response.json()) as { user: StoreUser };
      })
      .then((result) => {
        if (cancelled) return;
        setPricingReady(true);
        if (!result?.user) return;
        setUser(result.user);
      })
      .catch(() => {
        if (!cancelled) setPricingError(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function setQuantity(sku: string, quantity: number) {
    setCart((current) => {
      const next = { ...current };
      if (quantity <= 0) {
        const removed = products.find((item) => item.sku === sku);
        track("remove_from_cart", {
          productCode: removed?.id,
          sku,
          value: removed?.price,
          quantity: current[sku] || 1,
          properties: { location: "cart" },
        });
        delete next[sku];
      } else {
        const product = products.find((item) => item.sku === sku);
        const limit = product?.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20;
        next[sku] = Math.min(limit, quantity);
      }
      return next;
    });
  }

  function beginCheckout() {
    if (
      cartStatus !== "ready" ||
      missingCartItems > 0 ||
      cartLines.some((item) => item.available === false)
    )
      return;
    track("begin_checkout", {
      value: subtotal,
      quantity: cartCount,
      properties: { items: cartLines.length },
    });
    window.location.assign("/checkout");
  }

  return (
    <div className="cart-checkout-host">
      {!checkoutPage && (
        <CartPanel
          guest={pricingReady && !user && !pricingError}
          lines={cartLines}
          subtotal={subtotal}
          onQuantityChange={setQuantity}
          onCheckout={beginCheckout}
          status={cartStatus}
          error={cartError}
          missingCount={missingCartItems}
          onRetry={onCartRetry}
        />
      )}

      {checkoutPage && (
        <CheckoutPageContent
          cartLines={cartLines}
          cartCount={cartCount}
          setCart={setCart}
          user={user}
          pricingReady={pricingReady}
          pricingError={pricingError}
          cartStatus={cartStatus}
          cartError={cartError}
          missingCartItems={missingCartItems}
          onCartRetry={onCartRetry}
        />
      )}
    </div>
  );
}
