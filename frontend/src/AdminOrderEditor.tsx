import { AdminOrderProductSearch } from "./AdminOrderProductSearch";
import { NumericInput } from "./NumericInput";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { api, money } from "./adminShared";
import type { Order, Product } from "./adminTypes";
import { AdminOrderPacking } from "./AdminOrderPacking";

export type OrderReviewState = {
  hasPreorder: boolean;
  deliveryFeePending: boolean;
  packagingDimensionsMissing: boolean;
  cdekCreateState: string;
  cdekLastError: string;
  paymentIssues: Array<{ id: number; needsReview: boolean }>;
};

type RefundEntry = {
  id: number;
  amount: number;
  status: string;
  providerId: string;
  actorId?: number;
  needsReview: boolean;
  receiptStatus?: string;
  lastError: string;
};

type RefundOption = {
  paymentId: number;
  itemIndex: number;
  quantity: number;
  name: string;
  price: number;
};
type PaymentBalance = {
  fiscalTasks?: Array<{
    paymentId: number;
    kind: "income" | "settlement";
    status: string;
    reference: string;
  }>;
  refundOptions?: RefundOption[];
  refunds?: RefundEntry[];
  total: number;
  paid: number;
  refunded: number;
  netPaid: number;
  due: number;
  overpaid: number;
  ready: boolean;
  paymentStatus: string;
  issues: Array<{
    id: number;
    amount: number;
    createdAt: string;
    recoveryDeadline: string;
    attempts: number;
    lastAttemptAt?: string;
    lastError: string;
    needsReview: boolean;
  }>;
};

type Adjustment = {
  id: number;
  orderNumber: string;
  subtotal: number;
  discountType?: "fixed" | "percent";
  discountValue?: number;
  discountAmount?: number;
  deliveryFee: number;
  deliveryPayee?: "shop" | "carrier";
  deliveryFeePending: boolean;
  hasPreorder: boolean;
  status: string;
  deliveryMethod: string;
  cdekTariffCode?: number;
  cdekCreateState: string;
  cdekStatus: string;
  cdekStatusReason: string;
  cdekLastError: string;
  items: Array<{
    id: number;
    productId: number;
    sku: string;
    variantLabel: string;
    productName: string;
    unitPrice: number;
    quantity: number;
    packageLengthCm: number;
    packageWidthCm: number;
    packageHeightCm: number;
    packageWeightGrams: number;
  }>;
  shipmentOffers: ShipmentOffer[];
};

type ShipmentOffer = {
  id: number;
  version: number;
  status: string;
  deliveryFee: number;
  deliveryPayee?: "shop" | "carrier";
  subtotal: number;
  total: number;
  notifiedAt?: string;
  expiresAt?: string;
  managerNote: string;
  cdekCreateState: string;
  cdekTrackNumber: string;
  cdekStatus: string;
  cdekStatusReason: string;
  cdekLastError: string;
  items: Array<{
    orderItemId: number;
    productName: string;
    unitPrice: number;
    originalUnitPrice: number;
    quantity: number;
  }>;
  boxes: Array<{
    boxNo: number;
    lengthCm: number;
    widthCm: number;
    heightCm: number;
    weightGrams: number;
  }>;
};
const ShipmentOffers = lazy(() =>
  import("./AdminShipmentOfferBuilder").then((module) => ({ default: module.AdminShipmentOffers })),
);

const emptyPayment: PaymentBalance = {
  total: 0,
  paid: 0,
  refunded: 0,
  netPaid: 0,
  due: 0,
  overpaid: 0,
  ready: false,
  paymentStatus: "pending",
  issues: [],
};

export function AdminOrderEditor({
  canRefund = false,
  order,
  onSaved,
  onError,
  registerBeforeLeave,
  onDirtyChange,
  onReviewState,
  disabled = false,
}: {
  canRefund?: boolean;
  order: Order;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
  registerBeforeLeave?: (guard: () => Promise<boolean>) => () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onReviewState?: (state: OrderReviewState) => void;
  disabled?: boolean;
}) {
  const [adjustment, setAdjustment] = useState<Adjustment | null>(null);
  const [payment, setPayment] = useState<PaymentBalance>(emptyPayment);
  const [products, setProducts] = useState<Product[]>([]);
  const [lines, setLines] = useState<Adjustment["items"]>([]);
  const [deliveryFee, setDeliveryFee] = useState(0);
  const [discountType, setDiscountType] = useState<"fixed" | "percent">("fixed");
  const [discountValue, setDiscountValue] = useState(0);
  const [addProduct, setAddProduct] = useState("");
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const [packingBoxesReady, setPackingBoxesReady] = useState(false);
  const [packingDirty, setPackingDirty] = useState(false);
  const [packingFrozen, setPackingFrozen] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [saved, setSaved] = useState(false);
  const readOnly = ["cancelled", "completed", "shipped"].includes(order.status);
  const compositionReadOnly = readOnly || packingFrozen;
  const formDisabled = busy || disabled || compositionReadOnly;

  const [refundAmount, setRefundAmount] = useState("");
  const [refundQuantities, setRefundQuantities] = useState<Record<string, number>>({});
  const refundSelections = (payment.refundOptions ?? [])
    .map((item) => ({
      paymentId: item.paymentId,
      itemIndex: item.itemIndex,
      quantity: refundQuantities[`${item.paymentId}:${item.itemIndex}`] ?? 0,
    }))
    .filter((item) => item.quantity > 0);
  const selectedMinor = (payment.refundOptions ?? []).reduce(
    (sum, item) =>
      sum +
      Math.round(item.price * 100) * (refundQuantities[`${item.paymentId}:${item.itemIndex}`] ?? 0),
    0,
  );
  const requestedRefund = refundSelections.length ? selectedMinor / 100 : Number(refundAmount);
  const [refundReason, setRefundReason] = useState("");
  const [refundRequest, setRefundRequest] = useState<{ signature: string; id: string } | null>(
    null,
  );
  const [refundMessage, setRefundMessage] = useState("");
  const [refundProviderIDs, setRefundProviderIDs] = useState<Record<number, string>>({});
  const [refundBusy, setRefundBusy] = useState(false);
  const refreshMoney = async () => {
    const state = await api<{ payment: PaymentBalance }>(
      `/api/v1/admin/orders/${order.id}/adjustment`,
    );
    setPayment(state.payment);
    onSaved({ ...order, paymentStatus: state.payment.paymentStatus });
  };
  const sendRefund = async () => {
    const amount = requestedRefund;
    if (!Number.isFinite(amount) || amount <= 0 || amount > payment.netPaid || !refundReason.trim())
      return;
    if (!window.confirm(`Вернуть ${money.format(amount)} покупателю через ЮKassa?`)) return;
    const signature = JSON.stringify({
      amount,
      reason: refundReason.trim(),
      selections: refundSelections,
    });
    const requestId =
      refundRequest?.signature === signature ? refundRequest.id : crypto.randomUUID();
    setRefundRequest({ signature, id: requestId });
    setRefundBusy(true);
    setRefundMessage("");
    try {
      await api(`/api/v1/admin/orders/${order.id}/refund`, {
        method: "POST",
        body: JSON.stringify({
          amount,
          reason: refundReason.trim(),
          requestId,
          selections: refundSelections,
        }),
      });
      await refreshMoney();
      setRefundAmount("");
      setRefundQuantities({});
      setRefundReason("");
      setRefundRequest(null);
      setRefundMessage("Запрос принят. Результат возврата указан в истории ниже.");
    } catch (error) {
      onError((error as Error).message);
      await refreshMoney().catch(() => {});
    } finally {
      setRefundBusy(false);
    }
  };
  const resolveRefund = async (id: number) => {
    setRefundBusy(true);
    try {
      await api(`/api/v1/admin/orders/${order.id}/refunds/${id}/resolve`, {
        method: "POST",
        body: JSON.stringify({ providerId: refundProviderIDs[id]?.trim() }),
      });
      await refreshMoney();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRefundBusy(false);
    }
  };
  const settleReceipt = async (paymentId: number) => {
    if (!window.confirm("Товар уже выдан покупателю? Сформировать чек зачёта предоплаты в ЮKassa?"))
      return;
    setRefundBusy(true);
    try {
      await api(`/api/v1/admin/orders/${order.id}/payments/${paymentId}/settlement-receipt`, {
        method: "POST",
        body: JSON.stringify({ confirmed: true }),
      });
      await refreshMoney();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRefundBusy(false);
    }
  };
  const load = async () => {
    setLoadError("");
    try {
      const [state, catalog] = await Promise.all([
        api<{ order: Adjustment; payment: PaymentBalance }>(
          `/api/v1/admin/orders/${order.id}/adjustment`,
        ),
        api<{ products: Product[] }>("/api/v1/admin/products"),
      ]);
      setAdjustment(state.order);
      setPayment(state.payment ?? emptyPayment);
      setLines(state.order.items);
      setDeliveryFee(state.order.deliveryFee);
      setDiscountType(state.order.discountType ?? "fixed");
      setDiscountValue(state.order.discountValue ?? 0);
      setProducts(catalog.products.filter((product) => product.status === "published"));
    } catch (error) {
      setLoadError((error as Error).message);
      onError((error as Error).message);
    }
  };

  useEffect(() => {
    void load();
  }, [order.id, order.status]); // eslint-disable-line react-hooks/exhaustive-deps

  const availableProducts = useMemo(() => {
    const used = new Set(lines.map((line) => line.sku));
    return products.filter((product) => !used.has(product.sku));
  }, [products, lines]);

  const draftSubtotal = useMemo(
    () => lines.reduce((total, line) => total + line.unitPrice * line.quantity, 0),
    [lines],
  );
  const grossMinor = Math.round(draftSubtotal * 100);
  const discountAmount =
    Math.min(
      grossMinor,
      discountType === "percent"
        ? Math.round((grossMinor * Math.round(discountValue * 100)) / 10000)
        : Math.round(discountValue * 100),
    ) / 100;
  const draftTotal =
    draftSubtotal -
    discountAmount +
    (adjustment?.deliveryPayee === "carrier" ? 0 : Math.max(0, deliveryFee));

  const compositionSaved = useMemo(() => {
    if (!adjustment || adjustment.items.length !== lines.length) return false;
    return adjustment.items.every(
      (line, index) => line.sku === lines[index]?.sku && line.quantity === lines[index]?.quantity,
    );
  }, [adjustment, lines]);
  const deliverySaved = !!adjustment && Math.abs(deliveryFee - adjustment.deliveryFee) < 0.005;
  const hasUnsavedChanges =
    !!adjustment &&
    (!compositionSaved ||
      !deliverySaved ||
      discountType !== (adjustment.discountType ?? "fixed") ||
      Math.abs(discountValue - (adjustment.discountValue ?? 0)) >= 0.005);
  const hasAnyUnsavedChanges = hasUnsavedChanges || packingDirty;

  useEffect(
    () =>
      registerBeforeLeave?.(async () => {
        if (busy) {
          onError("Подождите завершения операции с заказом.");
          return false;
        }
        return (
          !hasAnyUnsavedChanges ||
          window.confirm("В заказе есть несохранённые изменения. Уйти без сохранения?")
        );
      }),
    [hasAnyUnsavedChanges, busy, registerBeforeLeave, onError],
  );

  useEffect(() => {
    if (!hasAnyUnsavedChanges && !busy) return;
    const preventUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", preventUnload);
    return () => window.removeEventListener("beforeunload", preventUnload);
  }, [hasAnyUnsavedChanges, busy]);

  useEffect(() => {
    onDirtyChange?.(hasAnyUnsavedChanges || busy);
  }, [hasAnyUnsavedChanges, busy, onDirtyChange]);
  useEffect(() => {
    if (!adjustment) return;
    onReviewState?.({
      hasPreorder: adjustment.hasPreorder,
      deliveryFeePending: adjustment.deliveryFeePending,
      packagingDimensionsMissing:
        !packingBoxesReady &&
        adjustment.items.some((item) =>
          [
            item.packageLengthCm,
            item.packageWidthCm,
            item.packageHeightCm,
            item.packageWeightGrams,
          ].some((value) => !Number.isFinite(value) || value <= 0),
        ),
      cdekCreateState: adjustment.cdekCreateState,
      cdekLastError: adjustment.cdekLastError,
      paymentIssues: payment.issues ?? [],
    });
  }, [adjustment, payment.issues, onReviewState, packingBoxesReady]);

  const compositionChanged = () => setSaved(false);

  const discardChanges = () => {
    if (!adjustment) return;
    setLines(adjustment.items);
    setDeliveryFee(adjustment.deliveryFee);
    setDiscountType(adjustment.discountType ?? "fixed");
    setDiscountValue(adjustment.discountValue ?? 0);
    setAddProduct("");
    compositionChanged();
  };

  const changeQuantity = (index: number, quantity: number) => {
    setLines((current) =>
      current.map((line, position) =>
        position === index
          ? { ...line, quantity: Math.max(1, Math.min(100, quantity || 1)) }
          : line,
      ),
    );
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
    const product = products.find((item) => item.sku === addProduct);
    if (!product) return;
    setLines((current) => [
      ...current,
      {
        id: 0,
        productId: product.id,
        sku: product.sku,
        variantLabel: product.variantLabel,
        productName: product.name,
        unitPrice: product.price,
        quantity: 1,
        packageLengthCm: product.packageLengthCm ?? 0,
        packageWidthCm: product.packageWidthCm ?? 0,
        packageHeightCm: product.packageHeightCm ?? 0,
        packageWeightGrams: product.packageWeightGrams ?? 0,
      },
    ]);
    setAddProduct("");
    compositionChanged();
  };

  const save = async () => {
    setBusy(true);
    setSaving(true);
    try {
      // Редактор доступен только сотруднику с orders.edit. Поэтому одно
      // действие «Сохранить изменения» одновременно подтверждает и состав,
      // и указанную менеджером стоимость доставки. Отдельный флаг здесь
      // только создавал ложное состояние «менеджер сохранил, но не подтвердил».
      const body = {
        items: lines.map((line) => ({ sku: line.sku, quantity: line.quantity })),
        ...(order.deliveryMethod === "cdek" ? {} : { deliveryFee }),
        discount: { type: discountType, value: discountValue },
      };
      const result = await api<{ order: Order; adjustment: Adjustment; payment: PaymentBalance }>(
        `/api/v1/admin/orders/${order.id}/contents`,
        { method: "PATCH", body: JSON.stringify(body) },
      );
      setAdjustment(result.adjustment);
      setLines(result.adjustment.items);
      setDeliveryFee(result.adjustment.deliveryFee);
      setDiscountType(result.adjustment.discountType ?? "fixed");
      setDiscountValue(result.adjustment.discountValue ?? 0);
      setPayment(result.payment);
      setSaved(true);
      onSaved({ ...result.order, paymentStatus: result.payment.paymentStatus });
    } catch (error) {
      // Не откатываем черновик на экране: менеджер должен видеть, что именно
      // пытался сохранить, и иметь возможность повторить после исправления.
      onError((error as Error).message);
    } finally {
      setBusy(false);
      setSaving(false);
    }
  };

  if (!adjustment)
    return (
      <div className="order-workspace-loading" role={loadError ? "alert" : "status"}>
        <p>{loadError || "Загружаем состав и оплату заказа…"}</p>
        {loadError ? (
          <button type="button" className="admin-action" onClick={() => void load()}>
            Попробовать снова
          </button>
        ) : (
          <div aria-hidden="true" className="order-workspace-skeleton" />
        )}
      </div>
    );

  return (
    <div className="admin-order-editor">
      {(payment.paid > 0 || (payment.refunds ?? []).length > 0) && (
        <section className="admin-block" aria-label="Оплата и возвраты">
          <h3>Оплата и возвраты</h3>
          <p>
            Получено: {money.format(payment.paid)} · Возвращено: {money.format(payment.refunded)}
          </p>
          {payment.overpaid > 0 && (
            <p className="admin-flag">
              Переплата: {money.format(payment.overpaid)}. Возврат подтверждает владелец.
            </p>
          )}
          {refundMessage && <p role="status">{refundMessage}</p>}
          {(payment.fiscalTasks ?? []).map((task) => (
            <article key={`${task.paymentId}:${task.kind}`}>
              <strong>
                {task.kind === "income" ? "Чек прихода" : "Чек зачёта предоплаты"} · платёж №
                {task.paymentId}
              </strong>
              <p>
                {(
                  {
                    ready: "Ожидает формирования чека зачёта в ЮKassa",
                    failed: "Чек не принят — нужна сверка в ЮKassa",
                    manual_review: "Нужна ручная сверка чека в ЮKassa",
                    succeeded: "Зарегистрирован ЮKassa",
                    pending: "Ожидает регистрации ЮKassa",
                    canceled: "Не зарегистрирован — нужна проверка в ЮKassa",
                    unknown: "Регистрация ЮKassa пока не подтверждена",

                    legacy_unverified: "Исторический платёж: сверьте чек в ЮKassa",
                  } as Record<string, string>
                )[task.status] ?? "Требуется проверка"}
                {task.reference ? ` · ${task.reference}` : ""}
              </p>
              {canRefund &&
                task.kind === "settlement" &&
                ["ready", "pending"].includes(task.status) && (
                  <button
                    type="button"
                    disabled={refundBusy}
                    onClick={() => void settleReceipt(task.paymentId)}
                  >
                    {task.status === "ready"
                      ? "Сформировать чек зачёта в ЮKassa"
                      : "Сверить / повторить запрос чека с тем же ключом"}
                  </button>
                )}
            </article>
          ))}
          {(payment.refunds ?? []).map((refund) => (
            <article key={refund.id}>
              <p>
                <strong>{money.format(refund.amount)}</strong> ·{" "}
                {refund.needsReview
                  ? "Нужна сверка в ЮKassa"
                  : ({
                      pending: "Ожидает подтверждения ЮKassa",
                      succeeded: "Деньги возвращены",
                      canceled: "Возврат отменён ЮKassa",
                      failed: "Запрос отклонён",
                    }[refund.status] ?? refund.status)}
              </p>
              {refund.actorId && <small>Сотрудник № {refund.actorId}</small>}
              {refund.providerId && <small> · ID ЮKassa: {refund.providerId}</small>}
              {refund.lastError && <p>{refund.lastError}</p>}
              {refund.needsReview && canRefund && (
                <div className="admin-form-grid">
                  <label>
                    ID возврата в ЮKassa
                    <input
                      value={refundProviderIDs[refund.id] ?? ""}
                      onChange={(event) =>
                        setRefundProviderIDs((current) => ({
                          ...current,
                          [refund.id]: event.target.value,
                        }))
                      }
                      disabled={refundBusy}
                    />
                  </label>
                  <button
                    type="button"
                    disabled={refundBusy || !refundProviderIDs[refund.id]?.trim()}
                    onClick={() => void resolveRefund(refund.id)}
                  >
                    Сверить с ЮKassa
                  </button>
                </div>
              )}
              {refund.receiptStatus && (
                <p>
                  Чек:{" "}
                  {(
                    {
                      succeeded: "зарегистрирован ЮKassa",
                      pending: "ожидает регистрации",
                      canceled: "не зарегистрирован — нужна проверка в ЮKassa",
                      unknown: "статус регистрации не подтверждён",
                      legacy_unverified: "Сверьте регистрацию чека в ЮKassa",
                    } as Record<string, string>
                  )[refund.receiptStatus] ?? "нужна проверка"}
                </p>
              )}
            </article>
          ))}
          {canRefund && payment.netPaid > 0 && (
            <details>
              <summary>Вернуть деньги покупателю</summary>
              {(payment.refunds ?? []).some((refund) => refund.status === "pending") && (
                <p>Новый возврат доступен после завершения предыдущего.</p>
              )}
              {(payment.refundOptions ?? []).length > 0 && (
                <fieldset>
                  <legend>Позиции исходных платежей</legend>
                  <p>
                    Выберите возвращаемое количество. Цены взяты из сохранённых платежей, включая
                    скидки. Доставка выбирается отдельно.
                  </p>
                  {payment.refundOptions!.map((item) => (
                    <label key={`${item.paymentId}:${item.itemIndex}`}>
                      {item.name} · {money.format(item.price)} · платёж №{item.paymentId}
                      <NumericInput
                        aria-label={`Вернуть: ${item.name}, платёж ${item.paymentId}`}
                        min="0"
                        max={item.quantity}
                        step="1"
                        value={refundQuantities[`${item.paymentId}:${item.itemIndex}`] ?? 0}
                        disabled={refundBusy}
                        onChange={(event) =>
                          setRefundQuantities((current) => ({
                            ...current,
                            [`${item.paymentId}:${item.itemIndex}`]: Math.max(
                              0,
                              Math.min(item.quantity, Math.trunc(Number(event.target.value) || 0)),
                            ),
                          }))
                        }
                      />
                    </label>
                  ))}
                </fieldset>
              )}
              <div className="admin-form-grid">
                <label>
                  Сумма возврата, ₽
                  <NumericInput
                    min="0.01"
                    step="0.01"
                    max={payment.netPaid}
                    value={refundSelections.length ? requestedRefund : refundAmount}
                    disabled={refundBusy || refundSelections.length > 0}
                    onChange={(event) => setRefundAmount(event.target.value)}
                  />
                </label>
                <label>
                  Причина возврата
                  <input
                    value={refundReason}
                    disabled={refundBusy}
                    onChange={(event) => setRefundReason(event.target.value)}
                  />
                </label>
              </div>
              <button
                type="button"
                disabled={
                  disabled ||
                  busy ||
                  refundBusy ||
                  hasUnsavedChanges ||
                  !Number.isFinite(requestedRefund) ||
                  requestedRefund <= 0 ||
                  requestedRefund > payment.netPaid ||
                  !refundReason.trim() ||
                  (payment.refunds ?? []).some((refund) => refund.status === "pending")
                }
                onClick={() => void sendRefund()}
              >
                {refundBusy ? "Отправляем запрос…" : "Вернуть через ЮKassa"}
              </button>
            </details>
          )}
          {(payment.refunds ?? []).some((refund) => refund.status === "pending") && (
            <button
              type="button"
              disabled={refundBusy}
              onClick={() =>
                void refreshMoney().catch((error) => onError((error as Error).message))
              }
            >
              Обновить статус возврата
            </button>
          )}
        </section>
      )}
      {order.deliveryMethod === "cdek" && readOnly && (
        <section className="admin-block" aria-label="Стоимость заказа и доставки">
          <dl className="order-packing-total">
            <div>
              <dt>Товары</dt>
              <dd>{money.format(adjustment.subtotal)}</dd>
            </div>
            <div>
              <dt>
                Доставка{adjustment.deliveryPayee === "carrier" ? " · перевозчику отдельно" : ""}
              </dt>
              <dd>{money.format(adjustment.deliveryFee)}</dd>
            </div>
            <div>
              <dt>Итого{adjustment.deliveryPayee === "carrier" ? "" : " с доставкой"}</dt>
              <dd>
                {money.format(
                  adjustment.subtotal +
                    (adjustment.deliveryPayee === "carrier" ? 0 : adjustment.deliveryFee),
                )}
              </dd>
            </div>
          </dl>
        </section>
      )}
      {order.deliveryMethod === "cdek" && !readOnly && (
        <AdminOrderPacking
          orderId={order.id}
          items={adjustment.items}
          subtotal={adjustment.subtotal}
          netPaid={payment.netPaid}
          email={order.email}
          address={order.address}
          disabled={disabled || busy}
          compositionDirty={hasUnsavedChanges}
          onDirtyChange={setPackingDirty}
          onBoxesReadyChange={setPackingBoxesReady}
          onBusyChange={setBusy}
          onOrderUpdated={onSaved}
          onStateChange={(state) => {
            setPackingFrozen(!["draft", "cancelled", "stale"].includes(state.state));
            if (state.state !== "draft") void load();
          }}
        />
      )}
      <section className="admin-block">
        <div className="admin-block-heading">
          <div>
            <h3 id="order-composition" tabIndex={-1}>
              Состав заказа
            </h3>
            {packingFrozen && <small>Упаковка подтверждена</small>}
          </div>
          <span className="order-line-count">Позиций: {lines.length}</span>
        </div>
        {lines.map((line, index) => (
          <div className="admin-order-edit-line" key={`${line.sku}-${index}`}>
            <span>
              <strong>{line.productName}</strong>
              {line.variantLabel && <small>{line.variantLabel}</small>}
              <small>{money.format(line.unitPrice)} / шт.</small>
            </span>
            <NumericInput
              aria-label={`Количество ${line.productName}`}
              min="1"
              max="100"
              value={line.quantity}
              disabled={formDisabled}
              onChange={(event) => changeQuantity(index, Number(event.target.value))}
            />
            <strong>{money.format(line.unitPrice * line.quantity)}</strong>
            <button
              type="button"
              className="admin-action"
              disabled={formDisabled}
              onClick={() => removeLine(index)}
            >
              Удалить
            </button>
          </div>
        ))}
        <div className="admin-order-add-line">
          <AdminOrderProductSearch
            products={availableProducts}
            value={addProduct}
            disabled={formDisabled}
            onChange={setAddProduct}
          />
          <button
            type="button"
            className="admin-action"
            disabled={formDisabled || !addProduct}
            onClick={appendProduct}
          >
            Добавить
          </button>
        </div>
        <div className="order-discount">
          <label>
            Скидка на товары
            <NumericInput
              aria-label="Скидка на заказ"
              min="0"
              max={discountType === "percent" ? 100 : draftSubtotal}
              step="0.01"
              value={discountValue}
              disabled={formDisabled || payment.paid > 0}
              onChange={(event) => {
                setDiscountValue(Math.max(0, Number(event.target.value) || 0));
                compositionChanged();
              }}
            />
          </label>
          <select
            aria-label="Единица скидки"
            value={discountType}
            disabled={formDisabled || payment.paid > 0}
            onChange={(event) => {
              setDiscountType(event.target.value as "fixed" | "percent");
              setDiscountValue(0);
              compositionChanged();
            }}
          >
            <option value="fixed">₽</option>
            <option value="percent">%</option>
          </select>
          {discountAmount > 0 && <strong>−{money.format(discountAmount)}</strong>}
        </div>
        <div className="admin-order-draft-total">
          <small>
            {hasUnsavedChanges
              ? "После сохранения эта сумма станет итогом заказа"
              : adjustment.deliveryPayee === "carrier"
                ? "Доставка оплачивается отдельно перевозчику"
                : "Растения и доставка"}
          </small>
          <span>
            {hasUnsavedChanges
              ? adjustment.deliveryPayee === "carrier"
                ? "Новая сумма к оплате за растения"
                : "Новая сумма к оплате"
              : "Сумма заказа"}
          </span>
          <strong>{money.format(draftTotal)}</strong>
        </div>
      </section>

      {!["pickup", "cdek"].includes(order.deliveryMethod) && (
        <details className="order-manual-actions">
          <summary>Стоимость доставки вручную</summary>
          <section className="admin-block">
            <h3>Стоимость доставки</h3>
            <div className="admin-form-grid">
              <label>
                {adjustment.deliveryPayee === "carrier"
                  ? "Стоимость доставки, справочно, ₽"
                  : "Стоимость доставки, ₽"}
                <NumericInput
                  min="0"
                  step="1"
                  value={deliveryFee}
                  disabled={formDisabled}
                  onChange={(event) => {
                    setDeliveryFee(Math.max(0, Number(event.target.value)));
                    compositionChanged();
                  }}
                />
              </label>
            </div>
            <small>
              {adjustment.deliveryPayee === "carrier"
                ? "Доставка оплачивается перевозчику при получении и не входит в сумму оплаты растений."
                : "Стоимость доставки входит в сумму оплаты заказа."}
            </small>
            {order.deliveryMethod === "cdek" && adjustment.cdekCreateState === "unknown" && (
              <p className="admin-flag">
                СДЭК не подтвердил создание. Система ищет заявку по номеру заказа без повторной
                отправки.
              </p>
            )}
            {order.deliveryMethod === "cdek" && adjustment.cdekCreateState === "manual_review" && (
              <p className="admin-flag">
                {adjustment.cdekLastError?.includes("остатка растений недостаточно")
                  ? `Отправка оплаченного заказа ${adjustment.orderNumber} остановлена: остатка растений недостаточно. Проверьте наличие и решите вручную.`
                  : `Проверьте заказ ${adjustment.orderNumber} в кабинете СДЭК. Новая заявка автоматически не создаётся.`}
              </p>
            )}
            {order.deliveryMethod === "cdek" && adjustment.cdekStatus && (
              <small>Статус СДЭК: {adjustment.cdekStatusReason || adjustment.cdekStatus}</small>
            )}
            {order.deliveryMethod === "cdek" &&
              adjustment.cdekLastError &&
              ["unknown", "manual_review", "retry"].includes(adjustment.cdekCreateState) &&
              !(
                adjustment.cdekCreateState === "manual_review" &&
                adjustment.cdekLastError.includes("остатка растений недостаточно")
              ) && <small>{adjustment.cdekLastError}</small>}
          </section>
        </details>
      )}

      {(hasUnsavedChanges || saved) && (
        <div className="dialog-actions order-editor-savebar">
          <span role="status">
            {packingFrozen
              ? "Упаковка подтверждена, состав и доставка зафиксированы"
              : readOnly
                ? "Редактирование состава закрыто для этого статуса"
                : hasUnsavedChanges
                  ? "Есть несохранённые изменения"
                  : saved
                    ? "Изменения сохранены"
                    : "Состав и стоимость сохраняются вместе"}
          </span>
          {hasUnsavedChanges && (
            <button type="button" disabled={formDisabled} onClick={discardChanges}>
              Отменить изменения
            </button>
          )}
          {!compositionReadOnly && (
            <button type="button" className="primary" disabled={formDisabled} onClick={save}>
              {saving ? "Сохраняем…" : "Сохранить изменения"}
            </button>
          )}
        </div>
      )}

      {adjustment.shipmentOffers?.some(
        (offer) =>
          !["draft", "packaging_required", "stale", "cancelled", "expired"].includes(offer.status),
      ) && (
        <details className="order-partial-shipments">
          <summary>
            <span>Отправления</span>
            <small>
              {adjustment.shipmentOffers?.length
                ? `Отправок: ${adjustment.shipmentOffers.length}`
                : "Если заказ нужно отправить по частям"}
            </small>
          </summary>
          {hasUnsavedChanges && (
            <p className="admin-flag">Сохраните изменения состава перед подготовкой отправки.</p>
          )}
          <Suspense fallback={<p>Готовим частичные отправки…</p>}>
            <ShipmentOffers
              orderId={order.id}
              items={adjustment.items}
              offers={adjustment.shipmentOffers ?? []}
              deliveryMethod={adjustment.deliveryMethod}
              deliveryFee={adjustment.deliveryFee}
              cdekTariffCode={adjustment.cdekTariffCode}
              busy={busy || disabled}
              readOnly={compositionReadOnly || hasUnsavedChanges}
              setBusy={setBusy}
              onCreated={load}
              onError={onError}
            />
          </Suspense>
        </details>
      )}
    </div>
  );
}
