import { useEffect, useState } from "react";
import { api } from "./adminShared";
import type { NomenclatureCandidate, ProcurementOrderLine } from "./adminTypes";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export default function ProcurementSiteMatchDialog({
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
  const [query, setQuery] = useState(line.catalogueName || line.rawName || line.invoiceRawName);
  const [items, setItems] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const { error, errorRef, notify } = useProcurementError(onError);
  const close = () => {
    if (!saving) onClose();
  };
  const ref = useProcurementDialog(close);
  useEffect(() => {
    const term = query.trim();
    if (term.length < 2) return;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      setSearching(true);
      try {
        const result = await api<{ items: NomenclatureCandidate[] }>(
          `/api/v1/admin/procurement/nomenclature?source=site&q=${encodeURIComponent(term)}`,
          { signal: controller.signal },
        );
        if (!controller.signal.aborted) setItems(result.items);
      } catch (failure) {
        if (!controller.signal.aborted) notify((failure as Error).message);
      } finally {
        if (!controller.signal.aborted) setSearching(false);
      }
    }, 200);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, notify]);
  const select = async (variantId: number) => {
    setSaving(true);
    try {
      const match = line.matchAlias
        ? {
            endpoint: `aliases/${line.matchAlias.id}`,
            body: { matchStatus: "confirmed", variantId, orderLineId: line.id },
          }
        : { endpoint: `order-lines/${line.id}`, body: { siteVariantId: variantId } };
      await api(`/api/v1/admin/procurement/${match.endpoint}`, {
        method: "PATCH",
        body: JSON.stringify(match.body),
      });
      onSaved();
    } catch (failure) {
      notify((failure as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        aria-label="Закрыть выбор"
        className="admin-dialog-backdrop procurement-match-backdrop"
        onClick={close}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="procurement-site-match-title"
        className="admin-dialog procurement-match-dialog"
        ref={ref}
      >
        <header>
          <h2 id="procurement-site-match-title">Выбрать карточку сайта</h2>
          <button type="button" aria-label="Закрыть" disabled={saving} onClick={close}>
            ×
          </button>
        </header>
        <p>Инвойс: {line.invoiceRawName || line.rawName}</p>
        <label>
          Название или артикул на сайте
          <input
            value={query}
            disabled={saving}
            onChange={(event) => {
              setQuery(event.target.value);
              setItems([]);
              setSearching(event.target.value.trim().length >= 2);
            }}
          />
        </label>
        {error && (
          <p ref={errorRef} tabIndex={-1} role="alert" className="procurement-warning">
            {error}
          </p>
        )}
        <div className="procurement-site-match-results">
          {searching ? (
            <p role="status">Ищем в каталоге сайта…</p>
          ) : items.length ? (
            items.map((item) => (
              <div key={item.variantId}>
                <span>
                  <strong>{item.name}</strong>
                  <small>{item.code}</small>
                </span>
                <button type="button" disabled={saving} onClick={() => void select(item.variantId)}>
                  Выбрать
                </button>
              </div>
            ))
          ) : (
            <p>
              {query.trim().length < 2
                ? "Введите хотя бы два символа."
                : "Нет подходящих карточек."}
            </p>
          )}
        </div>
        <div className="dialog-actions">
          <a href="/admin?section=products&create=1" target="_blank" rel="noreferrer">
            Создать карточку сайта
          </a>
        </div>
        <p>После создания вернитесь к инвойсу и найдите карточку по названию.</p>
      </div>
    </>
  );
}
