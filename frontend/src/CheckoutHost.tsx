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
  const [paymentReturn, setPaymentReturn] = useState(
    () => new URLSearchParams(window.location.search).get("paid") || "",
  );
  const [user, setUser] = useState<StoreUser | null>(null);

  const cartLines = products
    .filter((product) => cart[product.sku])
    .map((product) => ({ ...product, id: product.sku, quantity: cart[product.sku] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const checkout = useCheckout({ cartLines, cartCount, setCart, setNotice, initialOpen: checkoutPage });
  const { checkoutOpen, setCheckoutOpen, setCheckoutProfile } = checkout;

  useEffect(() => {
    if ((cartPage || checkoutPage) && cartLines.length) track("view_cart", { value: subtotal, quantity: cartCount, properties: { items: cartLines.length } });
  }, [cartPage, checkoutPage, cartLines.length, cartCount, subtotal]);

  useEffect(() => {
    if (!new URLSearchParams(window.location.search).has("paid")) return;
    window.history.replaceState({}, "", window.location.pathname);
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetch("/api/v1/auth/me", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (response.status === 401) return null;
        if (!response.ok) throw new Error("Не удалось загрузить профиль");
        return (await response.json()) as { user: StoreUser };
      })
      .then((result) => {
        if (cancelled || !result?.user) return;
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
        // Guests can still place an order when profile loading fails.
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
      {notice && <div className="toast" role="status">{notice}</div>}
      {cartStatus === "error" && !cartPage && !checkoutPage && <div className="toast cart-error-toast" role="alert"><span>{cartError || "Изменение корзины не сохранено"}</span>{onCartRetry && <button type="button" onClick={onCartRetry}>Повторить</button>}</div>}

      {paymentReturn && (
        <div className="payment-return" role="status">
          <b>Заказ {paymentReturn} оформлен</b>
          <p>
            Мы получим подтверждение оплаты в течение минуты. Состояние заказа видно
            {user ? " в личном кабинете" : ", если войти в личный кабинет"}.
          </p>
          <div>
            {user && <a className="primary-button" href={`/account/orders/${paymentReturn}`}>Открыть заказ</a>}
            <button onClick={() => setPaymentReturn("")}>Продолжить покупки</button>
          </div>
        </div>
      )}

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

      {checkoutPage && cartStatus === "loading" ? <section className="checkout-load-state" role="status"><h1>Загружаем оформление…</h1><p>Проверяем состав и актуальные цены корзины.</p></section>
        : checkoutPage && cartStatus === "error" ? <section className="checkout-load-state error" role="alert"><h1>Не удалось открыть оформление</h1><p>{cartError}</p>{onCartRetry && <button type="button" className="primary-button" onClick={onCartRetry}>Повторить</button>}</section>
          : checkoutPage && (missingCartItems > 0 || cartLines.some((item) => item.available === false)) ? <section className="checkout-load-state error" role="alert"><h1>Состав корзины изменился</h1><p>Вернитесь в корзину и удалите недоступные товары.</p><a className="primary-button" href="/cart">Вернуться в корзину</a></section>
            : checkoutPage && cartStatus === "ready" && !cartLines.length && !checkout.panelProps.orderNumber ? <section className="checkout-load-state"><h1>Корзина пуста</h1><p>Перед оформлением добавьте товары.</p><a className="primary-button" href="/#catalog">Перейти в каталог</a></section>
              : <CheckoutPanel user={!!user} page={checkoutPage} {...checkout.panelProps} />}
    </div>
  );
}
