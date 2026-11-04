import { useEffect, useState } from "react";
import { api } from "./adminShared";

type OperationsResponse = {
  operations: { status: string; checks: Array<{ code: string; affected: number }> };
  diagnostics?: {
    overreserved: Array<{
      variantId: number;
      productName: string;
      variantLabel: string;
      warehouseName: string;
      available: number;
      reserved: number;
      syncedAt: string;
      orderNumbers: string[];
    }>;
    failedReceipts: Array<{
      batchId: number;
      orderId: number;
      orderNumber: string;
      lineId: number;
      productName: string;
      error: string;
    }>;
  };
};

// Коды проверок приходят из backend/internal/operations/probe.go. Сотруднику
// нужен не код, а что случилось и что с этим делать.
const checkLabels: Record<string, string> = {
  inventory_overreserved: "Зарезервировано больше, чем есть в СБИС",
  stale_pending_payment: "Заказы давно ждут оплаты",
  metrika_delivery_review: "Метрика: нужно проверить отправку",
  metrika_delivery_overdue: "Метрика: события не отправлены вовремя",
  stale_outbox: "Обновления для каналов задерживаются",
  exhausted_outbox: "Обновления для каналов не отправились",
  cdek_manual_review: "СДЭК: отправления ждут ручной проверки",
  cdek_retry_overdue: "СДЭК: повторная отправка задерживается",
  payment_receipt_required: "Оплаты без чека прихода",
  payment_settlement_receipt_required: "Нужен чек зачёта предоплаты",
  refund_manual_review: "Возвраты денег ждут проверки",
  refund_receipt_required: "Возвраты денег без чека",
  expired_procurement_lock: "Закупка зависла в обработке",
  duplicate_paid_payments: "Двойная оплата одного заказа",
};

export function AdminOperations({ onOpenProcurement }: { onOpenProcurement: () => void }) {
  const [operations, setOperations] = useState<OperationsResponse | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api<OperationsResponse>("/api/v1/admin/operations")
      .then((result) => {
        if (!result.operations || !Array.isArray(result.operations.checks))
          throw new Error("Ответ диагностики неполный");
        if (active) setOperations(result);
      })
      .catch((caught) => {
        if (active)
          setError(caught instanceof Error ? caught.message : "Не удалось загрузить диагностику");
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <section className="workspace-panel" aria-label="Операционная диагностика">
      <header className="workspace-panel-heading">
        <div>
          <h2>Требует внимания</h2>
        </div>
      </header>
      {error && <p role="alert">Диагностика недоступна: {error}</p>}
      {!operations && !error && <p>Проверяем состояние…</p>}
      {operations && (
        <>
          {operations.operations.checks.every((check) => check.affected === 0) ? (
            <p>Всё в порядке: проблем с остатками, оплатами и отправками нет.</p>
          ) : (
            <ul className="ws-checklist">
              {operations.operations.checks
                .filter((check) => check.affected > 0)
                .map((check) => (
                  <li key={check.code}>
                    <span>{checkLabels[check.code] || "Другая проверка системы"}</span>
                    <strong>{check.affected}</strong>
                  </li>
                ))}
            </ul>
          )}
          {operations.diagnostics?.overreserved.map((item) => (
            <article key={`${item.variantId}-${item.warehouseName}`} className="admin-alert">
              <div>
                <strong>
                  {item.productName} · {item.variantLabel}
                </strong>
                <p>
                  {item.warehouseName}: в СБИС {item.available}, зарезервировано {item.reserved}.
                  Заказы: {item.orderNumbers.join(", ") || "не найдены"}. Синхронизация:{" "}
                  {new Date(item.syncedAt).toLocaleString("ru-RU")}.
                </p>
              </div>
            </article>
          ))}
          {operations.diagnostics?.failedReceipts.map((item) => (
            <article key={`${item.batchId}-${item.lineId}`} className="admin-alert">
              <div>
                <strong>
                  Закупка {item.orderNumber || `№${item.orderId}`} · {item.productName}
                </strong>
                <p>
                  Партия {item.batchId}, строка {item.lineId}:{" "}
                  {item.error || "Причина не сохранена"}
                </p>
                <button type="button" onClick={onOpenProcurement}>
                  Открыть закупки ↗
                </button>
              </div>
            </article>
          ))}
        </>
      )}
    </section>
  );
}
