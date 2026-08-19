import type { Dispatch, FormEventHandler, SetStateAction } from "react";
import { useRef, useState } from "react";
import { formatRussianPhoneInput } from "./lib/phone";

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
  page?: boolean;
};

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);

function LineIcon({ name }: { name: "close" | "trash" | "plant" | "courier" | "post" | "card" | "wallet" }) {
  const paths = {
    close: <><path d="m7 7 10 10M17 7 7 17" /></>,
    trash: <><path d="M8 8h8l-.7 11H8.7L8 8Z" /><path d="M6.5 8h11M10 5h4l1 3M11 11v5M14 11v5" /></>,
    plant: <><path d="M7 19h10l1-7H6l1 7Z" /><path d="M12 12V5M12 8c-3 0-5-1.5-5-4 3 0 5 1.5 5 4ZM12 10c3 0 5-1.5 5-4-3 0-5 1.5-5 4Z" /></>,
    courier: <><circle cx="7" cy="17" r="2" /><circle cx="17" cy="17" r="2" /><path d="M5 17H3l2-6h8l2 6M10 11l2-4h3M15 9h3l2 5h-5" /></>,
    post: <><path d="M4 7h16v11H4z" /><path d="m4 8 8 6 8-6" /></>,
    card: <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="M3 10h18M7 15h4" /></>,
    wallet: <><path d="M4 7h14a2 2 0 0 1 2 2v10H6a2 2 0 0 1-2-2V7Z" /><path d="M4 7l12-3v3M15 12h6v4h-6a2 2 0 1 1 0-4Z" /></>,
  } as const;
  return <svg className="line-icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

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
  page = false,
}: CartDrawerProps) {
  const lineCards = lines.map((item) => (
    <div className="cart-line" key={item.id}>
      <img src={item.image} alt="" />
      <div className="cart-line-copy">
        <h3>{item.name}</h3>
        <small>Живое растение</small>
        <span className="cart-stock">В наличии</span>
        <div className="cart-line-mobile-price">{money(item.price)}</div>
      </div>
      <strong className="cart-unit-price">{money(item.price)}</strong>
      <div className="quantity">
        <button onClick={() => onQuantityChange(item.id, item.quantity - 1)} aria-label="Уменьшить">−</button>
        <span>{item.quantity}</span>
        <button onClick={() => onQuantityChange(item.id, item.quantity + 1)} aria-label="Увеличить">+</button>
      </div>
      <strong className="cart-line-total">{money(item.price * item.quantity)}</strong>
      <button className="remove" onClick={() => onQuantityChange(item.id, 0)} aria-label={`Удалить ${item.name}`}><LineIcon name="trash" /></button>
    </div>
  ));
  return (
    <aside className={`drawer ${page ? "cart-page-panel" : ""} ${open ? "open" : ""}`} aria-hidden={!open}>
      <div className="drawer-head">
        <div>
          <p className="eyebrow">Ваш выбор</p>
          <h2>Корзина</h2>
        </div>
        <button onClick={onClose} aria-label="Закрыть корзину"><LineIcon name="close" /></button>
      </div>
      <div className="cart-content">
      <section className="cart-table">
        {page && !!lines.length && <div className="cart-table-head"><span>Товар</span><span>Цена</span><span>Количество</span><span>Сумма</span><i /></div>}
        <div className="cart-lines">
        {lineCards}
        {!lines.length && (
          <div className="empty-cart">
            <span>⌁</span>
            <h3>Корзина пока пуста</h3>
            <p>Добавьте растения из каталога — они появятся здесь.</p>
            <button onClick={onClose}>Перейти в каталог</button>
          </div>
        )}
        </div>
        {page && !!lines.length && <div className="cart-page-actions"><a href="/#catalog">←&nbsp;&nbsp; Продолжить покупки</a><button className="primary-button" onClick={onCheckout}>Оформить заказ <span>→</span></button></div>}
      </section>
      {!!lines.length && (
        <aside className="cart-summary">
          <dl><div><dt>Итого товаров</dt><dd>{lines.length}</dd></div><div><dt>Подытог</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{page ? "от 250 ₽" : "при оформлении"}</dd></div></dl>
          <div className="cart-summary-total"><span>Итого</span><strong>{money(subtotal + (page ? 250 : 0))}</strong></div>
          {!page && <div className="cart-bonus" hidden />}
          {!page && <button className="primary-button" onClick={onCheckout}>
            Оформить заказ <span>→</span>
          </button>}
          {page && <button className="primary-button cart-summary-checkout" onClick={onCheckout}>
            Оформить заказ <span>→</span>
          </button>}
          {page && <img className="cart-summary-art" src="/assets/redesign/checkout-summary-art.png" alt="" />}
        </aside>
      )}
      </div>
    </aside>
  );
}
type CheckoutProfile = {
  name: string;
  phone: string;
  email: string;
  address: string;
};

type PaymentMethod = { id: string; title: string; note: string };
type DeliveryOption = {
  id: string;
  title: string;
  detail: string;
  fee: number | null;
};
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

type CheckoutPanelProps = {
  page?: boolean;
  checkoutOpen: boolean;
  setCheckoutOpen: (open: boolean) => void;
  orderNumber: string;
  submitOrder: FormEventHandler<HTMLFormElement>;
  user: boolean;
  checkoutProfile: CheckoutProfile;
  setCheckoutProfile: Dispatch<SetStateAction<CheckoutProfile>>;
  availableDelivery: DeliveryOption[];
  delivery: string;
  setDelivery: Dispatch<SetStateAction<string>>;
  cdekQuote: CdekQuote | null;
  cdekCityQuery: string;
  setCdekCityQuery: Dispatch<SetStateAction<string>>;
  setCdekCity: Dispatch<SetStateAction<CdekCity | null>>;
  setCdekCities: Dispatch<SetStateAction<CdekCity[]>>;
  setCdekOffices: Dispatch<SetStateAction<CdekOffice[]>>;
  cdekOfficeCode: string;
  setCdekOfficeCode: Dispatch<SetStateAction<string>>;
  cdekOfficeQuery: string;
  setCdekOfficeQuery: Dispatch<SetStateAction<string>>;
  setCdekQuotes: Dispatch<SetStateAction<CdekQuote[]>>;
  setCdekTariffCode: Dispatch<SetStateAction<number>>;
  cdekCities: CdekCity[];
  chooseCdekCity: (city: CdekCity) => void | Promise<void>;
  cdekLoading: boolean;
  cdekError: string;
  cdekOffices: CdekOffice[];
  setCdekOfficeListOpen: Dispatch<SetStateAction<boolean>>;
  cdekOfficeListOpen: boolean;
  cdekOfficeMatches: CdekOffice[];
  selectedOffice: CdekOffice | null;
  cdekFeePending: boolean;
  cdekRepack: boolean;
  setCdekRepack: Dispatch<SetStateAction<boolean>>;
  cartCount: number;
  cdekQuotes: CdekQuote[];
  paymentMethods: PaymentMethod[];
  paymentMethod: string;
  setPaymentMethod: Dispatch<SetStateAction<string>>;
  subtotal: number;
  deliveryFee: number;
  total: number;
  submitting: boolean;
};

export function CheckoutPanel(props: CheckoutPanelProps) {
  const {
    page = false,
    checkoutOpen,
    setCheckoutOpen,
    orderNumber,
    submitOrder,
    user,
    checkoutProfile,
    setCheckoutProfile,
    availableDelivery,
    delivery,
    setDelivery,
    cdekQuote,
    cdekCityQuery,
    setCdekCityQuery,
    setCdekCity,
    setCdekCities,
    setCdekOffices,
    cdekOfficeCode,
    setCdekOfficeCode,
    cdekOfficeQuery,
    setCdekOfficeQuery,
    setCdekQuotes,
    setCdekTariffCode,
    cdekCities,
    chooseCdekCity,
    cdekLoading,
    cdekError,
    cdekOffices,
    setCdekOfficeListOpen,
    cdekOfficeListOpen,
    cdekOfficeMatches,
    selectedOffice,
    cdekFeePending,
    cdekRepack,
    setCdekRepack,
    cartCount,
    cdekQuotes,
    paymentMethods,
    paymentMethod,
    setPaymentMethod,
    subtotal,
    deliveryFee,
    total,
    submitting,
  } = props;
  const [step, setStep] = useState<1|2|3>(1);
  const formRef = useRef<HTMLFormElement>(null);
  const advance = (next: 2|3) => {
    const current = formRef.current?.querySelector<HTMLElement>(`[data-checkout-step="${step}"]`);
    const fields = Array.from(current?.querySelectorAll<HTMLInputElement>("input,textarea,select") || []);
    if (fields.some((field) => !field.reportValidity())) return;
    setStep(next);
    window.scrollTo({ top: 0, behavior: "smooth" });
  };
  const deliveryIcon = (id: string) => id === "pickup" ? "plant" : id === "cdek" ? "post" : id.includes("post") ? "post" : "courier";

  return (
  <aside className={`checkout ${page ? "checkout-page-panel" : ""} ${checkoutOpen ? "open" : ""}`} aria-hidden={!checkoutOpen}>
    <div className="drawer-head"><div><p className="eyebrow">Бережно соберём и доставим</p><h2>{orderNumber ? "Заказ принят" : "Оформление заказа"}</h2></div>{page ? <a href="/cart" aria-label="Вернуться в корзину">←</a> : <button onClick={() => setCheckoutOpen(false)} aria-label="Закрыть оформление">×</button>}</div>
    {orderNumber ? (
      <div className="success">
        <div className="success-copy"><span>♡</span><h2>Заказ принят</h2><p>Спасибо! Мы уже готовим ваши растения к отправке.</p><div className="success-number"><small>Номер заказа</small><strong>{orderNumber}</strong></div><p>Письмо с деталями заказа отправили на указанную почту.</p><a className="primary-button" href="/account/orders">Отслеживать заказ →</a></div>
        <img src="/assets/redesign/checkout-success-art.png" alt="" />
      </div>
    ) : (
      <div className="checkout-layout"><form ref={formRef} onSubmit={submitOrder}>
        <nav className="checkout-steps" aria-label="Этапы оформления">{([[1,"Контактные данные"],[2,"Доставка"],[3,"Оплата"],[4,"Подтверждение"]] as const).map(([number,label])=><span className={(step===number||(number===4&&!!orderNumber))?"active":number<step?"complete":""} key={number}><b>{number<step?"✓":number}</b><small>{label}</small></span>)}</nav>
        <fieldset data-checkout-step="1" hidden={step!==1}>
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
          <div className="checkout-navigation"><a href="/cart">← Вернуться в корзину</a><button type="button" className="primary-button" onClick={() => advance(2)}>Продолжить →</button></div>
        </fieldset>
        <fieldset data-checkout-step="2" hidden={step!==2}>
          <legend>Способ доставки</legend>
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
                <i className="option-icon"><LineIcon name={deliveryIcon(item.id)} /></i><span><b>{item.title}</b><small>{item.detail}</small></span>
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
          <div className="checkout-navigation"><button type="button" onClick={() => setStep(1)}>← Назад</button><button type="button" className="primary-button" disabled={delivery === "cdek" && !cdekOfficeCode} onClick={() => advance(3)}>Продолжить →</button></div>
        </fieldset>
        <div data-checkout-step="3" hidden={step!==3}>
        {paymentMethods.length > 0 && (
          <fieldset>
            <legend>Способ оплаты</legend>
            <div className="delivery-options">
              {paymentMethods.map((option) => (
                <label key={option.id} className={paymentMethod === option.id ? "active" : ""}>
                  <input
                    type="radio"
                    name="paymentMethod"
                    checked={paymentMethod === option.id}
                    onChange={() => setPaymentMethod(option.id)}
                  />
                  <i className="option-icon"><LineIcon name={option.id === "online" ? "card" : "wallet"} /></i><span>
                    <b>{option.title}</b>
                    <small>{option.note}</small>
                  </span>
                </label>
              ))}
            </div>
            {paymentMethod === "online" && cdekFeePending && (
              <p className="cdek-status">
                Оплата после подтверждения заказа менеджером.
              </p>
            )}
          </fieldset>
        )}
        <fieldset><legend>Комментарий</legend><label><textarea name="comment" rows={3} placeholder="Удобное время, пожелания к заказу" /></label></fieldset>
        <div className="checkout-total"><div><span>Товары</span><span>{money(subtotal)}</span></div><div><span>Доставка</span><span>{delivery === "cdek" && !cdekOfficeCode ? "после выбора ПВЗ" : cdekFeePending ? "рассчитает менеджер" : money(deliveryFee)}</span></div><div className="total"><strong>Итого</strong><strong>{cdekFeePending && cdekOfficeCode ? `${money(total)} + доставка` : money(total)}</strong></div>{cdekFeePending && cdekOfficeCode && <p className="cdek-status">Оплата после подтверждения заказа менеджером.</p>}</div>
        {!paymentMethods.length && <div className="payment-note"><b>Не удалось загрузить способы оплаты</b><p>Обновите страницу или попробуйте ещё раз позже. Заказ без выбранного способа оплаты не отправится.</p></div>}
        <label className="consent-check"><input type="checkbox" name="consent" required /><span>Я даю согласие на обработку персональных данных в соответствии с <a href="/privacy" target="_blank">политикой</a> и принимаю условия <a href="/offer" target="_blank">оферты</a>.</span></label>
        <div className="checkout-navigation"><button type="button" onClick={() => setStep(2)}>← Назад</button><button className="primary-button" disabled={submitting || !paymentMethods.length || (delivery === "cdek" && !cdekOfficeCode)}>{submitting ? "Оформляем…" : paymentMethod === "online" && !cdekFeePending && paymentMethods.length ? "Перейти к оплате →" : "Подтвердить заказ →"}</button></div>
        </div>
      </form><aside className="checkout-order-summary"><h3>Ваш заказ</h3><dl><div><dt>Товаров</dt><dd>{cartCount}</dd></div><div><dt>Подытог</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{deliveryFee ? money(deliveryFee) : "при оформлении"}</dd></div></dl><div><span>Итого</span><strong>{money(total)}</strong></div><img src="/assets/redesign/checkout-summary-art.png" alt="" /></aside></div>
    )}
  </aside>
  );
}
