import { useEffect, useState } from "react";
import { ProcurementEconomicsFields, PricingQuoteSummary } from "./ProcurementEconomics";
import { api } from "./adminShared";
import type { ProcurementRequest, ProcurementSettings } from "./adminTypes";

export const availabilityLabel = (value: string) =>
  ({
    available: "Есть",
    check: "Проверить",
    temporarily_unavailable: "Временно нет",
    discontinued: "Снят с продажи",
    unknown: "Неизвестно",
  })[value] || value;

export const salesChannelLabel = (value: string) =>
  ({ site: "Сайт", saby: "СБИС / магазин", wb: "Wildberries", ozon: "Ozon" })[value] || value;

export const salesSyncLabel = (value: string) =>
  ({
    pending: "Ожидает обновления",
    running: "Обновляется",
    ok: "Актуально",
    error: "Ошибка",
    disabled: "Не подключено",
  })[value] || value;

export const recommendationStatusLabel = (value: string) =>
  ({
    recommended: "К заказу",
    already_ordered: "Уже заказано",
    check_availability: "Проверить наличие",
    supplier_unavailable: "Нет у поставщика",
    excluded: "Не закупаем",
  })[value] || value;

export const recommendationEmptyTitle = (value: string) =>
  ({
    recommended: "Дефицита нет",
    already_ordered: "Товаров в пути нет",
    check_availability: "Проверять нечего",
    supplier_unavailable: "Нет отмеченных отсутствующих товаров",
    excluded: "Ничего не снято с закупки",
  })[value] || "Список пуст";

export const recommendationEmptyText = (value: string) =>
  value === "recommended"
    ? "Текущий остаток и товары в пути покрывают рассчитанный спрос."
    : value === "already_ordered"
      ? "Здесь появятся позиции из действующих закупок, чтобы не заказать их повторно."
      : value === "check_availability"
        ? "Отметьте кнопкой «Проверить наличие» то, чего у поставщика может не оказаться."
        : value === "supplier_unavailable"
          ? "Растения, которых у поставщика нет, уходят сюда и не мешают собирать заказ."
          : "Снятое с закупки решением магазина видно здесь и в заказ не попадает.";

export const integrationChannelLabel = (value: string) =>
  ({ saby: "СБИС / Saby", wb: "Wildberries", ozon: "Ozon" })[value] || value;

export async function updateAvailability(
  supplierId: number,
  sabyId: string,
  status: string,
  reload: () => Promise<unknown>,
  onError: (message: string) => void,
  details: { checkAfter?: string; reason?: string; comment?: string } = {},
) {
  const date = "";
  try {
    await api("/api/v1/admin/procurement/availability", {
      method: "PATCH",
      body: JSON.stringify({
        supplierId,
        sabyId,
        status,
        checkAfter: details.checkAfter ?? date,
        reason: details.reason || "",
        comment: details.comment || "",
      }),
    });
    await reload();
  } catch (error) {
    onError((error as Error).message);
  }
}

export async function setExclusion(
  sabyId: string,
  excluded: boolean,
  reason: string,
  reload: () => Promise<unknown>,
  onError: (message: string) => void,
) {
  const note = excluded ? reason.trim() : "";
  try {
    await api("/api/v1/admin/procurement/exclusions", {
      method: "PUT",
      body: JSON.stringify({ sabyId, excluded, reason: note }),
    });
    await reload();
  } catch (error) {
    onError((error as Error).message);
  }
}

export async function updateRequestStatus(
  item: ProcurementRequest,
  status: string,
  reload: () => Promise<unknown>,
  onError: (value: string) => void,
) {
  if (
    ["cancelled", "fulfilled"].includes(status) &&
    !window.confirm(
      status === "cancelled"
        ? "Отменить запрос? Он останется в истории, распределённое количество будет освобождено."
        : "Отметить запрос выполненным?",
    )
  )
    return;
  try {
    await api(`/api/v1/admin/procurement/requests/${item.id}`, {
      method: "PATCH",
      body: JSON.stringify({
        sabyId: item.sabyId,
        requestedName: item.requestedName,
        quantity: item.quantity,
        status,
        notes: item.notes,
        dueDate: item.dueDate || "",
        assignee: item.assignee || "",
      }),
    });
    await reload();
  } catch (error) {
    onError((error as Error).message);
  }
}

export function ProcurementSettingsPanel({
  settings,
  onSaved,
  onError,
}: {
  settings: ProcurementSettings;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState(() => ({
    ...settings,
    economics: settings.economics || { monthlyFixedCostsRub: 0, channels: {} },
  }));
  const [saving, setSaving] = useState(false);
  type Example = {
    before: { cost: number; retail: number };
    after: {
      cost: number;
      retail: number;
      channelQuotes?: Record<string, import("./adminTypes").ProcurementChannelQuote>;
    };
    history: { version: number; at: string }[];
  };
  const [exampleKey, setExampleKey] = useState("");
  const [example, setExample] = useState<Example | null>(null);
  const [exampleError, setExampleError] = useState("");
  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void api<Example>("/api/v1/admin/procurement/settings/example", {
        method: "POST",
        body: JSON.stringify(draft),
      })
        .then((value) => {
          if (!cancelled) {
            if (value.before && value.after) {
              setExample(value);
              setExampleKey(JSON.stringify(draft));
              setExampleError("");
            } else setExampleError("Пример расчёта пока недоступен");
          }
        })
        .catch(() => {
          if (!cancelled)
            setExampleError("Не удалось получить пример расчёта. Проверьте значения.");
        });
    }, 350);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [draft]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(settings);
  const changed = Object.keys(draft).filter(
    (key) =>
      JSON.stringify(draft[key as keyof ProcurementSettings]) !==
      JSON.stringify(settings[key as keyof ProcurementSettings]),
  );
  const number = (key: keyof ProcurementSettings, value: string) =>
    setDraft((current) => ({ ...current, [key]: Number(value) }));
  const save = async () => {
    if (
      !dirty ||
      !window.confirm(`Сохранить новую версию формулы? Изменено параметров: ${changed.length}.`)
    )
      return;
    setSaving(true);
    try {
      await api("/api/v1/admin/procurement/settings", {
        method: "PUT",
        body: JSON.stringify(draft),
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <section className="admin-block procurement-block">
      <div className="admin-block-heading">
        <div>
          <p className="eyebrow">Версия {settings.version}</p>
          <h2>Формула цены</h2>
        </div>
        <button className="admin-primary" disabled={saving || !dirty} onClick={save}>
          {saving ? "Сохраняем…" : "Сохранить новую версию"}
        </button>
      </div>
      <details className="procurement-explanation">
        <summary>Пример расчёта и покрытие расходов</summary>
        <p>
          12 растений по 4,85 €, курс 120 ₽/€, горшок 14 см, высота 40 см, доставка 1 200 ₽ на
          заказ.
        </p>
        {exampleError ? (
          <p className="procurement-warning">{exampleError}</p>
        ) : (
          example &&
          exampleKey === JSON.stringify(draft) && (
            <p>
              Себестоимость: {example.before.cost.toLocaleString("ru-RU")} →{" "}
              {example.after.cost.toLocaleString("ru-RU")} ₽/шт. · Розница:{" "}
              {example.before.retail.toLocaleString("ru-RU")} →{" "}
              {example.after.retail.toLocaleString("ru-RU")} ₽/шт.
            </p>
          )
        )}
        {example?.after && exampleKey === JSON.stringify(draft) && !exampleError && (
          <PricingQuoteSummary quotes={example.after.channelQuotes} />
        )}
        <p>
          Это пример одного товара, а не прогноз прибыли всего магазина. В реальном месяце сумма
          остатков со всех завершённых продаж должна превысить постоянные расходы.
        </p>
      </details>
      {Boolean(example?.history?.length) && (
        <details className="procurement-explanation">
          <summary>История версий формулы</summary>
          {example?.history.map((entry, index) => (
            <p key={index}>
              Версия {entry.version} · {new Date(entry.at).toLocaleString("ru-RU")}
            </p>
          ))}
        </details>
      )}
      {dirty && (
        <div className="procurement-checklist">
          <strong>{`Изменено параметров: ${changed.length}`}</strong>
          <p>
            Новая версия применяется при следующем расчёте. Прежние снимки сохраняются в истории;
            для новой отправки цен потребуется пересчёт.
          </p>
          {dirty && (
            <ul>
              {changed.map((key) => (
                <li key={key}>
                  {(
                    {
                      returnLossRate: "Возвраты",
                      marketplaceCostRate: "Расходы маркетплейсов",
                      taxRate: "Налог",
                      reserveRate: "Резерв",
                      packageRub: "Упаковка",
                      marketplaceLogisticsPerCm: "Логистика МП",
                      priceChangeThreshold: "Порог изменения",
                      retailMarkupMultiplier: "Наценка",
                      marketplaceStrikeMarkup: "Цена МП до скидки",
                      recommendationDays: "Период продаж",
                      targetCoverDays: "Запас",
                      economics: "Расходы по каналам и месячные расходы",
                      roundPrices: "Округление",
                    } as Record<string, string>
                  )[key] || key}
                  :{" "}
                  {key === "economics"
                    ? "значения показаны в полях ниже"
                    : `${String(settings[key as keyof ProcurementSettings])} → ${String(draft[key as keyof ProcurementSettings])}`}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <h3>Ценообразование и расходы</h3>
      <div className="procurement-settings-grid">
        <PercentField
          label="Налог от выручки"
          value={draft.taxRate}
          onChange={(value) => setDraft({ ...draft, taxRate: value })}
        />
        <PercentField
          label="Менять цену при отклонении более"
          value={draft.priceChangeThreshold}
          onChange={(value) => setDraft({ ...draft, priceChangeThreshold: value })}
        />
        {!draft.economics.unified?.enabled && (
          <PercentField
            label="Стартовая наценка на себестоимость"
            value={draft.retailMarkupMultiplier - 1}
            onChange={(value) => setDraft({ ...draft, retailMarkupMultiplier: 1 + value })}
          />
        )}
      </div>
      <p>
        Стартовая наценка даёт предложение для проверки по рынку. Сравнивайте одинаковые D, H и
        наличие; одна наценка не гарантирует прибыль. Старые общие «расходы МП», резерв и логистика
        по высоте в новую цену не входят.
      </p>
      <ProcurementEconomicsFields
        value={draft.economics}
        onChange={(economics) => setDraft({ ...draft, economics })}
      />
      <p>
        Цена МП = (себестоимость + упаковка + логистика + потери + общий плановый заработок) / (1 −
        комиссия − эквайринг − прочие сборы − налог). Это сумма на покрытие расходов и прибыль до
        вычета месячных затрат.
      </p>
      <h3>Планирование спроса</h3>
      <div className="procurement-settings-grid">
        <label>
          Период анализа продаж, дней
          <input
            type="number"
            value={draft.recommendationDays}
            onChange={(event) => number("recommendationDays", event.target.value)}
          />
        </label>
        <label>
          Закупаем запас на, дней
          <input
            type="number"
            value={draft.targetCoverDays}
            onChange={(event) => number("targetCoverDays", event.target.value)}
          />
        </label>
        {!draft.economics.unified?.enabled && (
          <label className="admin-checkbox">
            <input
              type="checkbox"
              checked={draft.roundPrices}
              onChange={(event) => setDraft({ ...draft, roundPrices: event.target.checked })}
            />
            Округлять цены до ближайших 50 или 90
          </label>
        )}
      </div>
    </section>
  );
}

export function PercentField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <label>
      {label}, %
      <input
        type="number"
        step="0.1"
        value={Math.round(value * 10000) / 100}
        onChange={(event) => onChange(Number(event.target.value) / 100)}
      />
    </label>
  );
}
