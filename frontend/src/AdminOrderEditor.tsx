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
  const deliverySaved = !!adjustment && Math.abs(deliveryFee - adjustment.deliveryFee) < 0.005;
  const hasUnsavedChanges = !compositionSaved || !deliverySaved;
  const shownTotal = hasUnsavedChanges ? draftTotal : payment.total;
  const shownDue = Math.max(0, shownTotal - payment.netPaid);
  const shownOverpaid = Math.max(0, payment.netPaid - shownTotal);

  const compositionChanged = () => setPaymentLink("");

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
      // Редактор доступен только сотруднику с orders.edit. Поэтому одно
      // действие «Сохранить изменения» одновременно подтверждает и состав,
      // и указанную менеджером стоимость доставки. Отдельный флаг здесь
      // только создавал ложное состояние «менеджер сохранил, но не подтвердил».
      const body = {
        items: lines.map((line) => ({ productId: line.productId, quantity: line.quantity })),
        deliveryFee,
      };
      const result = await api<{ order: Order; adjustment: Adjustment; payment: PaymentBalance }>(
        `/api/v1/admin/orders/${order.id}/contents`,
        { method: "PATCH", body: JSON.stringify(body) },
      );
      setAdjustment(result.adjustment);
      setLines(result.adjustment.items);
      setDeliveryFee(result.adjustment.deliveryFee);
      setPayment(result.payment);
      onSaved({ ...result.order, paymentStatus: result.payment.paymentStatus });
    } catch (error) {
      // Не откатываем черновик на экране: менеджер должен видеть, что именно
      // пытался сохранить, и иметь возможность повторить после исправления.
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
      <div className="admin-block-heading"><div><strong>Состав заказа</strong><small>Менеджер может изменить заказ до отправки</small></div></div>
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
        <small>После сохранения эта сумма станет итогом заказа</small>
        <span>Новая сумма</span>
        <strong>{money.format(draftTotal)}</strong>
      </div>
    </section>

    {order.deliveryMethod !== "pickup" && <section className="admin-block">
      <strong>Доставка</strong>
      <div className="admin-form-grid">
        <label>Стоимость доставки, ₽<input type="number" min="0" step="1" value={deliveryFee}
          onChange={(event) => { setDeliveryFee(Math.max(0, Number(event.target.value))); setPaymentLink(""); }} /></label>
      </div>
      <small>Нажатие «Сохранить изменения» подтверждает эту стоимость для клиента.</small>
    </section>}

    <div className="dialog-actions">
      <button type="button" className="primary" disabled={busy} onClick={save}>Сохранить изменения</button>
    </div>

    <section className="admin-block admin-order-payment-block">
      <strong>Оплата</strong>
      <p>Итого: <b>{money.format(shownTotal)}</b>{hasUnsavedChanges && <small> · после сохранения</small>} · получено: <b>{money.format(payment.paid)}</b> · возвращено: <b>{money.format(payment.refunded)}</b></p>
      {shownDue > 0 && <p className="admin-flag">К доплате: <b>{money.format(shownDue)}</b></p>}
      {shownOverpaid > 0 && <p className="admin-flag">Переплата: <b>{money.format(shownOverpaid)}</b></p>}
      {hasUnsavedChanges && shownDue > 0 && <p>Сначала сохраните изменения — старая ссылка больше не используется.</p>}
      {!hasUnsavedChanges && !payment.ready && payment.due > 0 && <p>Оплата закрыта: в заказе есть товар без подтверждённого наличия.</p>}
      {!hasUnsavedChanges && payment.ready && payment.due > 0 && <button type="button" className="admin-action" disabled={busy} onClick={createPaymentLink}>
        {payment.netPaid > 0 ? "Создать ссылку на доплату" : "Создать ссылку на оплату"}
      </button>}
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
