import type { Dispatch, FormEventHandler, SetStateAction } from "react";
import { useRef, useState } from "react";
import { catalogSize } from "./lib/storefront-format";
import { normalizeRussianPhone, formatRussianPhoneInput } from "./lib/phone";
import { formatMoney } from "./lib/money";
import { track } from "./lib/analytics";

export type CartLine = {
  id: string;
  productCode?: string;
  name: string;
  price: number;
  image: string;
  stock?: number;
  potDiameterCm?: number;
  heightCm?: number;
  variantLabel?: string;
  available?: boolean;
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
  status?: "loading" | "ready" | "saving" | "error";
  error?: string;
  missingCount?: number;
  onRetry?: () => void;
};

const money = formatMoney;

function LineIcon({ name }: { name: "close" | "trash" }) {
  const paths = {
    close: <><path d="m7 7 10 10M17 7 7 17" /></>,
    trash: <><path d="M8 8h8l-.7 11H8.7L8 8Z" /><path d="M6.5 8h11M10 5h4l1 3M11 11v5M14 11v5" /></>,
  } as const;
  return <svg className="line-icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

type CheckoutIconName = "pickup" | "cdek" | "card" | "wallet";

function CheckoutOptionIcon({ name }: { name: CheckoutIconName }) {
  if (name === "cdek") return <span className="checkout-brand-icon cdek-icon" aria-hidden="true">CDEK</span>;
  const artwork = {
    pickup: <><path fill="#d5a35f" d="m9 28 15-7 14 7-15 8Z"/><path fill="#bd8146" d="m9 28 14 8v17L9 45Z"/><path fill="#e4bb7d" d="m23 36 15-8v17l-15 8Z"/><path fill="#f5e3c2" d="m18 24 14 8 4-2-14-8Z"/><path fill="#355b2d" d="M34 23c-2-10 3-16 11-18-1 8-4 13-11 18Z"/><path fill="#5e7b38" d="M34 24c1-9-4-14-11-16 0 8 4 13 11 16Z"/><path d="M34 13v18"/></>,
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
  status = "ready",
  error = "",
  missingCount = 0,
  onRetry,
}: CartDrawerProps) {
  const lineCards = lines.map((item) => (
    <div className={`cart-line ${item.available === false ? "unavailable" : ""}`} key={item.id}>
      <img src={item.image} alt="" />
      <div className="cart-line-copy">
        <h3><a href={`/product/${item.productCode || item.id}`}>{item.name}</a></h3>
        <small>{catalogSize(item.variantLabel || "", item.potDiameterCm)}</small>
        <span className={`cart-stock ${item.available === false || item.stock === 0 ? "out" : ""}`}>{item.available === false ? "Больше не продаётся" : item.stock === 0 ? "Под заказ" : "В наличии"}</span>
        <div className="cart-line-mobile-price">{money(item.price)}</div>
      </div>
      <strong className="cart-unit-price">{money(item.price)}</strong>
      <div className="quantity">
        <button type="button" onClick={() => onQuantityChange(item.id, item.quantity - 1)} aria-label={`Уменьшить количество ${item.name}`}>−</button>
        <span aria-live="polite">{item.quantity}</span>
        <button type="button" disabled={item.quantity >= Math.min(item.stock && item.stock > 0 ? item.stock : 20, 20)} onClick={() => onQuantityChange(item.id, item.quantity + 1)} aria-label={`Увеличить количество ${item.name}`}>+</button>
      </div>
      <strong className="cart-line-total">{money(item.price * item.quantity)}</strong>
      <button className="remove" onClick={() => onQuantityChange(item.id, 0)} aria-label={`Удалить ${item.name}`}><LineIcon name="trash" /></button>
    </div>
  ));
  return (
    <aside className={`drawer ${page ? "cart-page-panel" : ""} ${open ? "open" : ""}`} aria-hidden={!open} inert={!open}>
      <div className="drawer-head">
        <div><p className="eyebrow">Ваш выбор</p>{page ? <h1>Корзина</h1> : <h2>Корзина</h2>}</div>
        <button onClick={onClose} aria-label="Закрыть корзину"><LineIcon name="close" /></button>
      </div>
      <div className="cart-content">
        <section className="cart-table">
          {page && !!lines.length && <div className="cart-table-head"><span>Товар</span><span>Цена</span><span>Количество</span><span>Сумма</span><i /></div>}
          <div className="cart-lines">
            {status === "loading" && !lines.length && <div className="cart-state" role="status"><strong>Загружаем корзину…</strong><p>Ваши товары появятся здесь после загрузки.</p></div>}
            {status === "error" && <div className="cart-state error" role="alert"><strong>Не удалось загрузить корзину</strong><p>{error || "Проверьте соединение и попробуйте ещё раз."}</p>{onRetry && <button type="button" onClick={onRetry}>Повторить</button>}</div>}
            {(missingCount > 0 || lines.some((item) => item.available === false)) && <div className="cart-state warning" role="alert"><strong>Состав корзины изменился</strong><p>Некоторые товары больше недоступны. Удалите их перед оформлением.</p></div>}
            {lines.some((item) => item.stock !== undefined && item.stock < item.quantity) && <div className="cart-state warning"><strong>Есть растения под заказ</strong><p>Сейчас платить не нужно. Менеджер проверит поставку, уточнит срок и подтвердит итог заказа до оплаты.</p></div>}
            {lineCards}
            {status === "ready" && !lines.length && !missingCount && (
              <div className="empty-cart">
                <span>⌁</span><h3>Корзина пока пуста</h3>
                <p>Добавьте растения из каталога — они появятся здесь.</p>
                <button onClick={onClose}>Перейти в каталог</button>
              </div>
            )}
          </div>
          {page && !!lines.length && <div className="cart-page-actions"><a href="/#catalog">← Продолжить покупки</a></div>}
        </section>
        {!!lines.length && (
          <aside className="cart-summary">
            <dl><div><dt>Итого товаров</dt><dd>{lines.reduce((sum, item) => sum + item.quantity, 0)}</dd></div><div><dt>Растения</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>рассчитаем при оформлении</dd></div></dl>
            <div className="cart-summary-total"><span>Растения</span><strong>{money(subtotal)}</strong></div>
            <p>Стоимость доставки войдёт в итог заказа.</p>
            {!page && <div className="cart-bonus" hidden />}
            {<button className="primary-button" disabled={status !== "ready" || missingCount > 0 || lines.some((item) => item.available === false)} onClick={onCheckout}>Оформить заказ <span>→</span></button>}

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

type CheckoutPanelProps = {
  cartLines: CartLine[];
  step: 1 | 2 | 3;
  setStep: Dispatch<SetStateAction<1 | 2 | 3>>;
  deliveryConfirmed: boolean;
  setDeliveryConfirmed: Dispatch<SetStateAction<boolean>>;
  orderError: string;
  retryCdek: () => void;
  retryPaymentMethods: () => void;
  page?: boolean;
  checkoutOpen: boolean;
  setCheckoutOpen: (open: boolean) => void;
  orderNumber: string;
  orderConfirmationPending: boolean;
  submitOrder: FormEventHandler<HTMLFormElement>;
  user: boolean;
  pricingReady: boolean;
  pricingError: boolean;
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
  hasPreorder: boolean;
  cdekQuotes: CdekQuote[];
  paymentMethods: PaymentMethod[];
  paymentMethod: string;
  setPaymentMethod: Dispatch<SetStateAction<string>>;
  subtotal: number;
  originalSubtotal: number;
  discountAmount: number;
  discountBps: number;
  deliveryFee: number;
  total: number;
  submitting: boolean;
  deliveryFeePending: boolean;
};

export function CheckoutPanel(props: CheckoutPanelProps) {
  const {
    cartLines, step, setStep, deliveryConfirmed, setDeliveryConfirmed, orderError, retryPaymentMethods, retryCdek,
    page = false, checkoutOpen, setCheckoutOpen, orderNumber, orderConfirmationPending, submitOrder, user, pricingReady, pricingError,
    checkoutProfile, setCheckoutProfile, availableDelivery, delivery, setDelivery,
    cdekQuote, cdekCityQuery, setCdekCityQuery, setCdekCity, setCdekCities,
    setCdekOffices, cdekOfficeCode, setCdekOfficeCode, cdekOfficeQuery,
    setCdekOfficeQuery, setCdekQuotes, setCdekTariffCode, cdekCities, chooseCdekCity,
    cdekLoading, cdekError, cdekOffices, setCdekOfficeListOpen, cdekOfficeListOpen,
    cdekOfficeMatches, selectedOffice, cdekFeePending, cdekRepack, setCdekRepack,
    cartCount, hasPreorder, cdekQuotes, paymentMethods, paymentMethod, setPaymentMethod, subtotal,
    deliveryFee, total, submitting, deliveryFeePending, originalSubtotal, discountAmount, discountBps,
  } = props;
  const [contactErrors, setContactErrors] = useState<Record<string, string>>({});
  const formRef = useRef<HTMLFormElement>(null);
  const go = (next: 1 | 2 | 3) => {
    setStep(next);
    window.requestAnimationFrame(() => {
      const section = formRef.current?.querySelector<HTMLElement>(`[data-checkout-step="${next}"]`);
      if (!section || section.hidden) return;
      // Autofill or a fast tap may already have put the buyer in a field.
      if (document.activeElement instanceof HTMLInputElement && section.contains(document.activeElement)) return;
      section.querySelector<HTMLElement>("legend, h3")?.focus();
      window.scrollTo({ top: 0, behavior: "instant" });
    });
  };
  const validateContact = () => {
    const phone = formRef.current?.elements.namedItem("phone") as HTMLInputElement | null;
    if (phone) phone.setCustomValidity(phone.value && !normalizeRussianPhone(phone.value) ? "Введите полный российский номер, например +7 915 123-45-67" : "");
    const fields = Array.from(formRef.current?.querySelectorAll<HTMLInputElement>('[data-checkout-step="1"] input') || []);
    const invalid = fields.filter(field => !field.checkValidity());
    setContactErrors(Object.fromEntries(invalid.map(field => [field.name, field.validationMessage])));
    if (invalid.length) { setStep(1); window.requestAnimationFrame(() => invalid[0].focus()); return false; }
    return true;
  };
  const advance = (next: 1|2|3) => {
    if (step === 1 && !validateContact()) return;
    if (step === 2) {
      if (delivery === "cdek" && (!cdekOfficeCode || cdekLoading)) return;
      setDeliveryConfirmed(true);
    }
    track("checkout_step", { value: total, quantity: cartCount, properties: { from: step, to: next } });
    go(next);
  };
  const deliveryIcon = (id: string): CheckoutIconName => id === "cdek" ? "cdek" : "pickup";
  const selectedDelivery = availableDelivery.find((item) => item.id === delivery);
  const selectedPayment = paymentMethods.find((item) => item.id === paymentMethod);
  const deliveryBlocked = delivery === "cdek" && (!cdekOfficeCode || cdekLoading);
  const deliveryNotChosen = !deliveryConfirmed;
  const deliveryUnknown = deliveryNotChosen || deliveryFeePending;
  const managerConfirmation = hasPreorder || deliveryFeePending || paymentMethod === "manager_confirmation";
  const confirmationPending = Boolean(orderNumber && orderConfirmationPending);

  return (
    <aside className={`checkout ${page ? "checkout-page-panel" : ""} ${orderNumber ? "checkout-order-complete" : ""} ${checkoutOpen ? "open" : ""}`} aria-hidden={!checkoutOpen} inert={!checkoutOpen}>
      <div className="drawer-head"><div><h2>{confirmationPending ? "Заказ ждёт подтверждения" : orderNumber ? "Заказ принят" : "Оформление заказа"}</h2></div>{page ? <a href="/cart" aria-label="Вернуться в корзину">←</a> : <button onClick={() => setCheckoutOpen(false)} aria-label="Закрыть оформление">×</button>}</div>
      {orderNumber ? (
        <div className="success">
          <div className="success-copy">
            <span className="success-heart" aria-hidden="true">♡</span>
            <h2>{confirmationPending ? "Заказ отправлен менеджеру" : "Заказ принят"}</h2>
            <p className="success-lead">{confirmationPending ? "Спасибо! Менеджер уточнит наличие растений и свяжется с вами. Оплата пока не требуется." : "Спасибо! Мы приняли ваш заказ."}</p>
            <div className="success-columns">
              <div className="success-order-info">
                <div className="success-number"><small>Номер заказа</small><strong>#{orderNumber}</strong></div>
                <p><i aria-hidden="true">⌖</i>{selectedDelivery?.title || "Способ получения выбран"}</p>
                <p><i aria-hidden="true">▱</i>{confirmationPending ? "Оплата после проверки менеджером" : (selectedPayment?.title || "Способ оплаты выбран")}</p>
                <a className="primary-button" href={`/account/orders/${encodeURIComponent(orderNumber)}`}>Следить за заказом <span aria-hidden="true">→</span></a>
                <a className="success-catalog-link" href="/#catalog">Вернуться в каталог <span aria-hidden="true">↗</span></a>
              </div>
              <div className="success-next"><h3>Что будет дальше</h3>{confirmationPending ? <ol>
                <li><b>1</b><span>Проверим наличие растений</span></li>
                <li><b>2</b><span>Менеджер свяжется с вами после поступления</span></li>
                <li><b>3</b><span>После решения менеджера вы сможете оплатить растения и доставку</span></li>
              </ol> : <ol>
                <li><b>1</b><span>Соберём и проверим растения</span></li>
                <li><b>2</b><span>Аккуратно упакуем</span></li>
                <li><b>3</b><span>{delivery === "pickup" ? "Сообщим, когда заказ будет готов" : "Передадим заказ в доставку"}</span></li>
              </ol>}</div>
            </div>
          </div>
        </div>
      ) : (
        <div className="checkout-layout"><form ref={formRef} noValidate onSubmit={event => {
          event.preventDefault();
          if (!validateContact()) return;
          if (deliveryBlocked) { go(2); return; }
          const consent = event.currentTarget.elements.namedItem("consent") as HTMLInputElement;
          if (!consent.checked) { consent.reportValidity(); return; }
          submitOrder(event);
        }}>
          <nav className="checkout-steps" aria-label="Этапы оформления">{([[2,"Получение"],[1,"Контакты"],[3,"Проверка"]] as const).map(([number,label], index)=><span className={step===number?"active":""} key={number}><button type="button" aria-current={step===number ? "step" : undefined} onClick={() => { if (number === 2) go(2); else if (number === 1 && !deliveryBlocked) { setDeliveryConfirmed(true); go(1); } else if (number === 3 && validateContact() && !deliveryBlocked) { setDeliveryConfirmed(true); go(3); } }}><b>{index+1}</b><small>{label}</small></button></span>)}</nav>
          <fieldset data-checkout-step="1" hidden={step!==1}>
            <legend tabIndex={-1}>Контактные данные</legend>
            {user && <p className="profile-prefill">Данные заполнены из личного кабинета</p>}
            <div className="checkout-contact-fields">
              <label className="checkout-contact-name">Имя<input name="name" aria-label="Имя" required aria-invalid={!!contactErrors.name} aria-describedby={contactErrors.name ? "name-error" : undefined} placeholder="Александр" autoComplete="name" value={checkoutProfile.name} onChange={(event) => { setContactErrors((current) => ({ ...current, name: "" })); setCheckoutProfile((current) => ({ ...current, name: event.target.value })); }} />{contactErrors.name && <small className="field-error" id="name-error">{contactErrors.name}</small>}</label>
              <div className="checkout-contact-row"><label>Телефон<input name="phone" aria-label="Телефон" required inputMode="tel" autoComplete="tel" maxLength={18} aria-invalid={!!contactErrors.phone} aria-describedby={contactErrors.phone ? "phone-error" : undefined} placeholder="+7 900 000-00-00" value={checkoutProfile.phone} onChange={(event) => { event.currentTarget.setCustomValidity(""); setContactErrors((current) => ({ ...current, phone: "" })); const value = formatRussianPhoneInput(event.currentTarget.value); setCheckoutProfile((current) => ({ ...current, phone: value })); }} />{contactErrors.phone && <small className="field-error" id="phone-error">{contactErrors.phone}</small>}</label><label>Email для чека<input name="email" aria-label="Email для чека" required type="email" autoComplete="email" aria-invalid={!!contactErrors.email} aria-describedby={contactErrors.email ? "email-error" : undefined} placeholder="mail@example.ru" value={checkoutProfile.email} onChange={(event) => { setContactErrors((current) => ({ ...current, email: "" })); setCheckoutProfile((current) => ({ ...current, email: event.target.value })); }} />{contactErrors.email && <small className="field-error" id="email-error">{contactErrors.email}</small>}</label></div>
            </div>
            <div className="checkout-navigation"><button type="button" onClick={() => go(2)}>← Получение</button><button type="button" className="primary-button" onClick={() => advance(3)}>Проверить заказ <span>→</span></button></div>
          </fieldset>

          <fieldset data-checkout-step="2" hidden={step!==2}>
            <legend tabIndex={-1}>Способ доставки</legend>
            <div className="delivery-options">
              {availableDelivery.map((item) => {
                return <label className={delivery === item.id ? "selected" : ""} key={item.id}>
                  <input type="radio" name="delivery" value={item.id} checked={delivery === item.id} onChange={() => { setDeliveryConfirmed(true); setDelivery(item.id); track("add_shipping_info",{value:total,quantity:cartCount,properties:{delivery:item.id}}); }} />
                  <i className="option-icon"><CheckoutOptionIcon name={deliveryIcon(item.id)} /></i><span><b>{item.title}</b><small>{item.detail}</small></span>
                  {item.id === "cdek" && <strong>{delivery === "cdek" && cdekQuote ? money(cdekQuote.price) : "Рассчитать"}</strong>}
                </label>;
              })}
            </div>

            {delivery === "cdek" ? (
              <div className="cdek-picker">
                <label>Город получения<input value={cdekCityQuery} onChange={(event) => { setCdekCityQuery(event.target.value); setCdekCity(null); setCdekCities([]); setCdekOffices([]); setCdekOfficeCode(""); setCdekOfficeQuery(""); setCdekQuotes([]); setCdekTariffCode(0); }} autoComplete="off" placeholder="Начните вводить город" /></label>
                {!!cdekCities.length && <div className="cdek-suggestions" role="group" aria-label="Найденные города">{cdekCities.map((city) => <button type="button" key={city.code} onClick={() => chooseCdekCity(city)}><b>{city.city}</b><span>{city.region || "Россия"}</span></button>)}</div>}
                {cdekLoading && <p className="cdek-status">Получаем данные СДЭК…</p>}
                {cdekError && <div className="cdek-status error" role="alert"><p>{cdekError}</p><button type="button" onClick={retryCdek}>Повторить загрузку СДЭК</button></div>}
                {!!cdekOffices.length && <label>Пункт выдачи<input value={cdekOfficeQuery} onChange={(event) => { setCdekOfficeQuery(event.target.value); setCdekOfficeCode(""); setCdekOfficeListOpen(true); }} onFocus={() => setCdekOfficeListOpen(true)} autoComplete="off" placeholder="Найдите пункт по улице или адресу" /></label>}
                {cdekOfficeListOpen && !!cdekOfficeMatches.length && !cdekOfficeCode && <div className="cdek-suggestions" role="group" aria-label="Пункты выдачи">{cdekOfficeMatches.map((office) => <button type="button" key={office.code} onClick={() => { setCdekOfficeCode(office.code); setCdekOfficeQuery(office.location.address); setCdekOfficeListOpen(false); }}><b>{office.location.address}</b><span>{office.work_time || office.name}</span></button>)}</div>}
                {!!cdekOffices.length && !cdekOfficeMatches.length && <p className="cdek-status">Ничего не нашлось — попробуйте другую улицу</p>}
                {selectedOffice && <p className="cdek-status">Пункт выбран: {selectedOffice.location.address}</p>}
                {cdekFeePending && !!cdekOffices.length && <div className="cdek-quote pending"><b>Рассчитает менеджер</b><span>после оформления</span><small>{cdekRepack ? "Менеджер проверит, поместятся ли растения в одну коробку, посчитает доставку и свяжется с вами до оплаты." : "Стоимость доставки менеджер посчитает и сообщит вам до оплаты. Оформить заказ можно уже сейчас."}</small></div>}
                {cartCount > 1 && !!cdekQuotes.length && <div className="cdek-repack"><label><input type="checkbox" checked={cdekRepack} onChange={(event) => setCdekRepack(event.target.checked)} />Упаковать в одну коробку, если поместятся</label><small>Сейчас доставка посчитана по отдельной коробке на каждое растение. Менеджер проверит, поместятся ли они вместе, и пересчитает — обычно выходит дешевле.</small></div>}
                {!cdekRepack && cdekQuotes.length > 1 && <div className="cdek-tariffs" role="radiogroup" aria-label="Тарифы СДЭК">{cdekQuotes.map((option) => <label key={option.tariffCode} className="cdek-tariff"><input type="radio" name="cdek-tariff" checked={option.tariffCode === cdekQuote?.tariffCode} onChange={() => setCdekTariffCode(option.tariffCode)} /><span><b>{option === cdekQuotes[0] ? "До пункта выдачи" : "Быстрее до пункта выдачи"}</b><small>{option.daysMin === option.daysMax ? `${option.daysMin} дн.` : `${option.daysMin}–${option.daysMax} дн.`}</small></span><strong>{money(option.price)}</strong></label>)}</div>}
                {!cdekRepack && cdekQuote && cdekQuotes.length === 1 && <div className="cdek-quote"><b>{money(cdekQuote.price)}</b><span>{cdekQuote.daysMin === cdekQuote.daysMax ? `${cdekQuote.daysMin} дн.` : `${cdekQuote.daysMin}–${cdekQuote.daysMax} дн.`}</span><small>Расчёт по габаритам упаковки выбранных растений</small></div>}
              </div>
            ) : (
              <div className="pickup-address"><strong>Рязань, ул. Новосёлов, 40А</strong><p>Самовывоз бесплатный. Сообщим, когда заказ будет готов к выдаче.</p></div>
            )}
            <div className="checkout-navigation"><a href="/cart">← Корзина</a><button type="button" className="primary-button" disabled={deliveryBlocked} onClick={() => advance(1)}>К контактам →</button></div>
          </fieldset>

          <div data-checkout-step="3" hidden={step!==3}>
            <section className="checkout-review" aria-label="Проверка данных"><h3 tabIndex={-1}>Проверьте заказ</h3><div><p><strong>Получение</strong><br/>{delivery === "cdek" ? `${cdekCityQuery}, ${selectedOffice?.location.address || "Проверяем выбранный пункт…"}` : "Самовывоз: Рязань, Новосёлов, 40А"}</p><button type="button" onClick={() => go(2)}>Изменить получение</button></div><div><p><strong>Контакты</strong><br/>{checkoutProfile.name}<br/>{checkoutProfile.phone}<br/>{checkoutProfile.email}</p><button type="button" onClick={() => go(1)}>Изменить контакты</button></div></section>
            {managerConfirmation ? <div className="payment-note"><b>Оформить без оплаты</b><p>{hasPreorder ? "В заказе есть растения под заказ. Менеджер уточнит поставку и согласует с вами срок и получение всего заказа или доступной части. Срок поставки пока не подтверждён." : deliveryFeePending ? "Стоимость доставки требует проверки. Менеджер сообщит окончательную сумму до оплаты." : "Менеджер проверит заказ и сообщит, как его оплатить."} Сейчас платить не нужно.</p></div> : paymentMethods.length > 0 ? <fieldset><legend>Способ оплаты заказа</legend><div className="delivery-options">{paymentMethods.map((option) => <label key={option.id} className={paymentMethod === option.id ? "active" : ""}><input type="radio" name="paymentMethod" value={option.id} checked={paymentMethod === option.id} onChange={() => { setPaymentMethod(option.id); track("add_payment_info",{value:total,quantity:cartCount,properties:{paymentMethod:option.id}}); }} /><i className="option-icon"><CheckoutOptionIcon name={option.id === "online" ? "card" : "wallet"} /></i><span><b>{option.title}</b><small>{option.note}</small></span></label>)}</div><p className="cdek-status">{paymentMethod === "online" ? "После оформления откроется защищённая страница оплаты. Стоимость растений и выбранной доставки входит в сумму заказа." : "Оплатите заказ в магазине, когда будете забирать растения. Сейчас платить не нужно."}</p></fieldset> : <div className="payment-note"><b>Не удалось загрузить способы оплаты</b><p>Проверьте соединение и повторите загрузку. Введённые данные сохранятся.</p><button type="button" onClick={retryPaymentMethods}>Загрузить способы оплаты</button></div>}
            {orderError && <p className="checkout-submit-error" role="alert">{orderError} Данные сохранены — можно попробовать ещё раз.</p>}
            <label className="consent-check"><input type="checkbox" name="consent" required /><span>Я даю согласие на обработку персональных данных в соответствии с <a href="/privacy" target="_blank">политикой</a> и принимаю условия <a href="/offer" target="_blank">оферты</a>.</span></label>
            <div className="checkout-navigation"><button type="button" onClick={() => go(1)}>← Контакты</button><button className="primary-button" disabled={submitting || !pricingReady || pricingError || (!managerConfirmation && !paymentMethods.length) || deliveryBlocked}>{submitting ? "Оформляем…" : managerConfirmation ? "Отправить заказ →" : paymentMethod === "online" ? `Перейти к оплате · ${money(total)}` : "Оформить самовывоз →"}</button></div>
          </div>
        </form><aside className="checkout-order-summary" aria-label="Состав и сумма заказа">
          <details className="checkout-items" open={window.matchMedia("(min-width: 900px)").matches || undefined}><summary><span>Ваш заказ · {cartCount} шт.</span><strong>{pricingReady ? money(subtotal) : "Проверяем цену…"}</strong></summary><ul>{cartLines.map(line => <li key={line.id}><img src={line.image} alt=""/><div><a href={`/product/${line.productCode || line.id}`}>{line.name}</a><small>{catalogSize(line.variantLabel || "", line.potDiameterCm)} · {line.quantity} шт.</small><strong>{money(line.price * line.quantity)}</strong></div></li>)}</ul><a href="/cart">Изменить состав</a></details>
          <dl>{discountAmount > 0 && <><div><dt>Растения без скидки</dt><dd>{money(originalSubtotal)}</dd></div><div><dt>Персональная скидка {discountBps / 100}%</dt><dd>−{money(discountAmount)}</dd></div></>}<div><dt>Растения</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{deliveryNotChosen ? "Выберите способ получения" : deliveryFeePending ? "Уточнит менеджер" : deliveryFee === 0 ? "Бесплатно" : money(deliveryFee)}</dd></div></dl>
          <div className="checkout-total"><span>{hasPreorder ? "Предварительная сумма" : deliveryUnknown ? "Растения, без доставки" : paymentMethod === "on_delivery" ? "К оплате при получении" : "Итого к оплате"}</span><strong>{pricingReady ? money(deliveryNotChosen ? subtotal : total) : "Проверяем цену…"}</strong></div>
          {pricingError && <p role="alert">Не удалось проверить персональную скидку. Обновите страницу — черновик оформления сохранён.</p>}{hasPreorder ? <p>Сейчас платить не нужно. Срок поставки и итог подтвердит менеджер.</p> : deliveryFeePending && <p>Менеджер уточнит доставку и сообщит итог до оплаты.</p>}
        </aside></div>
      )}
    </aside>
  );
}
