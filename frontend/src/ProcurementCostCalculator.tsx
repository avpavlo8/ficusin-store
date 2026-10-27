import { useEffect, useState } from "react";
import { api } from "./adminShared";
import { PricingQuoteSummary } from "./ProcurementEconomics";
import type { ProcurementChannelQuote, ProcurementSettings } from "./adminTypes";

const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(
    value,
  );
const number = (value: string) => Number(value.replace(",", "."));
type Mode = "purchase" | "cost" | "retail";
export type ProcurementCalculatorDraft = Record<
  | "name"
  | "price"
  | "rate"
  | "pot"
  | "height"
  | "markup"
  | "trolleyCost"
  | "trolleyVolume"
  | "fill",
  string
> &
  Partial<Record<"mode" | "cost" | "retail" | "boxLength" | "boxWidth" | "boxHeight", string>>;
type Preview = { retailRub: number; quotes: Record<string, ProcurementChannelQuote> };

export function ProcurementCostCalculator({
  settings,
  onRequest,
  initialDraft,
  onDraftChange,
}: {
  settings: ProcurementSettings;
  onRequest: (name: string, notes: string) => void;
  initialDraft: ProcurementCalculatorDraft | null;
  onDraftChange: (draft: ProcurementCalculatorDraft) => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const [draft, setDraft] = useState<ProcurementCalculatorDraft>(
    () =>
      initialDraft || {
        name: "",
        price: "",
        rate: String(settings.defaultExchangeRate),
        pot: "",
        height: "",
        markup: String(
          Math.round(
            (settings.economics?.unified?.enabled
              ? settings.economics.unified.markupRate
              : settings.retailMarkupMultiplier - 1) * 10000,
          ) / 100,
        ),
        trolleyCost: String(settings.trolleyCostRub),
        trolleyVolume: String(settings.trolleyVolumeCm3),
        fill: String(settings.trolleyFillRatio * 100),
      },
  );
  const mode: Mode = draft.mode === "cost" || draft.mode === "retail" ? draft.mode : "purchase";
  const change = (key: keyof ProcurementCalculatorDraft, value: string) => {
    const next = { ...draft, [key]: value };
    if (key === "price") next.mode = "purchase";
    if (key === "cost") next.mode = value.trim() ? "cost" : "purchase";
    if (key === "retail")
      next.mode = value.trim() ? "retail" : draft.cost?.trim() ? "cost" : "purchase";
    setDraft(next);
    onDraftChange(next);
    setCopied(false);
    setCopyError("");
  };
  const positive = (value: string | undefined, max = 1e9) =>
    Boolean(value?.trim()) &&
    Number.isFinite(number(value!)) &&
    number(value!) > 0 &&
    number(value!) <= max;
  const nonnegative = (value: string, max = 1e9) =>
    Boolean(value.trim()) &&
    Number.isFinite(number(value)) &&
    number(value) >= 0 &&
    number(value) <= max;
  const purchase = number(draft.price) * number(draft.rate);
  const delivery =
    (number(draft.trolleyCost) * Math.PI * (number(draft.pot) / 2) ** 2 * number(draft.height)) /
    ((number(draft.trolleyVolume) * number(draft.fill)) / 100);
  const cost = mode === "purchase" ? purchase + delivery : number(draft.cost || "");
  const percent = number(draft.markup);
  const valid =
    mode === "retail"
      ? positive(draft.retail)
      : nonnegative(draft.markup, 1000) &&
        (mode === "cost"
          ? positive(draft.cost)
          : [draft.price, draft.rate, draft.pot, draft.height, draft.trolleyVolume].every((v) =>
              positive(v),
            ) &&
            nonnegative(draft.trolleyCost) &&
            positive(draft.fill, 100) &&
            Number.isFinite(cost) &&
            cost > 0 &&
            cost <= 1e9);
  const boxKeys = ["boxLength", "boxWidth", "boxHeight"] as const;
  const boxValid = boxKeys.every((key) => positive(draft[key], 1000));
  const boxTouched = boxKeys.some((key) => Boolean(draft[key]?.trim()));
  const requestKey = valid
    ? JSON.stringify({
        mode: mode === "retail" ? "retail" : "cost",
        ...(mode === "retail"
          ? { retailRub: number(draft.retail!) }
          : { costRub: cost, markupRate: percent / 100 }),
        package: boxValid
          ? {
              lengthCm: number(draft.boxLength!),
              widthCm: number(draft.boxWidth!),
              heightCm: number(draft.boxHeight!),
              source: "Ручной расчёт",
            }
          : undefined,
      })
    : "";
  const [preview, setPreview] = useState<{ key: string; data: Preview } | null>(null);
  const [failure, setFailure] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(() => {
      if (!requestKey) return;
      void api<Preview>("/api/v1/admin/procurement/pricing/preview", {
        method: "POST",
        body: requestKey,
      })
        .then((data) => {
          if (!cancelled) {
            setPreview({ key: requestKey, data });
            setFailure("");
          }
        })
        .catch(() => {
          if (!cancelled) {
            setPreview(null);
            setFailure(requestKey);
          }
        });
    }, 350);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [requestKey, attempt]);
  const checked = preview?.key === requestKey ? preview.data : null;
  const failed = failure === requestKey && Boolean(requestKey);
  const canQuote = Boolean(checked && (mode === "retail" || checked.quotes.site?.priceRub));
  const quote = `${draft.name.trim() || "Растение под заказ"} — магазин: ${money(checked?.retailRub || 0)} ₽.${(["wb", "ozon"] as const).map((ch) => (checked?.quotes[ch]?.priceRub != null ? ` ${ch === "wb" ? "WB" : "Ozon"}: ${money(checked.quotes[ch].priceRub!)} ₽.` : "")).join("")} Предварительный расчёт от ${new Date().toLocaleDateString("ru-RU")}. Окончательная цена после подтверждения поставщика и доставки.`;
  const field = (
    key: keyof ProcurementCalculatorDraft,
    label: string,
    isValid: boolean,
    hint = "Введите число больше нуля",
    disabled = false,
    displayValue = draft[key] || "",
  ) => (
    <label key={key}>
      {label}
      <input
        inputMode="decimal"
        value={displayValue}
        disabled={disabled}
        aria-invalid={!disabled && Boolean(displayValue) && !isValid}
        onChange={(e) => change(key, e.target.value)}
      />
      {!disabled && Boolean(displayValue) && !isValid && (
        <small className="procurement-warning">{hint}</small>
      )}
    </label>
  );
  const costValue =
    mode === "purchase"
      ? valid
        ? String(Math.round(cost * 100) / 100)
        : ""
      : mode === "cost"
        ? draft.cost || ""
        : "";
  const retailValue =
    mode === "retail" ? draft.retail || "" : checked ? String(checked.retailRub) : "";
  return (
    <section className="procurement-calculator" aria-labelledby="procurement-calculator-title">
      <div className="procurement-calculator-heading">
        <div>
          <h2 id="procurement-calculator-title">Калькулятор цен</h2>
          <p>
            Введите закупку в евро, готовую себестоимость или цену магазина — остальные цены
            рассчитаются автоматически.
          </p>
        </div>
      </div>
      <div className="procurement-calculator-layout">
        <div className="procurement-calculator-form">
          <div className="procurement-calculator-group">
            <h3>Растение и цены</h3>
            <div className="procurement-calculator-fields">
              <label className="wide">
                Название
                <input
                  value={draft.name}
                  onChange={(e) => change("name", e.target.value)}
                  placeholder="Необязательно"
                />
              </label>
              {field(
                "price",
                "Цена поставщика, €",
                mode !== "purchase" || positive(draft.price),
                undefined,
                false,
                mode === "purchase" ? draft.price : "",
              )}
              {field("rate", "Курс, ₽/€", positive(draft.rate), undefined, mode !== "purchase")}
              {field(
                "pot",
                "Диаметр горшка, см",
                positive(draft.pot),
                undefined,
                mode !== "purchase",
              )}
              {field(
                "height",
                "Высота растения, см",
                positive(draft.height),
                undefined,
                mode !== "purchase",
              )}
              {field(
                "cost",
                "Себестоимость, ₽",
                mode !== "cost" || positive(draft.cost),
                undefined,
                false,
                costValue,
              )}
              {field(
                "markup",
                "Наценка, %",
                nonnegative(draft.markup, 1000),
                "Укажите наценку от 0 до 1000%",
                mode === "retail",
              )}
              {field(
                "retail",
                "Цена в магазине, ₽",
                mode !== "retail" || positive(draft.retail),
                undefined,
                false,
                retailValue,
              )}
              {(["wb", "ozon"] as const).map((ch) => (
                <label key={ch}>
                  {ch === "wb" ? "Цена WB, ₽" : "Цена Ozon, ₽"}
                  <input
                    readOnly
                    value={checked?.quotes[ch]?.priceRub ?? ""}
                    placeholder={boxValid ? "Ожидает расчёта" : "Нужны размеры коробки"}
                  />
                </label>
              ))}
            </div>
            <p role="status">
              {mode === "cost"
                ? "Считаем от введённой себестоимости с доставкой до Рязани. Размеры растения и закупочная логистика не участвуют."
                : mode === "retail"
                  ? "Считаем от введённой цены магазина. Цены МП сохраняют ту же сумму после расходов продажи. Себестоимость и прибыль без закупочных данных неизвестны."
                  : "Считаем от закупки в евро и логистики до Рязани. Можно сразу ввести готовую себестоимость или цену магазина."}
            </p>
          </div>
          <div className="procurement-calculator-group">
            <h3>Коробка для маркетплейсов</h3>
            <p>
              Габариты упакованного товара нужны во всех режимах для расчёта упаковки и логистики WB
              и Ozon.
            </p>
            <div className="procurement-calculator-fields">
              {boxKeys.map((key, i) =>
                field(
                  key,
                  ["Длина коробки, см", "Ширина коробки, см", "Высота коробки, см"][i],
                  positive(draft[key], 1000),
                  "Укажите размер больше 0 и не больше 1000 см",
                ),
              )}
            </div>
            {boxTouched && !boxValid && (
              <p className="procurement-warning">
                Заполните все три размера коробки корректными числами.
              </p>
            )}
          </div>
          {
            <details className="procurement-calculator-assumptions">
              <summary>Параметры телеги и доставки</summary>
              <div className="procurement-calculator-fields">
                {field(
                  "trolleyCost",
                  "Стоимость телеги, ₽",
                  nonnegative(draft.trolleyCost),
                  "Стоимость не может быть отрицательной",
                  mode !== "purchase",
                )}
                {field(
                  "fill",
                  "Заполнение, %",
                  positive(draft.fill, 100),
                  "Введите заполнение больше 0 и не больше 100%",
                  mode !== "purchase",
                )}
                {field(
                  "trolleyVolume",
                  "Объём телеги, см³",
                  positive(draft.trolleyVolume),
                  undefined,
                  mode !== "purchase",
                )}
              </div>
            </details>
          }
        </div>
        <aside className="procurement-calculator-result" aria-live="polite">
          <h3>Цена в магазине</h3>
          {valid ? (
            <>
              <strong>
                {checked
                  ? `${money(checked.retailRub)} ₽`
                  : failed
                    ? "Расчёт недоступен"
                    : "Считаем цены…"}
              </strong>
              <p>{draft.name.trim() || "Разовая позиция"}</p>
              {mode !== "retail" && (
                <dl>
                  {mode === "purchase" && (
                    <>
                      <div>
                        <dt>Растение по курсу</dt>
                        <dd>{money(purchase)} ₽</dd>
                      </div>
                      <div>
                        <dt>Логистика до Рязани</dt>
                        <dd>{money(delivery)} ₽</dd>
                      </div>
                    </>
                  )}
                  <div className="total">
                    <dt>Себестоимость</dt>
                    <dd>{money(cost)} ₽</dd>
                  </div>
                  <div>
                    <dt>Наценка {money(percent)}%</dt>
                    <dd>{money((cost * percent) / 100)} ₽</dd>
                  </div>
                </dl>
              )}
              {mode === "retail" ? (
                <div className="procurement-settings-grid">
                  {(["wb", "ozon"] as const).map((ch) => (
                    <div className="procurement-checklist" key={ch}>
                      <strong>{ch === "wb" ? "Wildberries · FBS" : "Ozon · FBS"}</strong>
                      <p>
                        {checked?.quotes[ch]?.priceRub != null
                          ? `${money(checked.quotes[ch].priceRub!)} ₽`
                          : checked?.quotes[ch]?.missing?.join(", ") || "Нужен расчёт расходов."}
                      </p>
                    </div>
                  ))}
                </div>
              ) : (
                <PricingQuoteSummary quotes={checked?.quotes} />
              )}
              {!boxValid && (
                <p className="procurement-warning">
                  Введите размеры коробки, чтобы увидеть цены WB и Ozon.
                </p>
              )}
              {failed && (
                <p role="alert">
                  Не удалось рассчитать цены.{" "}
                  <button
                    onClick={() => {
                      setFailure("");
                      setAttempt((v) => v + 1);
                    }}
                  >
                    Повторить расчёт
                  </button>
                </p>
              )}
              <div className="procurement-inline-actions">
                <button
                  disabled={!canQuote}
                  onClick={() => {
                    void navigator.clipboard
                      .writeText(quote)
                      .then(() => {
                        setCopied(true);
                        setCopyError("");
                      })
                      .catch(() =>
                        setCopyError("Не удалось скопировать. Выделите текст расчёта вручную."),
                      );
                  }}
                >
                  {copied ? "Расчёт скопирован" : "Копировать расчёт"}
                </button>
                <button
                  disabled={!canQuote}
                  onClick={() => onRequest(draft.name.trim() || "Растение под заказ", quote)}
                >
                  Создать запрос
                </button>
              </div>
              {copyError && (
                <p role="status">
                  {copyError} {quote}
                </p>
              )}
              <small>
                Предварительный расчёт по действующим настройкам расходов. Цены товаров не
                изменяются.
              </small>
            </>
          ) : (
            <p>
              {mode === "purchase"
                ? "Введите цену, курс и размеры растения, чтобы увидеть расчёт."
                : mode === "cost"
                  ? "Введите себестоимость, чтобы увидеть расчёт."
                  : "Введите цену в магазине, чтобы увидеть расчёт."}
            </p>
          )}
        </aside>
      </div>
    </section>
  );
}
