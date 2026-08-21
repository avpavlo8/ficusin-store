import { useEffect, useMemo, useState } from "react";
import { api, money } from "./adminShared";
import type { Order, Product } from "./adminTypes";

type PaymentBalance = {
  total: number;
  paid: number;
  refunded: number;
  netPaid: number;
  due: number;
  overpaid: number;
  ready: boolean;
  paymentStatus: string;
};

type Adjustment = {
  id: number;
  orderNumber: string;
  subtotal: number;
  deliveryFee: number;
  deliveryFeePending: boolean;
  hasPreorder: boolean;
  status: string;
  items: Array<{ productId: string; productName: string; unitPrice: number; quantity: number }>;
};

const emptyPayment: PaymentBalance = {
  total: 0, paid: 0, refunded: 0, netPaid: 0, due: 0, overpaid: 0,
  ready: false, paymentStatus: "pending",
};

export function AdminOrderEditor({ order, onSaved, onError }: {
  order: Order;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
}) {
  const [adjustment, setAdjustment] = useState<Adjustment | null>(null);
  const [payment, setPayment] = useState<PaymentBalance>(emptyPayment);
  const [products, setProducts] = useState<Product[]>([]);
  const [lines, setLines] = useState<Adjustment["items"]>([]);
  const [deliveryFee, setDeliveryFee] = useState(0);
  const [confirmDelivery, setConfirmDelivery] = useState(order.deliveryMethod === "pickup");
  const [addProduct, setAddProduct] = useState("");
  const [refundAmount, setRefundAmount] = useState("");
  const [paymentLink, setPaymentLink] = useState("");
  const [busy, setBusy] = useState(false);

  const load = async () => {
    try {
      const [state, catalog] = await Promise.all([
        api<{ order: Adjustment; payment: PaymentBalance }>(`/api/v1/admin/orders/${order.id}/adjustment`),
        api<{ products: Product[] }>("/api/v1/admin/products"),
      ]);
      setAdjustment(state.order);
      setPayment(state.payment ?? emptyPayment);
      setLines(state.order.items);
      setDeliveryFee(state.order.deliveryFee);
      setConfirmDelivery(order.deliveryMethod === "pickup" || !state.order.deliveryFeePending);
      setProducts(catalog.products.filter((product) => product.status === "published"));
    } catch (error) {
      onError((error as Error).message);
    }
  };

  useEffect(() => { void load(); }, [order.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const availableProducts = useMemo(() => {
    const used = new Set(lines.map((line) => line.productId));
    return products.filter((product) => !used.has(product.slug));
  }, [products, lines]);

  const draftSubtotal = useMemo(
    () => lines.reduce((total, line) => total + line.unitPrice * line.quantity, 0),
    [lines],
  );
  const draftTotal = draftSubtotal + Math.max(0, deliveryFee);

  const compositionSaved = useMemo(() => {
    if (!adjustment || adjustment.items.length !== lines.length) return false;
    return adjustment.items.every((line, index) =>
      line.productId === lines[index]?.productId && line.quantity === lines[index]?.quantity,
    );
  }, [adjustment, lines]);
  const savedDeliveryConfirmed = order.deliveryMethod === "pickup" || !adjustment?.deliveryFeePending;
  const deliverySaved = !!adjustment && Math.abs(deliveryFee - adjustment.deliveryFee) < 0.005 &&
    confirmDelivery === savedDeliveryConfirmed;
  const hasUnsavedChanges = !compositionSaved || !deliverySaved;
  const shownTotal = hasUnsavedChanges ? draftTotal : payment.total;
  const shownDue = Math.max(0, shownTotal - payment.netPaid);
  const shownOverpaid = Math.max(0, payment.netPaid - shownTotal);

  const compositionChanged = () => {
    // A manager has already confirmed the current delivery amount. Editing the
    // contents must not silently revoke that confirmation: pressing Save is
    // the explicit confirmation of the whole updated order. If delivery really
    // needs recalculation, the manager can uncheck it manually.
    setPaymentLink("");
  };

  const changeQuantity = (index: number, quantity: number) => {
    setLines((current) => current.map((line, position) => position === index
      ? { ...line, quantity: Math.max(1, Math.min(100, quantity || 1)) }
      : line));
    compositionChanged();
  };

  const removeLine = (index: number) => {
    if (lines.length <= 1) {
      onError("Последний товар нельзя удалить: отмените заказ целиком");
      return;
    }
    setLines((current) => current.filter((_, position) => position !== index));
    compositionChanged();
  };

  const appendProduct = () => {
    const product = products.find((item) => item.slug === addProduct);
    if (!product) return;
    setLines((current) => [...current, {
      productId: product.slug,
      productName: product.name,
      unitPrice: product.price,
      quantity: 1,
    }]);
    setAddProduct("");
    compositionChanged();
  };

  const save = async () => {
    setBusy(true);
    setPaymentLink("");
    try {
      const body: Record<string, unknown> = {
        items: lines.map((line) => ({ productId: line.productId, quantity: line.quantity })),
      };
      if (order.deliveryMethod === "pickup" || confirmDelivery) body.deliveryFee = deliveryFee;
      const result = await api<{ order: Order; adjustment: Adjustment; payment: PaymentBalance }>(
        `/api/v1/admin/orders/${order.id}/contents`,
        { method: "PATCH", body: JSON.stringify(body) },
      );
      setAdjustment(result.adjustment);
      setLines(result.adjustment.items);
      setDeliveryFee(result.adjustment.deliveryFee);
      setConfirmDelivery(order.deliveryMethod === "pickup" || !result.adjustment.deliveryFeePending);
      setPayment(result.payment);
      onSaved({ ...result.order, paymentStatus: result.payment.paymentStatus });
    } catch (error) {
      // Do not reload the old order here. A failed save must leave the
      // manager's draft visible so it can be retried instead of making an
      // added product look as if the editor silently deleted it.
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const createPaymentLink = async () => {
    setBusy(true);
    try {
      const result = await api<{ confirmationUrl: string; payment: PaymentBalance }>(
        `/api/v1/admin/orders/${order.id}/payment-link`, { method: "POST" },
      );
      setPayment(result.payment);
      setPaymentLink(result.confirmationUrl);
      try { await navigator.clipboard.writeText(result.confirmationUrl); } catch { /* link stays visible */ }
    } catch (error) {
      onError((error as Error).message);
    } finally { setBusy(false); }
  };

  const refund = async (amount: number) => {
    if (!Number.isFinite(amount) || amount <= 0) {
      onError("Укажите сумму возврата");
      return;
    }
    if (!window.confirm(`Вернуть покупателю ${money.format(amount)} на карту?`)) return;
    setBusy(true);
    try {
      const result = await api<{ payment: PaymentBalance }>(
        `/api/v1/admin/orders/${order.id}/refund`,
        { method: "POST", body: JSON.stringify({ amount, reason: "Возврат менеджером по заказу" }) },
      );
      setPayment(result.payment);
      setRefundAmount("");
      setPaymentLink("");
      onSaved({ ...order, paymentStatus: result.payment.paymentStatus });
    } catch (error) {
      onError((error as Error).message);
    } finally { setBusy(false); }
  };

  if (!adjustment) return <p>Загружаем заказ…</p>;

  return <div className="admin-order-editor">
    <section className="admin-block">
      <div className="admin-block-heading"><div><strong>Состав заказа</strong><small>Можно менять до отправки заказа</small></div></div>
      {lines.map((line, index) => <div className="admin-order-edit-line" key={`${line.productId}-${index}`}>
        <span><strong>{line.productName}</strong><small>{money.format(line.unitPrice)} / шт.</small></span>
        <input aria-label={`Количество ${line.productName}`} type="number" min="1" max="100" value={line.quantity}
          onChange={(event) => changeQuantity(index, Number(event.target.value))} />
        <strong>{money.format(line.unitPrice * line.quantity)}</strong>
        <button type="button" className="admin-action" onClick={() => removeLine(index)}>Удалить</button>
      </div>)}
      <div className="admin-order-add-line">
        <select value={addProduct} onChange={(event) => setAddProduct(event.target.value)}>
          <option value="">Добавить товар…</option>
          {availableProducts.map((product) => <option value={product.slug} key={product.id}>
            {product.name} · {money.format(product.price)} · остаток {product.stock}
          </option>)}
        </select>
        <button type="button" className="admin-action" disabled={!addProduct} onClick={appendProduct}>Добавить</button>
      </div>
      <div className="admin-order-draft-total">
        <small>{order.deliveryMethod !== "pickup" && !confirmDelivery
          ? "Предварительно с текущей стоимостью доставки — подтвердите доставку перед сохранением"
          : "После сохранения эта сумма станет итогом заказа"}</small>
        <span>Новая сумма</span>
        <strong>{money.format(draftTotal)}</strong>
      </div>
    </section>

    {order.deliveryMethod !== "pickup" && <section className="admin-block">
      <strong>Доставка</strong>
      <div className="admin-form-grid">
        <label>Стоимость доставки, ₽<input type="number" min="0" step="1" value={deliveryFee}
          onChange={(event) => { setDeliveryFee(Math.max(0, Number(event.target.value))); setConfirmDelivery(true); setPaymentLink(""); }} /></label>
        <label className="admin-checkbox"><input type="checkbox" checked={confirmDelivery}
          onChange={(event) => { setConfirmDelivery(event.target.checked); setPaymentLink(""); }} />Стоимость подтверждена менеджером</label>
      </div>
      {!confirmDelivery && <small className="admin-flag">Пока доставка не подтверждена, клиент оплатить заказ не сможет.</small>}
    </section>}

    <div className="dialog-actions">
      <button type="button" className="primary" disabled={busy} onClick={save}>Сохранить изменения</button>
    </div>

    <section className="admin-block admin-order-payment-block">
      <strong>Оплата</strong>
      <p>Итого: <b>{money.format(shownTotal)}</b>{hasUnsavedChanges && <small> · после сохранения</small>} · получено: <b>{money.format(payment.paid)}</b> · возвращено: <b>{money.format(payment.refunded)}</b></p>
      {shownDue > 0 && <p className="admin-flag">К доплате: <b>{money.format(shownDue)}</b></p>}
      {shownOverpaid > 0 && <p className="admin-flag">Переплата: <b>{money.format(shownOverpaid)}</b></p>}
      {hasUnsavedChanges && shownDue > 0 && <p>Сначала сохраните изменения — ссылка будет создана уже на новую сумму.</p>}
      {!hasUnsavedChanges && !payment.ready && payment.due > 0 && <p>Оплата закрыта до подтверждения наличия всех товаров и доставки.</p>}
      {!hasUnsavedChanges && payment.ready && payment.due > 0 && <button type="button" className="admin-action" disabled={busy} onClick={createPaymentLink}>Создать ссылку на доплату</button>}
      {paymentLink && <p><a href={paymentLink} target="_blank" rel="noreferrer">Ссылка на оплату</a> <small>скопирована в буфер, если браузер разрешил</small></p>}
      {payment.netPaid > 0 && <div className="admin-refund admin-order-refund-form">
        <input type="number" min="1" max={payment.netPaid} step="1" placeholder="Сумма возврата"
          value={refundAmount} onChange={(event) => setRefundAmount(event.target.value)} />
        <button type="button" className="admin-action" disabled={busy} onClick={() => refund(Number(refundAmount))}>Вернуть часть</button>
        <button type="button" className="admin-action" disabled={busy} onClick={() => refund(payment.netPaid)}>Вернуть всё</button>
      </div>}
    </section>
  </div>;
}
