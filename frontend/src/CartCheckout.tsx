import type { Dispatch, FormEventHandler, SetStateAction } from "react";
import { useState } from "react";
import { OrderLamp } from "./OrderLamp";
import { DeliveryArt } from "./DeliveryArt";
import { formatRussianPhoneInput, normalizeRussianPhone } from "./lib/phone";
import { formatMoney } from "./lib/money";
import { track } from "./lib/analytics";
import { ProductImage } from "./ProductImage";

export type CartLine = {
  id: string;
  name: string;
  price: number;
  image: string;
  stock?: number;
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

function CartSignature() {
  return <div className="cart-signature" aria-hidden="true">
    <svg className="cart-brand-stamp" viewBox="0 0 132 132" fill="none">
      <defs><path id="cart-stamp-top" d="M25 64 A41 41 0 0 1 107 64" /><path id="cart-stamp-bottom" d="M25 74 A41 41 0 0 0 107 74" /></defs>
      <circle cx="66" cy="66" r="58" /><circle cx="66" cy="66" r="52" strokeDasharray="2 4" />
      <text><textPath href="#cart-stamp-top" startOffset="50%" textAnchor="middle">ЖИВЫЕ РАСТЕНИЯ</textPath></text>
      <text><textPath href="#cart-stamp-bottom" startOffset="50%" textAnchor="middle">ДЛЯ ЖИВЫХ ЛЮДЕЙ</textPath></text>
      <image href="/assets/brand/graphite-v1/mark-graphite.svg" x="39" y="39" width="54" height="54" />
    </svg>
  </div>;
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
  const hasPreorder = lines.some((item) => item.stock !== undefined && item.stock < item.quantity);
  const cartQuantity = lines.reduce((sum, item) => sum + item.quantity, 0);
  const lineCards = lines.map((item) => (
    <div className={`cart-line ${item.available === false ? "unavailable" : ""}`} key={item.id}>
      <ProductImage src={item.image} alt="" />
      <div className="cart-line-copy">
        <h3>{item.name}</h3>
        <small>Вариант: {item.variantLabel || item.id}</small>
        <span className={`cart-stock ${item.available === false || item.stock === 0 ? "out" : ""}`}>{item.available === false ? "Больше не продаётся" : item.stock === 0 ? "Под заказ" : "В наличии"}</span>
        {!page && <div className="cart-line-mobile-price">{money(item.price)}</div>}
      </div>
      {page && <div className="cart-line-mobile-price">{money(item.price * item.quantity)}{item.quantity > 1 && <small>{money(item.price)} / шт.</small>}</div>}
      <strong className="cart-unit-price">{money(item.price)}</strong>
      <div className="quantity">
        <button type="button" onClick={() => onQuantityChange(item.id, item.quantity - 1)} aria-label={`Уменьшить количество ${item.name}`}>−</button>
        <span aria-live="polite" title={item.quantity >= 20 ? "Не более 20 шт. в одной позиции" : undefined}>{item.quantity}</span>
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
        {page && <CartSignature />}
        <button onClick={onClose} aria-label="Закрыть корзину"><LineIcon name="close" /></button>
      </div>
      <div className="cart-content">
        <section className="cart-table">
          {page && !!lines.length && <div className="cart-table-head"><span>Товар</span><span>Цена</span><span>Количество</span><span>Сумма</span><i /></div>}
          <div className="cart-lines">
            {status === "loading" && !lines.length && <div className="cart-state" role="status"><strong>Загружаем корзину…</strong><p>Ваши товары появятся здесь после загрузки.</p></div>}
            {status === "error" && <div className="cart-state error" role="alert"><strong>Не удалось загрузить корзину</strong><p>{error || "Проверьте соединение и попробуйте ещё раз."}</p>{onRetry && <button type="button" onClick={onRetry}>Повторить</button>}</div>}
            {(missingCount > 0 || lines.some((item) => item.available === false)) && <div className="cart-state warning" role="alert"><strong>Состав корзины изменился</strong><p>Некоторые товары больше недоступны. Удалите их перед оформлением.</p></div>}
            {!page && hasPreorder && <div className="cart-state warning"><strong>Есть растения под заказ</strong><p>Сейчас платить не нужно. Менеджер проверит поставку, уточнит срок и подтвердит итог заказа до оплаты.</p></div>}
            {lineCards}
            {status === "ready" && !lines.length && !missingCount && (
              <div className="empty-cart">
                <span>⌁</span><h3>Корзина пока пуста</h3>
                <p>Добавьте растения из каталога — они появятся здесь.</p>
                <button onClick={onClose}>Перейти в каталог</button>
              </div>
            )}
          </div>
        </section>
        {!!lines.length && (
          <aside className={`cart-summary ${page ? "cart-summary-lamp" : ""}`} aria-label="Итог корзины">
            {page ? <>
              <h2>Ваш заказ</h2>
              <OrderLamp label="Стоимость товаров" amount={money(subtotal)}>
                <dl>
                  <div><dt>Стоимость товаров</dt><dd>{money(subtotal)}</dd></div>
                  <div><dt>Количество</dt><dd>{cartQuantity}</dd></div>
                  <div><dt>Доставка</dt><dd>Рассчитаем при оформлении</dd></div>
                </dl>
                <p>Самовывоз из магазина — бесплатно.</p>
                {hasPreorder && <div className="cart-preorder-note" role="note"><strong>Есть растения под заказ</strong><p>Сейчас платить не нужно. Менеджер проверит поставку, уточнит срок и подтвердит итог заказа до оплаты.</p></div>}
              </OrderLamp>
            </> : <>
              <dl><div><dt>Товаров</dt><dd>{cartQuantity}</dd></div><div><dt>Растения</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>рассчитаем при оформлении</dd></div></dl>
              <div className="cart-summary-total"><span>Сумма товаров</span><strong>{money(subtotal)}</strong></div>
              <p>Стоимость доставки войдёт в итог заказа.</p>
              <div className="cart-bonus" hidden />
              <button className="primary-button" disabled={status !== "ready" || missingCount > 0 || lines.some((item) => item.available === false)} onClick={onCheckout}>Оформить заказ <span>→</span></button>
            </>}
          </aside>
        )}
        {page && !!lines.length && <div className="cart-page-actions"><a href="/#catalog">←&nbsp;&nbsp; Продолжить покупки</a><button type="button" className="primary-button" disabled={status !== "ready" || missingCount > 0 || lines.some((item) => item.available === false)} onClick={onCheckout}>Оформить заказ <span aria-hidden="true">→</span></button></div>}
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
  page?: boolean;
  checkoutOpen: boolean;
  setCheckoutOpen: (open: boolean) => void;
  orderNumber: string;
  orderConfirmationPending: boolean;
  orderConfirmationReason?: "preorder" | "delivery" | "manager" | null;
  feedbackMessage?: string;
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
  cdekCity: CdekCity | null;
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
  cartLines: CartLine[];
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
    page = false, checkoutOpen, setCheckoutOpen, orderNumber, orderConfirmationPending, orderConfirmationReason, feedbackMessage, submitOrder, user, pricingReady, pricingError,
    checkoutProfile, setCheckoutProfile, availableDelivery, delivery, setDelivery,
    cdekQuote, cdekCity, cdekCityQuery, setCdekCityQuery, setCdekCity, setCdekCities,
    setCdekOffices, cdekOfficeCode, setCdekOfficeCode, cdekOfficeQuery,
    setCdekOfficeQuery, setCdekQuotes, setCdekTariffCode, cdekCities, chooseCdekCity,
    cdekLoading, cdekError, cdekOffices, setCdekOfficeListOpen, cdekOfficeListOpen,
    cdekOfficeMatches, selectedOffice, cdekFeePending, cdekRepack, setCdekRepack,
    cartCount, cartLines, hasPreorder, cdekQuotes, paymentMethods, paymentMethod, setPaymentMethod, subtotal,
    deliveryFee, total, submitting, deliveryFeePending, originalSubtotal, discountAmount, discountBps,
  } = props;
  const [deliveryChosen, setDeliveryChosen] = useState(false);
  const [deliveryAnimationId, setDeliveryAnimationId] = useState(0);
  const [deliveryError, setDeliveryError] = useState("");
  const [contactErrors, setContactErrors] = useState<Record<string, string>>({});
  const [consentGiven, setConsentGiven] = useState(false);
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [submitted, setSubmitted] = useState(false);
  const [deliveryFocus, setDeliveryFocus] = useState("");
  const handleSubmit: FormEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    setSubmitted(true);
    const form = event.currentTarget;
    const fields = Array.from(form.querySelectorAll<HTMLInputElement>("input,textarea,select"));
    const phoneField = fields.find((field) => field.name === "phone");
    if (phoneField) phoneField.setCustomValidity(normalizeRussianPhone(phoneField.value) ? "" : "Укажите номер телефона");
    const errors = Object.fromEntries(fields.filter((field) => !field.checkValidity()).map((field) => [field.name, field.name === "consent" ? "Подтвердите согласие перед оформлением" : field.validity.valueMissing ? "Заполните это поле" : field.validity.typeMismatch && field.type === "email" ? "Проверьте адрес электронной почты" : field.validationMessage]));
    setContactErrors(errors);
    setDeliveryError(!deliveryChosen ? "Выберите способ получения" : deliveryBlocked ? "Выберите город и пункт выдачи СДЭК" : "");
    const invalidContact = fields.find((field) => ["name", "phone", "email"].includes(field.name) && !field.checkValidity());
    if (!checkoutProfile.name.trim()) { form.querySelector<HTMLInputElement>('input[name="name"]')?.focus(); return; }
    if (invalidContact) { invalidContact.focus(); return; }
    if (!deliveryChosen) { form.querySelector<HTMLInputElement>('input[name="delivery"]')?.focus(); return; }
    if (deliveryBlocked) { form.querySelector<HTMLInputElement>(cdekCity ? 'input[name="cdekOffice"]' : 'input[name="cdekCity"]')?.focus(); return; }
    if (delivery === "cdek" && cdekLoading) return;
    const invalid = fields.find((field) => !field.checkValidity());
    if (invalid) { invalid.focus(); return; }
    if (submitHint) return;
    submitOrder(event);
  };
  const selectedDelivery = availableDelivery.find((item) => item.id === delivery);
  const selectedPayment = paymentMethods.find((item) => item.id === paymentMethod);
  const deliveryBlocked = delivery === "cdek" && (!cdekCity || !selectedOffice);
  const deliveryNotChosen = !deliveryChosen;
  const deliveryCalculating = deliveryChosen && delivery === "cdek" && cdekLoading;
  const deliveryNeedsManager = deliveryChosen && !deliveryBlocked && !deliveryCalculating && deliveryFeePending;
  const deliveryUnknown = deliveryNotChosen || deliveryBlocked || deliveryCalculating || deliveryFeePending;
  const deliveryStatus = deliveryNotChosen ? "Выберите способ получения" : deliveryBlocked ? cdekCity ? "Выберите пункт выдачи" : "Выберите город" : deliveryCalculating ? "Рассчитываем…" : deliveryNeedsManager ? "Уточнит менеджер" : deliveryFee === 0 ? "Бесплатно" : money(deliveryFee);
  const submitHint = !checkoutProfile.name.trim() ? "Укажите имя" : !normalizeRussianPhone(checkoutProfile.phone) ? "Укажите номер телефона" : !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(checkoutProfile.email.trim()) ? "Укажите корректный email для чека" : deliveryNotChosen ? "Выберите способ получения" : deliveryBlocked ? cdekCity ? "Выберите пункт выдачи СДЭК" : "Выберите город из списка" : deliveryCalculating ? "Дождитесь расчёта доставки" : !consentGiven ? "Подтвердите согласие перед оформлением" : !pricingReady ? "Проверяем стоимость заказа…" : pricingError ? "Не удалось проверить цену. Обновите страницу" : !hasPreorder && !paymentMethods.length ? "Способы оплаты ещё не загружены" : "";
  const emailError = contactErrors.email || ((touched.email || submitted) && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(checkoutProfile.email.trim()) ? "Укажите корректный email для чека" : "");
  const phoneError = contactErrors.phone || ((touched.phone || submitted) && !normalizeRussianPhone(checkoutProfile.phone) ? "Укажите номер телефона" : "");
  const nameError = contactErrors.name || ((touched.name || submitted) && !checkoutProfile.name.trim() ? "Укажите имя" : "");
  const cityError = deliveryChosen && delivery === "cdek" && !cdekCity && deliveryFocus !== "cdekCity" && !cdekCities.length && !cdekLoading && (touched.cdekCity || submitted) ? "Выберите город из списка" : "";
  const officeError = deliveryChosen && delivery === "cdek" && cdekCity && !selectedOffice && deliveryFocus !== "cdekOffice" && !cdekOfficeListOpen && (touched.cdekOffice || submitted) ? "Выберите пункт выдачи СДЭК" : "";
  const deliveryChoiceError = !deliveryChosen && submitted ? deliveryError : "";
  // Readiness is neutral guidance; an untouched field is never an error.
  const footerHint = submitHint;
  const confirmationPending = Boolean(orderNumber && orderConfirmationPending);
  const cashOnPickup = delivery === "pickup" && paymentMethod === "on_delivery";
  const confirmationLead = orderConfirmationReason === "preorder"
    ? "Спасибо! Менеджер уточнит наличие растений и свяжется с вами. Оплата пока не требуется."
    : orderConfirmationReason === "delivery"
      ? "Спасибо! Менеджер уточнит стоимость доставки и свяжется с вами. Оплата пока не требуется."
      : "Спасибо! Менеджер проверит детали заказа и свяжется с вами. Оплата пока не требуется.";

  return (
    <aside className={`checkout ${page ? "checkout-page-panel" : ""} ${orderNumber ? "checkout-order-complete" : ""} ${checkoutOpen ? "open" : ""}`} aria-hidden={!checkoutOpen} inert={!checkoutOpen}>
      <div className="drawer-head"><div>{page ? <h1>{confirmationPending ? "Заказ ждёт подтверждения" : orderNumber ? "Заказ принят" : "Оформление заказа"}</h1> : <h2>{confirmationPending ? "Заказ ждёт подтверждения" : orderNumber ? "Заказ принят" : "Оформление заказа"}</h2>}</div>{!page && <button onClick={() => setCheckoutOpen(false)} aria-label="Закрыть оформление">×</button>}</div>
      {orderNumber ? (
        <div className="success">
          <div className="success-copy">
            <span className="success-heart" aria-hidden="true">♡</span>
            <h2 aria-level={page ? 1 : 2}>{confirmationPending ? "Заказ отправлен менеджеру" : "Заказ принят"}</h2>
            <p className="success-lead">{confirmationPending ? confirmationLead : "Спасибо! Мы приняли ваш заказ."}</p>
            {feedbackMessage && <p className="success-lead" role="status">{feedbackMessage}</p>}
            <div className="success-columns">
              <div className="success-order-info">
                <div className="success-number"><small>Номер заказа</small><strong>#{orderNumber}</strong></div>
                <p><i aria-hidden="true">⌖</i>{selectedDelivery?.title || "Способ получения выбран"}</p>
                <p><i aria-hidden="true">▱</i>{cashOnPickup ? "Наличными при получении" : confirmationPending ? "Оплата после проверки менеджером" : (selectedPayment?.title || "Способ оплаты выбран")}</p>
                <a className="primary-button" href={`/account/orders/${encodeURIComponent(orderNumber)}`}>Следить за заказом <span aria-hidden="true">→</span></a>
                <a className="success-catalog-link" href="/#catalog">Вернуться в каталог <span aria-hidden="true">↗</span></a>
              </div>
              <div className="success-next"><h3>Что будет дальше</h3>{confirmationPending ? <ol>
                <li><b>1</b><span>{orderConfirmationReason === "preorder" ? "Проверим наличие растений" : orderConfirmationReason === "delivery" ? "Уточним стоимость доставки" : "Проверим детали заказа"}</span></li>
                <li><b>2</b><span>Менеджер свяжется с вами и подтвердит детали</span></li>
                <li><b>3</b><span>{cashOnPickup ? "Оплатите растения наличными при получении в магазине" : "После подтверждения сможете перейти к оплате"}</span></li>
              </ol> : <ol>
                <li><b>1</b><span>Соберём и проверим растения</span></li>
                <li><b>2</b><span>Аккуратно упакуем</span></li>
                <li><b>3</b><span>{delivery === "pickup" ? "Сообщим, когда заказ будет готов" : "Передадим заказ в доставку"}</span></li>
              </ol>}</div>
            </div>
          </div>
        </div>
      ) : (
        <form className="checkout-layout" onSubmit={handleSubmit} onFocus={(event) => setDeliveryFocus(event.target instanceof HTMLInputElement ? event.target.name : "")}
          onBlur={(event) => {
            if (event.target instanceof HTMLInputElement) {
              const field = event.target.name;
              setTouched((current) => ({ ...current, [field]: true }));
            }
            setDeliveryFocus("");
          }}
          onChange={(event) => {
            const consent = event.currentTarget.elements.namedItem("consent") as HTMLInputElement;
            setConsentGiven(consent.checked);
          }} noValidate>
          <div className="checkout-form-main">
          <fieldset className="checkout-form-section checkout-contact-section">
            <legend>Контактные данные</legend>
            <p className="checkout-step-description">Укажите, с кем связаться по заказу и куда отправить чек.</p>
            {user && <p className="profile-prefill">Данные заполнены из личного кабинета</p>}
            <div className="checkout-contact-fields">
              <label className="checkout-contact-name">Имя<input name="name" aria-label="Имя" required aria-invalid={!!nameError} aria-describedby={nameError ? "name-error" : undefined} placeholder="Александр" autoComplete="name" value={checkoutProfile.name} onChange={(event) => { setContactErrors((current) => ({ ...current, name: "" })); setCheckoutProfile((current) => ({ ...current, name: event.target.value })); }} />{nameError && <small className="field-error checkout-contact-error" id="name-error" role="status">{nameError}</small>}</label>
              <div className="checkout-contact-row"><label>Телефон<input name="phone" aria-label="Телефон" type="tel" required inputMode="tel" autoComplete="tel" maxLength={18} aria-invalid={!!phoneError} aria-describedby={phoneError ? "phone-error" : undefined} placeholder="+7 900 000-00-00" value={checkoutProfile.phone} onChange={(event) => { event.currentTarget.setCustomValidity(""); setContactErrors((current) => ({ ...current, phone: "" })); const value = formatRussianPhoneInput(event.currentTarget.value); setCheckoutProfile((current) => ({ ...current, phone: value })); }} />{phoneError && <small className="field-error checkout-contact-error" id="phone-error" role="status">{phoneError}</small>}</label><label>Email для чека<input name="email" aria-label="Email для чека" required type="email" autoComplete="email" aria-invalid={!!emailError} aria-describedby={emailError ? "email-error" : undefined} placeholder="mail@example.ru" value={checkoutProfile.email} onChange={(event) => { setContactErrors((current) => ({ ...current, email: "" })); setCheckoutProfile((current) => ({ ...current, email: event.target.value })); }} />{emailError && <small className="field-error checkout-contact-error" id="email-error" role="status">{emailError}</small>}</label></div>
            </div>
          </fieldset>

          <fieldset className="checkout-form-section checkout-delivery-section">
            <legend>Способ доставки</legend>
            <div className="delivery-options" role="radiogroup" aria-label="Способ получения" aria-describedby={deliveryChoiceError ? "delivery-error" : undefined}>
              {availableDelivery.map((item) => {
                const selected = deliveryChosen && delivery === item.id;
                const selectDelivery = () => {
                  setDeliveryChosen(true);
                  setDeliveryError("");
                  setDelivery(item.id);
                  setDeliveryAnimationId((current) => current + 1);
                  if (!selected) track("add_shipping_info", { value: total, quantity: cartCount, properties: { delivery: item.id } });
                };
                return <div className={`checkout-delivery-choice ${selected ? "is-selected" : ""}`} key={item.id}>
                  <label>
                    <input type="radio" name="delivery" value={item.id} checked={selected} onChange={selectDelivery} onClick={() => { if (selected) selectDelivery(); }} />
                    <span><b>{item.title}</b><small>{item.detail}</small></span>
                    <DeliveryArt kind={item.id === "cdek" ? "cdek" : "pickup"} selected={selected} animationId={deliveryAnimationId} />
                  </label>
                </div>;
              })}
            </div>

            {deliveryChoiceError && <p className="field-error checkout-contact-error" id="delivery-error" role="status">{deliveryChoiceError}</p>}
            {deliveryChosen && (delivery === "cdek" ? (
              <div className="cdek-picker">
                <label>Город получения<input name="cdekCity" aria-label="Город получения" required aria-invalid={!!cityError} aria-describedby={cityError ? "cdek-city-error" : undefined} value={cdekCityQuery} onChange={(event) => { setDeliveryError(""); setCdekCityQuery(event.target.value); setCdekCity(null); setCdekCities([]); setCdekOffices([]); setCdekOfficeCode(""); setCdekOfficeQuery(""); setCdekQuotes([]); setCdekTariffCode(0); }} autoComplete="off" placeholder="Начните вводить город" />{cityError && <small className="field-error checkout-contact-error" id="cdek-city-error" role="status">{cityError}</small>}</label>
                {!!cdekCities.length && <div className="cdek-suggestions" role="listbox" aria-label="Найденные города">{cdekCities.map((city) => <button type="button" key={city.code} onClick={() => { setDeliveryError(""); chooseCdekCity(city); }}><b>{city.city}</b><span>{city.region || "Россия"}</span></button>)}</div>}
                {cdekLoading && <p className="cdek-status">Получаем данные СДЭК…</p>}
                {cdekError && <p className="cdek-status error">{cdekError}</p>}
                {!!cdekOffices.length && <label>Пункт выдачи<input name="cdekOffice" aria-label="Пункт выдачи" required aria-invalid={!!officeError} aria-describedby={officeError ? "cdek-office-error" : undefined} value={cdekOfficeQuery} onChange={(event) => { setDeliveryError(""); setCdekOfficeQuery(event.target.value); setCdekOfficeCode(""); setCdekOfficeListOpen(true); }} onFocus={() => setCdekOfficeListOpen(true)} autoComplete="off" placeholder="Улица или дом — покажем ближайшие пункты" />{officeError && <small className="field-error checkout-contact-error" id="cdek-office-error" role="status">{officeError}</small>}</label>}
                {cdekOfficeListOpen && !!cdekOfficeMatches.length && !cdekOfficeCode && <div className="cdek-suggestions" role="listbox" aria-label="Пункты выдачи">{cdekOfficeMatches.map((office) => <button type="button" key={office.code} onClick={() => { setDeliveryError(""); setCdekOfficeCode(office.code); setCdekOfficeQuery(office.location.address); setCdekOfficeListOpen(false); }}><b>{office.location.address}</b><span>{office.work_time || office.name}</span></button>)}</div>}
                {!!cdekOffices.length && !cdekOfficeMatches.length && <p className="cdek-status">Ничего не нашлось — попробуйте другую улицу</p>}
                {selectedOffice && <p className="cdek-status">Пункт выбран: {selectedOffice.location.address}</p>}
                {cartCount > 1 && selectedOffice && <div className="cdek-repack"><label><input type="checkbox" role="switch" checked={cdekRepack} onChange={(event) => setCdekRepack(event.target.checked)} />Упаковать в одну коробку, если поместятся</label><small aria-live="polite">{cdekFeePending ? "Доставку рассчитает менеджер после оформления." : "Проверим, поместятся ли растения в одну коробку."}</small></div>}
                {cartCount <= 1 && cdekFeePending && selectedOffice && <p className="cdek-quote pending" role="status">Доставку рассчитает менеджер после оформления.</p>}
                {!cdekRepack && cdekQuotes.length > 1 && <div className="cdek-tariffs" role="radiogroup" aria-label="Тарифы СДЭК">{cdekQuotes.map((option) => <label key={option.tariffCode} className="cdek-tariff"><input type="radio" name="cdek-tariff" checked={option.tariffCode === cdekQuote?.tariffCode} onChange={() => setCdekTariffCode(option.tariffCode)} /><span><b>{option.tariffName}</b><small>{option.daysMin === option.daysMax ? `${option.daysMin} дн.` : `${option.daysMin}–${option.daysMax} дн.`}</small></span><strong>{money(option.price)}</strong></label>)}</div>}
              </div>
            ) : (
              <div className="checkout-pickup-location"><div><strong>Рязань, Новосёлов, 40А</strong><p>Самовывоз из магазина · бесплатно</p></div></div>
            ))}
          </fieldset>

          {deliveryChosen && ((!hasPreorder || delivery === "pickup") && paymentMethods.length > 0 ? <fieldset className="checkout-form-section checkout-payment-section"><legend>Способ оплаты заказа</legend><div className="delivery-options">{paymentMethods.map((option) => <label key={option.id} className={paymentMethod === option.id ? "active" : ""}><input type="radio" name="paymentMethod" value={option.id} checked={paymentMethod === option.id} onChange={() => { setPaymentMethod(option.id); track("add_payment_info",{value:total,quantity:cartCount,properties:{paymentMethod:option.id}}); }} /><i className="option-icon"><CheckoutOptionIcon name={option.id === "online" ? "card" : "wallet"} /></i><span><b>{option.title}</b><small>{hasPreorder && option.id === "online" ? "После подтверждения наличия" : option.note}</small></span></label>)}</div></fieldset> : !hasPreorder && <div className="checkout-form-section payment-note"><b>Не удалось загрузить способы оплаты</b><p>Обновите страницу или попробуйте ещё раз позже. Заказ без выбранного способа оплаты не отправится.</p></div>)}
          </div>
          <aside className="checkout-order-summary" aria-label="Итог заказа">
          <div className="checkout-summary-heading"><h2>Ваш заказ</h2></div>
          <div className="checkout-summary-paper">
            <ul className="checkout-summary-items" aria-label="Состав заказа">{cartLines.map((item) => <li key={item.id}>
              <ProductImage src={item.image} alt="" />
              <div><strong>{item.name}</strong><small>{item.variantLabel} · {item.quantity} шт.</small></div>
              <b>{money(item.price * item.quantity)}</b>
            </li>)}</ul>
            <div className="checkout-summary-total"><span>{hasPreorder ? "Предварительная сумма" : deliveryUnknown ? "Растения, без доставки" : "Итого"}</span><strong>{pricingReady ? money(deliveryNotChosen || deliveryBlocked || deliveryCalculating ? subtotal : total) : <span>Проверяем цену…</span>}</strong></div>
            <dl><div><dt>Количество</dt><dd>{cartCount}</dd></div>{discountAmount > 0 && <><div><dt>Растения без скидки</dt><dd>{money(originalSubtotal)}</dd></div><div><dt>Персональная скидка {discountBps / 100}%</dt><dd>−{money(discountAmount)}</dd></div></>}<div><dt>Растения</dt><dd>{money(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{deliveryStatus}</dd></div></dl>
            {pricingError && <p role="alert">Не удалось проверить персональную скидку. Обновите страницу перед оформлением.</p>}
            {hasPreorder ? <div className="checkout-summary-note"><strong>Есть растения под заказ</strong><p>Сейчас платить не нужно. Менеджер проверит наличие и подтвердит итог заказа.</p></div> : deliveryNeedsManager && <p>Менеджер уточнит доставку и сообщит итог до оплаты.</p>}
          </div>
        </aside>
          <section className="checkout-submit-section" aria-label="Подтверждение заказа">
            <label className="consent-check"><input type="checkbox" name="consent" required aria-invalid={!!contactErrors.consent} aria-describedby={contactErrors.consent ? "consent-error" : undefined} onChange={() => setContactErrors((current) => ({ ...current, consent: "" }))} /><span>Принимаю <a href="/offer" target="_blank">оферту</a> и даю <a href="/privacy" target="_blank">согласие на обработку персональных данных</a>.</span></label>
            {contactErrors.consent && <p className="field-error" id="consent-error">{contactErrors.consent}</p>}
            {footerHint && <p className="checkout-submit-hint" id="checkout-submit-hint" role="status">{footerHint}</p>}
            {feedbackMessage && <p className="field-error" role="alert">{feedbackMessage}</p>}
            <div className="checkout-navigation"><a href="/cart" aria-label="Назад в корзину"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M19 12H5m7-7-7 7 7 7" /></svg>Назад</a><button className="primary-button" aria-describedby={footerHint ? "checkout-submit-hint" : undefined} disabled={submitting || !!submitHint}>{submitting ? "Оформляем…" : hasPreorder ? "Отправить заказ →" : "Оформить заказ →"}</button></div>
          </section>
        </form>
      )}
    </aside>
  );
}
