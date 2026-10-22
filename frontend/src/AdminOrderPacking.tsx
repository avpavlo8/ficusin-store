import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, money } from "./adminShared";
import type { Order } from "./adminTypes";

type Item = { id: number; sku?: string; productName: string; quantity: number };
type Box = { lengthCm: number; widthCm: number; heightCm: number; weightGrams: number; weightInput?: string; contents: Array<{ orderItemId: number; quantity: number }> };
type Quote = { tariffCode: number; tariffName: string; price: number; daysMin: number; daysMax: number };
type City = { code: number; city: string; region?: string };
type Office = { code: string; name: string; location: { address: string; address_full?: string }; work_time?: string };
export type OrderPackingState = {
  revision: number;
  boxes: Box[];
  tariffCode: number;
  tariffName: string;
  deliveryFee: number;
  deliveryPayee: string;
  officeCode: string;
  cityCode: number;
  state: string;
  cdekState: string;
  trackNumber: string;
  paymentUrl: string;
  error: string;
  notification: { state: string; sentAt?: string; error?: string };
  blockers: string[];
  quoteBlockers?: string[];
  push?: { state: string; error?: string };
};

const activeStates = new Set(["queued", "waiting_cdek", "preparing_payment"]);
const stateLabels: Record<string, string> = {
  draft: "Подготовка отправления", queued: "Заказ принят в обработку", waiting_cdek: "Регистрируем отправление в СДЭК",
  preparing_payment: "Готовим ссылку на оплату", notification_queued: "Заказ подготовлен", notified: "Заказ подготовлен",
  failed: "Обработка остановлена", cancelled: "Обработка отменена", stale: "Состав заказа изменился",
};
const emptyBox = (items: Item[] = []): Box => ({ lengthCm: 0, widthCm: 0, heightCm: 0, weightGrams: 0, contents: items.map((item) => ({ orderItemId: item.id, quantity: item.quantity })) });

export function AdminOrderPacking({ orderId, items, subtotal, netPaid, address, disabled, compositionDirty, onDirtyChange, onBusyChange, onStateChange, onOrderUpdated }: {
  orderId: number;
  items: Item[];
  subtotal: number;
  netPaid: number;
  email: string;
  address: string;
  disabled: boolean;
  compositionDirty: boolean;
  onDirtyChange: (dirty: boolean) => void;
  onBusyChange: (busy: boolean) => void;
  onStateChange?: (packing: OrderPackingState) => void;
  onOrderUpdated: (order: Order) => void;
}) {
  const [packing, setPacking] = useState<OrderPackingState | null>(null);
  const [boxes, setBoxes] = useState<Box[]>([]);
  const [quotes, setQuotes] = useState<Quote[]>([]);
  const [tariffCode, setTariffCode] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"" | "quote" | "finalize">("");
  const [dirty, setDirty] = useState(false);
  const [editingDestination, setEditingDestination] = useState(false);
  const [cityQuery, setCityQuery] = useState("");
  const [city, setCity] = useState<City | null>(null);
  const [cities, setCities] = useState<City[]>([]);
  const [offices, setOffices] = useState<Office[]>([]);
  const [officeCode, setOfficeCode] = useState("");
  const [destinationLoading, setDestinationLoading] = useState(false);
  const [destinationError, setDestinationError] = useState("");
  const [searchedCity, setSearchedCity] = useState(false);
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  const lastReported = useRef("");
  const previousItems = useRef(items);
  const stateChange = useRef(onStateChange);
  const orderUpdated = useRef(onOrderUpdated);
  useEffect(() => { stateChange.current = onStateChange; orderUpdated.current = onOrderUpdated; }, [onStateChange, onOrderUpdated]);

  const load = useCallback(async (reset: boolean) => {
    const result = await api<{ packing: OrderPackingState; order?: Order }>(`/api/v1/admin/orders/${orderId}/packing`);
    const next = result.packing;
    setPacking(next);
    if (reset) {
      const priorItems = previousItems.current;
      previousItems.current = items;
      setBoxes((current) => {
        if (next.boxes?.length) return next.boxes;
        if (!current.length) return [emptyBox(items)];
        if (current.length === 1 || items.length === 1) return [{ ...current[0], contents: emptyBox(items).contents }];
        // Saving the composition can recreate order-item IDs. Keep measured
        // boxes and explicit allocations by SKU, then ask the operator to
        // allocate any new or changed quantities before another quote.
        return current.slice(0, Math.min(20, items.length)).map((box) => ({ ...box, contents: box.contents.flatMap((line) => {
          const previous = priorItems.find((item) => item.id === line.orderItemId);
          const currentItem = items.find((item) => previous?.sku ? item.sku === previous.sku : item.id === line.orderItemId);
          return currentItem ? [{ orderItemId: currentItem.id, quantity: line.quantity }] : [];
        }) }));
      });
      setQuotes([]);
      setTariffCode(0);
    }
    if (next.state !== "draft") setDirty(false);
    const signature = `${next.revision}:${next.state}:${next.trackNumber}:${next.notification.state}:${next.push?.state}`;
    if (lastReported.current !== signature) {
      lastReported.current = signature;
      stateChange.current?.(next);
      if (result.order) orderUpdated.current(result.order);
    }
    return next;
  }, [orderId, items]);

  useEffect(() => {
    let live = true;
    void load(true).catch((reason: Error) => { if (live) setError(reason.message); });
    return () => { live = false; };
  }, [load]);

  useEffect(() => {
    if (!packing || (!activeStates.has(packing.state) && packing.push?.state !== "queued" && packing.notification.state !== "queued")) return;
    const timer = window.setInterval(() => { void load(false).catch((reason: Error) => setError(`Не удалось обновить статус: ${reason.message}`)); }, 4000);
    return () => window.clearInterval(timer);
  }, [packing, load]);

  useEffect(() => { onDirtyChange(dirty); }, [dirty, onDirtyChange]);

  useEffect(() => {
    if (city || cityQuery.trim().length < 2) return;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      setDestinationLoading(true);
      setDestinationError("");
      try {
        const result = await api<{ cities: City[] }>(`/api/v1/delivery/cdek?action=cities&city=${encodeURIComponent(cityQuery.trim())}`, { signal: controller.signal });
        setCities(result.cities ?? []);
        setSearchedCity(true);
      } catch (reason) { if (!controller.signal.aborted) setDestinationError((reason as Error).message); }
      finally { if (!controller.signal.aborted) setDestinationLoading(false); }
    }, 350);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [cityQuery, city]);

  useEffect(() => {
    if (!city) return;
    const controller = new AbortController();
    void api<{ offices: Office[] }>(`/api/v1/delivery/cdek?action=offices&cityCode=${city.code}`, { signal: controller.signal }).then((result) => {
      setOffices(result.offices ?? []);
      if (!result.offices?.length) setDestinationError("В выбранном городе нет доступных пунктов выдачи СДЭК.");
    }).catch((reason: Error) => { if (!controller.signal.aborted) setDestinationError(reason.message); })
      .finally(() => { if (!controller.signal.aborted) setDestinationLoading(false); });
    return () => controller.abort();
  }, [city]);

  const editable = packing?.state === "draft";
  const destinationNeeded = !!packing && (!packing.cityCode || !packing.officeCode || editingDestination);
  const destinationOverride = !!city && !!officeCode;
  const blockers = (packing?.blockers ?? []).filter((blocker) => !(destinationOverride && ["В заказе не указан пункт выдачи СДЭК", "В заказе не указан город СДЭК"].includes(blocker)));
  const quoteBlockers = (packing?.quoteBlockers ?? packing?.blockers ?? []).filter((blocker) => !(destinationOverride && ["В заказе не указан пункт выдачи СДЭК", "В заказе не указан город СДЭК"].includes(blocker)));
  const maxBoxes = Math.min(20, items.length);
  const validBoxes = boxes.length > 0 && boxes.length <= maxBoxes && boxes.every((box) => box.lengthCm > 0 && box.widthCm > 0 && box.heightCm > 0 && box.lengthCm <= 300 && box.widthCm <= 300 && box.heightCm <= 300 && box.weightGrams > 0 && box.weightGrams <= 200000 && box.contents.some((line) => line.quantity > 0));
  const allocationComplete = items.every((item) => boxes.reduce((total, box) => total + (box.contents.find((line) => line.orderItemId === item.id)?.quantity ?? 0), 0) === item.quantity);
  const formDisabled = disabled || !!busy || !editable;
  const canQuote = validBoxes && allocationComplete && !quoteBlockers.length && (!destinationNeeded || destinationOverride) && !compositionDirty && !formDisabled;
  const selectedQuote = useMemo(() => quotes.find((quote) => quote.tariffCode === tariffCode), [quotes, tariffCode]);

  const changed = () => { setDirty(true); setQuotes([]); setTariffCode(0); setError(""); };
  const updateBox = (index: number, update: Partial<Box>) => { setBoxes((current) => current.map((box, position) => position === index ? { ...box, ...update } : box)); changed(); };
  const payload = () => ({ revision: packing!.revision, boxes: boxes.map((box) => ({ lengthCm: box.lengthCm, widthCm: box.widthCm, heightCm: box.heightCm, weightGrams: box.weightGrams, contents: box.contents })), ...(destinationOverride ? { cityCode: city!.code, officeCode } : {}) });
  const quote = async () => {
    if (!canQuote) return;
    setBusy("quote"); onBusyChange(true); setError("");
    try {
      const result = await api<{ quotes: Quote[]; revision?: number }>(`/api/v1/admin/orders/${orderId}/packing/quote`, { method: "POST", body: JSON.stringify(payload()) });
      setQuotes(result.quotes ?? []);
      setTariffCode(result.quotes?.[0]?.tariffCode ?? 0);
      if (!result.quotes?.length) setError("СДЭК не вернул доступных тарифов. Проверьте коробку и пункт выдачи, затем повторите расчёт.");
      if (result.revision !== undefined) setPacking((current) => current ? { ...current, revision: result.revision! } : current);
    } catch (reason) { setError((reason as Error).message); }
    finally { setBusy(""); onBusyChange(false); }
  };

  const finalize = async () => {
    if (!canQuote || blockers.length || !selectedQuote) return;
    const body = { ...payload(), tariffCode, quotedDeliveryFee: selectedQuote.price };
    const fingerprint = JSON.stringify(body);
    if (attempt.current?.fingerprint !== fingerprint) attempt.current = { fingerprint, key: crypto.randomUUID() };
    setBusy("finalize"); onBusyChange(true); setError("");
    try {
      const result = await api<{ packing: OrderPackingState; order?: Order }>(`/api/v1/admin/orders/${orderId}/packing/finalize`, { method: "POST", body: JSON.stringify({ ...body, idempotencyKey: attempt.current.key }) });
      setPacking(result.packing);
      setDirty(false);
      stateChange.current?.(result.packing);
      if (result.order) orderUpdated.current(result.order);
    } catch (reason) {
      setError((reason as Error).message);
      // A lost response is not a failed submission. Read its state; never
      // automatically repeat the operation that registers and notifies.
      try { await load(false); } catch { /* Keep the explicit status retry visible. */ }
    } finally { setBusy(""); onBusyChange(false); }
  };

  return <section className="admin-block admin-order-packing" aria-labelledby="order-packing-title">
    <div className="admin-block-heading"><div><h3 id="order-packing-title">Подготовить заказ к отправке</h3></div></div>
    {!packing ? <div role={error ? "alert" : "status"}><p>{error || "Загружаем условия отправления…"}</p>{error && <button type="button" className="admin-action" onClick={() => void load(true).then(() => setError("")).catch((reason: Error) => setError(reason.message))}>Повторить загрузку</button>}</div> : <>
      {compositionDirty && <p className="order-packing-warning">Сохраните состав заказа перед расчётом доставки.</p>}
      {!!blockers.length && <div className="order-packing-blockers"><strong>Для подтверждения заказа</strong><ul>{blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul></div>}
      {editable && <>
        <div className="order-packing-destination"><div><strong>Пункт выдачи</strong><p>{address || "Адрес не указан"}</p></div>{!destinationNeeded && <button type="button" className="admin-action" disabled={formDisabled} onClick={() => setEditingDestination(true)}>Изменить</button>}</div>
        {destinationNeeded && <fieldset className="order-packing-destination-form" disabled={formDisabled}>
          <legend>Уточните город и пункт выдачи</legend>
          <label>Город получения<input aria-label="Город получения СДЭК" value={cityQuery} placeholder="Начните вводить город" autoComplete="off" onChange={(event) => { setCityQuery(event.target.value); setCity(null); setCities([]); setOffices([]); setOfficeCode(""); setSearchedCity(false); setDestinationLoading(false); setDestinationError(""); changed(); }} /></label>
          {!city && cities.length > 0 && <div className="order-packing-city-results" aria-label="Найденные города">{cities.map((option) => <button type="button" key={option.code} onClick={() => { setCity(option); setCityQuery(option.city); setCities([]); setOffices([]); setOfficeCode(""); setDestinationLoading(true); setDestinationError(""); changed(); }}><strong>{option.city}</strong><small>{option.region}</small></button>)}</div>}
          {!city && searchedCity && !cities.length && !destinationLoading && <p>Город не найден. Проверьте название.</p>}
          {city && <label>Пункт выдачи<select aria-label="Пункт выдачи СДЭК" value={officeCode} onChange={(event) => { setOfficeCode(event.target.value); changed(); }}><option value="">Выберите пункт выдачи</option>{offices.map((office) => <option key={office.code} value={office.code}>{office.location.address_full || office.location.address} · {office.name || office.code}</option>)}</select></label>}
          {destinationLoading && <small role="status">Загружаем данные СДЭК…</small>}{destinationError && <p role="alert">{destinationError}</p>}
          {destinationOverride && <small>Выбранный пункт будет сохранён при отправке заказа в обработку.</small>}
          {packing.cityCode > 0 && packing.officeCode && editingDestination && <button type="button" className="admin-action" onClick={() => { setEditingDestination(false); setCity(null); setCityQuery(""); setOfficeCode(""); setDestinationError(""); setDestinationLoading(false); changed(); }}>Оставить прежний пункт выдачи</button>}
        </fieldset>}
        <p className="order-packing-instruction">Размеры и вес — с упаковкой.</p>
        {boxes.map((box, index) => <fieldset className="order-packing-box" disabled={formDisabled} key={index}>
          <legend>Коробка {index + 1}</legend>
          <div className="order-packing-dimensions">{([
            ["lengthCm", "Длина", "см", 300], ["widthCm", "Ширина", "см", 300], ["heightCm", "Высота", "см", 300], ["weightGrams", "Вес с упаковкой", "кг", 200],
          ] as const).map(([field, label, unit, max]) => <label key={field}>{label}, {unit}<input aria-label={boxes.length === 1 ? `${label}${field !== "weightGrams" ? " коробки" : ""}, ${unit}` : `${label} коробки ${index + 1}, ${unit}`} type="number" min={field === "weightGrams" ? "0.001" : "1"} max={max} step={field === "weightGrams" ? "0.001" : "1"} inputMode={field === "weightGrams" ? "decimal" : "numeric"} value={field === "weightGrams" ? box.weightInput ?? (box.weightGrams ? String(box.weightGrams / 1000) : "") : box[field] || ""} placeholder="0" onChange={(event) => updateBox(index, { [field]: Math.max(0, Math.round((Number(event.target.value) || 0) * (field === "weightGrams" ? 1000 : 1))), ...(field === "weightGrams" ? { weightInput: event.target.value } : {}) })} /></label>)}</div>
          {boxes.length === 1 ? <p className="order-packing-contents">В коробке: {items.reduce((sum, item) => sum + item.quantity, 0)} шт.</p> : <div className="order-packing-allocation">{items.map((item) => <label key={item.id}><span>{item.productName}<small>Всего в заказе: {item.quantity} шт.</small></span><input aria-label={`${item.productName} в коробке ${index + 1}`} type="number" min="0" max={item.quantity} value={box.contents.find((line) => line.orderItemId === item.id)?.quantity ?? 0} onChange={(event) => updateBox(index, { contents: [...box.contents.filter((line) => line.orderItemId !== item.id), { orderItemId: item.id, quantity: Math.max(0, Math.min(item.quantity, Number(event.target.value) || 0)) }] })} /></label>)}<button type="button" className="admin-action" onClick={() => { setBoxes((current) => { const next = current.filter((_, position) => position !== index); return next.length === 1 ? [{ ...next[0], contents: emptyBox(items).contents }] : next; }); changed(); }}>Удалить коробку</button></div>}
        </fieldset>)}
        <div className="order-packing-tools">{boxes.length < maxBoxes && <button type="button" className="admin-action" disabled={formDisabled} onClick={() => { setBoxes((current) => current.length < maxBoxes ? [...current, emptyBox()] : current); changed(); }}>Добавить коробку</button>}{boxes.length > 1 && !allocationComplete && <small>Распределите все растения по коробкам без повторов.</small>}</div>
        <div className="order-packing-quote-action">{!validBoxes && <p className="order-packing-warning">Заполните размеры и вес коробки</p>}{validBoxes && !selectedQuote && !quoteBlockers.length && <p>Рассчитайте доставку, чтобы выбрать тариф СДЭК.</p>}<button type="button" className="order-packing-button" disabled={!canQuote} onClick={() => void quote()}>{busy === "quote" ? "Рассчитываем…" : "Рассчитать доставку"}</button></div>
        {quotes.length > 0 && <div className="order-packing-quotes"><h4>Тариф доставки</h4>{quotes.map((option) => <label key={option.tariffCode}><input type="radio" name={`order-packing-tariff-${orderId}`} value={option.tariffCode} checked={tariffCode === option.tariffCode} disabled={formDisabled} onChange={() => { setTariffCode(option.tariffCode); setDirty(true); }} /><span><strong>{option.tariffName}</strong><small>{option.daysMin}–{option.daysMax} дн.</small></span><strong>{money.format(option.price)}</strong></label>)}</div>}
        {selectedQuote && <div className="order-packing-submit"><dl className="order-packing-total"><div><dt>Растения</dt><dd>{money.format(subtotal)}</dd></div><div><dt>Доставка</dt><dd>{money.format(selectedQuote.price)}</dd></div><div><dt>Итого с доставкой</dt><dd>{money.format(subtotal + selectedQuote.price)}</dd></div>{netPaid > 0 && <div><dt>К доплате с учётом полученной оплаты</dt><dd>{money.format(Math.max(0, subtotal + selectedQuote.price - netPaid))}</dd></div>}</dl><p>Покупателю отправим push и письмо, если эти каналы доступны.</p><button type="button" className="order-packing-button primary" disabled={!canQuote || blockers.length > 0} onClick={() => void finalize()}>{busy === "finalize" ? "Передаём в обработку…" : "Сохранить и подтвердить"}</button></div>}
      </>}
      {packing.state !== "draft" && <div className="order-packing-progress" aria-live="polite">
        <strong>{stateLabels[packing.state] || "Проверяем состояние обработки"}</strong>
        <dl><div><dt>Упаковка</dt><dd>{packing.boxes.map((box, index) => <div key={index}>{packing.boxes.length > 1 && <>Коробка {index + 1}: </>}{box.lengthCm} × {box.widthCm} × {box.heightCm} см</div>)}</dd></div>
          <div><dt>СДЭК:</dt><dd>{packing.trackNumber ? <strong>{packing.trackNumber}</strong> : packing.cdekState === "manual_review" ? "Нужна проверка заявки" : "Трек в процессе создания"}</dd></div>
          <div><dt>Оплата</dt><dd>{packing.paymentUrl ? <a href={packing.paymentUrl} target="_blank" rel="noreferrer">Ссылка на оплату готова</a> : subtotal + packing.deliveryFee <= netPaid ? "Оплата не требуется" : "Ссылка ещё не готова"}</dd></div>
          <div><dt>Push покупателю</dt><dd>{packing.push?.state === "sent" ? "Отправлен в push-сервис" : packing.push?.state === "queued" ? "В очереди на отправку" : packing.push?.state === "failed" ? "Не удалось отправить" : packing.push?.state === "unavailable" ? "Push недоступен" : "Ещё не отправлен"}{packing.push?.error && <small>{packing.push.error}</small>}</dd></div>
          <div><dt>Письмо покупателю</dt><dd>{packing.notification.state === "sent" ? `Отправлено${packing.notification.sentAt ? ` ${new Date(packing.notification.sentAt).toLocaleString("ru-RU")}` : ""}` : packing.notification.state === "queued" ? "В очереди на отправку" : packing.notification.state === "failed" ? "Не удалось отправить" : packing.notification.state === "unavailable" ? packing.notification.error || "Email недоступен" : "Ещё не отправлено"}</dd></div></dl>
        {["queued", "waiting_cdek", "preparing_payment"].includes(packing.state) && <p>Обработка продолжается автоматически. Статус обновляется на этой странице.</p>}
      </div>}
      {(error || packing.error) && <div className="order-packing-error" role="alert"><p>{error || packing.error}</p><button type="button" className="admin-action" disabled={!!busy} onClick={() => void load(false).then(() => setError("")).catch((reason: Error) => setError(reason.message))}>Проверить статус</button></div>}
    </>}
  </section>;
}
