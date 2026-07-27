"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { normalizeRussianPhone } from "../lib/phone";

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

const categories = ["Все растения", "Крупные", "Неприхотливые", "Цветущие", "Ампельные"];
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
  const [category, setCategory] = useState("Все растения");
  const [query, setQuery] = useState("");
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
    let cancelled = false;
    fetch("/api/catalog", { cache: "no-store" })
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
      setCdekCities([]);
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        setCdekLoading(true);
        setCdekError("");
        const response = await fetch(
          `/api/delivery/cdek?action=cities&city=${encodeURIComponent(cdekCityQuery.trim())}`,
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

  const filtered = useMemo(
    () =>
      products.filter((product) => {
        const inCategory =
          category === "Все растения" ||
          product.category === category ||
          product.category === "Растения";
        const searchable = `${product.name} ${product.latin}`.toLowerCase();
        return inCategory && searchable.includes(query.toLowerCase().trim());
      }),
    [products, category, query],
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
        fetch(`/api/delivery/cdek?action=offices&cityCode=${city.code}`),
        fetch("/api/delivery/cdek", {
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
      const response = await fetch("/api/orders", {
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
          <button className="icon-button search-button" onClick={() => document.getElementById("search")?.focus()} aria-label="Поиск">
            <span aria-hidden="true">⌕</span>
          </button>
          <a className="account-button" href="/account" aria-label="Открыть личный кабинет">
            <span aria-hidden="true">◯</span>
            <span>Кабинет</span>
          </a>
          <button className="cart-button" onClick={() => setCartOpen(true)} aria-label={`Корзина, товаров: ${cartCount}`}>
            <span aria-hidden="true">Корзина</span>
            <b>{cartCount}</b>
          </button>
          <button className="menu-button" onClick={() => setMenuOpen(true)} aria-label="Открыть меню">☰</button>
        </div>
      </header>

      <section className="hero" id="top">
        <div className="hero-copy">
          <p className="eyebrow">Растения с характером</p>
          <h1>Живые растения<br />для дома и души</h1>
          <p className="hero-text">Подберём зелёного жителя под ваш интерьер и образ жизни. Доставим курьером по Рязани, СДЭК или Почтой России.</p>
          <div className="hero-actions">
            <a className="primary-button" href="#catalog">Выбрать растение <span>→</span></a>
            <a className="text-link" href="#help">Помочь с выбором</a>
          </div>
          <div className="trust-row">
            <span>✓ Проверяем перед отправкой</span>
            <span>✓ Поддержка после покупки</span>
          </div>
        </div>
        <div className="hero-visual">
          <img src="/assets/hero-monstera.png" alt="Большая монстера в терракотовом кашпо" />
          <div className="hero-note">
            <span>Монстера Делициоза</span>
            <strong>от 4 590 ₽</strong>
          </div>
        </div>
      </section>

      <section className="category-strip" aria-label="Категории растений">
        {categories.slice(1).map((item, index) => (
          <button key={item} onClick={() => { setCategory(item); document.getElementById("catalog")?.scrollIntoView({ behavior: "smooth" }); }}>
            <span>{["⌁", "☀", "✿", "⌇"][index]}</span>
            {item}
          </button>
        ))}
      </section>

      <section className="catalog-section" id="catalog">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Каталог</p>
            <h2>Найдите своё растение</h2>
          </div>
          <p>Каталог, цены и наличие обновляются из Saby автоматически.</p>
        </div>

        <div className="catalog-toolbar">
          <div className="filters" role="group" aria-label="Фильтр по категории">
            {categories.map((item) => (
              <button className={category === item ? "active" : ""} key={item} onClick={() => setCategory(item)}>{item}</button>
            ))}
          </div>
          <label className="search-field">
            <span aria-hidden="true">⌕</span>
            <input id="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск по названию" />
          </label>
        </div>

        <div className="product-grid" id="new">
          {catalogLoading && (
            <p className="catalog-status" role="status">
              Загружаем актуальные товары из Saby…
            </p>
          )}
          {!catalogLoading && catalogError && (
            <p className="catalog-status catalog-status-error" role="alert">
              {catalogError}. Обновите страницу через несколько секунд.
            </p>
          )}
          {filtered.map((product) => (
            <article className="product-card" key={product.id}>
              <div className="product-image">
                <img src={product.image} alt={product.name} />
                {product.badge && <span className="badge">{product.badge}</span>}
                <button className="favorite" aria-label={`Добавить ${product.name} в избранное`}>♡</button>
              </div>
              <div className="product-info">
                <p className="latin">{product.latin}</p>
                <h3>{product.name}</h3>
                <div className="product-meta"><span>{product.light}</span><span>{product.size}</span></div>
                <div className="product-bottom">
                  <strong>{money(product.price)}</strong>
                  <button
                    onClick={() => addToCart(product.id)}
                    disabled={product.stock === 0}
                  >
                    {product.stock === 0 ? "Нет в наличии" : "В корзину"}
                  </button>
                </div>
              </div>
            </article>
          ))}
          {!catalogLoading && !catalogError && filtered.length === 0 && (
            <p className="empty-state">
              По вашему запросу ничего не найдено. Попробуйте другую категорию.
            </p>
          )}
        </div>
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
        <div><h3>Режим работы</h3><span>Ежедневно</span><span>10:00–20:00</span></div>
        <small>© 2026 Фикусин · Реквизиты и политика будут добавлены перед запуском</small>
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
            <fieldset><legend>Контактные данные</legend><div className="field-grid"><label>Имя<input name="name" required placeholder="Александр" /></label><label>Телефон<input name="phone" required inputMode="tel" autoComplete="tel" maxLength={18} placeholder="+7 900 000-00-00" onInput={(event) => event.currentTarget.setCustomValidity("")} onBlur={(event) => { const normalized = normalizeRussianPhone(event.currentTarget.value); if (normalized) event.currentTarget.value = normalized; }} /></label></div><label>Email для чека<input name="email" required type="email" placeholder="mail@example.ru" /></label></fieldset>
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
            <p className="legal-note">Нажимая кнопку, вы соглашаетесь с обработкой персональных данных.</p>
          </form>
        )}
      </aside>

      <aside className={`mobile-menu ${menuOpen ? "open" : ""}`} aria-hidden={!menuOpen}>
        <button onClick={() => setMenuOpen(false)} aria-label="Закрыть меню">×</button>
        <a href="/account">Личный кабинет</a><a href="#catalog" onClick={() => setMenuOpen(false)}>Каталог</a><a href="#new" onClick={() => setMenuOpen(false)}>Новинки</a><a href="#care" onClick={() => setMenuOpen(false)}>Уход</a><a href="#delivery" onClick={() => setMenuOpen(false)}>Доставка</a>
      </aside>
    </main>
  );
}
