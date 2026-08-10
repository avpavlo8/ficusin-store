import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  formatRussianPhoneInput,
  normalizeRussianPhone,
} from "./lib/phone";
import { StoreHeader } from "./StoreHeader";

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
export type CartProduct = Pick<Product, "id" | "name" | "price" | "image" | "stock">;
type PaymentMethod = { id: string; title: string; note: string };
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
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [delivery, setDelivery] = useState("pickup");
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [orderNumber, setOrderNumber] = useState("");
  const [cdekCityQuery, setCdekCityQuery] = useState("");
  const [cdekCities, setCdekCities] = useState<CdekCity[]>([]);
  const [cdekCity, setCdekCity] = useState<CdekCity | null>(null);
  const [cdekOffices, setCdekOffices] = useState<CdekOffice[]>([]);
  const [cdekOfficeCode, setCdekOfficeCode] = useState("");
  // Moscow has hundreds of pick-up points. A dropdown of them all is a scroll
  // through a phone book, so the customer types part of the address instead.
  const [cdekOfficeQuery, setCdekOfficeQuery] = useState("");
  const [cdekOfficeListOpen, setCdekOfficeListOpen] = useState(false);
  const [cdekQuotes, setCdekQuotes] = useState<CdekQuote[]>([]);
  const [cdekTariffCode, setCdekTariffCode] = useState(0);
  // Every plant is quoted in its own box. For an order of several the
  // customer can ask whether they fit into one — only the packer can tell.
  const [cdekRepack, setCdekRepack] = useState(false);
  // Which ways to pay this customer may use is decided by the server: it
  // depends on how they collect and on whether they are a wholesale buyer.
  const [paymentMethods, setPaymentMethods] = useState<PaymentMethod[]>([]);
  const [paymentMethod, setPaymentMethod] = useState("online");
  // Set when the customer comes back from the payment page.
  const [paymentReturn, setPaymentReturn] = useState("");
  const [cdekLoading, setCdekLoading] = useState(false);
  const [cdekError, setCdekError] = useState("");
  // Pick-up points need API keys. Without them the option is hidden rather
  // than offered and then failing at the last step of the checkout.
  const [cdekAvailable, setCdekAvailable] = useState(true);
  const [user, setUser] = useState<StoreUser | null>(null);
  const [checkoutProfile, setCheckoutProfile] = useState<CheckoutProfile>({
    name: "",
    phone: "",
    email: "",
    address: "",
  });
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

  // The options depend on the delivery method, so this is asked again
  // whenever the customer switches between pick-up and shipping.
  useEffect(() => {
    let cancelled = false;
    fetch(`/api/v1/payments/methods?delivery=${encodeURIComponent(delivery)}`, {
      credentials: "same-origin",
      cache: "no-store",
    })
      .then((response) => response.json())
      .then((data: { methods?: PaymentMethod[] }) => {
        if (cancelled) return;
        const methods = data.methods ?? [];
        setPaymentMethods(methods);
        setPaymentMethod((current) =>
          methods.some((item) => item.id === current) ? current : (methods[0]?.id ?? "online"),
        );
      })
      .catch(() => {
        if (!cancelled) setPaymentMethods([]);
      });
    return () => {
      cancelled = true;
    };
  }, [delivery]);

  useEffect(() => {
    fetch("/api/v1/delivery/cdek?action=status", { cache: "no-store" })
      .then((response) => response.json())
      .then((data: { available?: boolean }) => setCdekAvailable(Boolean(data.available)))
      .catch(() => setCdekAvailable(false));
  }, []);

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
  }, []);

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

  const productsForCart = cartProducts ?? products;
  const cartLines = productsForCart
    .filter((product) => cart[product.id])
    .map((product) => ({ ...product, quantity: cart[product.id] }));
  const cartCount = cartLines.reduce((sum, item) => sum + item.quantity, 0);
  const subtotal = cartLines.reduce((sum, item) => sum + item.price * item.quantity, 0);
  const availableDelivery = deliveryOptions.filter((item) => item.id !== "cdek" || cdekAvailable);
  const deliveryOption = deliveryOptions.find((item) => item.id === delivery) ?? deliveryOptions[0];
  // The cheapest tariff comes first and is what we preselect; the customer
  // may pay more for a faster one.
  const cdekQuote =
    cdekQuotes.find((item) => item.tariffCode === cdekTariffCode) ?? cdekQuotes[0] ?? null;
  // Either we have a price we can stand behind, or a person will work it
  // out. There is no third case worth showing a number for.
  const cdekFeePending = delivery === "cdek" && (!cdekQuote || cdekRepack);
  const deliveryFee =
    delivery === "cdek"
      ? cdekFeePending
        ? 0
        : (cdekQuote?.price ?? 0)
      : (deliveryOption.fee ?? 0);
  const officeSearch = cdekOfficeQuery.trim().toLowerCase();
  const cdekOfficeMatches = (
    officeSearch
      ? cdekOffices.filter((office) =>
          `${office.location.address} ${office.name}`.toLowerCase().includes(officeSearch),
        )
      : cdekOffices
  ).slice(0, 12);
  const selectedOffice = cdekOffices.find((office) => office.code === cdekOfficeCode) ?? null;
  const total = subtotal + deliveryFee;

  async function chooseCdekCity(city: CdekCity) {
    setCdekCity(city);
    setCdekCityQuery(city.city);
    setCdekCities([]);
    setCdekOffices([]);
    setCdekOfficeCode("");
    setCdekOfficeQuery("");
    setCdekQuotes([]);
    setCdekTariffCode(0);
    setCdekLoading(true);
    setCdekError("");
    try {
      const [officesResponse, quoteResponse] = await Promise.all([
        fetch(`/api/v1/delivery/cdek?action=offices&cityCode=${city.code}`),
        fetch("/api/v1/delivery/cdek", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          // The price follows the boxes of the actual plants in the cart,
          // so the server needs to know what is in it.
          body: JSON.stringify({
            cityCode: city.code,
            items: cartLines.map((line) => ({ id: line.id, quantity: line.quantity })),
          }),
        }),
      ]);
      const officesData = (await officesResponse.json()) as {
        offices?: CdekOffice[];
        error?: string;
      };
      const quoteData = (await quoteResponse.json()) as {
        quotes?: CdekQuote[];
        pending?: boolean;
        error?: string;
      };
      if (!officesResponse.ok) {
        throw new Error(officesData.error || "Не удалось загрузить пункты выдачи");
      }
      if (!officesData.offices?.length) {
        throw new Error("В этом городе нет доступных пунктов выдачи");
      }
      setCdekOffices(officesData.offices);
      // No price is not a failure. Some plants have no box measured yet and
      // CDEK is not always up; either way the order goes through and the
      // manager works the cost out.
      if (quoteData.quotes?.length) {
        setCdekQuotes(quoteData.quotes);
        setCdekTariffCode(quoteData.quotes[0].tariffCode);
      } else {
        setCdekQuotes([]);
        setCdekTariffCode(0);
      }
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
              tariffCode: cdekRepack ? 0 : (cdekQuote?.tariffCode ?? 0),
              repack: cdekRepack,
            }
          : undefined,
      items: cartLines.map((item) => ({ id: item.id, quantity: item.quantity })),
      // The checkbox is required in the markup, so reaching this point
      // means it was ticked. The server records the agreement against the
      // order — that record is the only evidence of it we would ever have.
      consent: form.get("consent") === "on",
      paymentMethod,
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
      // Paying by card sends the customer straight to the payment page.
      // If that fails, the order still exists and can be paid from the
      // account later, so the failure is a notice rather than an error.
      if (paymentMethod === "online" && !cdekFeePending) {
        try {
          const payment = await fetch(`/api/v1/payments/orders/${data.orderNumber}`, {
            method: "POST",
            credentials: "same-origin",
          });
          const result = (await payment.json()) as { confirmationUrl?: string };
          if (result.confirmationUrl) {
            window.location.assign(result.confirmationUrl);
            return;
          }
        } catch {
          setNotice("Заказ оформлен. Оплатить можно из личного кабинета");
        }
      }
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
          {availableDelivery.map((item, index) => (
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
                {availableDelivery.map((item) => (
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
                        setCdekOfficeQuery("");
                        setCdekQuotes([]);
                        setCdekTariffCode(0);
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
                      <input
                        value={cdekOfficeQuery}
                        onChange={(event) => {
                          setCdekOfficeQuery(event.target.value);
                          setCdekOfficeCode("");
                          setCdekOfficeListOpen(true);
                        }}
                        onFocus={() => setCdekOfficeListOpen(true)}
                        autoComplete="off"
                        placeholder="Улица или дом — покажем ближайшие пункты"
                      />
                    </label>
                  )}
                  {cdekOfficeListOpen && !!cdekOfficeMatches.length && !cdekOfficeCode && (
                    <div className="cdek-suggestions" role="listbox" aria-label="Пункты выдачи">
                      {cdekOfficeMatches.map((office) => (
                        <button
                          type="button"
                          key={office.code}
                          onClick={() => {
                            setCdekOfficeCode(office.code);
                            setCdekOfficeQuery(office.location.address);
                            setCdekOfficeListOpen(false);
                          }}
                        >
                          <b>{office.location.address}</b>
                          <span>{office.work_time || office.name}</span>
                        </button>
                      ))}
                    </div>
                  )}
                  {!!cdekOffices.length && !cdekOfficeMatches.length && (
                    <p className="cdek-status">Ничего не нашлось — попробуйте другую улицу</p>
                  )}
                  {selectedOffice && (
                    <p className="cdek-status">Пункт выбран: {selectedOffice.location.address}</p>
                  )}
                  {cdekFeePending && !!cdekOffices.length && (
                    <div className="cdek-quote pending">
                      <b>Рассчитает менеджер</b>
                      <span>после оформления</span>
                      <small>
                        {cdekRepack
                          ? "Менеджер проверит, поместятся ли растения в одну коробку, посчитает доставку и свяжется с вами до отправки."
                          : "Стоимость доставки менеджер посчитает и сообщит вам до отправки заказа. Оформить заказ можно уже сейчас."}
                      </small>
                    </div>
                  )}
                  {/* Three of the same plant are three boxes too, so the
                      offer depends on how many go in the van, not on how
                      many lines the cart has. */}
                  {cartCount > 1 && !!cdekQuotes.length && (
                    <div className="cdek-repack">
                      <label>
                        <input
                          type="checkbox"
                          checked={cdekRepack}
                          onChange={(event) => setCdekRepack(event.target.checked)}
                        />
                        Упаковать в одну коробку, если поместятся
                      </label>
                      <small>
                        Сейчас доставка посчитана по отдельной коробке на каждое растение. Менеджер
                        проверит, поместятся ли они вместе, и пересчитает — обычно выходит дешевле.
                      </small>
                    </div>
                  )}
                  {!cdekRepack && cdekQuotes.length > 1 && (
                    <div className="cdek-tariffs" role="radiogroup" aria-label="Тарифы СДЭК">
                      {cdekQuotes.map((option) => (
                        <label key={option.tariffCode} className="cdek-tariff">
                          <input
                            type="radio"
                            name="cdek-tariff"
                            checked={option.tariffCode === cdekQuote?.tariffCode}
                            onChange={() => setCdekTariffCode(option.tariffCode)}
                          />
                          <span>
                            <b>{option.tariffName}</b>
                            <small>
                              {option.daysMin === option.daysMax
                                ? `${option.daysMin} дн.`
                                : `${option.daysMin}–${option.daysMax} дн.`}
                            </small>
                          </span>
                          <strong>{money(option.price)}</strong>
                        </label>
                      ))}
                    </div>
                  )}
                  {!cdekRepack && cdekQuote && cdekQuotes.length === 1 && (
                    <div className="cdek-quote">
                      <b>{money(cdekQuote.price)}</b>
                      <span>
                        {cdekQuote.daysMin === cdekQuote.daysMax
                          ? `${cdekQuote.daysMin} дн.`
                          : `${cdekQuote.daysMin}–${cdekQuote.daysMax} дн.`}
                      </span>
                      <small>Расчёт по габаритам упаковки выбранных растений</small>
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
            {paymentMethods.length > 0 && (
              <fieldset>
                <legend>Оплата</legend>
                <div className="delivery-options">
                  {paymentMethods.map((option) => (
                    <label key={option.id} className={paymentMethod === option.id ? "active" : ""}>
                      <input
                        type="radio"
                        name="paymentMethod"
                        checked={paymentMethod === option.id}
                        onChange={() => setPaymentMethod(option.id)}
                      />
                      <span>
                        <b>{option.title}</b>
                        <small>{option.note}</small>
                      </span>
                    </label>
                  ))}
                </div>
                {paymentMethod === "online" && cdekFeePending && (
                  <p className="cdek-status">
                    Оплатить можно будет после того, как менеджер рассчитает доставку — ссылка
                    появится в личном кабинете.
                  </p>
                )}
              </fieldset>
            )}
            <fieldset><legend>Комментарий</legend><label><textarea name="comment" rows={3} placeholder="Удобное время, пожелания к заказу" /></label></fieldset>
            <div className="checkout-total"><div><span>Товары</span><span>{money(subtotal)}</span></div><div><span>Доставка</span><span>{delivery === "cdek" && !cdekOfficeCode ? "после выбора ПВЗ" : cdekFeePending ? "рассчитает менеджер" : money(deliveryFee)}</span></div><div className="total"><strong>Итого</strong><strong>{cdekFeePending && cdekOfficeCode ? `${money(total)} + доставка` : money(total)}</strong></div></div>
            {!paymentMethods.length && <div className="payment-note"><b>Оплата при получении</b><p>Онлайн-оплата пока не подключена. Менеджер свяжется с вами и подскажет, как оплатить заказ.</p></div>}
            <button className="primary-button full" disabled={submitting || (delivery === "cdek" && !cdekOfficeCode)}>{submitting ? "Оформляем…" : paymentMethod === "online" && !cdekFeePending && paymentMethods.length ? "Перейти к оплате" : "Подтвердить заказ"}</button>
            <label className="consent-check"><input type="checkbox" name="consent" required /><span>Я даю согласие на обработку персональных данных в соответствии с <a href="/privacy" target="_blank">политикой</a> и принимаю условия <a href="/offer" target="_blank">оферты</a>.</span></label>
          </form>
        )}
      </aside>

    </RootElement>
  );
}
