import { useState } from "react";
import type { SalesSyncStatus } from "./adminTypes";
import { salesChannelLabel, salesSyncLabel } from "./AdminProcurementPanels";
import { salesDataProblems } from "./lib/procurement-state";

const date = (value: string) => new Date(value).toLocaleDateString("ru-RU");

export function ProcurementDataHealth({
  channels,
  onUnlinked,
  onIntegrations,
}: {
  channels: SalesSyncStatus[];
  onUnlinked: () => void;
  onIntegrations?: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const problems = salesDataProblems(channels);
  const failed = problems.filter(
    (item) => item.lastError || item.status === "error" || item.freshness === "stale",
  );
  const unlinked = channels.reduce((sum, item) => sum + (item.rowsUnlinked || 0), 0);
  return (
    <section
      className="procurement-data-health"
      data-warning={problems.length > 0 || undefined}
      aria-label="Качество данных продаж"
    >
      <div className="procurement-data-summary">
        <div>
          <strong>{problems.length ? "Рекомендации по неполным данным" : "Данные продаж"}</strong>
          <p>
            {failed.length > 0
              ? failed
                  .map(
                    (item) =>
                      `${salesChannelLabel(item.channel)}: ${item.lastSuccessAt ? `обновлено ${date(item.lastSuccessAt)}` : "нет успешной загрузки"}`,
                  )
                  .join(" · ") + ". Количества требуют проверки."
              : problems.length
                ? "Часть продаж не участвует в расчёте потребности."
                : "Проверьте актуальность каналов перед оформлением закупки."}
            {unlinked > 0 && ` Кодов без связи с товаром: ${unlinked}.`}
          </p>
        </div>
        <div className="procurement-data-actions">
          {unlinked > 0 && <button onClick={onUnlinked}>Сопоставить продажи</button>}
          <button aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
            {expanded ? "Свернуть" : "По каналам"}
          </button>
        </div>
      </div>
      {expanded && (
        <div className="procurement-data-channels">
          {channels.map((sync) => (
            <article key={sync.channel}>
              <div>
                <strong>{salesChannelLabel(sync.channel)}</strong>
                <span
                  className={
                    salesDataProblems([sync]).length ? "procurement-warning" : "procurement-ok"
                  }
                >
                  {sync.freshness === "stale" ? "Устарело" : salesSyncLabel(sync.status)}
                </span>
              </div>
              <p>
                {sync.lastSuccessAt
                  ? `Обновлено ${new Date(sync.lastSuccessAt).toLocaleString("ru-RU")}`
                  : "Успешной загрузки ещё не было"}
              </p>
              <p>
                {sync.latestSale
                  ? `Последняя продажа ${date(`${sync.latestSale}T00:00:00`)}`
                  : "Продаж за период нет"}{" "}
                · загружено {sync.rowsSynced}
              </p>
              {sync.rowsUnlinked > 0 && (
                <p>{sync.rowsUnlinked} кодов не сопоставлены и исключены из расчёта.</p>
              )}
              {(sync.lastError || sync.freshness === "stale") && (
                <p>
                  Проверьте подключение канала. До обновления в расчёте остаются ранее загруженные
                  продажи.
                </p>
              )}
              {sync.lastError && (
                <details>
                  <summary>Технические подробности</summary>
                  <pre>{sync.lastError}</pre>
                </details>
              )}
            </article>
          ))}
          {onIntegrations && <button onClick={onIntegrations}>Открыть интеграции</button>}
        </div>
      )}
    </section>
  );
}
