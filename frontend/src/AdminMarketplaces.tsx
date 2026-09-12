import { useCallback, useEffect, useState } from "react";
import { PageHeading, api } from "./adminShared";
import { WorkspaceState } from "./WorkspaceUI";
import type { IntegrationSyncStatus, ProcurementData } from "./adminTypes";

const channelName = (value: string) => ({ saby: "СБИС", wb: "Wildberries", ozon: "Ozon" }[value] || value);
const resourceName = (value: string) => value === "catalog" ? "Карточки и цены" : "Продажи";
const statusName = (value: string) => ({ pending: "Ожидает первой загрузки", queued: "В очереди", running: "Обновляется", ok: "Завершено", error: "Нужен повтор", disabled: "Не подключено" }[value] || value);
const when = (value?: string) => value ? new Date(value).toLocaleString("ru-RU") : "не было";

export function AdminMarketplaces({ onError }: { onError: (value: string) => void }) {
  const [data, setData] = useState<ProcurementData | null>(null);
  const [queued, setQueued] = useState<string[]>([]);
  const load = useCallback(() => api<ProcurementData>("/api/v1/admin/procurement").then(setData).catch((error) => onError((error as Error).message)), [onError]);
  useEffect(() => { void load(); const timer = window.setInterval(() => void load(), 15000); return () => window.clearInterval(timer); }, [load]);
  const request = async (channel: "saby" | "wb" | "ozon") => {
    setQueued((current) => [...new Set([...current, channel])]);
    try { await api(`/api/v1/admin/procurement/integrations/${channel}/catalog`, { method: "POST" }); await load(); }
    catch (error) { onError((error as Error).message); }
    finally { setQueued((current) => current.filter((item) => item !== channel)); }
  };
  if (!data) return <WorkspaceState kind="loading" title="Загружаем состояние обмена…" />;
  const lanes = data.integrationSync || [];
  return <>
    <PageHeading eyebrow="Интеграции" title="Маркетплейсы" text="Карточки, продажи и очередь обновлений из локальной базы." />
    <div className="integration-channel-grid">{(["ozon", "wb", "saby"] as const).map((channel) => {
      const health = data.integrationHealth.find((item) => item.channel === channel);
      const channelLanes = lanes.filter((item) => item.channel === channel);
      const running = channelLanes.some((item) => item.status === "running");
      const successes = channelLanes.map((item) => item.lastSuccessAt).filter((item): item is string => Boolean(item)).sort();
      const retries = channelLanes.map((item) => item.cooldownUntil || item.nextAttemptAt).filter((item): item is string => Boolean(item)).sort();
      return <article key={channel} className={health?.lastError ? "attention" : health?.configured ? "connected" : ""}>
        <div><strong>{channelName(channel)}</strong><span>{!health?.configured ? "Не подключён" : running ? "Обновляется" : health.lastError ? "Есть ошибка" : "Подключён"}</span></div>
        <small>Последний успешный обмен: {when(successes.at(-1))}</small>
        {retries[0] && <small>Следующая попытка: {when(retries[0])}</small>}
        <button className="secondary-button" disabled={!health?.configured || queued.includes(channel)} onClick={() => void request(channel)}>{queued.includes(channel) ? "Добавляем в очередь…" : channel === "wb" ? "Сопоставить из зеркала" : "Обновить данные"}</button>
      </article>;
    })}</div>
    <section className="admin-block marketplace-queue"><div className="admin-block-heading"><div><p className="eyebrow">Единая очередь</p><h2>История обмена</h2></div></div>
      {lanes.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Задача</th><th>Состояние</th><th>Попытка / успех</th><th>Границы данных</th><th>Следующий запуск</th></tr></thead><tbody>{lanes.map((item: IntegrationSyncStatus) => <tr key={`${item.channel}-${item.resource}`}>
        <td><strong>{resourceName(item.resource)}</strong><small>{channelName(item.channel)} · {item.priority === "interactive" ? "ручной приоритет" : "фоновая"}</small></td>
        <td><span className={`sync-status sync-${item.status}`}>{statusName(item.status)}</span>{item.lastError && <small className="sync-error">{item.lastError}</small>}</td>
        <td><small>Попытка: {when(item.lastAttemptAt)}</small><small>Успех: {when(item.lastSuccessAt)}</small></td>
        <td>{item.periodFrom && item.periodTo ? `${item.periodFrom} — ${item.periodTo}` : "Граница пока неизвестна"}<small>{item.latestEventAt ? `Последнее событие: ${when(item.latestEventAt)}` : "Последнее событие неизвестно"}</small></td>
        <td>{when(item.cooldownUntil || item.nextAttemptAt)}<small>{item.cooldownUntil ? "Пауза площадки" : item.nextDeepAt ? `Глубокая сверка: ${when(item.nextDeepAt)}` : ""}</small></td>
      </tr>)}</tbody></table></div> : <WorkspaceState kind="unknown" title="Состояние обмена ещё не создано" detail="Оно появится после применения миграции." />}
    </section>
    <p className="admin-hint procurement-note">Ноль новых строк означает только, что в полученном окне изменений не найдено. Полнота определяется границами периода, датой последнего события и отдельной глубокой сверкой.</p>
  </>;
}
