import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  formatRussianPhoneInput,
  normalizeRussianPhone,
} from "./lib/phone";
import { AccountMenu } from "./StoreHeader";

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

type CheckoutProfile = {
  name: string;
  phone: string;
  email: string;
  address: string;
};

type Cart = Record<string, number>;
type CdekCity = { code: number; city: string; region?: string };
type CdekOffice = {
  code: string;
  name: string;
  location: { city: string; address: string; address_full?: string };
  work_time?: string;
};
type CdekQuote = {
  tariffCode: number;
  tariffName: string;
  price: number;
  daysMin: number;
  daysMax: number;
};

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
const deliveryOptions = [
  { id: "pickup", title: "Самовывоз в Рязани", detail: "из магазина, бесплатно", fee: 0 },
  { id: "courier", title: "Курьер по Рязани", detail: "в согласованный день", fee: 490 },
  { id: "cdek", title: "СДЭК по России", detail: "до выбранного пункта выдачи", fee: null },
  { id: "post", title: "Почта России", detail: "для населённых пунктов без СДЭК", fee: 590 },
];

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);

export default function Home() {
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
  const [cart, setCart] = useState<Cart>({});
  const [cartOpen, setCartOpen] = useState(false);
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [delivery, setDelivery] = useState("pickup");
  const [menuOpen, setMenuOpen] = useState(false);
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [orderNumber, setOrderNumber] = useState("");
  const [cdekCityQuery, setCdekCityQuery] = useState("");
  const [cdekCities, setCdekCities] = useState<CdekCity[]>([]);
  const [cdekCity, setCdekCity] = useState<CdekCity | null>(null);
  const [cdekOffices, setCdekOffices] = useState<CdekOffice[]>([]);
  const [cdekOfficeCode, setCdekOfficeCode] = useState("");
  const [cdekQuote, setCdekQuote] = useState<CdekQuote | null>(null);
  const [cdekLoading, setCdekLoading] = useState(false);
  const [cdekError, setCdekError] = useState("");
  const [user, setUser] = useState<StoreUser | null>(null);
  const [checkoutProfile, setCheckoutProfile] = useState<CheckoutProfile>({
    name: "",
    phone: "",
    email: "",
    address: "",
  });

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      const saved = window.localStorage.getItem("ficusin-cart");
      if (saved) {
        try {
          setCart(JSON.parse(saved));
        } catch {
          window.localStorage.removeItem("ficusin-cart");
        }
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, []);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("cart") === "1") setCartOpen(true);
    // Search started from a page that has no product list of its own.
    const incomingQuery = params.get("q");
    if (incomingQuery) setQuery(incomingQuery);
  }, []);

  useEffect(() => {
    fetch("/api/v1/categories", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { categories?: Category[] }) => {
        const items = data.categories || [];
        setCategories(items);
        const plants = items.find((item) => item.slug === "plants");
        if (plants) setSelectedCategory(plants.id);
      }).catch(() => setCategories([]));
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
  }, []);

  useEffect(() => {
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
  }, []);

  useEffect(() => {
    window.localStorage.setItem("ficusin-cart", JSON.stringify(cart));
  }, [cart]);

  useEffect(() => {
    document.body.style.overflow = cartOpen || checkoutOpen || menuOpen ? "hidden" : "";
    return () => {
      document.body.style.overflow = "";
    };
  }, [cartOpen, checkoutOpen, menuOpen]);

  useEffect(() => {
    if (
      delivery !== "cdek" ||
      cdekCityQuery.trim().length < 2 ||
      cdekCityQuery.trim() === cdekCity?.city
    ) {
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        setCdekLoading(true);
        setCdekError("");
        const response = await fetch(
          `/api/v1/delivery/cdek?action=cities&city=${encodeURIComponent(cdekCityQuery.trim())}`,
          { signal: controller.signal },
        );
        const data = (await response.json()) as {
          cities?: CdekCity[];
          error?: string;
        };
        if (!response.ok) throw new Error(data.error || "Не удалось найти город");
        setCdekCities(data.cities ?? []);
      } catch (error) {
        if ((error as Error).name !== "AbortError") {
          setCdekError(error instanceof Error ? error.message : "Не удалось найти город");
        }
      } finally {
        setCdekLoading(false);
      }
    }, 350);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [delivery, cdekCityQuery, cdekCity]);

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

  const cartLines = products
    .filter((product) => cart[product.id])
    .map((product) => ({ ...product, quantity: cart[product.id] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const deliveryOption = deliveryOptions.find((item) => item.id === delivery) ?? deliveryOptions[0];
  const deliveryFee = delivery === "cdek" ? (cdekQuote?.price ?? 0) : (deliveryOption.fee ?? 0);
  const total = subtotal + deliveryFee;

  async function chooseCdekCity(city: CdekCity) {
    setCdekCity(city);
    setCdekCityQuery(city.city);
    setCdekCities([]);
    setCdekOffices([]);
    setCdekOfficeCode("");
    setCdekQuote(null);
    setCdekLoading(true);
    setCdekError("");
    try {
      const [officesResponse, quoteResponse] = await Promise.all([
        fetch(`/api/v1/delivery/cdek?action=offices&cityCode=${city.code}`),
        fetch("/api/v1/delivery/cdek", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ cityCode: city.code, itemCount: cartCount }),
        }),
      ]);
      const officesData = (await officesResponse.json()) as {
        offices?: CdekOffice[];
        error?: string;
      };
      const quoteData = (await quoteResponse.json()) as {
        quote?: CdekQuote;
        error?: string;
      };
      if (!officesResponse.ok) {
        throw new Error(officesData.error || "Не удалось загрузить пункты выдачи");
      }
      if (!quoteResponse.ok || !quoteData.quote) {
        throw new Error(quoteData.error || "Не удалось рассчитать доставку");
      }
      if (!officesData.offices?.length) {
        throw new Error("В этом городе нет доступных пунктов выдачи");
      }
      setCdekOffices(officesData.offices);
      setCdekQuote(quoteData.quote);
    } catch (error) {
      setCdekError(
        error instanceof Error ? error.message : "Не удалось рассчитать доставку",
      );
    } finally {
      setCdekLoading(false);
    }
  }

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
        const product = products.find((item) => item.id === id);
        next[id] = Math.min(product?.stock ?? 20, quantity);
      }
      return next;
    });
  }

  async function submitOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    const form = new FormData(event.currentTarget);
    const phoneInput = event.currentTarget.elements.namedItem(
      "phone",
    ) as HTMLInputElement;
    const phone = normalizeRussianPhone(String(form.get("phone") ?? ""));
    if (!phone) {
      phoneInput.setCustomValidity(
        "Введите российский номер: 9151234567, 79151234567 или 89151234567",
      );
      phoneInput.reportValidity();
      setSubmitting(false);
      return;
    }
    phoneInput.setCustomValidity("");
    phoneInput.value = phone;
    const payload = {
      customer: {
        name: String(form.get("name") ?? ""),
        phone,
        email: String(form.get("email") ?? ""),
        address: String(form.get("address") ?? ""),
        comment: String(form.get("comment") ?? ""),
      },
      delivery,
      cdek:
        delivery === "cdek"
          ? {
              cityCode: cdekCity?.code,
              cityName: cdekCity?.city,
              officeCode: cdekOfficeCode,
            }
          : undefined,
      items: cartLines.map((item) => ({ id: item.id, quantity: item.quantity })),
    };

    try {
      const response = await fetch("/api/v1/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = (await response.json()) as { orderNumber?: string; error?: string };
      if (!response.ok || !data.orderNumber) throw new Error(data.error || "Не удалось оформить заказ");
      setOrderNumber(data.orderNumber);
      setCart({});
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Не удалось оформить заказ");
    } finally {
      setSubmitting(false);
    }
  }

  function beginCheckout() {
    setCartOpen(false);
    setCheckoutOpen(true);
    setOrderNumber("");
  }

  const roots = categories.filter((item) => !item.parentId);
  const childrenOf = (id: number) => categories.filter((item) => item.parentId === id);
  const selectedCategoryName = categories.find((item) => item.id === selectedCategory)?.name || "Все товары";

  return (
    <main>
      <div className="announcement">
        <span>Бережно упакуем каждое растение</span>
        <span>Доставка по Рязани и всей России</span>
      </div>

      <header className="header">
        <a className="brand" href="#top" aria-label="Фикусин — на главную">
          <span className="brand-mark">⌇</span>
          <span>Фикусин</span>
        </a>
        <nav className="desktop-nav" aria-label="Основная навигация">
          <a href="#catalog">Каталог</a>
          <a href="#new">Новинки</a>
          <a href="#care">Уход</a>
          <a href="#delivery">Доставка</a>
        </nav>
        <div className="header-actions">
          <label className="header-search"><span aria-hidden="true">⌕</span><input id="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск по каталогу" /></label>
          <AccountMenu user={user} />
          <a className="favorites-button" href="/favorites" aria-label={`Избранное, товаров: ${favorites.size}`}>
            <span aria-hidden="true">♥</span><b>{favorites.size}</b>
          </a>
          <button className="cart-button" onClick={() => setCartOpen(true)} aria-label={`Корзина, товаров: ${cartCount}`}>
            <span aria-hidden="true">Корзина</span>
            <b>{cartCount}</b>
          </button>
          <button className="menu-button" onClick={() => setMenuOpen(true)} aria-label="Открыть меню">☰</button>
        </div>
      </header>

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
                <div className="product-bottom"><strong>{money(product.price)}</strong><button onClick={() => addToCart(product.id)} disabled={product.stock === 0}>{product.stock === 0 ? "Нет в наличии" : "В корзину"}</button></div>
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
          {deliveryOptions.map((item, index) => (
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

      {notice && <div className="toast" role="status">{notice}</div>}

      {(cartOpen || checkoutOpen || menuOpen) && <button className="overlay" aria-label="Закрыть" onClick={() => { setCartOpen(false); setCheckoutOpen(false); setMenuOpen(false); }} />}

      <aside className={`drawer ${cartOpen ? "open" : ""}`} aria-hidden={!cartOpen}>
        <div className="drawer-head"><div><p className="eyebrow">Ваш выбор</p><h2>Корзина</h2></div><button onClick={() => setCartOpen(false)} aria-label="Закрыть корзину">×</button></div>
        <div className="cart-lines">
          {cartLines.map((item) => (
            <div className="cart-line" key={item.id}>
              <img src={item.image} alt="" />
              <div><h3>{item.name}</h3><p>{money(item.price)}</p><div className="quantity"><button onClick={() => setQuantity(item.id, item.quantity - 1)} aria-label="Уменьшить">−</button><span>{item.quantity}</span><button onClick={() => setQuantity(item.id, item.quantity + 1)} aria-label="Увеличить">+</button></div></div>
              <button className="remove" onClick={() => setQuantity(item.id, 0)} aria-label={`Удалить ${item.name}`}>×</button>
            </div>
          ))}
          {!cartLines.length && <div className="empty-cart"><span>⌁</span><h3>Корзина пока пуста</h3><p>Добавьте растения из каталога — они появятся здесь.</p><button onClick={() => setCartOpen(false)}>Перейти в каталог</button></div>}
        </div>
        {!!cartLines.length && <div className="cart-summary"><div><span>Товары</span><strong>{money(subtotal)}</strong></div><p>Доставка рассчитывается при оформлении</p><button className="primary-button" onClick={beginCheckout}>Оформить заказ</button></div>}
      </aside>

      <aside className={`checkout ${checkoutOpen ? "open" : ""}`} aria-hidden={!checkoutOpen}>
        <div className="drawer-head"><div><p className="eyebrow">Последний шаг</p><h2>Оформление заказа</h2></div><button onClick={() => setCheckoutOpen(false)} aria-label="Закрыть оформление">×</button></div>
        {orderNumber ? (
          <div className="success">
            <span>✓</span><h2>Заказ принят</h2><p>Номер заказа: <strong>{orderNumber}</strong></p>
            <p>Менеджер свяжется с вами, подтвердит наличие и пришлёт ссылку на оплату после подключения эквайринга.</p>
            <button className="primary-button" onClick={() => setCheckoutOpen(false)}>Вернуться в магазин</button>
          </div>
        ) : (
          <form onSubmit={submitOrder}>
            <fieldset>
              <legend>Контактные данные</legend>
              {user && <p className="profile-prefill">Данные заполнены из личного кабинета</p>}
              <div className="field-grid">
                <label>
                  Имя
                  <input
                    name="name"
                    required
                    placeholder="Александр"
                    autoComplete="name"
                    value={checkoutProfile.name}
                    onChange={(event) =>
                      setCheckoutProfile((current) => ({ ...current, name: event.target.value }))
                    }
                  />
                </label>
                <label>
                  Телефон
                  <input
                    name="phone"
                    required
                    inputMode="tel"
                    autoComplete="tel"
                    maxLength={18}
                    placeholder="+7 900 000-00-00"
                    value={checkoutProfile.phone}
                    onChange={(event) => {
                      event.currentTarget.setCustomValidity("");
                      const value = formatRussianPhoneInput(event.currentTarget.value);
                      setCheckoutProfile((current) => ({ ...current, phone: value }));
                    }}
                  />
                </label>
              </div>
              <label>
                Email для чека
                <input
                  name="email"
                  required
                  type="email"
                  autoComplete="email"
                  placeholder="mail@example.ru"
                  value={checkoutProfile.email}
                  onChange={(event) =>
                    setCheckoutProfile((current) => ({ ...current, email: event.target.value }))
                  }
                />
              </label>
            </fieldset>
            <fieldset>
              <legend>Получение</legend>
              <div className="delivery-options">
                {deliveryOptions.map((item) => (
                  <label className={delivery === item.id ? "selected" : ""} key={item.id}>
                    <input
                      type="radio"
                      name="delivery"
                      value={item.id}
                      checked={delivery === item.id}
                      onChange={() => setDelivery(item.id)}
                    />
                    <span><b>{item.title}</b><small>{item.detail}</small></span>
                    <strong>
                      {item.id === "cdek"
                        ? cdekQuote
                          ? money(cdekQuote.price)
                          : "Рассчитать"
                        : item.fee
                          ? money(item.fee)
                          : "0 ₽"}
                    </strong>
                  </label>
                ))}
              </div>
              {delivery === "cdek" ? (
                <div className="cdek-picker">
                  <label>
                    Город получения
                    <input
                      value={cdekCityQuery}
                      onChange={(event) => {
                        setCdekCityQuery(event.target.value);
                        setCdekCity(null);
                        setCdekCities([]);
                        setCdekOffices([]);
                        setCdekOfficeCode("");
                        setCdekQuote(null);
                      }}
                      autoComplete="off"
                      placeholder="Начните вводить город"
                    />
                  </label>
                  {!!cdekCities.length && (
                    <div className="cdek-suggestions" role="listbox" aria-label="Найденные города">
                      {cdekCities.map((city) => (
                        <button
                          type="button"
                          key={city.code}
                          onClick={() => chooseCdekCity(city)}
                        >
                          <b>{city.city}</b>
                          <span>{city.region || "Россия"}</span>
                        </button>
                      ))}
                    </div>
                  )}
                  {cdekLoading && <p className="cdek-status">Получаем данные СДЭК…</p>}
                  {cdekError && <p className="cdek-status error">{cdekError}</p>}
                  {!!cdekOffices.length && (
                    <label>
                      Пункт выдачи
                      <select
                        value={cdekOfficeCode}
                        onChange={(event) => setCdekOfficeCode(event.target.value)}
                        required
                      >
                        <option value="">Выберите адрес</option>
                        {cdekOffices.map((office) => (
                          <option key={office.code} value={office.code}>
                            {office.location.address}
                            {office.work_time ? ` · ${office.work_time}` : ""}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {cdekQuote && (
                    <div className="cdek-quote">
                      <b>{money(cdekQuote.price)}</b>
                      <span>
                        {cdekQuote.daysMin === cdekQuote.daysMax
                          ? `${cdekQuote.daysMin} дн.`
                          : `${cdekQuote.daysMin}–${cdekQuote.daysMax} дн.`}
                      </span>
                      <small>Предварительный расчёт по габаритам растений</small>
                    </div>
                  )}
                </div>
              ) : (
                <label>
                  {delivery === "pickup" ? "Самовывоз" : "Адрес доставки"}
                  <input
                    name="address"
                    required={delivery !== "pickup"}
                    disabled={delivery === "pickup"}
                    autoComplete="street-address"
                    value={checkoutProfile.address}
                    onChange={(event) =>
                      setCheckoutProfile((current) => ({ ...current, address: event.target.value }))
                    }
                    placeholder={
                      delivery === "pickup"
                        ? "Рязань, Новосёлов, 40А"
                        : "Город, улица, дом, квартира"
                    }
                  />
                </label>
              )}
            </fieldset>
            <fieldset><legend>Комментарий</legend><label><textarea name="comment" rows={3} placeholder="Удобное время, пожелания к заказу" /></label></fieldset>
            <div className="checkout-total"><div><span>Товары</span><span>{money(subtotal)}</span></div><div><span>Доставка</span><span>{delivery === "cdek" && !cdekQuote ? "после выбора ПВЗ" : money(deliveryFee)}</span></div><div className="total"><strong>Итого</strong><strong>{money(total)}</strong></div></div>
            <div className="payment-note"><b>Онлайн-оплата готовится</b><p>Платёжный сервис пока не выбран. Заказ сохранится, но деньги списываться не будут.</p></div>
            <button className="primary-button full" disabled={submitting || (delivery === "cdek" && (!cdekQuote || !cdekOfficeCode))}>{submitting ? "Оформляем…" : "Подтвердить заказ"}</button>
            <label className="consent-check"><input type="checkbox" required /><span>Я даю согласие на обработку персональных данных в соответствии с <a href="/privacy" target="_blank">политикой</a> и принимаю условия <a href="/offer" target="_blank">оферты</a>.</span></label>
          </form>
        )}
      </aside>

      <aside className={`mobile-menu ${menuOpen ? "open" : ""}`} aria-hidden={!menuOpen}>
        <button onClick={() => setMenuOpen(false)} aria-label="Закрыть меню">×</button>
        {user ? (
          <><a href="/account">{user.fullName.trim().split(/\s+/)[0] || "Профиль"}</a>
          {(user.adminRole === "manager" || user.adminRole === "owner") && <a href="/admin">Панель управления</a>}</>
        ) : (
          <><a href="/login">Войти</a><a href="/register">Регистрация</a></>
        )}<a href="/favorites" onClick={() => setMenuOpen(false)}>Избранное ({favorites.size})</a><a href="#catalog" onClick={() => setMenuOpen(false)}>Каталог</a><a href="#new" onClick={() => setMenuOpen(false)}>Новинки</a><a href="#care" onClick={() => setMenuOpen(false)}>Уход</a><a href="#delivery" onClick={() => setMenuOpen(false)}>Доставка</a>
      </aside>
    </main>
  );
}
