import { useEffect, useState } from "react";
import { api, money } from "./adminShared";
import { NumericInput } from "./NumericInput";

type Folder = { id: string; name: string; path: string[] };
type Candidate = {
  id: string;
  code: string;
  article: string;
  name: string;
  missing: boolean;
  linkedVariantId?: number;
  linkedProductId?: number;
  mappedProductId?: number;
};
export type SabyStepDraft = {
  mode: "create" | "link";
  folderId: string;
  initialStock: string;
  query: string;
  selected: Candidate | null;
};
const emptySabyDraft: SabyStepDraft = {
  mode: "create",
  folderId: "",
  initialStock: "",
  query: "",
  selected: null,
};
export type SabyCreationPlan =
  | { mode: "create"; folderId: string; folderName: string; initialStock: number }
  | { mode: "link"; existingId: string; existingName: string };

export function ProductCreationSabyStep({
  name,
  pot,
  height,
  price,
  value,
  onBack,
  onNext,
  draft,
  onDraftChange,
  showActions = true,
  plant = true,
}: {
  name: string;
  pot: string;
  height: string;
  price: string;
  value: SabyCreationPlan | null;
  onBack: () => void;
  onNext: (plan: SabyCreationPlan) => void;
  draft?: SabyStepDraft;
  onDraftChange?: (draft: SabyStepDraft) => void;
  showActions?: boolean;
  plant?: boolean;
}) {
  const [localDraft, setLocalDraft] = useState<SabyStepDraft>(() => ({
    ...emptySabyDraft,
    mode: value?.mode || "create",
    folderId: value?.mode === "create" ? value.folderId : "",
    initialStock: value?.mode === "create" ? String(value.initialStock) : "",
    selected:
      value?.mode === "link"
        ? { id: value.existingId, name: value.existingName, code: "", article: "", missing: false }
        : null,
  }));
  const { mode, folderId, initialStock, query, selected } = draft ?? localDraft;
  const change = (patch: Partial<SabyStepDraft>) => {
    const next = { ...(draft ?? localDraft), ...patch };
    if (onDraftChange) onDraftChange(next);
    else setLocalDraft(next);
  };
  const [folders, setFolders] = useState<Folder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  useEffect(() => {
    let active = true;
    api<{ folders: Folder[] }>("/api/v1/admin/procurement/products?limit=1&offset=0", {
      signal: AbortSignal.timeout(15000),
    })
      .then((data) => {
        if (active) {
          setFolders(data.folders || []);
          setLoading(false);
        }
      })
      .catch((cause) => {
        if (active) {
          setError(
            (cause as Error).name === "TimeoutError"
              ? "Справочник СБИС не ответил. Попробуйте ещё раз."
              : (cause as Error).message,
          );
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [revision]);
  const [searchBusy, setSearchBusy] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [searched, setSearched] = useState(false);
  const search = async () => {
    if (query.trim().length < 2 || searchBusy) return;
    setSearchBusy(true);
    setSearchError("");
    change({ selected: null });
    setCandidates([]);
    setSearched(false);
    try {
      const data = await api<{ candidates: Candidate[] }>(
        `/api/v1/admin/saby/link-candidates?q=${encodeURIComponent(query.trim())}`,
        { signal: AbortSignal.timeout(15000) },
      );
      setCandidates(data.candidates || []);
      setSearched(true);
    } catch (cause) {
      setSearchError(
        (cause as Error).name === "TimeoutError"
          ? "Поиск СБИС не ответил. Попробуйте ещё раз."
          : (cause as Error).message,
      );
    } finally {
      setSearchBusy(false);
    }
  };
  const folder = folders.find((item) => item.id === folderId);
  const stockValid =
    initialStock.trim() !== "" &&
    Number.isSafeInteger(Number(initialStock)) &&
    Number(initialStock) >= 0;
  const visible = folders.filter((item) =>
    (item.path.length ? item.path.join(" / ") : item.name)
      .toLocaleLowerCase("ru")
      .includes(query.trim().toLocaleLowerCase("ru")),
  );
  const ready =
    mode === "create"
      ? Boolean(folder && stockValid && !loading && !error)
      : Boolean(selected && !searchBusy);
  const proceed = () => {
    if (!ready) return;
    if (mode === "create" && folder)
      onNext({
        mode,
        folderId: folder.id,
        folderName: folder.path.length ? folder.path.join(" / ") : folder.name,
        initialStock: Number(initialStock),
      });
    else if (mode === "link" && selected)
      onNext({ mode, existingId: selected.id, existingName: selected.name });
  };
  return (
    <section className="product-create-saby">
      <p className="product-create-summary">
        <strong>{name}</strong>
        <span>
          {plant && (
            <>
              Горшок {pot || "—"} см · Высота {height || "—"} см ·{" "}
            </>
          )}
          {price ? money.format(Number(price)) : "Цена не указана"}
        </span>
      </p>
      <div className="product-create-modes" role="group" aria-label="Способ подключения СБИС">
        <button
          type="button"
          aria-pressed={mode === "create"}
          onClick={() => {
            change({ mode: "create", query: "" });
          }}
        >
          Создать новый товар
        </button>
        <button
          type="button"
          aria-pressed={mode === "link"}
          onClick={() => {
            change({ mode: "link", query: "" });
          }}
        >
          Связать с существующим
        </button>
      </div>
      {mode === "create" ? (
        <>
          <div className="admin-field">
            <label className="admin-field-label" htmlFor="saby-create-category">
              Категория в СБИС
            </label>
            <input
              id="saby-create-category"
              placeholder="Найти категорию СБИС"
              value={query}
              onChange={(event) => change({ query: event.target.value })}
            />
            <div className="product-create-folders" role="group" aria-label="Категории СБИС">
              {loading ? (
                <p role="status">Загружаем категории…</p>
              ) : error ? (
                <p role="alert">
                  {error}{" "}
                  <button
                    type="button"
                    onClick={() => {
                      setLoading(true);
                      setError("");
                      setRevision((current) => current + 1);
                    }}
                  >
                    Повторить
                  </button>
                </p>
              ) : visible.length ? (
                visible.map((item) => (
                  <button
                    type="button"
                    key={item.id}
                    aria-pressed={folderId === item.id}
                    onClick={() => change({ folderId: item.id })}
                  >
                    {item.path.length ? item.path.join(" / ") : item.name}
                  </button>
                ))
              ) : (
                <p>
                  {folders.length
                    ? "Категории не найдены."
                    : "В справочнике нет категорий СБИС. Можно связать с существующим товаром."}
                </p>
              )}
            </div>
          </div>
          <div className="admin-field product-create-stock">
            <label className="admin-field-label" htmlFor="saby-initial-stock">
              Начальный остаток, шт.
            </label>
            <NumericInput
              id="saby-initial-stock"
              required
              min="0"
              step="1"
              value={initialStock}
              onChange={(event) => change({ initialStock: event.target.value })}
              placeholder="0"
            />
          </div>
        </>
      ) : (
        <>
          <div className="admin-field">
            <label className="admin-field-label" htmlFor="saby-existing-search">
              Товар в СБИС
            </label>
            <div className="product-create-search">
              <input
                id="saby-existing-search"
                placeholder="Название, код или артикул"
                value={query}
                onChange={(event) => {
                  change({ query: event.target.value, selected: null });
                  setCandidates([]);
                  setSearched(false);
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    void search();
                  }
                }}
              />
              <button
                type="button"
                disabled={query.trim().length < 2 || searchBusy}
                onClick={() => void search()}
              >
                {searchBusy ? "Ищем…" : "Найти"}
              </button>
            </div>
          </div>
          {searchError && (
            <p role="alert" className="admin-inline-error">
              {searchError}
            </p>
          )}
          <div className="product-create-folders" role="group" aria-label="Товары СБИС">
            {selected && !candidates.length && (
              <button type="button" aria-pressed="true">
                {selected.name}
              </button>
            )}
            {candidates.map((item) => {
              const occupied = Boolean(
                item.linkedVariantId || item.linkedProductId || item.mappedProductId,
              );
              return (
                <button
                  type="button"
                  key={item.id}
                  disabled={item.missing || occupied}
                  aria-pressed={selected?.id === item.id}
                  onClick={() => change({ selected: item })}
                >
                  <span>
                    {item.name}
                    <small>
                      {[
                        item.code && `Код ${item.code}`,
                        item.article && `Артикул ${item.article}`,
                        item.missing
                          ? "Нет в актуальном справочнике"
                          : occupied
                            ? "Уже связан с карточкой сайта"
                            : "",
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </small>
                  </span>
                </button>
              );
            })}
            {searched && !candidates.length && (
              <p>Товары не найдены. Попробуйте другое название или код.</p>
            )}
          </div>
          <p className="product-create-stock-note">Остаток связанного товара берётся из СБИС.</p>
        </>
      )}
      {showActions && (
        <div className="dialog-actions">
          <button type="button" onClick={onBack}>
            Назад
          </button>
          <button type="button" className="primary" disabled={!ready} onClick={proceed}>
            Далее
          </button>
        </div>
      )}
    </section>
  );
}
