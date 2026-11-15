import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { PageHeading, api } from "./adminShared";
import AdminFlowwow from "./AdminFlowwow";
import { WorkspaceState } from "./WorkspaceUI";
import type { IntegrationSyncStatus, ProcurementData } from "./adminTypes";

const AdminAvito = lazy(() =>
  import("./AdminAvito").then((module) => ({ default: module.AdminAvito })),
);
const marketplaceChannels = [
  { id: "flowwow", label: "Flowwow" },
  { id: "wb", label: "Wildberries" },
  { id: "ozon", label: "Ozon" },
  { id: "avito", label: "Авито" },
] as const;

const channelName = (value: string) =>
  ({ saby: "СБИС", wb: "Wildberries", ozon: "Ozon" })[value] || value;
const resourceName = (value: string) => (value === "catalog" ? "Карточки и цены" : "Продажи");
const statusName = (value: string) =>
  ({
    pending: "Ожидает обновления",
    queued: "В очереди",
    running: "Обновляется",
    ok: "Завершено",
    error: "Нужен повтор",
    disabled: "Не подключено",
  })[value] || value;
const when = (value?: string) =>
  value ? new Date(value).toLocaleString("ru-RU", { timeZone: "Europe/Moscow" }) : "не было";

function MarketplaceOverview({
  onError,
  channel,
}: {
  onError: (value: string) => void;
  channel?: "wb" | "ozon";
}) {
  const [data, setData] = useState<ProcurementData | null>(null);
  const [queued, setQueued] = useState<string[]>([]);
  const load = useCallback(
    () =>
      api<ProcurementData>("/api/v1/admin/procurement")
        .then(setData)
        .catch((error) => onError((error as Error).message)),
    [onError],
  );
  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 15000);
    return () => window.clearInterval(timer);
  }, [load]);
  const request = async (channel: "saby" | "wb" | "ozon") => {
    setQueued((current) => [...new Set([...current, channel])]);
    try {
      await api(`/api/v1/admin/procurement/integrations/${channel}/catalog`, { method: "POST" });
      await load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setQueued((current) => current.filter((item) => item !== channel));
    }
  };
  if (!data) return <WorkspaceState kind="loading" title="Загружаем состояние обмена…" />;
  const lanes = (data.integrationSync || []).filter((item) => !channel || item.channel === channel);
  return (
    <>
      <PageHeading
        title={channel ? channelName(channel) : "Обзор обмена"}
        text={
          channel
            ? "Карточки, цены, продажи и состояние обмена с площадкой."
            : "Что отправилось на Ozon, Wildberries и в СБИС и что требует внимания."
        }
      />
      <div className="integration-channel-grid">
        {(channel ? [channel] : (["ozon", "wb", "saby"] as const)).map((channel) => {
          const health = data.integrationHealth.find((item) => item.channel === channel);
          const channelLanes = lanes.filter((item) => item.channel === channel);
          const enabledLanes = channelLanes.filter((item) => item.status !== "disabled");
          const running = enabledLanes.some((item) => item.status === "running");
          const pending = enabledLanes.some((item) => item.status === "pending");
          const hasError =
            enabledLanes.some((item) => item.status === "error") ||
            (Boolean(health?.lastError) && !pending);
          const partial = Boolean(
            health?.configured &&
              (channelLanes.length < 2 || channelLanes.some((item) => item.status === "disabled")),
          );
          const successes = enabledLanes
            .map((item) => item.lastSuccessAt)
            .filter((item): item is string => Boolean(item))
            .sort();
          const retries = enabledLanes
            .map((item) => item.cooldownUntil || item.nextAttemptAt)
            .filter((item): item is string => Boolean(item))
            .sort();
          return (
            <article
              key={channel}
              className={hasError ? "attention" : health?.configured ? "connected" : ""}
            >
              <div>
                <strong>{channelName(channel)}</strong>
                <span>
                  {!health?.configured
                    ? "Не подключён"
                    : running
                      ? "Обновляется"
                      : hasError
                        ? "Есть ошибка"
                        : pending
                          ? "Ожидает обновления"
                          : partial
                            ? "Частично"
                            : "Подключён"}
                </span>
              </div>
              <small>Последний успешный обмен: {when(successes.at(-1))}</small>
              {retries[0] && <small>Следующая попытка: {when(retries[0])}</small>}
              <button
                className="secondary-button"
                disabled={!health?.configured || queued.includes(channel)}
                onClick={() => void request(channel)}
              >
                {queued.includes(channel)
                  ? "Добавляем в очередь…"
                  : channel === "wb"
                    ? "Сопоставить из зеркала"
                    : "Обновить данные"}
              </button>
            </article>
          );
        })}
      </div>
      <section className="admin-block marketplace-queue">
        <div className="admin-block-heading">
          <div>
            <p className="eyebrow">Единая очередь · время Москвы</p>
            <h2>История обмена</h2>
          </div>
        </div>
        {lanes.length ? (
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>Задача</th>
                  <th>Состояние</th>
                  <th>Попытка / успех</th>
                  <th>Границы данных</th>
                  <th>Следующий запуск</th>
                </tr>
              </thead>
              <tbody>
                {lanes.map((item: IntegrationSyncStatus) => (
                  <tr key={`${item.channel}-${item.resource}`}>
                    <td>
                      <strong>{resourceName(item.resource)}</strong>
                      <small>
                        {channelName(item.channel)} ·{" "}
                        {item.priority === "interactive" ? "ручной приоритет" : "фоновая"}
                      </small>
                    </td>
                    <td>
                      <span className={`sync-status sync-${item.status}`}>
                        {statusName(item.status)}
                      </span>
                      {/* Текст ошибки — для разработчика: прячем под «Подробнее»,
                          чтобы таблица читалась, а детали оставались под рукой. */}
                      {item.lastError && (
                        <details className="sync-error">
                          <summary>Подробнее</summary>
                          <small>{item.lastError}</small>
                        </details>
                      )}
                    </td>
                    <td>
                      <small>Попытка: {when(item.lastAttemptAt)}</small>
                      <small>Успех: {when(item.lastSuccessAt)}</small>
                    </td>
                    <td>
                      {item.periodFrom && item.periodTo
                        ? `${item.periodFrom} — ${item.periodTo}`
                        : "Граница пока неизвестна"}
                      <small>
                        {item.latestEventAt
                          ? `Последнее событие: ${when(item.latestEventAt)}`
                          : "Последнее событие неизвестно"}
                      </small>
                    </td>
                    <td>
                      {when(item.cooldownUntil || item.nextAttemptAt)}
                      <small>
                        {item.cooldownUntil
                          ? "Пауза площадки"
                          : item.channel !== "wb" && item.nextDeepAt
                            ? `Глубокая сверка: ${when(item.nextDeepAt)}`
                            : ""}
                      </small>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <WorkspaceState
            kind="unknown"
            title="Состояние обмена ещё не создано"
            detail="Оно появится после применения миграции."
          />
        )}
      </section>
      <p className="admin-hint procurement-note">
        Ноль новых строк означает только, что в полученном окне изменений не найдено. Следите за
        границами периода и датой последнего события. СБИС и Ozon дополнительно проводят глубокую
        сверку; Wildberries перечитывает 90-дневное окно продаж при каждом обмене.
      </p>
    </>
  );
}

export function AdminMarketplaces({ onError }: { onError: (value: string) => void }) {
  const channel = new URLSearchParams(window.location.search).get("channel");
  const selected = marketplaceChannels.some((item) => item.id === channel) ? channel : "overview";
  return (
    <div className="marketplace-workspace">
      <nav className="marketplace-channel-nav" aria-label="Маркетплейсы">
        {marketplaceChannels.map((item) => (
          <a
            key={item.id}
            href={`/admin?section=marketplaces&channel=${item.id}`}
            aria-current={selected === item.id ? "page" : undefined}
          >
            {item.label}
          </a>
        ))}
        <a
          className="marketplace-overview-link"
          href="/admin?section=marketplaces"
          aria-current={selected === "overview" ? "page" : undefined}
        >
          Обзор обмена
        </a>
      </nav>
      <div className="marketplace-channel-content">
        {selected === "flowwow" ? (
          <AdminFlowwow />
        ) : selected === "avito" ? (
          <Suspense fallback={<WorkspaceState kind="loading" title="Загружаем Авито…" />}>
            <AdminAvito onError={onError} />
          </Suspense>
        ) : (
          <MarketplaceOverview
            key={selected}
            channel={selected === "wb" || selected === "ozon" ? selected : undefined}
            onError={onError}
          />
        )}
      </div>
    </div>
  );
}
