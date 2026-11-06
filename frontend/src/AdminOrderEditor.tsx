import { AdminOrderProductSearch } from "./AdminOrderProductSearch";
import { NumericInput } from "./NumericInput";
import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { api, money } from "./adminShared";
import type { Order, Product } from "./adminTypes";
import { AdminOrderPacking } from "./AdminOrderPacking";

// Заказ показывается в одном из режимов. «summary» — только чтение, всё на
// одном экране. Остальные — один блок правки, без соседних полей: сотрудник
// нажал «Изменить» у состава и видит только состав.
export type OrderView =
  | "summary"
  | "items"
  | "delivery"
  | "payment"
  | "shipments";

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
  import("./AdminShipmentOfferBuilder").then((module) => ({
    default: module.AdminShipmentOffers,
  })),
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
  view = "summary",
  onViewChange,
  nowBlock,
  statusControl,
  renderActions,
}: {
  canRefund?: boolean;
  order: Order;
  onSaved: (order: Order) => void;
  onError: (message: string) => void;
  registerBeforeLeave?: (guard: () => Promise<boolean>) => () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onReviewState?: (state: OrderReviewState) => void;
  disabled?: boolean;
  view?: OrderView;
  onViewChange?: (view: OrderView) => void;
  nowBlock?: ReactNode;
  statusControl?: ReactNode;
  renderActions?: (editTarget: OrderView | null) => ReactNode;
}) {
  const [adjustment, setAdjustment] = useState<Adjustment | null>(null);
  const [payment, setPayment] = useState<PaymentBalance>(emptyPayment);
  const [products, setProducts] = useState<Product[]>([]);
  const [lines, setLines] = useState<Adjustment["items"]>([]);
  const [deliveryFee, setDeliveryFee] = useState(0);
  const [discountType, setDiscountType] = useState<"fixed" | "percent">(
    "fixed",
  );
  const [discountValue, setDiscountValue] = useState(0);
  const [addProduct, setAddProduct] = useState("");
  const [busy, setBusy] = useState(false);
  const [renewing, setRenewing] = useState(false);
  const [renewNotice, setRenewNotice] = useState("");
  const [renewURL, setRenewURL] = useState("");
  const [saving, setSaving] = useState(false);
  const [packingBoxesReady, setPackingBoxesReady] = useState(false);
  const [packingDirty, setPackingDirty] = useState(false);
  const [packingFrozen, setPackingFrozen] = useState(false);
  const [packingLoaded, setPackingLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [saved, setSaved] = useState(false);
  const readOnly = ["cancelled", "completed", "shipped"].includes(order.status);
  const packingLoading =
    order.deliveryMethod === "cdek" && !readOnly && !packingLoaded;
  const compositionReadOnly = readOnly || packingFrozen || packingLoading;
  const formDisabled = busy || disabled || compositionReadOnly;

  const [refundAmount, setRefundAmount] = useState("");
  const [refundQuantities, setRefundQuantities] = useState<
    Record<string, number>
  >({});
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
      Math.round(item.price * 100) *
        (refundQuantities[`${item.paymentId}:${item.itemIndex}`] ?? 0),
    0,
  );
  const requestedRefund = refundSelections.length
    ? selectedMinor / 100
    : Number(refundAmount);
  const [refundReason, setRefundReason] = useState("");
  const [refundRequest, setRefundRequest] = useState<{
    signature: string;
    id: string;
  } | null>(null);
  const [refundMessage, setRefundMessage] = useState("");
  const [refundProviderIDs, setRefundProviderIDs] = useState<
    Record<number, string>
  >({});
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
    if (
      !Number.isFinite(amount) ||
      amount <= 0 ||
      amount > payment.netPaid ||
      !refundReason.trim()
    )
      return;
    if (
      !window.confirm(
        `Вернуть ${money.format(amount)} покупателю через ЮKassa?`,
      )
    )
      return;
    const signature = JSON.stringify({
      amount,
      reason: refundReason.trim(),
      selections: refundSelections,
    });
    const requestId =
      refundRequest?.signature === signature
        ? refundRequest.id
        : crypto.randomUUID();
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
      setRefundMessage(
        "Запрос принят. Результат возврата указан в истории ниже.",
      );
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
    if (
      !window.confirm(
        "Товар уже выдан покупателю? Сформировать чек зачёта предоплаты в ЮKassa?",
      )
    )
      return;
    setRefundBusy(true);
    try {
      await api(
        `/api/v1/admin/orders/${order.id}/payments/${paymentId}/settlement-receipt`,
        {
          method: "POST",
          body: JSON.stringify({ confirmed: true }),
        },
      );
      await refreshMoney();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRefundBusy(false);
    }
  };
  // Новая ссылка на оплату: прежняя перестаёт действовать, новую сервер
  // отправляет покупателю письмом и пуш-уведомлением.
  const renewPaymentLink = async () => {
    if (
      !window.confirm(
        "Создать новую ссылку на оплату? Прежняя ссылка перестанет работать, новую покупатель получит на почту и в уведомлении.",
      )
    )
      return;
    setRenewing(true);
    setRenewNotice("");
    setRenewURL("");
    try {
      const result = await api<{
        confirmationUrl: string;
        notice?: { email: string; push: string };
      }>(`/api/v1/admin/orders/${order.id}/payment-link/renew`, {
        method: "POST",
        body: "{}",
      });
      setRenewURL(result.confirmationUrl);
      const email =
        result.notice?.email === "queued"
          ? "Письмо со ссылкой добавлено в очередь отправки."
          : result.notice?.email === "no_email"
            ? "У заказа нет почты — передайте ссылку покупателю сами."
            : result.notice?.email === "failed"
              ? "Не удалось отправить письмо — передайте ссылку покупателю сами."
              : "Почта сейчас не настроена — передайте ссылку покупателю сами.";
      const push =
        result.notice?.push === "sent" ? " Уведомление тоже ушло." : "";
      setRenewNotice(
        `Новая ссылка создана, прежняя больше не действует. ${email}${push}`,
      );
      await refreshMoney().catch(() => {});
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRenewing(false);
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
      setProducts(
        catalog.products.filter((product) => product.status === "published"),
      );
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
    () =>
      lines.reduce((total, line) => total + line.unitPrice * line.quantity, 0),
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
      (line, index) =>
        line.sku === lines[index]?.sku &&
        line.quantity === lines[index]?.quantity,
    );
  }, [adjustment, lines]);
  const deliverySaved =
    !!adjustment && Math.abs(deliveryFee - adjustment.deliveryFee) < 0.005;
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
          window.confirm(
            "В заказе есть несохранённые изменения. Уйти без сохранения?",
          )
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
        items: lines.map((line) => ({
          sku: line.sku,
          quantity: line.quantity,
        })),
        ...(order.deliveryMethod === "cdek" ? {} : { deliveryFee }),
        discount: { type: discountType, value: discountValue },
      };
      const result = await api<{
        order: Order;
        adjustment: Adjustment;
        payment: PaymentBalance;
      }>(`/api/v1/admin/orders/${order.id}/contents`, {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      setAdjustment(result.adjustment);
      setLines(result.adjustment.items);
      setDeliveryFee(result.adjustment.deliveryFee);
      setDiscountType(result.adjustment.discountType ?? "fixed");
      setDiscountValue(result.adjustment.discountValue ?? 0);
      setPayment(result.payment);
      setSaved(true);
      setRenewURL("");
      setRenewNotice("");
      onSaved({ ...result.order, paymentStatus: result.payment.paymentStatus });
      // Сохранили — возвращаем к просмотру: сотрудник сразу видит итог.
      onViewChange?.("summary");
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
      <div
        className="order-workspace-loading"
        role={loadError ? "alert" : "status"}
      >
        <p>{loadError || "Загружаем состав и оплату заказа…"}</p>
        {loadError ? (
          <button
            type="button"
            className="admin-action"
            onClick={() => void load()}
          >
            Попробовать снова
          </button>
        ) : (
          <div aria-hidden="true" className="order-workspace-skeleton" />
        )}
      </div>
    );

  const hasShipments = !!adjustment.shipmentOffers?.some(
    (offer) =>
      ![
        "draft",
        "packaging_required",
        "stale",
        "cancelled",
        "expired",
      ].includes(offer.status),
  );
  const hasMoneyHistory =
    payment.paid > 0 || (payment.refunds ?? []).length > 0;
  const canEditDelivery =
    !readOnly &&
    order.deliveryMethod !== "pickup" &&
    !(order.deliveryMethod !== "cdek" && packingFrozen);

  const deliveryLabel =
    order.deliveryMethod === "cdek"
      ? "СДЭК"
      : order.deliveryMethod === "pickup"
        ? "Самовывоз"
        : order.deliveryMethod === "courier"
          ? "Курьер"
          : order.deliveryMethod;
  const itemCount = adjustment.items.reduce(
    (sum, line) => sum + line.quantity,
    0,
  );

  // Просмотр заказа: в центре — состав и три кнопки под ним, справа —
  // статус, деньги и контакты. Поля ввода появляются только после
  // «Изменить заказ».
  const editTarget: OrderView | null = packingLoading
    ? null
    : !compositionReadOnly
      ? "items"
      : canEditDelivery
        ? "delivery"
        : null;
  const unpaid =
    !["cancelled", "completed"].includes(order.status) &&
    !["cash", "on_delivery", "invoice", "manager_confirmation"].includes(
      order.paymentMethod || "",
    ) &&
    [
      "pending",
      "payment_provider_pending",
      "partially_paid",
      "failed",
      "cancelled",
    ].includes(order.paymentStatus);
  const paymentPill =
    order.paymentStatus === "paid"
      ? { tone: "ok", text: "Оплачен" }
      : order.paymentStatus === "refunded"
        ? { tone: "muted", text: "Возвращён" }
        : order.paymentStatus === "partially_paid"
          ? { tone: "attention", text: "Частично оплачен" }
          : order.status === "cancelled"
            ? { tone: "muted", text: "Не требуется" }
            : order.paymentMethod === "on_delivery" ||
                order.paymentMethod === "cash"
              ? { tone: "muted", text: "При получении" }
              : { tone: "attention", text: "Не оплачен" };

  const summary =
    view === "summary" ? (
      <div className="admin-order-editor order-page-grid">
        <div className="order-page-main">
          {nowBlock}
          <section
            className="order-section"
            aria-labelledby="order-items-title"
          >
            <div className="order-section-head">
              <h3 id="order-items-title">Состав заказа</h3>
              {packingFrozen && <small>Упаковка подтверждена</small>}
            </div>
            <ul className="order-pick-list">
              {adjustment.items.map((line, index) => (
                <li key={`${line.sku}-${index}`}>
                  <span className="order-item-photo" aria-hidden="true" />
                  <span className="order-item-name">
                    <strong>{line.productName}</strong>
                    <small>
                      {[line.variantLabel, `артикул ${line.sku}`]
                        .filter(Boolean)
                        .join(" · ")}
                    </small>
                  </span>
                  <span className="order-item-qty">
                    {money.format(line.unitPrice)} × {line.quantity}
                  </span>
                  <strong className="order-item-sum">
                    {money.format(line.unitPrice * line.quantity)}
                  </strong>
                </li>
              ))}
            </ul>
            {renderActions?.(editTarget)}
          </section>
        </div>

        <aside className="order-page-aside" aria-label="Итог заказа">
          {statusControl}
          <section className="order-total-card">
            <div>
              <span>Товары</span>
              <span>{money.format(adjustment.subtotal)}</span>
            </div>
            <div>
              <span>Количество</span>
              <span>{itemCount} шт.</span>
            </div>
            {(adjustment.discountAmount ?? 0) > 0 && (
              <div>
                <span>Скидка</span>
                <span>−{money.format(adjustment.discountAmount ?? 0)}</span>
              </div>
            )}
            {order.deliveryMethod !== "pickup" && (
              <div>
                <span>Доставка</span>
                <span>
                  {adjustment.deliveryFeePending
                    ? "посчитаем"
                    : adjustment.deliveryPayee === "carrier"
                      ? "платит в СДЭК"
                      : money.format(adjustment.deliveryFee)}
                </span>
              </div>
            )}
            <div className="order-total-sum">
              <span>Итого</span>
              <span>{money.format(order.total)}</span>
            </div>
            <div className="order-total-payment">
              <span>Оплата</span>
              <span className="order-pay-pill" data-tone={paymentPill.tone}>
                {paymentPill.text}
              </span>
            </div>
            {payment.due > 0 && payment.paid > 0 && (
              <div>
                <span>Осталось оплатить</span>
                <span>{money.format(payment.due)}</span>
              </div>
            )}
            {payment.overpaid > 0 && (
              <p className="admin-flag">
                Переплата {money.format(payment.overpaid)} — нужен возврат.
              </p>
            )}
            {unpaid && (
              <button
                type="button"
                className="ws-button order-renew-link"
                disabled={busy || renewing}
                onClick={() => void renewPaymentLink()}
              >
                {renewing
                  ? "Создаём ссылку…"
                  : "Создать новую ссылку на оплату"}
              </button>
            )}
            {renewNotice && (
              <p role="status" className="order-renew-notice">
                {renewNotice}
              </p>
            )}
            {renewURL && (
              <div className="order-payment-link">
                <a
                  className="ws-link-button"
                  href={renewURL}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Открыть ссылку на оплату
                </a>
                <CopyField label="ссылку на оплату" value={renewURL} />
              </div>
            )}
            {(hasMoneyHistory || hasShipments) && (
              <div className="order-aside-actions">
                {hasMoneyHistory && (
                  <button
                    type="button"
                    className="ws-link-button"
                    onClick={() => onViewChange?.("payment")}
                  >
                    {canRefund
                      ? "Оплата, возврат и чеки"
                      : "Подробнее об оплате"}
                  </button>
                )}
                {hasShipments && (
                  <button
                    type="button"
                    className="ws-link-button"
                    onClick={() => onViewChange?.("shipments")}
                  >
                    Отправления · {adjustment.shipmentOffers.length}
                  </button>
                )}
              </div>
            )}
          </section>

          <section
            className="order-contacts"
            aria-label="Покупатель и доставка"
          >
            <span className="order-contacts-label">Покупатель</span>
            <CopyField
              label="имя"
              value={order.customerName || ""}
              empty="Имя не указано"
            />
            <CopyField label="телефон" value={order.phone} />
            <CopyField label="почту" value={order.email} />
            <span className="order-contacts-label">Доставка</span>
            <span>{deliveryLabel}</span>
            <CopyField label="адрес" value={order.address} />
            {order.repackRequested && (
              <span>Просьба: собрать в одну коробку</span>
            )}
            {order.deliveryMethod !== "pickup" && (
              <>
                <span className="order-contacts-label">Трек-номер</span>
                <CopyField
                  label="трек-номер"
                  value={order.trackNumber || ""}
                  empty="Появится после отправки"
                />
              </>
            )}
            {order.comment && (
              <>
                <span className="order-contacts-label">Комментарий</span>
                <CopyField
                  label="комментарий"
                  value={order.comment}
                  display={`«${order.comment}»`}
                />
              </>
            )}
            <div className="order-contacts-actions">
              {order.customerId ? (
                <a
                  className="ws-button"
                  href={`/admin?section=chat&q=${encodeURIComponent(order.customerName || "")}`}
                >
                  Написать в чат
                </a>
              ) : null}
              {order.phone && (
                <a className="ws-button order-call" href={`tel:${order.phone}`}>
                  Позвонить
                </a>
              )}
            </div>
          </section>
        </aside>
      </div>
    ) : null;

  const viewTitles: Record<Exclude<OrderView, "summary">, string> = {
    items: "Изменить состав",
    delivery:
      order.deliveryMethod === "cdek"
        ? "Коробки и доставка"
        : "Стоимость доставки",
    payment: "Оплата, возвраты и чеки",
    shipments: "Отправления",
  };
  const backToSummary = () => {
    if (
      hasUnsavedChanges &&
      !window.confirm(
        "Изменения не сохранены. Вернуться к заказу без сохранения?",
      )
    )
      return;
    discardChanges();
    onViewChange?.("summary");
  };

  const editor = (
    <div
      className={view === "summary" ? undefined : "admin-order-editor"}
      data-view={view}
    >
      <div className="ws-edit-head">
        <button
          type="button"
          className="ws-back"
          disabled={busy}
          onClick={backToSummary}
        >
          <svg
            viewBox="0 0 24 24"
            width="18"
            height="18"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            aria-hidden="true"
          >
            <path d="m14 6-6 6 6 6" />
          </svg>
          К заказу
        </button>
        <h3>{view !== "summary" ? viewTitles[view] : null}</h3>
      </div>
      {(view === "items" || view === "delivery") &&
        !compositionReadOnly &&
        canEditDelivery && (
          <nav className="order-edit-switch" aria-label="Что изменить">
            {(["items", "delivery"] as const).map((target) => (
              <button
                key={target}
                type="button"
                aria-current={view === target ? "page" : undefined}
                disabled={busy}
                onClick={() => onViewChange?.(target)}
              >
                {target === "items" ? "Состав" : viewTitles.delivery}
              </button>
            ))}
          </nav>
        )}
      {view === "delivery" && packingFrozen && (
        <p className="ws-edit-note">Упаковка подтверждена</p>
      )}
      {view === "payment" && (
        <section className="admin-block" aria-label="Оплата и возвраты">
          <h3>Оплата и возвраты</h3>
          <p>
            Получено: {money.format(payment.paid)} · Возвращено:{" "}
            {money.format(payment.refunded)}
          </p>
          {payment.overpaid > 0 && (
            <p className="admin-flag">
              Переплата: {money.format(payment.overpaid)}. Возврат подтверждает
              владелец.
            </p>
          )}
          {refundMessage && <p role="status">{refundMessage}</p>}
          {(payment.fiscalTasks ?? []).map((task) => (
            <article key={`${task.paymentId}:${task.kind}`}>
              <strong>
                {task.kind === "income"
                  ? "Чек прихода"
                  : "Чек зачёта предоплаты"}{" "}
                · платёж №{task.paymentId}
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

                    legacy_unverified:
                      "Исторический платёж: сверьте чек в ЮKassa",
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
              {refund.providerId && (
                <small> · ID ЮKassa: {refund.providerId}</small>
              )}
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
                    disabled={
                      refundBusy || !refundProviderIDs[refund.id]?.trim()
                    }
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
              {(payment.refunds ?? []).some(
                (refund) => refund.status === "pending",
              ) && <p>Новый возврат доступен после завершения предыдущего.</p>}
              {(payment.refundOptions ?? []).length > 0 && (
                <fieldset>
                  <legend>Позиции исходных платежей</legend>
                  <p>
                    Выберите возвращаемое количество. Цены взяты из сохранённых
                    платежей, включая скидки. Доставка выбирается отдельно.
                  </p>
                  {payment.refundOptions!.map((item) => (
                    <label key={`${item.paymentId}:${item.itemIndex}`}>
                      {item.name} · {money.format(item.price)} · платёж №
                      {item.paymentId}
                      <NumericInput
                        aria-label={`Вернуть: ${item.name}, платёж ${item.paymentId}`}
                        min="0"
                        max={item.quantity}
                        step="1"
                        value={
                          refundQuantities[
                            `${item.paymentId}:${item.itemIndex}`
                          ] ?? 0
                        }
                        disabled={refundBusy}
                        onChange={(event) =>
                          setRefundQuantities((current) => ({
                            ...current,
                            [`${item.paymentId}:${item.itemIndex}`]: Math.max(
                              0,
                              Math.min(
                                item.quantity,
                                Math.trunc(Number(event.target.value) || 0),
                              ),
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
                    value={
                      refundSelections.length ? requestedRefund : refundAmount
                    }
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
                  (payment.refunds ?? []).some(
                    (refund) => refund.status === "pending",
                  )
                }
                onClick={() => void sendRefund()}
              >
                {refundBusy ? "Отправляем запрос…" : "Вернуть через ЮKassa"}
              </button>
            </details>
          )}
          {(payment.refunds ?? []).some(
            (refund) => refund.status === "pending",
          ) && (
            <button
              type="button"
              disabled={refundBusy}
              onClick={() =>
                void refreshMoney().catch((error) =>
                  onError((error as Error).message),
                )
              }
            >
              Обновить статус возврата
            </button>
          )}
        </section>
      )}
      {order.deliveryMethod === "cdek" && !readOnly && (
        // Упаковка смонтирована всегда, даже в режиме просмотра: от её
        // состояния зависит, можно ли менять состав, а повторная загрузка
        // при каждом открытии сбрасывала бы черновик коробок.
        <div hidden={view !== "delivery"}>
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
              setPackingLoaded(true);
              setPackingFrozen(
                !["draft", "cancelled", "stale"].includes(state.state),
              );
              if (state.state !== "draft") void load();
            }}
          />
        </div>
      )}
      {view === "items" && (
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
                onChange={(event) =>
                  changeQuantity(index, Number(event.target.value))
                }
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
                  setDiscountValue(
                    Math.max(0, Number(event.target.value) || 0),
                  );
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
            {discountAmount > 0 && (
              <strong>−{money.format(discountAmount)}</strong>
            )}
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
      )}

      {view === "delivery" &&
        !["pickup", "cdek"].includes(order.deliveryMethod) && (
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
            {order.deliveryMethod === "cdek" &&
              adjustment.cdekCreateState === "unknown" && (
                <p className="admin-flag">
                  СДЭК не подтвердил создание. Система ищет заявку по номеру
                  заказа без повторной отправки.
                </p>
              )}
            {order.deliveryMethod === "cdek" &&
              adjustment.cdekCreateState === "manual_review" && (
                <p className="admin-flag">
                  {adjustment.cdekLastError?.includes(
                    "остатка растений недостаточно",
                  )
                    ? `Отправка оплаченного заказа ${adjustment.orderNumber} остановлена: остатка растений недостаточно. Проверьте наличие и решите вручную.`
                    : `Проверьте заказ ${adjustment.orderNumber} в кабинете СДЭК. Новая заявка автоматически не создаётся.`}
                </p>
              )}
            {order.deliveryMethod === "cdek" && adjustment.cdekStatus && (
              <small>
                Статус СДЭК:{" "}
                {adjustment.cdekStatusReason || adjustment.cdekStatus}
              </small>
            )}
            {order.deliveryMethod === "cdek" &&
              adjustment.cdekLastError &&
              ["unknown", "manual_review", "retry"].includes(
                adjustment.cdekCreateState,
              ) &&
              !(
                adjustment.cdekCreateState === "manual_review" &&
                adjustment.cdekLastError.includes(
                  "остатка растений недостаточно",
                )
              ) && <small>{adjustment.cdekLastError}</small>}
          </section>
        )}

      {["items", "delivery"].includes(view) &&
        (hasUnsavedChanges ||
          saved ||
          (view === "items" && adjustment?.hasPreorder)) && (
          <div className="dialog-actions order-editor-savebar">
            <span role="status">
              {packingFrozen
                ? "Упаковка подтверждена, состав и доставка зафиксированы"
                : readOnly
                  ? "Редактирование состава закрыто для этого статуса"
                  : hasUnsavedChanges
                    ? "Есть несохранённые изменения"
                    : view === "items" && adjustment?.hasPreorder
                      ? "Состав не менялся — можно перепроверить наличие"
                      : saved
                        ? "Изменения сохранены"
                        : "Состав и стоимость сохраняются вместе"}
            </span>
            {hasUnsavedChanges && (
              <button
                type="button"
                disabled={formDisabled}
                onClick={discardChanges}
              >
                Отменить изменения
              </button>
            )}
            {!compositionReadOnly && (
              <button
                type="button"
                className="primary"
                disabled={formDisabled}
                onClick={save}
              >
                {saving
                  ? "Сохраняем…"
                  : view === "items" &&
                      adjustment?.hasPreorder &&
                      !hasUnsavedChanges
                    ? "Перепроверить наличие"
                    : "Сохранить изменения"}
              </button>
            )}
          </div>
        )}

      {view === "shipments" && hasShipments && (
        <section className="admin-block order-partial-shipments">
          {hasUnsavedChanges && (
            <p className="admin-flag">
              Сохраните изменения состава перед подготовкой отправки.
            </p>
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
        </section>
      )}
    </div>
  );

  return (
    <>
      {summary}
      {/* Keep packing mounted: it owns the box draft and reports whether
          the prepared order is frozen even while its summary is visible. */}
      <div hidden={view === "summary"}>{editor}</div>
    </>
  );
}

// Поле, которое копируется по нажатию: телефон, почта, трек-номер. После
// нажатия на секунду показывает «Скопировано».
function CopyField({
  label,
  value,
  empty,
  display,
}: {
  label: string;
  value: string;
  empty?: string;
  display?: string;
}) {
  const [copied, setCopied] = useState(false);
  if (!value)
    return empty ? <span className="order-muted">{empty}</span> : null;
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      setCopied(false);
    }
  };
  return (
    <button
      type="button"
      className="order-copy"
      aria-label={`Скопировать ${label}: ${value}`}
      title="Нажмите, чтобы скопировать"
      onClick={() => void copy()}
    >
      <span>{display || value}</span>
      <span className="order-copy-mark" aria-hidden="true">
        {copied ? (
          "Скопировано"
        ) : (
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <rect x="9" y="9" width="11" height="11" rx="2" />
            <path d="M5 15V5a2 2 0 0 1 2-2h10" />
          </svg>
        )}
      </span>
    </button>
  );
}
