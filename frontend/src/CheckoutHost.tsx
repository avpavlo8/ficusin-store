import { useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { CartDrawer, CheckoutPanel } from "./CartCheckout";
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
  cartOpen: boolean;
  onCartOpenChange: (open: boolean) => void;
  onCartChange: Dispatch<SetStateAction<Cart>>;
  cartPage?: boolean;
  checkoutPage?: boolean;
  cartStatus?: "loading" | "ready" | "saving" | "error";
  cartError?: string;
  missingCartItems?: number;
  onCartRetry?: () => void;
};

// Cart keys are Ficusin SKUs, not product-card codes. That is the only way
// two sizes of one product can coexist in the same order without collapsing
// into one line.
export default function CheckoutHost({
  cart: externalCart,
  products,
  cartOpen,
  onCartOpenChange,
  onCartChange,
  cartPage = false,
  checkoutPage = false,
  cartStatus = "ready",
  cartError = "",
  missingCartItems = 0,
  onCartRetry,
}: CheckoutHostProps) {
  const cart = externalCart;
  const setCart = onCartChange;
  const [notice, setNotice] = useState("");
  const [user, setUser] = useState<StoreUser | null>(null);
  const [pricingReady, setPricingReady] = useState(false);
  const [pricingError, setPricingError] = useState(false);

  const cartLines = products
    .filter((product) => cart[product.sku])
    .map((product) => ({ ...product, id: product.sku, quantity: cart[product.sku] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const checkout = useCheckout({ cartLines, cartCount, setCart, setNotice, discountBps: user?.retailDiscountBps ?? 0, initialOpen: checkoutPage });
  const { checkoutOpen, setCheckoutOpen, setCheckoutProfile } = checkout;
  // Local review of the post-order screens without creating an order or clearing the cart.
  const previewMode = import.meta.env.DEV && checkoutPage
    ? new URLSearchParams(window.location.search).get("preview")
    : null;
  const previewConfirmation = previewMode === "confirmation" || previewMode === "confirmation-pending";

  useEffect(() => {
    if ((cartPage || checkoutPage) && cartLines.length) track("view_cart", { value: subtotal, quantity: cartCount, properties: { items: cartLines.length } });
  }, [cartPage, checkoutPage, cartLines.length, cartCount, subtotal]);

  useEffect(() => {
    let cancelled = false;
    fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (response.status === 401) return null;
        if (!response.ok) throw new Error("Не удалось загрузить профиль");
        return (await response.json()) as { user: StoreUser };
      })
      .then((result) => {
        if (cancelled) return;
        setPricingReady(true);
        if (!result?.user) return;
        const profile = result.user;
        setUser(profile);
        setCheckoutProfile({
          name: [profile.lastName, profile.fullName, profile.patronymic].filter(Boolean).join(" "),
          phone: profile.phone,
          email: profile.email,
          address: profile.deliveryAddress,
        });
      })
      .catch(() => {
        if (!cancelled) setPricingError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [setCheckoutProfile]);

  useEffect(() => {
    document.body.classList.toggle("drawer-open", ((cartOpen && !cartPage) || checkoutOpen) && !checkoutPage);
    return () => document.body.classList.remove("drawer-open");
  }, [cartOpen, cartPage, checkoutOpen, checkoutPage]);

  function setQuantity(sku: string, quantity: number) {
    setCart((current) => {
      const next = { ...current };
      if (quantity <= 0) {
		const removed = products.find((item) => item.sku === sku);
		track("remove_from_cart", { productCode: removed?.id, sku, value: removed?.price, quantity: current[sku] || 1, properties: { location: "cart" } });
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
	if (cartStatus !== "ready" || missingCartItems > 0 || cartLines.some((item) => item.available === false)) return;
	track("begin_checkout", { value: subtotal, quantity: cartCount, properties: { items: cartLines.length } });
    window.location.assign("/checkout");
  }

  return (
    <div className="cart-checkout-host">
      {notice && !checkoutPage && <div className="toast" role="status">{notice}</div>}
      {cartStatus === "error" && !cartPage && !checkoutPage && <div className="toast cart-error-toast" role="alert"><span>{cartError || "Изменение корзины не сохранено"}</span>{onCartRetry && <button type="button" onClick={onCartRetry}>Повторить</button>}</div>}

      {((cartOpen && !cartPage) || checkoutOpen) && !checkoutPage && (
        <button
          className="overlay"
          aria-label="Закрыть"
          onClick={() => {
            onCartOpenChange(false);
            setCheckoutOpen(false);
          }}
        />
      )}

      {!checkoutPage && <CartDrawer
        open={cartOpen}
        lines={cartLines}
        subtotal={subtotal}
        onClose={() => onCartOpenChange(false)}
        onQuantityChange={setQuantity}
        onCheckout={beginCheckout}
        page={cartPage}
        status={cartStatus}
        error={cartError}
        missingCount={missingCartItems}
        onRetry={onCartRetry}
      />}

      {previewConfirmation ? <CheckoutPanel user={!!user} page pricingReady={pricingReady} pricingError={pricingError} {...checkout.panelProps} orderNumber="LOCAL-1024" orderConfirmationPending={previewMode === "confirmation-pending"} orderConfirmationReason={previewMode === "confirmation-pending" ? "preorder" : null} delivery="pickup" paymentMethod="on_delivery" />
        : checkoutPage && cartStatus === "loading" ? <section className="checkout-load-state" role="status"><h1>Загружаем оформление…</h1><p>Проверяем состав и актуальные цены корзины.</p></section>
        : checkoutPage && cartStatus === "error" ? <section className="checkout-load-state error" role="alert"><h1>Не удалось открыть оформление</h1><p>{cartError}</p>{onCartRetry && <button type="button" className="primary-button" onClick={onCartRetry}>Повторить</button>}</section>
          : checkoutPage && (missingCartItems > 0 || cartLines.some((item) => item.available === false)) ? <section className="checkout-load-state error" role="alert"><h1>Состав корзины изменился</h1><p>Вернитесь в корзину и удалите недоступные товары.</p><a className="primary-button" href="/cart">Вернуться в корзину</a></section>
            : checkoutPage && cartStatus === "ready" && !cartLines.length && !checkout.panelProps.orderNumber ? <section className="checkout-load-state"><h1>Корзина пуста</h1><p>Перед оформлением добавьте товары.</p><a className="primary-button" href="/#catalog">Перейти в каталог</a></section>
              : <CheckoutPanel user={!!user} page={checkoutPage} pricingReady={pricingReady} pricingError={pricingError} {...checkout.panelProps} feedbackMessage={checkoutPage ? notice : ""} />}
    </div>
  );
}
