import type { Dispatch, FormEventHandler, SetStateAction } from "react";
import { useEffect, useRef, useState } from "react";
import { formatRussianPhoneInput } from "./lib/phone";
import { track } from "./lib/analytics";

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

function LineIcon({ name }: { name: "close" | "trash" }) {
  const paths = {
    close: <><path d="m7 7 10 10M17 7 7 17" /></>,
    trash: <><path d="M8 8h8l-.7 11H8.7L8 8Z" /><path d="M6.5 8h11M10 5h4l1 3M11 11v5M14 11v5" /></>,
  } as const;
  return <svg className="line-icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

type CheckoutIconName = "pickup" | "courier" | "cdek" | "post" | "card" | "wallet";

function CheckoutOptionIcon({ name }: { name: CheckoutIconName }) {
  if (name === "cdek") return <span className="checkout-brand-icon cdek-icon" aria-hidden="true">CDEK</span>;
  if (name === "post") return <span className="checkout-brand-icon post-icon" aria-hidden="true"><svg viewBox="0 0 64 48"><path d="M7 35c13-2 20-10 27-25 3 11 11 18 23 20-10 3-18 3-25-1-7 7-15 9-25 6Z"/><path d="M13 40c16-1 28-8 36-20"/></svg></span>;
  const artwork = {
    pickup: <><path fill="#d5a35f" d="m9 28 15-7 14 7-15 8Z"/><path fill="#bd8146" d="m9 28 14 8v17L9 45Z"/><path fill="#e4bb7d" d="m23 36 15-8v17l-15 8Z"/><path fill="#f5e3c2" d="m18 24 14 8 4-2-14-8Z"/><path fill="#355b2d" d="M34 23c-2-10 3-16 11-18-1 8-4 13-11 18Z"/><path fill="#5e7b38" d="M34 24c1-9-4-14-11-16 0 8 4 13 11 16Z"/><path d="M34 13v18"/></>,
    courier: <><circle cx="17" cy="46" r="7" fill="#263f28"/><circle cx="47" cy="46" r="7" fill="#263f28"/><path fill="#c9a86f" d="M15 42h29l-4-17H23l-7 9Z"/><path fill="#e4c792" d="M34 16h12l6 19H39Z"/><path d="M28 25h15M42 16l-3-7h-8M8 37h10"/><path fill="#9f5c30" d="M10 24h14v11H10Z"/></>,
    card: <><rect x="7" y="15" width="50" height="34" rx="6" fill="#e8bb72"/><rect x="7" y="20" width="50" height="8" fill="#2b332a"/><rect x="13" y="35" width="13" height="7" rx="2" fill="#f7e9cf"/><circle cx="49" cy="40" r="5" fill="#d84b2b" opacity=".85"/></>,
    wallet: <><path fill="#d8ad72" d="M9 19h43a6 6 0 0 1 6 6v27H15a6 6 0 0 1-6-6Z"/><path fill="#f1d29e" d="m12 19 34-10 3 10Z"/><path fill="#bc7b43" d="M40 31h20v13H40a6 6 0 1 1 0-13Z"/><circle cx="45" cy="37.5" r="2.5" fill="#fff1d4"/></>,
  } as const;
  return <svg className={`checkout-option-icon ${name}`} viewBox="0 0 64 64" aria-hidden="true" fill="none" stroke="#3d3a2f" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">{artwork[name]}</svg>;
}

export function CartDrawer({
  open,
  lines,
  subtotal,
  onClose,
  onQuantityChange,
  onCheckout,
  page = false,
}: CartDrawerProps) {
  const checkoutActionRef = useRef<HTMLButtonElement>(null);
  const [checkoutActionVisible, setCheckoutActionVisible] = useState(false);
  useEffect(() => {
    if (!page || !lines.length || !checkoutActionRef.current) return;
    const observer = new IntersectionObserver(
      ([entry]) => setCheckoutActionVisible(entry.isIntersecting),
      { threshold: 0.55 },
    );
    observer.observe(checkoutActionRef.current);
    return () => observer.disconnect();
  }, [page, lines.length]);
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
        <div><p className="eyebrow">Ваш выбор</p><h2>Корзина</h2></div>
        <button onClick={onClose} aria-label="Закрыть корзину"><LineIcon name="close" /></button>
      </div>
      <div className="cart-content">
        <section className="cart-table">
          {page && !!lines.length && <div className="cart-table-head"><span>Товар</span><span>Цена</span><span>Количество</span><span>Сумма</span><i /></div>}
          <div className="cart-lines">
            {lineCards}
            {!lines.length && (
              <div className="empty-cart">
                <span>⌁</span><h3>Корзина пока пуста</h3>
                <p>Добавьте растения из каталога — они появятся здесь.</p>
                <button onClick={onClose}>Перейти в каталог</button>
              </div>
            )}
          </div>
          {page && !!lines.length && <div className="cart-page-actions"><a href="/#catalog">←&nbsp;&nbsp; Продолжить покупки</a><button ref={checkoutActionRef} className="primary-button" onClick={onCheckout}>Оформить заказ <span>→</span></button></div>}
        </section>
        {!!lines.length && (
          <aside className="cart-summary">
            <dl><div><dt>Итого товаров</dt><dd>{lines.length}</dd></div><div><dt>Подытог</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>при оформлении</dd></div></dl>
            <div className="cart-summary-total"><span>Итого</span><strong>{money(subtotal)}</strong></div>
            {!page && <div className="cart-bonus" hidden />}
            {!page && <button className="primary-button" onClick={onCheckout}>Оформить заказ <span>→</span></button>}
            {page && !checkoutActionVisible && <button className="primary-button cart-summary-checkout" aria-label="Перейти к оформлению" onClick={onCheckout}>Оформить заказ <span>→</span></button>}
            {page && <img className="cart-summary-art" src="/assets/redesign/checkout-summary-art.png" alt="" />}
          </aside>
        )}
      </div>
    </aside>
  );
}

type CheckoutProfile = { name: string; phone: string; email: string; address: string };
type PaymentMethod = { id: string; title: string; note: string };
type DeliveryOption = { id: string; title: string; detail: string; fee: number | null };
type CdekCity = { code: number; city: string; region?: string };
type CdekOffice = {
  code: string;
  name: string;
  location: { city: string; address: string; address_full?: string };
  work_time?: string;
};
type CdekQuote = { tariffCode: number; tariffName: string; price: number; daysMin: number; daysMax: number };
type AddressDeliveryQuote = { price: number; daysMin?: number; daysMax?: number; service?: string };

type CheckoutPanelProps = {
  page?: boolean;
  checkoutOpen: boolean;
  setCheckoutOpen: (open: boolean) => void;
  orderNumber: string;
  orderConfirmationPending: boolean;
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
  deliveryQuote: AddressDeliveryQuote | null;
  deliveryQuoteLoading: boolean;
  deliveryQuotePending: boolean;
  deliveryQuoteError: string;
  deliveryFeePending: boolean;
  addressDeliveryNeedsQuote: boolean;
  calculateAddressDelivery: () => void | Promise<void>;
};

function deliveryDays(quote: AddressDeliveryQuote) {
  if (!quote.daysMin && !quote.daysMax) return "";
  if (quote.daysMin === quote.daysMax) return `${quote.daysMin} дн.`;
  return `${quote.daysMin || 1}–${quote.daysMax || quote.daysMin} дн.`;
}

export function CheckoutPanel(props: CheckoutPanelProps) {
  const {
    page = false, checkoutOpen, setCheckoutOpen, orderNumber, orderConfirmationPending, submitOrder, user,
    checkoutProfile, setCheckoutProfile, availableDelivery, delivery, setDelivery,
    cdekQuote, cdekCityQuery, setCdekCityQuery, setCdekCity, setCdekCities,
    setCdekOffices, cdekOfficeCode, setCdekOfficeCode, cdekOfficeQuery,
    setCdekOfficeQuery, setCdekQuotes, setCdekTariffCode, cdekCities, chooseCdekCity,
    cdekLoading, cdekError, cdekOffices, setCdekOfficeListOpen, cdekOfficeListOpen,
    cdekOfficeMatches, selectedOffice, cdekFeePending, cdekRepack, setCdekRepack,
    cartCount, cdekQuotes, paymentMethods, paymentMethod, setPaymentMethod, subtotal,
    deliveryFee, total, submitting, deliveryQuote, deliveryQuoteLoading,
    deliveryQuotePending, deliveryQuoteError, deliveryFeePending,
    addressDeliveryNeedsQuote, calculateAddressDelivery,
  } = props;
  const [step, setStep] = useState<1|2|3>(1);
  const formRef = useRef<HTMLFormElement>(null);
  const advance = (next: 2|3) => {
    const current = formRef.current?.querySelector<HTMLElement>(`[data-checkout-step="${step}"]`);
    const fields = Array.from(current?.querySelectorAll<HTMLInputElement>("input,textarea,select") || []);
    if (fields.some((field) => !field.reportValidity())) return;
	track("checkout_step", { value: total, quantity: cartCount, properties: { from: step, to: next } });
    setStep(next);
    window.scrollTo({ top: 0, behavior: "smooth" });
  };
  const deliveryIcon = (id: string): CheckoutIconName => id === "pickup" ? "pickup" : id === "cdek" ? "cdek" : id.includes("post") ? "post" : "courier";
  const selectedDelivery = availableDelivery.find((item) => item.id === delivery);
  const selectedPayment = paymentMethods.find((item) => item.id === paymentMethod);
  const addressDelivery = delivery === "courier" || delivery === "post";
  const deliveryBlocked = (delivery === "cdek" && !cdekOfficeCode) || addressDeliveryNeedsQuote || deliveryQuoteLoading;
  const confirmationPending = Boolean(orderNumber && orderConfirmationPending);

  return (
    <aside className={`checkout ${page ? "checkout-page-panel" : ""} ${orderNumber ? "checkout-order-complete" : ""} ${checkoutOpen ? "open" : ""}`} aria-hidden={!checkoutOpen}>
      <div className="drawer-head"><div><p className="eyebrow">Бережно соберём и доставим</p><h2>{confirmationPending ? "Заказ ждёт подтверждения" : orderNumber ? "Заказ принят" : "Оформление заказа"}</h2></div>{page ? <a href="/cart" aria-label="Вернуться в корзину">←</a> : <button onClick={() => setCheckoutOpen(false)} aria-label="Закрыть оформление">×</button>}</div>
      {orderNumber ? (
        <div className="success">
          <div className="success-copy">
            <span className="success-heart" aria-hidden="true">♡</span>
            <h2>{confirmationPending ? "Заказ отправлен менеджеру" : "Заказ принят"}</h2>
            <p className="success-lead">{confirmationPending ? "Спасибо! Менеджер рассчитает доставку, уточнит детали и свяжется с вами для подтверждения заказа." : "Спасибо! Растения уже готовятся к встрече с вами."}</p>
            <div className="success-columns">
              <div className="success-order-info">
                <div className="success-number"><small>Номер заказа</small><strong>#{orderNumber}</strong></div>
                <p><i aria-hidden="true">⌖</i>{selectedDelivery?.title || "Способ получения выбран"}</p>
                <p><i aria-hidden="true">▱</i>{selectedPayment?.title || "Способ оплаты выбран"}</p>
                <a className="primary-button" href={`/account/orders/${encodeURIComponent(orderNumber)}`}>Следить за заказом <span aria-hidden="true">→</span></a>
                <a className="success-catalog-link" href="/#catalog">Вернуться в каталог <span aria-hidden="true">↗</span></a>
              </div>
              <div className="success-next"><h3>Что будет дальше</h3>{confirmationPending ? <ol>
                <li><b>1</b><span>Рассчитаем стоимость и условия доставки</span></li>
                <li><b>2</b><span>Менеджер свяжется с вами и согласует итоговую сумму</span></li>
                <li><b>3</b><span>{paymentMethod === "on_delivery" ? "После подтверждения начнём собирать заказ" : "После подтверждения пришлём ссылку на оплату"}</span></li>
              </ol> : <ol>
                <li><b>1</b><span>Соберём и проверим растения</span></li>
                <li><b>2</b><span>Аккуратно упакуем</span></li>
                <li><b>3</b><span>{delivery === "pickup" ? "Сообщим, когда заказ будет готов" : "Передадим заказ в доставку"}</span></li>
              </ol>}</div>
            </div>
          </div>
        </div>
      ) : (
        <div className="checkout-layout"><form ref={formRef} onSubmit={submitOrder}>
          <nav className="checkout-steps" aria-label="Этапы оформления">{([[1,"Контактные данные"],[2,"Доставка"],[3,"Оплата"],[4,"Подтверждение"]] as const).map(([number,label])=><span className={(step===number||(number===4&&!!orderNumber))?"active":number<step?"complete":""} key={number}><b>{number<step?"✓":number}</b><small>{label}</small></span>)}</nav>
          <fieldset data-checkout-step="1" hidden={step!==1}>
            <legend>Контактные данные</legend>
            {user && <p className="profile-prefill">Данные заполнены из личного кабинета</p>}
            <div className="checkout-contact-fields">
              <label className="checkout-contact-name">Имя<input name="name" required placeholder="Александр" autoComplete="name" value={checkoutProfile.name} onChange={(event) => setCheckoutProfile((current) => ({ ...current, name: event.target.value }))} /></label>
              <div className="checkout-contact-row"><label>Телефон<input name="phone" required inputMode="tel" autoComplete="tel" maxLength={18} placeholder="+7 900 000-00-00" value={checkoutProfile.phone} onChange={(event) => { event.currentTarget.setCustomValidity(""); const value = formatRussianPhoneInput(event.currentTarget.value); setCheckoutProfile((current) => ({ ...current, phone: value })); }} /></label><label>Email для чека<input name="email" required type="email" autoComplete="email" placeholder="mail@example.ru" value={checkoutProfile.email} onChange={(event) => setCheckoutProfile((current) => ({ ...current, email: event.target.value }))} /></label></div>
            </div>
            <label className="checkout-care-optin"><input type="checkbox" defaultChecked /><span>Хочу получать полезные советы по уходу за растениями</span></label>
            <div className="checkout-navigation"><a href="/cart">← Назад</a><button type="button" className="primary-button" onClick={() => advance(2)}>Продолжить <span>→</span></button></div>
          </fieldset>

          <fieldset data-checkout-step="2" hidden={step!==2}>
            <legend>Способ доставки</legend>
            <div className="delivery-options">
              {availableDelivery.map((item) => {
                const dynamic = item.id === "courier" || item.id === "post";
                const selectedDynamicQuote = dynamic && delivery === item.id ? deliveryQuote : null;
                return <label className={delivery === item.id ? "selected" : ""} key={item.id}>
                  <input type="radio" name="delivery" value={item.id} checked={delivery === item.id} onChange={() => { setDelivery(item.id); track("add_shipping_info",{value:total,quantity:cartCount,properties:{delivery:item.id}}); }} />
                  <i className="option-icon"><CheckoutOptionIcon name={deliveryIcon(item.id)} /></i><span><b>{item.title}</b><small>{item.detail}</small></span>
                  <strong>{item.id === "cdek" ? (delivery === "cdek" && cdekQuote ? money(cdekQuote.price) : "Рассчитать") : dynamic ? (selectedDynamicQuote ? money(selectedDynamicQuote.price) : "Рассчитать") : "0 ₽"}</strong>
                </label>;
              })}
            </div>

            {delivery === "cdek" ? (
              <div className="cdek-picker">
                <label>Город получения<input value={cdekCityQuery} onChange={(event) => { setCdekCityQuery(event.target.value); setCdekCity(null); setCdekCities([]); setCdekOffices([]); setCdekOfficeCode(""); setCdekOfficeQuery(""); setCdekQuotes([]); setCdekTariffCode(0); }} autoComplete="off" placeholder="Начните вводить город" /></label>
                {!!cdekCities.length && <div className="cdek-suggestions" role="listbox" aria-label="Найденные города">{cdekCities.map((city) => <button type="button" key={city.code} onClick={() => chooseCdekCity(city)}><b>{city.city}</b><span>{city.region || "Россия"}</span></button>)}</div>}
                {cdekLoading && <p className="cdek-status">Получаем данные СДЭК…</p>}
                {cdekError && <p className="cdek-status error">{cdekError}</p>}
                {!!cdekOffices.length && <label>Пункт выдачи<input value={cdekOfficeQuery} onChange={(event) => { setCdekOfficeQuery(event.target.value); setCdekOfficeCode(""); setCdekOfficeListOpen(true); }} onFocus={() => setCdekOfficeListOpen(true)} autoComplete="off" placeholder="Улица или дом — покажем ближайшие пункты" /></label>}
                {cdekOfficeListOpen && !!cdekOfficeMatches.length && !cdekOfficeCode && <div className="cdek-suggestions" role="listbox" aria-label="Пункты выдачи">{cdekOfficeMatches.map((office) => <button type="button" key={office.code} onClick={() => { setCdekOfficeCode(office.code); setCdekOfficeQuery(office.location.address); setCdekOfficeListOpen(false); }}><b>{office.location.address}</b><span>{office.work_time || office.name}</span></button>)}</div>}
                {!!cdekOffices.length && !cdekOfficeMatches.length && <p className="cdek-status">Ничего не нашлось — попробуйте другую улицу</p>}
                {selectedOffice && <p className="cdek-status">Пункт выбран: {selectedOffice.location.address}</p>}
                {cdekFeePending && !!cdekOffices.length && <div className="cdek-quote pending"><b>Рассчитает менеджер</b><span>после оформления</span><small>{cdekRepack ? "Менеджер проверит, поместятся ли растения в одну коробку, посчитает доставку и свяжется с вами до отправки." : "Стоимость доставки менеджер посчитает и сообщит вам до отправки заказа. Оформить заказ можно уже сейчас."}</small></div>}
                {cartCount > 1 && !!cdekQuotes.length && <div className="cdek-repack"><label><input type="checkbox" checked={cdekRepack} onChange={(event) => setCdekRepack(event.target.checked)} />Упаковать в одну коробку, если поместятся</label><small>Сейчас доставка посчитана по отдельной коробке на каждое растение. Менеджер проверит, поместятся ли они вместе, и пересчитает — обычно выходит дешевле.</small></div>}
                {!cdekRepack && cdekQuotes.length > 1 && <div className="cdek-tariffs" role="radiogroup" aria-label="Тарифы СДЭК">{cdekQuotes.map((option) => <label key={option.tariffCode} className="cdek-tariff"><input type="radio" name="cdek-tariff" checked={option.tariffCode === cdekQuote?.tariffCode} onChange={() => setCdekTariffCode(option.tariffCode)} /><span><b>{option.tariffName}</b><small>{option.daysMin === option.daysMax ? `${option.daysMin} дн.` : `${option.daysMin}–${option.daysMax} дн.`}</small></span><strong>{money(option.price)}</strong></label>)}</div>}
                {!cdekRepack && cdekQuote && cdekQuotes.length === 1 && <div className="cdek-quote"><b>{money(cdekQuote.price)}</b><span>{cdekQuote.daysMin === cdekQuote.daysMax ? `${cdekQuote.daysMin} дн.` : `${cdekQuote.daysMin}–${cdekQuote.daysMax} дн.`}</span><small>Расчёт по габаритам упаковки выбранных растений</small></div>}
              </div>
            ) : (
              <div className={addressDelivery ? "cdek-picker" : undefined}>
                <label>{delivery === "pickup" ? "Самовывоз" : "Адрес доставки"}<input name="address" required={delivery !== "pickup"} disabled={delivery === "pickup"} autoComplete="street-address" value={checkoutProfile.address} onChange={(event) => setCheckoutProfile((current) => ({ ...current, address: event.target.value }))} placeholder={delivery === "pickup" ? "Рязань, Новосёлов, 40А" : delivery === "courier" ? "Рязань, улица, дом, квартира" : "Город, улица, дом, квартира"} /></label>
                {addressDelivery && <>
                  <button type="button" className="primary-button" disabled={deliveryQuoteLoading || checkoutProfile.address.trim().length < 5} onClick={() => void calculateAddressDelivery()}>{deliveryQuoteLoading ? "Считаем…" : deliveryQuote ? "Пересчитать доставку" : "Рассчитать доставку"}</button>
                  {deliveryQuote && <div className="cdek-quote"><b>{money(deliveryQuote.price)}</b><span>{deliveryDays(deliveryQuote)}</span><small>{deliveryQuote.service || (delivery === "post" ? "Почта России" : "Яндекс Доставка")}. Расчёт по габаритам упаковки заказа.</small></div>}
                  {deliveryQuotePending && <div className="cdek-quote pending"><b>Стоимость уточнит менеджер</b><span>до оплаты</span><small>{deliveryQuoteError || "Заказ можно оформить сейчас; оплатить его получится после уточнения доставки."}</small></div>}
                  {!deliveryQuotePending && deliveryQuoteError && <p className="cdek-status error">{deliveryQuoteError}</p>}
                </>}
              </div>
            )}
            <div className="checkout-navigation"><button type="button" onClick={() => setStep(1)}>← Назад</button><button type="button" className="primary-button" disabled={deliveryBlocked} onClick={() => advance(3)}>Продолжить →</button></div>
          </fieldset>

          <div data-checkout-step="3" hidden={step!==3}>
            {paymentMethods.length > 0 && <fieldset><legend>Способ оплаты</legend><div className="delivery-options">{paymentMethods.map((option) => <label key={option.id} className={paymentMethod === option.id ? "active" : ""}><input type="radio" name="paymentMethod" value={option.id} checked={paymentMethod === option.id} onChange={() => { setPaymentMethod(option.id); track("add_payment_info",{value:total,quantity:cartCount,properties:{paymentMethod:option.id}}); }} /><i className="option-icon"><CheckoutOptionIcon name={option.id === "online" ? "card" : "wallet"} /></i><span><b>{option.title}</b><small>{option.note}</small></span></label>)}</div>{paymentMethod === "online" && deliveryFeePending && <p className="cdek-status">Оплата после подтверждения стоимости доставки менеджером.</p>}</fieldset>}
            {!paymentMethods.length && <div className="payment-note"><b>Не удалось загрузить способы оплаты</b><p>Обновите страницу или попробуйте ещё раз позже. Заказ без выбранного способа оплаты не отправится.</p></div>}
            <label className="consent-check"><input type="checkbox" name="consent" required /><span>Я даю согласие на обработку персональных данных в соответствии с <a href="/privacy" target="_blank">политикой</a> и принимаю условия <a href="/offer" target="_blank">оферты</a>.</span></label>
            <div className="checkout-navigation"><button type="button" onClick={() => setStep(2)}>← Назад</button><button className="primary-button" disabled={submitting || !paymentMethods.length || deliveryBlocked}>{submitting ? "Оформляем…" : "Продолжить →"}</button></div>
          </div>
        </form><aside className="checkout-order-summary"><h3>Ваш заказ</h3><dl><div><dt>Товаров</dt><dd>{cartCount}</dd></div><div><dt>Подытог</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{deliveryFee ? money(deliveryFee) : deliveryFeePending ? "уточняется" : "при оформлении"}</dd></div></dl><div><span>Итого</span><strong>{money(total)}</strong></div><img src="/assets/redesign/checkout-summary-art.png" alt="" /></aside></div>
      )}
    </aside>
  );
}
