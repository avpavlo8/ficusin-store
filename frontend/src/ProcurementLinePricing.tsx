import { useState } from "react";
import type { ProcurementOrderLine, ProcurementSettings } from "./adminTypes";
import { api } from "./adminShared";
import { ChannelCostsFields, PricingQuoteSummary } from "./ProcurementEconomics";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export function ProcurementLinePricing({
  line,
  settings,
  onClose,
  onSaved,
  onError,
}: {
  line: ProcurementOrderLine;
  settings?: ProcurementSettings;
  onClose: () => void;
  onSaved: () => void;
  onError: (message: string) => void;
}) {
  const [costs, setCosts] = useState(line.channelCosts || {});
  const [channelPrices, setChannelPrices] = useState(line.channelPrices || {});
  const [retail, setRetail] = useState(line.retailOverrideRub?.toString() || "");
  const [saving, setSaving] = useState(false);
  const { error, errorRef, notify } = useProcurementError(onError);
  const close = () => {
    if (!saving) onClose();
  };
  const ref = useProcurementDialog(close);
  const valid =
    (retail === "" || (Number.isInteger(Number(retail)) && Number(retail) > 0)) &&
    Object.values(channelPrices).every((price) => Number.isInteger(price) && price > 0);
  const save = async () => {
    if (saving || !valid) return;
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/order-lines/${line.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          channelCosts: costs,
          channelPrices,
          retailOverrideRub: retail === "" ? 0 : Number(retail),
        }),
      });
      onSaved();
    } catch (error) {
      notify((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  if (settings?.economics?.unified?.enabled)
    return (
      <>
        <button
          tabIndex={-1}
          className="admin-dialog-backdrop procurement-receiving-backdrop"
          aria-label="Закрыть расходы продажи"
          onClick={close}
        />
        <div
          className="admin-dialog procurement-receiving-dialog"
          ref={ref}
          role="dialog"
          aria-modal="true"
          aria-labelledby="line-pricing-title"
        >
          <header>
            <h2 id="line-pricing-title">Общая формула цены</h2>
            <button aria-label="Закрыть расходы продажи" onClick={close}>
              ×
            </button>
          </header>
          <div className="procurement-receiving-body">
            <p>
              Для всех растений действует одна формула. Измените общие нормативы в разделе «Формула
              цены», затем пересчитайте закупку. Старые ручные цены и расходы этой строки не
              участвуют в новом расчёте.
            </p>
            <PricingQuoteSummary quotes={line.channelQuotes} />
          </div>
          <div className="dialog-actions">
            <button onClick={close}>Закрыть</button>
          </div>
        </div>
      </>
    );
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop procurement-receiving-backdrop"
        aria-label="Закрыть расходы продажи"
        disabled={saving}
        onClick={close}
      />
      <div
        className="admin-dialog procurement-receiving-dialog"
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby="line-pricing-title"
      >
        <header>
          <div>
            <p className="eyebrow">{line.sabyName || line.rawName}</p>
            <h2 id="line-pricing-title">Цена и расходы продажи</h2>
          </div>
          <button aria-label="Закрыть расходы продажи" disabled={saving} onClick={close}>
            ×
          </button>
        </header>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <div className="procurement-receiving-body">
            <p>
              Себестоимость партии: {line.unitCostRub?.toLocaleString("ru-RU") || "нужен расчёт"} ₽.
              Текущая цена сайта: {line.currentRetailRub.toLocaleString("ru-RU")} ₽.
            </p>
            <label>
              Розничная цена после проверки рынка, ₽
              <input
                type="number"
                min="1"
                step="1"
                placeholder="По стартовой наценке"
                value={retail}
                disabled={saving}
                onChange={(event) => setRetail(event.target.value)}
              />
            </label>
            <p>
              Сравните одинаковые размер, сорт и наличие. Пустое поле возвращает расчёт по стартовой
              наценке. Новая цена появится в предложении после пересчёта закупки.
            </p>
            {["site", "wb", "ozon"].map((channel) => (
              <details key={channel} className="procurement-explanation">
                <summary>
                  {{ site: "Сайт / магазин", wb: "Wildberries · FBS", ozon: "Ozon · FBS" }[channel]}
                </summary>
                {channel !== "site" && (
                  <label>
                    Цена {channel === "wb" ? "WB" : "Ozon"} после проверки рынка, ₽
                    <input
                      type="number"
                      min="1"
                      step="1"
                      disabled={saving}
                      placeholder="Предложение по формуле"
                      value={channelPrices[channel] || ""}
                      onChange={(event) =>
                        setChannelPrices((previous) => {
                          const next = { ...previous };
                          if (event.target.value === "") delete next[channel];
                          else next[channel] = Number(event.target.value);
                          return next;
                        })
                      }
                    />
                  </label>
                )}
                <label className="admin-checkbox">
                  <input
                    type="checkbox"
                    checked={costs[channel] != null}
                    disabled={saving}
                    onChange={(event) =>
                      setCosts((previous) => {
                        const next = { ...previous };
                        if (event.target.checked)
                          next[channel] = { ...settings?.economics?.channels[channel] };
                        else delete next[channel];
                        return next;
                      })
                    }
                  />
                  Отдельные расходы этого товара
                </label>
                {costs[channel] != null ? (
                  <ChannelCostsFields
                    value={costs[channel]}
                    onChange={(value) => setCosts({ ...costs, [channel]: value })}
                  />
                ) : (
                  <p>Используются базовые расходы из настроек.</p>
                )}
              </details>
            ))}
            <details className="procurement-explanation">
              <summary>Последний сохранённый расчёт</summary>
              <p>
                После изменения данных нужен пересчёт; числа ниже относятся к предыдущему расчёту.
              </p>
              <PricingQuoteSummary quotes={line.channelQuotes} />
            </details>
            {error && (
              <div className="procurement-dialog-error" ref={errorRef} tabIndex={-1} role="alert">
                {error}
              </div>
            )}
          </div>
          <div className="dialog-actions">
            <button type="button" disabled={saving} onClick={close}>
              Отмена
            </button>
            <button className="primary" disabled={saving || !valid}>
              {saving ? "Сохраняем…" : "Сохранить для пересчёта"}
            </button>
          </div>
        </form>
      </div>
    </>
  );
}
