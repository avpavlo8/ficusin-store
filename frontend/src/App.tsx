import { useEffect, useMemo, useRef, useState } from "react";
import { CartDrawer, CheckoutPanel } from "./CartCheckout";
import { StoreHeader } from "./StoreHeader";
import { useCheckout } from "./useCheckout";

type Product = {
  id: string;
  name: string;
  latin: string;
  category: string;
  price: number;
  image: string;
  badge?: string;
  light: string;
  size: string;
  stock?: number;
  catalogSection: string;
  plantKind?: string;
  lightLevel?: string;
  watering?: string;
  heightClass?: string;
  careLevel?: string;
  placement?: string;
  petSafety?: string;
  growthHabit?: string;
  categoryId?: number;
};

type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number };

type StoreUser = {
  id: number;
  email: string;
  phone: string;
  fullName: string;
  lastName: string;
  patronymic: string;
  deliveryAddress: string;
  accountType: "retail" | "wholesale";
  adminRole?: "manager" | "owner";
  avatarUpdatedAt?: string;
};

type Cart = Record<string, number>;
export type CartProduct = Pick<Product, "id" | "name" | "price" | "image" | "stock">;

const collections: Array<{ id: string; title: string; text: string; field: keyof Product; value: string; icon: string }> = [
  { id: "sunny", title: "Для солнечной стороны", text: "Любят много света", field: "lightLevel", value: "sunny", icon: "☀" },
  { id: "low-light", title: "Для затемнённых мест", text: "Комфортно вдали от окна", field: "lightLevel", value: "low_light", icon: "◐" },
  { id: "bathroom", title: "В ванную комнату", text: "Подходят для влажных помещений", field: "placement", value: "bathroom", icon: "≈" },
  { id: "rare-water", title: "Редкий полив", text: "Прощают забывчивость", field: "watering", value: "rare", icon: "♢" },
  { id: "easy", title: "Лёгкий уход", text: "Почти не требуют заботы", field: "careLevel", value: "easy", icon: "✓" },
  { id: "pets", title: "Для дома с питомцами", text: "Безопасный выбор", field: "petSafety", value: "safe", icon: "♡" },
  { id: "tall", title: "Высокие растения", text: "Зелёный акцент в интерьере", field: "heightClass", value: "high", icon: "↟" },
  { id: "trailing", title: "Ампельные растения", text: "Красиво ниспадают с полок", field: "growthHabit", value: "trailing", icon: "⌇" },
];
const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

type HomeProps = {
  embedded?: boolean;
  externalCart?: Cart;
  cartProducts?: CartProduct[];
  controlledCartOpen?: boolean;
  onCartOpenChange?: (open: boolean) => void;
  onCartChange?: (cart: Cart) => void;
};

export default function Home({
  embedded = false,
  externalCart,
  cartProducts,
  controlledCartOpen,
  onCartOpenChange,
  onCartChange,
}: HomeProps = {}) {
  const [products, setProducts] = useState<Product[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [catalogError, setCatalogError] = useState("");
  const [catalogSection, setCatalogSection] = useState("plants");
  const [categories, setCategories] = useState<Category[]>([]);
  const [selectedCategory, setSelectedCategory] = useState<number | null>(null);
  const [collection, setCollection] = useState("");
  const [query, setQuery] = useState("");
  // The section list is open on arrival; individual branches still start
  // collapsed so the sidebar stays short.
  const [treeOpen, setTreeOpen] = useState(true);
  const [expandedCategories, setExpandedCategories] = useState<Set<number>>(new Set());
  const toggleCategory = (id: number) =>
    setExpandedCategories((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    try {
      return new Set(JSON.parse(window.localStorage.getItem("ficusin-favorites") || "[]") as string[]);
    } catch {
      return new Set();
    }
  });
  // The cart is read straight into the initial state. Reading it from an
  // effect used to lose it: the effect that saves the cart ran first, on the
  // very first render, and overwrote the stored basket with an empty one
  // before anything had been read back.
  const [cart, setCart] = useState<Cart>(() => {
    if (externalCart) return externalCart;
    try {
      const saved = window.localStorage.getItem("ficusin-cart");
      return saved ? (JSON.parse(saved) as Cart) : {};
    } catch {
      return {};
    }
  });
  const [cartOpen, setCartOpen] = useState(controlledCartOpen ?? false);
  const [notice, setNotice] = useState("");
  // Set when the customer comes back from the payment page.
  const [paymentReturn, setPaymentReturn] = useState("");
  const [user, setUser] = useState<StoreUser | null>(null);
  const productsForCart = cartProducts ?? products;
  const cartLines = productsForCart
    .filter((product) => cart[product.id])
    .map((product) => ({ ...product, quantity: cart[product.id] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const checkout = useCheckout({ cartLines, cartCount, setCart, setNotice });
  const { checkoutOpen, setCheckoutOpen, setCheckoutProfile } = checkout;
  // Guards the first save: until the server copy has been merged in we must
  // not push the local basket over it.
  const cartSynced = useRef(false);

  // The modern storefront owns the visible basket counter and product grid.
  // In embedded mode this component only owns the already battle-tested
  // checkout flow. Keep both sides on the same basket object without a page
  // navigation or a second source of truth.
  useEffect(() => {
    if (!externalCart) return;
    if (JSON.stringify(externalCart) !== JSON.stringify(cart)) setCart(externalCart);
    // `cart` is deliberately omitted: parent changes are the only reason to
    // pull state inward; local changes are pushed by the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [externalCart]);

  useEffect(() => {
    onCartChange?.(cart);
  }, [cart, onCartChange]);

  useEffect(() => {
    if (controlledCartOpen != null && controlledCartOpen !== cartOpen) {
      setCartOpen(controlledCartOpen);
    }
    // See the cart synchronization note above: this direction only follows
    // the parent value.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [controlledCartOpen]);

  useEffect(() => {
    onCartOpenChange?.(cartOpen);
  }, [cartOpen, onCartOpenChange]);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("cart") === "1") setCartOpen(true);
    // Search started from a page that has no product list of its own.
    const incomingQuery = params.get("q");
    if (incomingQuery) setQuery(incomingQuery);
    // Back from the payment page. YooKassa returns everyone here, whether
    // they paid or gave up, so the wording promises nothing about the money —
    // the order page is what shows the real state.
    const paidOrder = params.get("paid");
    if (paidOrder) {
      setPaymentReturn(paidOrder);
      window.history.replaceState({}, "", window.location.pathname);
    }
  }, []);

  useEffect(() => {
    if (embedded) return;
    fetch("/api/v1/categories", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { categories?: Category[] }) => {
        const items = data.categories || [];
        setCategories(items);
        const plants = items.find((item) => item.slug === "plants");
        if (plants) setSelectedCategory(plants.id);
      }).catch(() => setCategories([]));
  }, [embedded]);

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
          name: [profile.lastName, profile.fullName, profile.patronymic]
            .filter(Boolean)
            .join(" "),
          phone: profile.phone,
          email: profile.email,
          address: profile.deliveryAddress,
        });
      })
      .catch(() => {
        // Checkout remains available to guests if profile loading fails.
      });
    return () => {
      cancelled = true;
    };
  }, [setCheckoutProfile]);

  useEffect(() => {
    if (embedded) return;
    let cancelled = false;
    fetch("/api/v1/catalog", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("Каталог временно недоступен");
        return (await response.json()) as { products?: Product[] };
      })
      .then((data) => {
        if (!cancelled) {
          if (!data.products?.length) {
            throw new Error("В каталоге пока нет товаров в наличии");
          }
          setProducts(data.products);
          setCatalogError("");
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setCatalogError(
            error instanceof Error ? error.message : "Каталог временно недоступен",
          );
        }
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [embedded]);

  // A signed-in customer also keeps a copy on the server, so the basket
  // survives a cleared browser or a switch to another phone. The browser is
  // the working copy; the server one is merged into it once at sign-in and
  // only ever cleared by the customer or by placing an order.
  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    fetch("/api/v1/account/cart", { credentials: "same-origin", cache: "no-store" })
      .then((response) => (response.ok ? response.json() : { items: {} }))
      .then((data: { items?: Cart }) => {
        if (cancelled) return;
        const stored = data.items || {};
        setCart((current) => {
          const merged: Cart = { ...stored };
          for (const [id, quantity] of Object.entries(current)) {
            merged[id] = Math.max(merged[id] || 0, quantity);
          }
          return merged;
        });
        cartSynced.current = true;
      })
      .catch(() => {
        // Keep the local basket and try saving again on the next change.
        cartSynced.current = true;
      });
    return () => {
      cancelled = true;
    };
  }, [user]);

  useEffect(() => {
    window.localStorage.setItem("ficusin-cart", JSON.stringify(cart));
    if (!user || !cartSynced.current) return;
    // Waiting a moment turns a burst of "+" taps into one request.
    const timer = window.setTimeout(() => {
      fetch("/api/v1/account/cart", {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ items: cart }),
      }).catch(() => {
        // The browser copy is already saved; nothing is lost.
      });
    }, 800);
    return () => window.clearTimeout(timer);
  }, [cart, user]);

  useEffect(() => {
    document.body.classList.toggle("drawer-open", cartOpen || checkoutOpen);
    return () => document.body.classList.remove("drawer-open");
  }, [cartOpen, checkoutOpen]);

  const selectedCollection = collections.find((item) => item.id === collection);
  const categoryIDs = useMemo(() => {
    if (!selectedCategory) return new Set<number>();
    const result = new Set<number>([selectedCategory]);
    let changed = true;
    while (changed) {
      changed = false;
      categories.forEach((item) => {
        if (item.parentId && result.has(item.parentId) && !result.has(item.id)) { result.add(item.id); changed = true; }
      });
    }
    return result;
  }, [categories, selectedCategory]);
  const searchTerm = query.trim().toLowerCase();
  // A search is deliberately global: while the box has text we ignore the
  // selected category and collection, otherwise typing a plant name while
  // standing in the wrong section silently returns nothing.
  const filtered = useMemo(
    () =>
      products.filter((product) => {
        const searchable = `${product.name} ${product.latin} ${product.category}`.toLowerCase();
        if (searchTerm) return searchable.includes(searchTerm);
        const inSection = !selectedCategory || (!!product.categoryId && categoryIDs.has(product.categoryId));
        const inCollection = !selectedCollection || product[selectedCollection.field] === selectedCollection.value;
        return inSection && inCollection;
      }),
    [products, selectedCategory, categoryIDs, selectedCollection, searchTerm],
  );

  function toggleFavorite(id: string) {
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      window.localStorage.setItem("ficusin-favorites", JSON.stringify([...next]));
      return next;
    });
  }

  function addToCart(id: string) {
    const product = products.find((item) => item.id === id);
    setCart((current) => ({
      ...current,
      [id]: Math.min(product?.stock ?? 20, (current[id] ?? 0) + 1),
    }));
    setNotice("Растение добавлено в корзину");
    window.setTimeout(() => setNotice(""), 1800);
  }

  function setQuantity(id: string, quantity: number) {
    setCart((current) => {
      const next = { ...current };
      if (quantity <= 0) delete next[id];
      else {
        const product = productsForCart.find((item) => item.id === id);
        // Zero stock is a valid pre-order. It must not turn the first press
        // on "+" into quantity zero; only a positive stock value caps the
        // basket below the normal per-line limit.
        const limit = product?.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20;
        next[id] = Math.min(limit, quantity);
      }
      return next;
    });
  }


  function beginCheckout() {
    setCartOpen(false);
    checkout.beginCheckout();
  }

  const roots = categories.filter((item) => !item.parentId);
  const childrenOf = (id: number) => categories.filter((item) => item.parentId === id);
  const selectedCategoryName = categories.find((item) => item.id === selectedCategory)?.name || "Все товары";

  const RootElement = embedded ? "div" : "main";
  return (
    <RootElement className={embedded ? "cart-checkout-host" : undefined}>
      {!embedded && <>
      <StoreHeader
        query={query}
        onQueryChange={setQuery}
        favoritesCount={favorites.size}
        cartCount={cartCount}
        onCartClick={() => setCartOpen(true)}
      />

      <section className="catalog-hero" id="top">
        <div>
          <p className="eyebrow">Всё для зелёного дома</p>
          <h1>Каталог Фикусин</h1>
          <p>Растения, кашпо и всё необходимое для ухода — с актуальными ценами и остатками.</p>
        </div>
      </section>

      <section className="catalog-section" id="catalog">
        <div className="catalog-layout">
          <aside className="category-tree" aria-label="Категории каталога">
            <button
              className="category-tree-toggle"
              aria-expanded={treeOpen}
              onClick={() => setTreeOpen((value) => !value)}
            >
              <span>Каталог</span>
              <span aria-hidden="true">{treeOpen ? "−" : "+"}</span>
            </button>
            {treeOpen && roots.map((root) => {
              const levelTwo = childrenOf(root.id);
              const visible = levelTwo.length === 1 && childrenOf(levelTwo[0].id).length ? childrenOf(levelTwo[0].id) : levelTwo;
              const rootOpen = expandedCategories.has(root.id);
              return <div className="category-root" key={root.id}>
                <button
                  className={selectedCategory === root.id ? "active" : ""}
                  aria-expanded={visible.length > 0 ? rootOpen : undefined}
                  onClick={() => {
                    setSelectedCategory(root.id);
                    setCatalogSection(root.slug);
                    setCollection("");
                    if (visible.length > 0) toggleCategory(root.id);
                  }}
                >
                  <span>{root.name}</span>
                  {visible.length > 0 && <span className="category-caret" aria-hidden="true">{rootOpen ? "−" : "+"}</span>}
                </button>
                {visible.length > 0 && rootOpen && <div className="category-children">
                  {visible.map((child) => {
                    const leaves = childrenOf(child.id);
                    const childOpen = expandedCategories.has(child.id);
                    return <div key={child.id}>
                      <button
                        className={selectedCategory === child.id ? "active" : ""}
                        aria-expanded={leaves.length > 0 ? childOpen : undefined}
                        onClick={() => {
                          setSelectedCategory(child.id);
                          setCollection("");
                          if (leaves.length > 0) toggleCategory(child.id);
                        }}
                      >
                        <span>{child.name}</span>
                        {leaves.length > 0 && <span className="category-caret" aria-hidden="true">{childOpen ? "−" : "+"}</span>}
                      </button>
                      {childOpen && leaves.map((leaf) => <button className={selectedCategory === leaf.id ? "active leaf" : "leaf"} onClick={() => { setSelectedCategory(leaf.id); setCollection(""); }} key={leaf.id}>{leaf.name}</button>)}
                    </div>;
                  })}
                </div>}
              </div>;
            })}
          </aside>
          <div className="catalog-content">
          {catalogSection === "plants" && <>
            <div className="collection-section">
              <div className="section-heading compact"><div><p className="eyebrow">Подборки</p><h2>Подберите растение под себя</h2></div><p>Характеристики заполняет менеджер — одно растение может входить сразу в несколько подборок.</p></div>
              <div className="collection-grid">
                {collections.map((item) => (
                  <button key={item.id} className={collection === item.id ? "collection-card active" : "collection-card"} onClick={() => setCollection(collection === item.id ? "" : item.id)}>
                    <span>{item.icon}</span><strong>{item.title}</strong><small>{item.text}</small>
                  </button>
                ))}
              </div>
            </div>
          </>}

        <div className="catalog-result-bar">
          <div><p className="eyebrow">Каталог</p><h2>{selectedCollection?.title || selectedCategoryName}</h2></div>
          <div><span>{filtered.length} товаров</span></div>
        </div>

        <div className="product-grid" id="new">
          {catalogLoading && <p className="catalog-status" role="status">Загружаем актуальные товары из Saby…</p>}
          {!catalogLoading && catalogError && <p className="catalog-status catalog-status-error" role="alert">{catalogError}. Обновите страницу через несколько секунд.</p>}
          {filtered.map((product) => (
            <article className="product-card" key={product.id}>
              <button className={`favorite-button ${favorites.has(product.id) ? "active" : ""}`} onClick={() => toggleFavorite(product.id)} aria-label={favorites.has(product.id) ? `Убрать ${product.name} из избранного` : `Добавить ${product.name} в избранное`}>{favorites.has(product.id) ? "♥" : "♡"}</button>
              <a className="product-image" href={`/product/${product.id}`}>
                <img src={product.image} alt={product.name} />
                {product.badge && <span className="badge">{product.badge}</span>}
              </a>
              <div className="product-info">
                <p className="latin">{product.latin}</p>
                <h3><a href={`/product/${product.id}`}>{product.name}</a></h3>
                <div className="product-meta"><span>{product.light}</span><span>{product.size}</span></div>
                <div className="product-bottom"><strong>{money(product.price)}</strong><button className={cart[product.id] ? "in-cart" : undefined} onClick={() => (cart[product.id] ? setCartOpen(true) : addToCart(product.id))} disabled={product.stock === 0}>{product.stock === 0 ? "Нет в наличии" : cart[product.id] ? `В корзине · ${cart[product.id]} шт.` : "В корзину"}</button></div>
              </div>
            </article>
          ))}
          {!catalogLoading && !catalogError && filtered.length === 0 && (
            <div className="empty-state"><strong>Ничего не найдено</strong><span>Попробуйте другую категорию, подборку или измените запрос.</span></div>
          )}
        </div>
        </div></div>
      </section>

      <section className="help-section" id="help">
        <div>
          <p className="eyebrow">Не знаете, что выбрать?</p>
          <h2>Подберём растение под ваш дом</h2>
          <p>Расскажите, куда хотите поставить растение и сколько света в комнате. Предложим несколько подходящих вариантов.</p>
          <a className="secondary-button" href="https://t.me/ficusin62" target="_blank" rel="noreferrer">Написать консультанту</a>
        </div>
        <div className="help-cards">
          <div><b>01</b><span>Оценим освещение</span></div>
          <div><b>02</b><span>Учтём опыт ухода</span></div>
          <div><b>03</b><span>Подберём размер</span></div>
        </div>
      </section>

      <section className="care-section" id="care">
        <div className="care-photo"><img src="/assets/product-pothos.png" alt="Зелёное растение в кашпо" /></div>
        <div>
          <p className="eyebrow">Забота после покупки</p>
          <h2>Не оставим один на один с новым растением</h2>
          <p>К каждому заказу приложим понятную памятку по уходу. Если листья изменятся или появятся вопросы — поможем разобраться.</p>
          <ul>
            <li>Инструкция по поливу и свету</li>
            <li>Советы по пересадке и удобрениям</li>
            <li>Поддержка в мессенджере</li>
          </ul>
        </div>
      </section>

      <section className="delivery-section" id="delivery">
        <div className="section-heading">
          <div><p className="eyebrow">Получение заказа</p><h2>Доставим бережно</h2></div>
          <p>Итоговую стоимость и срок менеджер подтвердит после оформления заказа.</p>
        </div>
        <div className="delivery-grid">
          {checkout.availableDelivery.map((item, index) => (
            <article key={item.id}><span>0{index + 1}</span><h3>{item.title}</h3><p>{item.detail}</p><b>{item.id === "cdek" ? "По тарифу СДЭК" : item.fee ? `от ${money(item.fee)}` : "Бесплатно"}</b></article>
          ))}
        </div>
      </section>

      <footer>
        <div className="footer-brand"><a className="brand" href="#top"><span className="brand-mark">⌇</span><span>Фикусин</span></a><p>Комнатные растения в Рязани<br />с доставкой по России</p></div>
        <div><h3>Магазин</h3><a href="#catalog">Каталог</a><a href="#delivery">Доставка</a><a href="#care">Уход</a></div>
        <div><h3>Контакты</h3><a href="tel:+79156151100">+7 915 615-11-00</a><a href="https://t.me/ficusin62" target="_blank" rel="noreferrer">@ficusin62</a><span>Рязань, Новосёлов, 40А</span></div>
        <div><h3>Покупателям</h3><a href="/delivery-and-returns">Доставка и возврат</a><a href="/offer">Публичная оферта</a><a href="/privacy">Персональные данные</a><a href="/requisites">Реквизиты</a></div>
        <small>© 2026 Фикусин · Ежедневно 08:00–20:00 · ИП Павловский А. В. · ИНН 620201228029 · ОГРНИП 324620000031276</small>
      </footer>
      </>}

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

      {(cartOpen || checkoutOpen) && <button className="overlay" aria-label="Закрыть" onClick={() => { setCartOpen(false); setCheckoutOpen(false); }} />}

      <CartDrawer
        open={cartOpen}
        lines={cartLines}
        subtotal={subtotal}
        onClose={() => setCartOpen(false)}
        onQuantityChange={setQuantity}
        onCheckout={beginCheckout}
      />

      <CheckoutPanel user={!!user} {...checkout.panelProps} />

    </RootElement>
  );
}
