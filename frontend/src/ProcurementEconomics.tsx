import type {
  ProcurementChannelCosts,
  ProcurementChannelQuote,
  ProcurementEconomics,
} from "./adminTypes";

const names: Record<string, string> = {
  site: "Сайт / магазин",
  wb: "Wildberries · FBS",
  ozon: "Ozon · FBS",
};
const rub = (value: number) => value.toLocaleString("ru-RU", { maximumFractionDigits: 2 });

export function ChannelCostsFields({
  value,
  onChange,
  automaticParcel = false,
}: {
  value: ProcurementChannelCosts;
  onChange: (value: ProcurementChannelCosts) => void;
  automaticParcel?: boolean;
}) {
  const field = (key: keyof ProcurementChannelCosts, label: string, percent = false) => (
    <label key={key}>
      {label}
      {percent ? ", %" : ", ₽"}
      <input
        type="number"
        min="0"
        step={percent ? "0.01" : "0.01"}
        placeholder="Нет данных"
        value={
          value[key] == null ? "" : Math.round(value[key]! * (percent ? 100 : 1) * 10000) / 10000
        }
        onChange={(event) =>
          onChange({
            ...value,
            [key]:
              event.target.value === "" ? null : Number(event.target.value) / (percent ? 100 : 1),
          })
        }
      />
    </label>
  );
  return (
    <div className="procurement-settings-grid">
      {field("commissionRate", "Комиссия", true)}
      {field("acquiringRate", "Эквайринг", true)}
      {field("otherRate", "Программы и прочие сборы", true)}
      {!automaticParcel && (
        <>
          {field("packagingRub", "Упаковка")}
          {field("logisticsRub", "Логистика, последняя миля, обработка")}
        </>
      )}
      {field("lossRub", "Потери, возвраты и прочие затраты")}
    </div>
  );
}

export function PricingQuoteSummary({
  quotes,
}: {
  quotes?: Record<string, ProcurementChannelQuote>;
}) {
  return (
    <div className="procurement-settings-grid">
      {["site", "wb", "ozon"].map((channel) => {
        const q = quotes?.[channel];
        return (
          <div className="procurement-checklist" key={channel}>
            <strong>{names[channel]}</strong>
            {q?.priceRub != null && q.contributionRub != null ? (
              <>
                <p>
                  {q.manual ? "Выбранная цена" : "Предложение"}: {rub(q.priceRub)} ₽ · остаётся{" "}
                  {rub(q.contributionRub)} ₽ с продажи на постоянные расходы и прибыль.
                </p>
                <p>
                  Нижняя граница: {rub(q.breakEvenRub || 0)} ₽ — покрывает только товар и расходы
                  продажи.
                </p>
                {q.coverUnits != null && (
                  <p>
                    {q.coverUnits.toLocaleString("ru-RU")} таких продаж в месяц покроют постоянные
                    расходы. Прибыль появляется сверх этого объёма.
                  </p>
                )}
              </>
            ) : (
              <p className="procurement-warning">
                {q?.missing?.length ? `${q.missing.join(", ")}.` : "Нужен расчёт расходов."}
                {q?.breakEvenRub != null
                  ? ` Нижняя граница: ${rub(q.breakEvenRub)} ₽ без покрытия постоянных расходов.`
                  : ""}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}

export function ProcurementEconomicsFields({
  value,
  onChange,
}: {
  value: ProcurementEconomics;
  onChange: (value: ProcurementEconomics) => void;
}) {
  const unified = value.unified;
  const changeUnified = (
    key:
      | "materialsRub"
      | "smallBoxRub"
      | "mediumBoxRub"
      | "largeBoxRub"
      | "wbFirstLiterRub"
      | "wbAdditionalLiterRub"
      | "wbHandlingRub",
    raw: string,
  ) => {
    if (unified) onChange({ ...value, unified: { ...unified, [key]: Number(raw) } });
  };
  return (
    <>
      {unified?.enabled && (
        <>
          <h3>Общая формула для всех товаров</h3>
          <p>
            Заработок задаётся от полной себестоимости. Старые ручные цены и расходы строк не
            участвуют. Коробка берётся из PIM или наблюдений связанных карточек МП. Количество для
            склейки не меняет число продаваемых растений.
          </p>
          <div className="procurement-settings-grid">
            <label>
              Заработок от себестоимости, %
              <input
                type="number"
                min="0.1"
                max="1000"
                step="0.1"
                value={unified.markupRate * 100}
                onChange={(e) =>
                  onChange({
                    ...value,
                    unified: { ...unified, markupRate: Number(e.target.value) / 100 },
                  })
                }
              />
            </label>
            {(
              [
                ["materialsRub", "Материалы на отправление, ₽"],
                ["smallBoxRub", "Коробка до 5 л, ₽"],
                ["mediumBoxRub", "Коробка до 15 л, ₽"],
                ["largeBoxRub", "Коробка свыше 15 л, ₽"],
                ["wbFirstLiterRub", "WB: первый литр, ₽"],
                ["wbAdditionalLiterRub", "WB: дополнительный литр, ₽"],
                ["wbHandlingRub", "WB: обработка, ₽"],
              ] as const
            ).map(([key, label]) => (
              <label key={key}>
                {label}
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={unified[key]}
                  onChange={(e) => changeUnified(key, e.target.value)}
                />
              </label>
            ))}
          </div>
          <details className="procurement-explanation">
            <summary>Нормативы логистики Ozon · проверка {unified.verifiedAt}</summary>
            <p>
              Средние расходы из отчётов включают обработку и последнюю милю. Для промежуточного
              объёма используется следующая группа. Это плановый расход, а не стоимость каждого
              маршрута. Обычный обмен продажами не обновляет финансовые нормативы.
            </p>
            <div className="admin-table-wrap">
              <table className="admin-table">
                <thead>
                  <tr>
                    <th>Объём до, л</th>
                    <th>Логистика, ₽</th>
                    <th>Продаж в источнике</th>
                  </tr>
                </thead>
                <tbody>
                  {unified.norms.map((norm, index) => (
                    <tr key={norm.maxLiters}>
                      <td>{norm.maxLiters}</td>
                      <td>
                        <input
                          type="number"
                          min="0"
                          step="0.01"
                          aria-label={`Логистика Ozon до ${norm.maxLiters} л`}
                          value={norm.ozonRub}
                          onChange={(e) =>
                            onChange({
                              ...value,
                              unified: {
                                ...unified,
                                norms: unified.norms.map((n, i) =>
                                  i === index ? { ...n, ozonRub: Number(e.target.value) } : n,
                                ),
                              },
                            })
                          }
                        />
                      </td>
                      <td>{norm.samples}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </>
      )}
      <h3>Постоянные расходы</h3>
      <p>
        Аренда, зарплаты с начислениями, коммунальные и сервисы учитываются здесь один раз. Упаковку
        и доставку конкретной продажи заполняйте ниже. Если они уже входят в месячную сумму,
        исключите их из неё.
      </p>
      <label>
        Расходы в месяц, ₽
        <input
          type="number"
          min="0"
          value={value.monthlyFixedCostsRub}
          onChange={(event) =>
            onChange({ ...value, monthlyFixedCostsRub: Number(event.target.value) })
          }
        />
      </label>
      <h3>Расходы продажи по каналам</h3>
      <p>
        Указывайте удержания от цены продавца до скидок за счёт площадки. Пустое поле означает
        неизвестную стоимость; если расхода нет, введите 0. Логистику берите по упакованному товару
        из кабинета, с последней милей и обработкой. Потери — средняя невозмещённая стоимость отмен,
        возвратной доставки, повреждений и списаний на завершённую продажу. Включайте сюда также
        сдельную сборку, рекламу и другие переменные затраты; не дублируйте их в месячном бюджете.
      </p>
      {["site", "wb", "ozon"].map((channel) => (
        <details className="procurement-explanation" key={channel} open>
          <summary>{names[channel]}</summary>
          <ChannelCostsFields
            automaticParcel={Boolean(unified?.enabled && channel !== "site")}
            value={value.channels[channel] || {}}
            onChange={(costs) =>
              onChange({ ...value, channels: { ...value.channels, [channel]: costs } })
            }
          />
        </details>
      ))}
      <p>
        В общей формуле комиссия, сборы и резерв берутся отсюда для комнатных растений. Упаковка и
        логистика МП определяются по коробке. Данные нормативы не подходят для других товарных
        категорий без проверки тарифов.
      </p>
    </>
  );
}
