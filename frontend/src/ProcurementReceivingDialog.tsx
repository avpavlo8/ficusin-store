import { NumericInput } from "./NumericInput";
import { useEffect, useState } from "react";
import { api } from "./adminShared";
import type {
  NomenclatureCandidate,
  ProcurementOrderLine,
  ProcurementReceivingAllocation,
} from "./adminTypes";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export function ProcurementReceivingDialog({
  line,
  onClose,
  onSaved,
  onError,
}: {
  line: ProcurementOrderLine;
  onClose: () => void;
  onSaved: () => void;
  onError: (message: string) => void;
}) {
  const [mode, setMode] = useState<string>(line.receivingMode || "catalog");
  const [parts, setParts] = useState<ProcurementReceivingAllocation[]>(
    line.receivingAllocations || [],
  );
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const { error, errorRef, notify } = useProcurementError(onError);
  const close = () => {
    if (!saving) onClose();
  };
  const ref = useProcurementDialog(close);
  const quantity = line.invoicedQuantity || 0;
  const total = parts.reduce((sum, part) => sum + part.quantity, 0);
  const complete =
    parts.length === 0 ||
    (total === quantity &&
      parts.every((part) => Number.isInteger(part.quantity) && part.quantity > 0));
  useEffect(() => {
    let cancelled = false;
    if (mode !== "assortment" || query.trim().length < 2) {
      setCandidates([]);
      setSearching(false);
      return;
    }
    const timer = window.setTimeout(() => {
      setSearching(true);
      void api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`,
      )
        .then((result) => {
          if (!cancelled) setCandidates(result.items);
        })
        .catch((error) => {
          if (!cancelled) notify((error as Error).message);
        })
        .finally(() => {
          if (!cancelled) setSearching(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [mode, query, notify]);
  const save = async () => {
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/order-lines/${line.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          receivingMode: mode,
          ...(mode === "assortment"
            ? {
                receivingAllocations: parts.map((part) => ({
                  sabyId: part.sabyId,
                  quantity: part.quantity,
                })),
              }
            : {}),
        }),
      });
      onSaved();
    } catch (error) {
      notify((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop procurement-receiving-backdrop"
        aria-label="Закрыть способ приёмки"
        disabled={saving}
        onClick={close}
      />
      <div
        ref={ref}
        className="admin-dialog procurement-receiving-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="receiving-title"
      >
        <header>
          <div>
            <p className="eyebrow">{line.rawName}</p>
            <h2 id="receiving-title">Способ приёмки</h2>
          </div>
          <button aria-label="Закрыть способ приёмки" disabled={saving} onClick={close}>
            ×
          </button>
        </header>
        <div className="procurement-receiving-body">
          <p>
            <strong>{quantity} шт.</strong> · {line.invoiceRawName}
          </p>
          <label>
            Как принимаем растение
            <select
              aria-label="Способ приёмки растения"
              value={mode}
              disabled={saving}
              onChange={(event) => setMode(event.target.value)}
            >
              <option value="catalog">В одну карточку СБИС</option>
              <option value="awaiting_catalog">Карточки СБИС пока нет — выбрать позже</option>
              <option value="assortment">Разобрать по карточкам после приезда</option>
              <option value="customer_handoff">Под заказ — передать клиенту</option>
            </select>
          </label>
          {mode === "awaiting_catalog" && (
            <p>Текущая связь снимется. Выберите новую карточку перед поступлением.</p>
          )}
          {mode === "catalog" && (
            <p>
              {line.sabyName || "Выберите карточку в строке закупки."}{" "}
              {line.sabyCode && `· ${line.sabyCode}`}
            </p>
          )}
          {mode === "customer_handoff" && (
            <p>
              После приезда отметьте передачу клиенту. В поступление СБИС эта позиция не войдёт.
            </p>
          )}
          {mode === "assortment" && (
            <>
              <p>
                Выберите карточки и количество каждого сорта. Разбор можно оставить до приезда
                растений.
              </p>
              <div className="procurement-receiving-total" aria-live="polite">
                <strong>
                  Распределено {total} из {quantity} шт.
                </strong>
                <span>
                  {total === quantity
                    ? "Количество сходится"
                    : total > quantity
                      ? `Лишних: ${total - quantity} шт.`
                      : `Осталось: ${quantity - total} шт.`}
                </span>
              </div>
              {parts.map((part) => (
                <div key={part.sabyId} className="procurement-receiving-part">
                  <div>
                    <strong>{part.name}</strong>
                    <small>{part.code || part.sabyId}</small>
                  </div>
                  <label>
                    Количество
                    <NumericInput
                      aria-label={`Количество: ${part.name}`}
                      min="1"
                      step="1"
                      value={part.quantity || ""}
                      disabled={saving}
                      onChange={(event) =>
                        setParts((current) =>
                          current.map((item) =>
                            item.sabyId === part.sabyId
                              ? { ...item, quantity: Number(event.target.value) }
                              : item,
                          ),
                        )
                      }
                    />
                  </label>
                  <button
                    disabled={saving}
                    aria-label={`Убрать ${part.name}`}
                    onClick={() =>
                      setParts((current) => current.filter((item) => item.sabyId !== part.sabyId))
                    }
                  >
                    Убрать
                  </button>
                </div>
              ))}
              <label>
                Добавить существующую карточку
                <input
                  aria-label="Поиск карточки для разбора"
                  placeholder="Название, артикул или код СБИС"
                  value={query}
                  disabled={saving}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </label>
              {searching ? (
                <p>Ищем карточки…</p>
              ) : (
                query.trim().length >= 2 && (
                  <div className="procurement-candidates">
                    {candidates
                      .filter((item) => !parts.some((part) => part.sabyId === item.sabyId))
                      .map((item) => (
                        <article key={item.sabyId}>
                          <div>
                            <strong>{item.name}</strong>
                            <small>{[item.code, item.article].filter(Boolean).join(" · ")}</small>
                          </div>
                          <button
                            disabled={saving}
                            onClick={() => {
                              setParts((current) => [
                                ...current,
                                {
                                  sabyId: item.sabyId,
                                  name: item.name,
                                  code: item.code,
                                  quantity: 1,
                                },
                              ]);
                              setQuery("");
                            }}
                          >
                            Добавить
                          </button>
                        </article>
                      ))}
                    {candidates.length === 0 && <p>Карточки не найдены. Измените запрос.</p>}
                  </div>
                )
              )}
              {!complete && (
                <p className="procurement-warning">
                  Для сохранения разбора распределите все {quantity} шт. или уберите карточки, чтобы
                  отложить разбор.
                </p>
              )}
            </>
          )}
          {error && (
            <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
              {error}
            </div>
          )}
        </div>
        <div className="dialog-actions">
          <button disabled={saving} onClick={close}>
            Отмена
          </button>
          <button
            className="primary"
            disabled={saving || (mode === "assortment" && !complete)}
            onClick={() => void save()}
          >
            {saving
              ? "Сохраняем…"
              : mode === "assortment" && parts.length
                ? "Сохранить распределение"
                : "Сохранить способ приёмки"}
          </button>
        </div>
      </div>
    </>
  );
}
