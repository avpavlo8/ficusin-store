import type { Dispatch, FormEventHandler, SetStateAction } from "react";
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

  return (
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
  );
}
