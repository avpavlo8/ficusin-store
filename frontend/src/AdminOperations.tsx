import { useEffect, useState } from "react";
import { api } from "./adminShared";

type OperationsResponse = {
  operations: { status: string; checks: Array<{ code: string; affected: number }> };
  diagnostics?: {
    overreserved: Array<{ variantId: number; productName: string; variantLabel: string; warehouseName: string; available: number; reserved: number; syncedAt: string; orderNumbers: string[] }>;
    failedReceipts: Array<{ batchId: number; orderId: number; orderNumber: string; lineId: number; productName: string; error: string }>;
  };
};

export function AdminOperations({ onOpenProcurement }: { onOpenProcurement: () => void }) {
  const [operations, setOperations] = useState<OperationsResponse | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api<OperationsResponse>("/api/v1/admin/operations")
      .then((result) => { if (active) setOperations(result); })
      .catch((caught) => { if (active) setError(caught instanceof Error ? caught.message : "Не удалось загрузить диагностику"); });
    return () => { active = false; };
  }, []);

  return <section className="workspace-panel" aria-label="Операционная диагностика">
    <header className="workspace-panel-heading"><div><p className="eyebrow">Контроль</p><h2>Остатки и поступления</h2></div></header>
    {error && <p role="alert">Диагностика недоступна: {error}</p>}
    {!operations && !error && <p>Проверяем состояние…</p>}
    {operations && <>
      <p>Состояние: {operations.operations.status === "ok" ? "без предупреждений" : operations.operations.status === "degraded" ? "нужна проверка" : "критическая ошибка"}.</p>
      {operations.operations.checks.map((check) => <p key={check.code}><strong>{check.code}</strong>: {check.affected}</p>)}
      {operations.diagnostics?.overreserved.map((item) => <article key={`${item.variantId}-${item.warehouseName}`} className="admin-alert"><div><strong>{item.productName} · {item.variantLabel}</strong><p>{item.warehouseName}: в СБИС {item.available}, зарезервировано {item.reserved}. Заказы: {item.orderNumbers.join(", ") || "не найдены"}. Синхронизация: {new Date(item.syncedAt).toLocaleString("ru-RU")}.</p></div></article>)}
      {operations.diagnostics?.failedReceipts.map((item) => <article key={`${item.batchId}-${item.lineId}`} className="admin-alert"><div><strong>Закупка {item.orderNumber || `№${item.orderId}`} · {item.productName}</strong><p>Партия {item.batchId}, строка {item.lineId}: {item.error || "Причина не сохранена"}</p><button type="button" onClick={onOpenProcurement}>Открыть закупки ↗</button></div></article>)}
    </>}
  </section>;
}
