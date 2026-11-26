import { useId } from "react";
import { NumericInput } from "./NumericInput";
import { money } from "./adminShared";
import type { ProcurementOrderDetail, ProcurementOrderLine } from "./adminTypes";
import UnifiedPricingPreview from "./UnifiedPricingPreview";
import ProcurementChangesTable, { type PublicationNotice } from "./ProcurementChangesTable";

type OrderCosts = {
  exchangeRate: number;
  deliveryToMoscowRub: number;
  deliveryToRyazanRub: number;
  additionalCostsRub: number;
};

export default function ProcurementOrderCosts({
  costs,
  detail,
  closed,
  saving,
  uploading,
  needsInvoice,
  invoiceTotalMismatch,
  onCostsChange,
  onOpenReview,
  onCalculate,
  onPublish,
  onEditPrice,
  publicationNotices,
  priceChangeThreshold,
}: {
  costs: OrderCosts;
  detail: ProcurementOrderDetail;
  closed: boolean;
  saving: boolean;
  uploading: boolean;
  needsInvoice: boolean;
  invoiceTotalMismatch: boolean;
  onCostsChange: (value: OrderCosts) => void;
  onOpenReview: () => void;
  onCalculate: () => void;
  onPublish: (channel: string) => void;
  publicationNotices: Record<string, PublicationNotice>;
  priceChangeThreshold: number;
  onEditPrice: (line: ProcurementOrderLine) => void;
}) {
  const fieldId = useId();
  const isReady = detail.order.status === "ready_to_receive";
  const showChanges = isReady || detail.order.status === "received";
  return (
    <section aria-label="Расчёт и цены закупки">
      {!closed && !detail.validation.canCalculate && (
        <p id={`${fieldId}-calculation-blockers`} className="procurement-calculation-note">
          {needsInvoice
            ? "Загрузите инвойс перед расчётом."
            : invoiceTotalMismatch
              ? "Итог документа не совпадает с суммой строк."
              : "Завершите сверку перед расчётом."}{" "}
          <button type="button" onClick={onOpenReview}>
            {needsInvoice ? "Открыть документы" : "Перейти к сверке"}
          </button>
        </p>
      )}
      {showChanges && (
        <ProcurementChangesTable
          detail={detail}
          saving={saving}
          notices={publicationNotices}
          threshold={priceChangeThreshold}
          onPublish={onPublish}
          onEditPrice={onEditPrice}
        />
      )}
      <details className="procurement-cost-details" open={!showChanges}>
        <summary>
          Расчёт и доставка{" "}
          <span>
            {detail.order.currency !== "RUB"
              ? `Курс ${costs.exchangeRate} · доставка ${money.format(costs.deliveryToMoscowRub + costs.deliveryToRyazanRub)}`
              : "Закупка в рублях"}
          </span>
        </summary>
        <fieldset className="procurement-readonly-fields" disabled={closed || saving || uploading}>
          <div
            className={`procurement-costs ${detail.order.currency === "RUB" ? "procurement-costs-domestic" : ""}`}
          >
            {detail.order.currency !== "RUB" && (
              <>
                <label>
                  Курс оплаты
                  <NumericInput
                    step="0.01"
                    value={costs.exchangeRate}
                    onChange={(event) =>
                      onCostsChange({ ...costs, exchangeRate: Number(event.target.value) })
                    }
                  />
                </label>
                <label>
                  До Москвы, ₽
                  <NumericInput
                    step="0.01"
                    value={costs.deliveryToMoscowRub}
                    onChange={(event) =>
                      onCostsChange({
                        ...costs,
                        deliveryToMoscowRub: Number(event.target.value),
                      })
                    }
                  />
                </label>
                <label>
                  Москва → Рязань, ₽
                  <NumericInput
                    value={costs.deliveryToRyazanRub}
                    onChange={(event) =>
                      onCostsChange({
                        ...costs,
                        deliveryToRyazanRub: Number(event.target.value),
                      })
                    }
                  />
                </label>
              </>
            )}
            {detail.order.currency === "RUB" && (
              <label>
                Доставка до склада, ₽
                <NumericInput
                  min="0"
                  step="0.01"
                  value={costs.deliveryToMoscowRub}
                  onChange={(event) =>
                    onCostsChange({ ...costs, deliveryToMoscowRub: Number(event.target.value) })
                  }
                />
              </label>
            )}
            <label>
              Прочие расходы закупки, ₽
              <NumericInput
                min="0"
                step="0.01"
                value={costs.additionalCostsRub}
                onChange={(event) =>
                  onCostsChange({ ...costs, additionalCostsRub: Number(event.target.value) })
                }
              />
            </label>
            <button
              className="admin-primary"
              aria-describedby={
                !detail.validation.canCalculate
                  ? `${fieldId}-calculation-blockers`
                  : costs.exchangeRate <= 0
                    ? `${fieldId}-exchange-error`
                    : undefined
              }
              disabled={saving || !detail.validation.canCalculate || costs.exchangeRate <= 0}
              onClick={onCalculate}
            >
              {saving ? "Считаем…" : "Рассчитать"}
            </button>
          </div>
          {!closed && costs.exchangeRate <= 0 && (
            <p id={`${fieldId}-exchange-error`} className="procurement-warning">
              Для расчёта укажите курс оплаты больше нуля.
            </p>
          )}
          <p>
            Прочие расходы: банковская комиссия за закупку, пошлины, страхование и другие затраты до
            склада. Невозвратная тара и аренда из инвойса добавляются автоматически; повторно сюда
            их не включайте.
          </p>
          {Boolean(detail.costs.supplierChargesRub || detail.costs.returnableDepositRub) && (
            <p>
              Тара и аренда поставщика: {money.format(detail.costs.supplierChargesRub || 0)} — в
              себестоимости. Возвратный залог:{" "}
              {money.format(detail.costs.returnableDepositRub || 0)} — отдельно от расхода.
            </p>
          )}
        </fieldset>
      </details>
      {!showChanges && detail.unifiedPreview && (
        <UnifiedPricingPreview preview={detail.unifiedPreview} />
      )}
      {detail.order.status === "ready_to_receive" &&
        (Math.abs(detail.validation.allocatedTrolleyRub - detail.validation.expectedTrolleyRub) >
          1 ||
          Math.abs(detail.validation.allocatedRyazanRub - detail.validation.expectedRyazanRub) >
            1) && (
          <section className="procurement-checklist blocked" role="alert">
            <strong>Доставка распределена не полностью</strong>
            <p>
              До Москвы: {money.format(detail.validation.allocatedTrolleyRub)} из{" "}
              {money.format(detail.validation.expectedTrolleyRub)}. До Рязани:{" "}
              {money.format(detail.validation.allocatedRyazanRub)} из{" "}
              {money.format(detail.validation.expectedRyazanRub)}. Пересчитайте закупку.
            </p>
          </section>
        )}
    </section>
  );
}
