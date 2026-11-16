import { Fragment, useId, useState } from "react";
import type { ProcurementRecommendation, ProcurementSupplier } from "./adminTypes";
import { recommendedPackageCount } from "./lib/procurement-state";

export function ProcurementRecommendationTable({
  items,
  suppliers,
  selected,
  selectedSupplierId,
  savingSupplier,
  periodDays,
  onSelect,
  onSupplier,
  onAvailability,
  onExclude,
}: {
  items: ProcurementRecommendation[];
  suppliers: ProcurementSupplier[];
  selected: string[];
  selectedSupplierId?: number;
  savingSupplier: string;
  periodDays: number;
  onSelect: (key: string, checked: boolean) => void;
  onSupplier: (sabyId: string, supplierId: number) => void;
  onAvailability: (item: ProcurementRecommendation, status: string) => void;
  onExclude: (item: ProcurementRecommendation, excluded: boolean) => void;
}) {
  const [expanded, setExpanded] = useState<string[]>([]);
  const id = useId();
  return (
    <div
      className="admin-table-wrap procurement-recommendations-wrap"
      tabIndex={0}
      role="region"
      aria-label="Рекомендации к закупке"
    >
      <table className="admin-table procurement-recommendations-table">
        <colgroup>
          {Array.from({ length: 8 }, (_, index) => (
            <col key={index} />
          ))}
        </colgroup>
        <thead>
          <tr>
            <th scope="col">
              <span className="sr-only">Выбрать</span>
            </th>
            <th scope="col">Товар</th>
            <th scope="col">
              Остаток <small>в пути</small>
            </th>
            <th scope="col">
              Продажи <small>за {periodDays} дн.</small>
            </th>
            <th scope="col">Хватит на</th>
            <th scope="col">
              Заказать <small>штук</small>
            </th>
            <th scope="col">Поставщик</th>
            <th scope="col">
              <span className="sr-only">Подробности</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {items.map((item, index) => {
            const key = `${item.supplierId}:${item.sabyId}`;
            const isExpanded = expanded.includes(key);
            const isSelected = selected.includes(key);
            const urgent =
              item.balanceKnown !== false && item.daysOfCover != null && item.daysOfCover <= 7;
            return (
              <Fragment key={key}>
                <tr data-selected={isSelected || undefined}>
                  <td>
                    {item.status === "recommended" && item.supplierId > 0 && (
                      <input
                        type="checkbox"
                        aria-label={`Выбрать ${item.name}`}
                        checked={isSelected}
                        disabled={
                          selectedSupplierId != null && selectedSupplierId !== item.supplierId
                        }
                        onChange={(event) => onSelect(key, event.target.checked)}
                      />
                    )}
                  </td>
                  <td>
                    <strong>{item.name}</strong>
                    <small>
                      {item.supplierArticle ||
                        (item.supplierId ? "Артикул не заполнен" : "Нужен поставщик")}
                      {item.customerRequests > 0 && (
                        <span className="procurement-client-demand">
                          Клиенту: {item.customerRequests} шт.
                        </span>
                      )}
                    </small>
                  </td>
                  <td>
                    <strong>
                      {item.balanceKnown === false ? "Неизвестен" : `${item.balance} шт.`}
                    </strong>
                    <small>{item.incoming > 0 ? `Едет ${item.incoming} шт.` : "В пути нет"}</small>
                  </td>
                  <td>
                    <strong>{item.totalSales} шт.</strong>
                  </td>
                  <td>
                    <span className={urgent ? "procurement-cover-urgent" : "procurement-cover"}>
                      {item.daysOfCover == null
                        ? "Нет продаж"
                        : `${Math.round(item.daysOfCover)} дн.`}
                    </span>
                  </td>
                  <td>
                    <strong className="procurement-suggested-quantity">
                      {item.quantityKnown === false ? "Вручную" : `${item.suggestedQty} шт.`}
                    </strong>
                    <small>
                      {item.orderMultiple > 1
                        ? item.quantityKnown === false
                          ? `В упаковке ${item.orderMultiple} шт.`
                          : `${recommendedPackageCount(item)} уп. по ${item.orderMultiple} шт.`
                        : "Поштучно"}
                    </small>
                  </td>
                  <td>
                    <select
                      aria-label={`Поставщик для ${item.name}`}
                      value={item.supplierId}
                      disabled={savingSupplier === item.sabyId}
                      onChange={(event) => onSupplier(item.sabyId, Number(event.target.value))}
                    >
                      <option value={0}>Не назначен</option>
                      {suppliers.map((supplier) => (
                        <option key={supplier.id} value={supplier.id}>
                          {supplier.name}
                        </option>
                      ))}
                    </select>
                    {savingSupplier === item.sabyId && <small role="status">Сохраняем…</small>}
                  </td>
                  <td>
                    <button
                      className="procurement-expand-row"
                      aria-label={`Расчёт и действия: ${item.name}`}
                      aria-expanded={isExpanded}
                      aria-controls={`${id}-${index}`}
                      onClick={() =>
                        setExpanded((current) =>
                          isExpanded ? current.filter((value) => value !== key) : [...current, key],
                        )
                      }
                    >
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.7"
                        aria-hidden="true"
                      >
                        <path d={isExpanded ? "m6 15 6-6 6 6" : "m6 9 6 6 6-6"} />
                      </svg>
                    </button>
                  </td>
                </tr>
                {isExpanded && (
                  <tr className="procurement-recommendation-detail" id={`${id}-${index}`}>
                    <td colSpan={8}>
                      <div className="procurement-recommendation-detail-grid">
                        <div>
                          <strong>Почему столько</strong>
                          <p>{item.reason}</p>
                          <p>
                            {item.roundingExplanation ||
                              `Минимум ${item.minimumOrderQty} шт., кратность ${item.orderMultiple}.`}
                          </p>
                          <p>
                            Запросы: клиентам {item.customerRequests}, магазину {item.staffRequests}
                            .
                          </p>
                        </div>
                        <div>
                          <strong>Продажи и остаток</strong>
                          <p>
                            Магазин {item.sabySales} · сайт {item.siteSales} · WB {item.wbSales} ·
                            Ozon {item.ozonSales}
                          </p>
                          <p>
                            Всего {item.grossSales ?? item.totalSales} − возвраты{" "}
                            {item.returns ?? 0} = {item.totalSales} шт.
                          </p>
                          <p>
                            {item.balanceAsOf
                              ? `Остаток СБИС: ${new Date(item.balanceAsOf).toLocaleString("ru-RU")}`
                              : "Дата остатка неизвестна"}
                          </p>
                          {item.lastSaleAt && (
                            <p>
                              Последняя продажа:{" "}
                              {new Date(item.lastSaleAt).toLocaleDateString("ru-RU")}
                            </p>
                          )}
                          {item.lastOrderedAt && (
                            <p>
                              Последняя закупка:{" "}
                              {new Date(item.lastOrderedAt).toLocaleDateString("ru-RU")}
                            </p>
                          )}
                        </div>
                        <div className="procurement-recommendation-actions">
                          <strong>Действия с товаром</strong>
                          {item.supplierId > 0 && item.status !== "excluded" && (
                            <>
                              {item.status !== "check_availability" && (
                                <button onClick={() => onAvailability(item, "check")}>
                                  Проверить наличие
                                </button>
                              )}
                              {item.status !== "supplier_unavailable" && (
                                <button
                                  onClick={() => onAvailability(item, "temporarily_unavailable")}
                                >
                                  Нет у поставщика
                                </button>
                              )}
                            </>
                          )}
                          <button onClick={() => onExclude(item, item.status !== "excluded")}>
                            {item.status === "excluded" ? "Вернуть в закупку" : "Не закупать"}
                          </button>
                        </div>
                      </div>
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
