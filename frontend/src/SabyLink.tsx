import { useState } from "react";
import { api } from "./adminShared";

type SabyCandidate = {
  id: string;
  code: string;
  article: string;
  name: string;
  missing: boolean;
  linkedVariantId?: number;
  linkedProductId?: number;
  mappedProductId?: number;
};

export default function SabyLinkPicker({
  product,
  variant,
  onLinked,
  onError,
}: {
  product: { id: number; name: string };
  variant: { id: number; sku: string; label: string; sabyId: string };
  onLinked: () => void;
  onError: (message: string) => void;
}) {
  const replacing = Boolean(variant.sabyId);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<SabyCandidate[]>([]);
  const [selected, setSelected] = useState<SabyCandidate | null>(null);
  const [busy, setBusy] = useState(false);
  const search = async () => {
    if (query.trim().length < 2) return;
    setBusy(true);
    setSelected(null);
    try {
      const data = await api<{ candidates: SabyCandidate[] }>(
        `/api/v1/admin/saby/link-candidates?q=${encodeURIComponent(query.trim())}`,
      );
      setCandidates(data.candidates || []);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const link = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      await api(`/api/v1/admin/variants/${variant.id}/${replacing ? "saby-relink" : "saby-link"}`, {
        method: "POST",
        body: JSON.stringify(
          replacing
            ? { oldSabyId: variant.sabyId, newSabyId: selected.id }
            : { sabyId: selected.id },
        ),
      });
      setOpen(false);
      onLinked();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div onClick={(event) => event.stopPropagation()}>
      <button type="button" className="text-button" onClick={() => setOpen(!open)}>
        {open ? "Закрыть поиск" : `${replacing ? "Перепривязать" : "Связать"} SKU ${variant.sku}`}
      </button>
      {open && (
        <div className="admin-pim-editor">
          <p>
            Карточка сайта: <strong>{product.name}</strong> · SKU {variant.sku}
            {variant.label ? ` · ${variant.label}` : ""}
            {replacing ? ` · текущая запись СБИС ${variant.sabyId}` : ""}
          </p>
          <div>
            <input
              aria-label="Поиск номенклатуры СБИС"
              placeholder="Код, артикул или название в СБИС"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void search();
                }
              }}
            />{" "}
            <button
              type="button"
              disabled={busy || query.trim().length < 2}
              onClick={() => void search()}
            >
              Найти
            </button>
          </div>
          {candidates.map((candidate) => {
            const occupiedByOther = Boolean(
              (candidate.linkedProductId && candidate.linkedProductId !== product.id) ||
                (candidate.mappedProductId && candidate.mappedProductId !== product.id),
            );
            return (
              <label key={candidate.id} className="admin-checkbox">
                <input
                  type="radio"
                  name={`saby-link-${variant.id}`}
                  disabled={
                    candidate.missing ||
                    Boolean(candidate.linkedVariantId) ||
                    occupiedByOther ||
                    busy
                  }
                  checked={selected?.id === candidate.id}
                  onChange={() => setSelected(candidate)}
                />
                <span>
                  {candidate.name} · {candidate.code || candidate.id}
                  {candidate.article ? ` · ${candidate.article}` : ""}
                  {candidate.missing
                    ? " · отсутствует в выгрузке"
                    : candidate.linkedVariantId
                      ? ` · уже связан с SKU #${candidate.linkedVariantId}`
                      : occupiedByOther
                        ? ` · уже связан с карточкой #${candidate.linkedProductId || candidate.mappedProductId}`
                        : ""}
                </span>
              </label>
            );
          })}
          {selected && (
            <div>
              <p>
                {replacing
                  ? "Старая запись должна отсутствовать в актуальной выгрузке СБИС. История связи сохранится; остаток обнулится до следующей синхронизации."
                  : "Остаток появится после следующей синхронизации СБИС."}{" "}
                Подтвердите связь <strong>{variant.sku}</strong> →{" "}
                <strong>
                  {selected.name} ({selected.code || selected.id})
                </strong>
                .
              </p>
              <button type="button" disabled={busy} onClick={() => void link()}>
                {busy ? "Связываем…" : replacing ? "Подтвердить перепривязку" : "Подтвердить связь"}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
