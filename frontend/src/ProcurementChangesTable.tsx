import { useId, useState } from "react";
import { money } from "./adminShared";
import type { ProcurementOrderDetail, ProcurementOrderLine } from "./adminTypes";

const channels = [
  { id: "site", name: "Сайт", action: "Выгрузить на сайт" },
  { id: "saby_price", name: "СБИС", action: "Выгрузить для СБИС" },
  { id: "ozon", name: "Ozon", action: "Выгрузить на Ozon" },
  { id: "wb", name: "Wildberries", action: "Выгрузить на Wildberries" },
] as const;

type Channel = (typeof channels)[number]["id"];
export type PublicationNotice = { text: string; error?: boolean; batchId?: number };

type ChangesRow = {
  key: string;
  name: string;
  line?: ProcurementOrderLine;
  fromUnified?: boolean;
  retail?: number;
  wb?: number;
  ozon?: number;
  pot?: number;
  height?: number;
  conflictingMeasurements: boolean;
};

const formatSize = (value: number) => `${value.toLocaleString("ru-RU")} см`;
const maxValue = (previous?: number, next?: number) =>
  next == null ? previous : previous == null ? next : Math.max(previous, next);

function changeRows(detail: ProcurementOrderDetail) {
  const received = detail.order.status === "received";
  const unifiedByVariant = new Map(
    (detail.unifiedPreview?.lines ?? []).map((line) => [line.variantId, line]),
  );
  const result = new Map<string, ChangesRow>();
  for (const line of detail.lines) {
    if (
      line.invoiceExcluded ||
      line.reconciliationStatus === "superseded" ||
      line.matchStatus !== "confirmed" ||
      (line.invoicedQuantity ?? line.orderedQuantity) <= 0 ||
      (!line.canonicalVariantId && !line.sabyId)
    )
      continue;
    const key = line.canonicalVariantId
      ? `variant-${line.canonicalVariantId}`
      : `saby-${line.sabyId}`;
    const unified =
      received && line.canonicalVariantId
        ? unifiedByVariant.get(line.canonicalVariantId)
        : undefined;
    const pot = line.dimensionsReviewed ? line.potDiameterCm : undefined;
    const height = line.dimensionsReviewed ? line.heightCm : undefined;
    const retail = line.proposedRetailRub;
    const wb = unified?.quotes.wb?.priceRub ?? line.channelQuotes?.wb?.priceRub;
    const ozon = unified?.quotes.ozon?.priceRub ?? line.channelQuotes?.ozon?.priceRub;
    const current = result.get(key);
    if (!current) {
      result.set(key, {
        key,
        name: line.catalogueName || line.sabyName || line.rawName,
        line,
        retail,
        wb,
        ozon,
        pot,
        height,
        conflictingMeasurements: false,
      });
      continue;
    }
    if ((retail ?? 0) > (current.retail ?? 0)) current.line = line;
    current.retail = maxValue(current.retail, retail);
    current.wb = maxValue(current.wb, wb);
    current.ozon = maxValue(current.ozon, ozon);
    current.conflictingMeasurements ||= current.pot != null && pot != null && current.pot !== pot;
    current.conflictingMeasurements ||=
      current.height != null && height != null && current.height !== height;
    current.pot ??= pot;
    current.height ??= height;
  }
  if (received) {
    for (const unified of unifiedByVariant.values()) {
      const key = `variant-${unified.variantId}`;
      if (!unified.variantId || result.has(key)) continue;
      const source = detail.lines.find((line) => line.id === unified.sourceLineId);
      const legacy = source?.sabyId ? result.get(`saby-${source.sabyId}`) : undefined;
      // A received line may resolve to multiple site variants after an assortment was split.
      if (legacy && source?.sabyId) result.delete(`saby-${source.sabyId}`);
      result.set(key, {
        key,
        name: unified.name,
        line: source,
        fromUnified: true,
        wb: unified.quotes.wb?.priceRub,
        ozon: unified.quotes.ozon?.priceRub,
        retail: legacy?.retail,
        conflictingMeasurements: false,
      });
    }
  }
  return [...result.values()];
}

function ChangeValue({
  before,
  after,
  format,
  unavailable = false,
}: {
  before?: number;
  after?: number;
  format: (value: number) => string;
  unavailable?: boolean;
}) {
  if (unavailable) return <span className="procurement-change-muted">Недоступно</span>;
  if (after == null || before === after) return before == null ? "—" : format(before);
  return (
    <>
      <span
        className="procurement-change-muted"
        title={before == null ? "Прежнее значение неизвестно" : undefined}
      >
        {before == null ? "—" : format(before)}
      </span>
      {" → "}
      <strong>{format(after)}</strong>
    </>
  );
}

function ChannelChanges({
  row,
  channel,
  threshold,
}: {
  row: ChangesRow;
  channel: Channel;
  threshold: number;
}) {
  const { line } = row;
  const remote = line?.publicationValues?.[channel];
  const isSite = channel === "site";
  const isSaby = channel === "saby_price";
  const isMarketplace = channel === "wb" || channel === "ozon";
  const oldPrice = isSite ? line?.currentRetailRub || undefined : remote?.priceRub;
  let price = isMarketplace ? row[channel] : row.retail;
  if (
    (!isSite &&
      remote?.linked !== true &&
      !(isMarketplace && row.fromUnified && price != null && !remote) &&
      !(isSaby && !line?.canonicalVariantId && line?.sabyId)) ||
    (isSite && !line?.canonicalVariantId && !row.fromUnified)
  ) {
    return <span className="procurement-change-muted">Нет связи</span>;
  }
  const quote = line?.channelQuotes?.[isSaby ? "site" : channel];
  if ((isSite && !line?.priceChangeNeeded) || (isSaby && remote?.priceEligible === false))
    price = oldPrice;
  if (
    (channel === "wb" || channel === "ozon") &&
    oldPrice != null &&
    price != null &&
    !quote?.manual &&
    oldPrice > (quote?.breakEvenRub ?? 0) &&
    Math.abs(price - oldPrice) <= oldPrice * threshold
  ) {
    price = oldPrice;
  }
  const pot = row.conflictingMeasurements ? undefined : row.pot;
  const height = row.conflictingMeasurements ? undefined : row.height;
  const sizeAvailable = isSite || remote?.sizeLinked === true;
  return (
    <dl className="procurement-change-values">
      <dt>Цена</dt>
      <dd>
        <ChangeValue before={oldPrice} after={price} format={money.format} />
      </dd>
      <dt>Горшок</dt>
      <dd>
        <ChangeValue
          before={isSite ? line?.cataloguePotDiameterCm : remote?.potDiameterCm}
          after={sizeAvailable ? pot : undefined}
          format={formatSize}
          unavailable={!sizeAvailable}
        />
      </dd>
      <dt>Высота</dt>
      <dd>
        <ChangeValue
          before={isSite ? line?.catalogueHeightCm : remote?.heightCm}
          after={sizeAvailable ? height : undefined}
          format={formatSize}
          unavailable={isSaby || !sizeAvailable}
        />
      </dd>
    </dl>
  );
}

export default function ProcurementChangesTable({
  detail,
  saving,
  notices,
  onPublish,
  onEditPrice,
  threshold,
}: {
  detail: ProcurementOrderDetail;
  saving: boolean;
  notices: Record<string, PublicationNotice>;
  onPublish: (channel: string) => void;
  onEditPrice: (line: ProcurementOrderLine) => void;
  threshold: number;
}) {
  const heading = useId();
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(20);
  const term = query.trim().toLocaleLowerCase("ru");
  const ready = detail.order.status === "ready_to_receive";
  const rows = changeRows(detail).filter(({ name, line }) =>
    [name, line?.rawName, line?.invoiceRawName, line?.supplierArticle, line?.sabyCode]
      .join(" ")
      .toLocaleLowerCase("ru")
      .includes(term),
  );
  const priceJobs = detail.batches.flatMap((batch) =>
    batch.kind === "prices" && batch.status !== "cancelled" ? batch.items : [],
  );
  const canPublish = (channel: Channel) =>
    ready
      ? detail.validation.canPrepareActions
      : channel === "saby_price" ||
        ((channel === "ozon" || channel === "wb") &&
          Boolean(
            detail.unifiedPreview?.units &&
              detail.unifiedPreview.units === detail.unifiedPreview.pricedUnits,
          ));
  return (
    <section className="procurement-price-review" aria-labelledby={heading}>
      <div className="procurement-changes-heading">
        <h3 id={heading}>Изменения по каналам</h3>
        {(rows.length > 10 || term) && (
          <input
            aria-label="Найти растение по названию или артикулу"
            placeholder="Название или артикул"
            type="search"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setLimit(20);
            }}
          />
        )}
      </div>
      <div className="procurement-price-channels">
        {channels.map(({ id, action }) => (
          <button
            key={id}
            type="button"
            className={id === "site" ? "admin-primary" : undefined}
            disabled={
              saving ||
              !canPublish(id) ||
              priceJobs.some(
                (item) => item.channel === id && ["queued", "processing"].includes(item.status),
              )
            }
            onClick={() => onPublish(id)}
          >
            {action}
          </button>
        ))}
        {ready && (
          <button
            type="button"
            disabled={saving || !detail.validation.canPrepareActions}
            onClick={() => onPublish("avito")}
          >
            Выгрузить на Авито
          </button>
        )}
      </div>
      <div className="procurement-publication-feedback" aria-live="polite">
        {Object.entries(notices).map(([channel, notice]) => {
          const batch = detail.batches.find((candidate) => candidate.id === notice.batchId);
          const failed = batch?.items.find((item) =>
            ["failed", "not_configured", "skipped"].includes(item.status),
          );
          const name = channels.find(({ id }) => id === channel)?.name ?? "Авито";
          const sizeNote = batch?.measurementsQueued
            ? ` Обновление размеров в очереди (${batch.measurementsQueued}).`
            : "";
          const message = failed
            ? `${name} · ${failed.errorMessage || "Часть цен не удалось выгрузить."}${sizeNote}`
            : batch?.status === "completed" &&
                batch.items.length &&
                batch.items.every((item) => item.status === "completed")
              ? `${name} · Цены выгружены.${sizeNote}`
              : notice.text;
          return (
            <p key={channel} className={notice.error || failed ? "procurement-warning" : undefined}>
              {message}
            </p>
          );
        })}
      </div>
      {rows.length ? (
        <div className="admin-table-wrap">
          <table
            className="admin-table procurement-price-review-table"
            aria-label="Изменения цен и размеров по каналам"
          >
            <thead>
              <tr>
                <th scope="col">Растение</th>
                {channels.map(({ id, name }) => (
                  <th scope="col" key={id}>
                    {name}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.slice(0, limit).map((row) => (
                <tr key={row.key}>
                  <th scope="row">
                    <strong>{row.name}</strong>
                    {row.conflictingMeasurements && (
                      <small className="procurement-warning">Сверьте замеры</small>
                    )}
                    {ready && row.line?.canonicalVariantId && (
                      <button
                        className="procurement-change-edit"
                        type="button"
                        disabled={saving}
                        onClick={() => row.line && onEditPrice(row.line)}
                      >
                        Изменить цену
                      </button>
                    )}
                  </th>
                  {channels.map(({ id, name }) => (
                    <td key={id} data-label={name}>
                      <ChannelChanges row={row} channel={id} threshold={threshold} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p>
          {term ? "Таких растений нет в списке." : "Нет сопоставленных растений для публикации."}
        </p>
      )}
      {rows.length > limit && (
        <button type="button" onClick={() => setLimit(limit + 20)}>
          Показать ещё
        </button>
      )}
      <small className="procurement-change-note">
        Для СБИС цены выгружаются в XLSX. Тире означает, что значение неизвестно.
      </small>
    </section>
  );
}
