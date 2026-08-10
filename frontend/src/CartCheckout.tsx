export type CartLine = {
  id: string;
  name: string;
  price: number;
  image: string;
  stock?: number;
  quantity: number;
};

type CartDrawerProps = {
  open: boolean;
  lines: CartLine[];
  subtotal: number;
  onClose: () => void;
  onQuantityChange: (id: string, quantity: number) => void;
  onCheckout: () => void;
};

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

/**
 * The storefront owns the basket state; this component owns only its panel.
 * Keeping that boundary explicit prevents the product grid and checkout from
 * quietly growing back into one page-sized component.
 */
export function CartDrawer({
  open,
  lines,
  subtotal,
  onClose,
  onQuantityChange,
  onCheckout,
}: CartDrawerProps) {
  return (
    <aside className={`drawer ${open ? "open" : ""}`} aria-hidden={!open}>
      <div className="drawer-head">
        <div>
          <p className="eyebrow">Ваш выбор</p>
          <h2>Корзина</h2>
        </div>
        <button onClick={onClose} aria-label="Закрыть корзину">×</button>
      </div>
      <div className="cart-lines">
        {lines.map((item) => (
          <div className="cart-line" key={item.id}>
            <img src={item.image} alt="" />
            <div>
              <h3>{item.name}</h3>
              <p>{money(item.price)}</p>
              <div className="quantity">
                <button
                  onClick={() => onQuantityChange(item.id, item.quantity - 1)}
                  aria-label="Уменьшить"
                >
                  −
                </button>
                <span>{item.quantity}</span>
                <button
                  onClick={() => onQuantityChange(item.id, item.quantity + 1)}
                  aria-label="Увеличить"
                >
                  +
                </button>
              </div>
            </div>
            <button
              className="remove"
              onClick={() => onQuantityChange(item.id, 0)}
              aria-label={`Удалить ${item.name}`}
            >
              ×
            </button>
          </div>
        ))}
        {!lines.length && (
          <div className="empty-cart">
            <span>⌁</span>
            <h3>Корзина пока пуста</h3>
            <p>Добавьте растения из каталога — они появятся здесь.</p>
            <button onClick={onClose}>Перейти в каталог</button>
          </div>
        )}
      </div>
      {!!lines.length && (
        <div className="cart-summary">
          <div>
            <span>Товары</span>
            <strong>{money(subtotal)}</strong>
          </div>
          <p>Доставка рассчитывается при оформлении</p>
          <button className="primary-button" onClick={onCheckout}>
            Оформить заказ
          </button>
        </div>
      )}
    </aside>
  );
}
