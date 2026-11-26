import { useEffect, useState } from "react";
import { api, money } from "./adminShared";
import type { NomenclatureCandidate, ProcurementAlias } from "./adminTypes";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export default function ProcurementMatchDialog({
  alias,
  initialQuery,
  nested = false,
  onClose,
  onSaved,
  onError: externalError,
}: {
  alias: ProcurementAlias;
  initialQuery?: string;
  nested?: boolean;
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(onClose);
  const [query, setQuery] = useState(initialQuery || alias.rawName);
  const [items, setItems] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => {
    if (query.trim().length < 2) {
      setItems([]);
      setSearching(false);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      setSearching(true);
      api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`,
      )
        .then((result) => {
          if (!cancelled) setItems(result.items);
        })
        .catch((error) => {
          if (!cancelled) onError((error as Error).message);
        })
        .finally(() => {
          if (!cancelled) setSearching(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, onError]);
  const resolve = async (matchStatus: "confirmed" | "new_product" | "ignored", sabyId = "") => {
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/aliases/${alias.id}`, {
        method: "PATCH",
        body: JSON.stringify({ matchStatus, sabyId }),
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const refreshSaby = async () => {
    setRefreshing(true);
    try {
      await api("/api/v1/admin/procurement/integrations/saby/catalog", { method: "POST" });
      const result = await api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`,
      );
      setItems(result.items);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRefreshing(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className={`admin-dialog-backdrop ${nested ? "procurement-match-backdrop" : ""}`}
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        className="admin-dialog procurement-match-dialog"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="match-title"
      >
        <header>
          <div>
            <p className="eyebrow">{alias.supplierName}</p>
            <h2 id="match-title">Выбрать товар СБИС</h2>
          </div>
          <button onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        <div className="procurement-match-source">
          <strong>{alias.rawName}</strong>
          <span>
            {[
              alias.supplierArticle && `Артикул ${alias.supplierArticle}`,
              alias.potDiameterCm && `D${alias.potDiameterCm}`,
              alias.heightCm && `${alias.heightCm} см`,
            ]
              .filter(Boolean)
              .join(" · ") || "Размер не указан"}
          </span>
        </div>
        <p className="admin-hint">
          Выбор сохранится для следующих инвойсов с тем же названием и размером.
        </p>
        <label className="procurement-match-search">
          Поиск товара СБИС
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            autoFocus
            placeholder="Название, артикул или код X…"
          />
        </label>
        <div className="procurement-candidates">
          {searching ? (
            <div className="procurement-zero">
              <span>Ищем в справочнике…</span>
            </div>
          ) : items.length ? (
            items.map((item) => (
              <article key={item.sabyId}>
                <div>
                  <strong>{item.name}</strong>
                  <span>{[item.code, item.article].filter(Boolean).join(" · ")}</span>
                  <small>
                    Остаток СБИС: {item.balance} · {money.format(item.price)}
                  </small>
                </div>
                <button disabled={saving} onClick={() => void resolve("confirmed", item.sabyId)}>
                  Выбрать
                </button>
              </article>
            ))
          ) : (
            <div className="procurement-zero">
              <strong>Кандидаты не найдены</strong>
              <span>Импортируйте новую карточку СБИС или измените запрос.</span>
            </div>
          )}
        </div>

        <div className="dialog-actions procurement-match-actions">
          <button onClick={() => void resolve("ignored")} disabled={saving || refreshing}>
            Игнорировать строку
          </button>
          <button onClick={() => void resolve("new_product")} disabled={saving || refreshing}>
            Это новый товар
          </button>
          <button onClick={() => void refreshSaby()} disabled={saving || refreshing}>
            {refreshing ? "Обновляем СБИС…" : "Обновить из СБИС"}
          </button>
          <button onClick={onClose}>Отмена</button>
        </div>
      </div>
    </>
  );
}
