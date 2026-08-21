import { useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { CartDrawer, CheckoutPanel } from "./CartCheckout";
import { useCheckout } from "./useCheckout";

type Cart = Record<string, number>;

export type CartProduct = {
  id: string;
  sku: string;
  name: string;
  price: number;
  image: string;
  stock?: number;
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
    window.location.assign("/checkout");
  }

  return (
    <div className="cart-checkout-host">
      {notice && <div className="toast" role="status">{notice}</div>}

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
      />}

      <CheckoutPanel user={!!user} page={checkoutPage} {...checkout.panelProps} />
    </div>
  );
}
