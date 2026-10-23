import { NumericInput } from "./NumericInput";
import { useCallback, useEffect, useState } from "react";
import { api, money } from "./adminShared";
import { VariantMediaManager } from "./VariantMediaManager";
import { categoryPath } from "./adminCatalogLogic";
import type { Category } from "./adminTypes";

export type AttributeOption = {
  id?: number;
  code: string;
  label: string;
  sortOrder: number;
  active: boolean;
};
export type AttributeDefinition = {
  id: number;
  code: string;
  name: string;
  description: string;
  dataType: "text" | "string" | "number" | "boolean" | "enum" | "multi_enum";
  unit: string;
  audience: "customer" | "technical";
  scope: "product" | "variant";
  global: boolean;
  active: boolean;
  options: AttributeOption[];
};
export type EffectiveAttribute = AttributeDefinition & {
  required: boolean;
  filterable: boolean;
  showOnPdp: boolean;
  keyCharacteristic: boolean;
  badge: boolean;
  sortOrder: number;
  summaryPosition?: number;
  showInCharacteristics: boolean;
  excluded: boolean;
  inherited: boolean;
  sourceCategoryId?: number;
  sourceCategoryName: string;
};
type PriceProposal = {
  id: number;
  channel: string;
  externalArticle: string;
  oldValue: number | null;
  newValue: number;
  compareAtValue: number | null;
  status: string;
  errorMessage: string;
  canApprove: boolean;
  canRetry: boolean;
  blockReason: string;
  createdAt: string;
};
const priceChannelNames: Record<string, string> = {
  saby_price: "СБИС",
  wb: "Wildberries",
  ozon: "Ozon",
  avito: "Авито",
};

function VariantPriceProposals({
  variantId,
  onError,
}: {
  variantId: number;
  onError: (value: string) => void;
}) {
  const [proposals, setProposals] = useState<PriceProposal[]>([]);
  const [busy, setBusy] = useState<number | null>(null);
  const load = useCallback(() => {
    void api<{ proposals: PriceProposal[] }>(`/api/v1/admin/variants/${variantId}/price-proposals`)
      .then((data) => setProposals(data.proposals || []))
      .catch((error) => onError((error as Error).message));
  }, [variantId, onError]);
  useEffect(() => {
    load();
  }, [load]);
  const approve = async (proposal: PriceProposal) => {
    setBusy(proposal.id);
    try {
      await api(`/api/v1/admin/price-proposals/${proposal.id}/approve`, { method: "POST" });
      load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(null);
    }
  };
  const retry = async (proposal: PriceProposal) => {
    setBusy(proposal.id);
    try {
      await api(`/api/v1/admin/price-proposals/${proposal.id}/retry`, { method: "POST" });
      load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(null);
    }
  };
  const downloadSaby = async (proposal: PriceProposal) => {
    setBusy(proposal.id);
    try {
      const response = await fetch(`/api/v1/admin/price-proposals/${proposal.id}/saby-xlsx`, {
        credentials: "same-origin",
        cache: "no-store",
      });
      if (!response.ok) throw new Error(`Не удалось скачать файл СБИС: ${response.status}`);
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = `saby-manual-price-${proposal.id}.xlsx`;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(null);
    }
  };
  if (proposals.length === 0) return null;
  return (
    <section className="wide variant-price-proposals">
      <h4>Предложения цен для каналов</h4>
      <p className="admin-hint">
        Цена сайта сохранена. Изменения в каждом канале подтвердите отдельно.
      </p>
      <div className="admin-table-wrap">
        <table className="admin-table">
          <thead>
            <tr>
              <th>Канал</th>
              <th>Артикул</th>
              <th>Сейчас</th>
              <th>Предложено</th>
              <th>Статус</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {proposals.map((proposal) => (
              <tr key={proposal.id}>
                <td>{priceChannelNames[proposal.channel] || proposal.channel}</td>
                <td>{proposal.externalArticle || "—"}</td>
                <td>{proposal.oldValue == null ? "—" : money.format(proposal.oldValue)}</td>
                <td>{money.format(proposal.newValue)}</td>
                <td>
                  {proposal.status === "draft"
                    ? proposal.blockReason || "Ожидает подтверждения"
                    : proposal.status === "approved" && proposal.channel === "saby_price"
                      ? "Нужен импорт файла в СБИС"
                      : proposal.status === "queued"
                        ? "Отправляется"
                        : proposal.status === "completed"
                          ? "Применена"
                          : proposal.status === "skipped"
                            ? "Пропущена"
                            : proposal.status === "failed"
                              ? "Ошибка"
                              : proposal.status === "not_configured"
                                ? "Интеграция не подключена"
                                : proposal.status}
                  {proposal.errorMessage && proposal.errorMessage !== proposal.blockReason && (
                    <small>{proposal.errorMessage}</small>
                  )}
                  {proposal.status !== "draft" && proposal.blockReason && (
                    <small>{proposal.blockReason}</small>
                  )}
                </td>
                <td>
                  {proposal.status === "draft" && proposal.canApprove && (
                    <button
                      type="button"
                      disabled={busy !== null}
                      onClick={() => void approve(proposal)}
                    >
                      {busy === proposal.id ? "Подтверждаем…" : "Подтвердить"}
                    </button>
                  )}
                  {proposal.canRetry && (
                    <button
                      type="button"
                      disabled={busy !== null}
                      onClick={() => void retry(proposal)}
                    >
                      {busy === proposal.id ? "Повторяем…" : "Повторить"}
                    </button>
                  )}
                  {proposal.status === "approved" && proposal.channel === "saby_price" && (
                    <button
                      type="button"
                      disabled={busy !== null}
                      onClick={() => void downloadSaby(proposal)}
                    >
                      Скачать XLSX
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export type VariantMapping = { provider: string; type: string; externalId: string };
export type AdminVariant = {
  id: number;
  productId: number;
  sku: string;
  label: string;
  price: number;
  stock: number;
  wholesaleMinQty: number;
  active: boolean;
  archived: boolean;
  attributes: Record<string, string | number | boolean | string[]>;
  externalIds: VariantMapping[];
  images: string[];
  managedFields: string[];
};
export type CatalogFilter = {
  id: number;
  code: string;
  title: string;
  attributeId: number;
  attributeCode: string;
  categoryId?: number;
  displayMode: "select" | "chips" | "range";
  sortOrder: number;
  active: boolean;
};

type DraftDefinition = Omit<AttributeDefinition, "id" | "options"> & { optionsText: string };
const emptyDefinition: DraftDefinition = {
  code: "",
  name: "",
  description: "",
  dataType: "enum",
  unit: "",
  audience: "customer",
  scope: "product",
  global: false,
  active: true,
  optionsText: "",
};

function catalogCode(value: string) {
  const alphabet: Record<string, string> = {
    а: "a",
    б: "b",
    в: "v",
    г: "g",
    д: "d",
    е: "e",
    ё: "e",
    ж: "zh",
    з: "z",
    и: "i",
    й: "y",
    к: "k",
    л: "l",
    м: "m",
    н: "n",
    о: "o",
    п: "p",
    р: "r",
    с: "s",
    т: "t",
    у: "u",
    ф: "f",
    х: "h",
    ц: "ts",
    ч: "ch",
    ш: "sh",
    щ: "sch",
    ъ: "",
    ы: "y",
    ь: "",
    э: "e",
    ю: "yu",
    я: "ya",
  };
  return Array.from(value.toLowerCase())
    .map((x) => alphabet[x] ?? x)
    .join("")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 60);
}

function optionText(options: AttributeOption[]) {
  return options.map((option) => `${option.code} | ${option.label}`).join("\n");
}
function parseOptions(text: string): AttributeOption[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => {
      const [rawCode, ...rest] = line.split("|");
      const code = rawCode
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9_-]+/g, "_");
      const label = rest.join("|").trim() || rawCode.trim();
      return { code, label, sortOrder: (index + 1) * 10, active: true };
    });
}

function AttributeDefinitionEditor({
  value,
  onSaved,
  onCancel,
  onError,
}: {
  value?: AttributeDefinition;
  onSaved: () => void;
  onCancel: () => void;
  onError: (value: string) => void;
}) {
  const [codeEdited, setCodeEdited] = useState(Boolean(value));
  const [draft, setDraft] = useState<DraftDefinition>(() =>
    value
      ? {
          code: value.code,
          name: value.name,
          description: value.description,
          dataType: value.dataType,
          unit: value.unit,
          audience: value.audience,
          scope: value.scope,
          global: value.global,
          active: value.active,
          optionsText: optionText(value.options),
        }
      : emptyDefinition,
  );
  const save = async () => {
    try {
      const body = JSON.stringify({
        ...draft,
        optionsText: undefined,
        options: ["enum", "multi_enum"].includes(draft.dataType)
          ? parseOptions(draft.optionsText)
          : [],
      });
      await api(value ? `/api/v1/admin/attributes/${value.id}` : "/api/v1/admin/attributes", {
        method: value ? "PATCH" : "POST",
        body,
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  return (
    <div className="admin-pim-editor">
      <div className="admin-form-grid">
        <label>
          Название
          <input
            value={draft.name}
            onChange={(event) =>
              setDraft({
                ...draft,
                name: event.target.value,
                ...(!codeEdited ? { code: catalogCode(event.target.value) } : {}),
              })
            }
          />
        </label>
        <details>
          <summary>Технический код</summary>
          <label>
            Код характеристики
            <input
              disabled={Boolean(value)}
              value={draft.code}
              onChange={(event) => {
                setCodeEdited(true);
                setDraft({
                  ...draft,
                  code: event.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, "_"),
                });
              }}
            />
            <small>
              Создаётся из названия. После сохранения не меняется: используется правилами подборок.
            </small>
          </label>
        </details>
        <label>
          Тип
          <select
            value={draft.dataType}
            onChange={(event) =>
              setDraft({ ...draft, dataType: event.target.value as DraftDefinition["dataType"] })
            }
          >
            <option value="text">Текст</option>
            <option value="number">Число</option>
            <option value="boolean">Да / нет</option>
            <option value="enum">Один вариант</option>
            <option value="multi_enum">Несколько вариантов</option>
          </select>
        </label>
        <label>
          Единица
          <input
            value={draft.unit}
            onChange={(event) => setDraft({ ...draft, unit: event.target.value })}
            placeholder="см, г, мл"
          />
        </label>
        <label>
          Уровень
          <select
            value={draft.scope}
            onChange={(event) =>
              setDraft({ ...draft, scope: event.target.value as DraftDefinition["scope"] })
            }
          >
            <option value="product">Общий для товара</option>
            <option value="variant">Отдельный для размера</option>
          </select>
        </label>
        <label>
          Видимость
          <select
            value={draft.audience}
            onChange={(event) =>
              setDraft({ ...draft, audience: event.target.value as DraftDefinition["audience"] })
            }
          >
            <option value="customer">Виден покупателю</option>
            <option value="technical">Технический / внутренний</option>
          </select>
        </label>
        <label className="wide">
          Описание
          <textarea
            rows={2}
            value={draft.description}
            onChange={(event) => setDraft({ ...draft, description: event.target.value })}
          />
        </label>
        {["enum", "multi_enum"].includes(draft.dataType) && (
          <label className="wide">
            Варианты — по одному на строку: <code>code | Название</code>
            <textarea
              rows={6}
              value={draft.optionsText}
              onChange={(event) => setDraft({ ...draft, optionsText: event.target.value })}
            />
          </label>
        )}
        <label className="admin-checkbox">
          <input
            type="checkbox"
            checked={draft.global}
            onChange={(event) => setDraft({ ...draft, global: event.target.checked })}
          />
          Доступен всем категориям
        </label>
        <label className="admin-checkbox">
          <input
            type="checkbox"
            checked={draft.active}
            onChange={(event) => setDraft({ ...draft, active: event.target.checked })}
          />
          Активен
        </label>
      </div>
      <div className="dialog-actions">
        <button onClick={onCancel}>Отмена</button>
        <button
          className="primary"
          disabled={!draft.name.trim() || !draft.code.trim()}
          onClick={save}
        >
          Сохранить
        </button>
      </div>
    </div>
  );
}

function AttributeAssignmentRow({
  item,
  categoryId,
  onSaved,
  onError,
}: {
  item: EffectiveAttribute;
  categoryId: number;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState(item);
  const save = async () => {
    try {
      await api(`/api/v1/admin/categories/${categoryId}/attributes/${item.id}`, {
        method: "PUT",
        body: JSON.stringify({
          attributeId: item.id,
          required: draft.required,
          filterable: draft.filterable,
          showOnPdp: draft.showOnPdp,
          keyCharacteristic: draft.keyCharacteristic,
          badge: draft.badge,
          sortOrder: draft.sortOrder,
          summaryPosition: draft.summaryPosition || null,
          showInCharacteristics: draft.showInCharacteristics,
          excluded: draft.excluded,
        }),
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const reset = async () => {
    try {
      await api(`/api/v1/admin/categories/${categoryId}/attributes/${item.id}`, {
        method: "DELETE",
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const storefrontDisabled = item.audience === "technical" || draft.excluded;
  return (
    <div className={`pim-assignment ${item.inherited ? "inherited" : "local"}`}>
      <div>
        <strong>{item.name}</strong>
        <small>{item.unit || "Без единицы измерения"}</small>
        <small>
          {item.scope === "variant" ? "Размер" : "Товар"} ·{" "}
          {item.audience === "technical" ? "технический" : "клиентский"}
          {item.inherited ? ` · унаследовано от ${item.sourceCategoryName}` : " · локально"}
        </small>
      </div>
      <div className="pim-flags">
        <label>
          <input
            type="checkbox"
            disabled={draft.excluded}
            checked={draft.required}
            onChange={(event) => setDraft({ ...draft, required: event.target.checked })}
          />
          Обязательно
        </label>
        <label>
          <input
            type="checkbox"
            disabled={storefrontDisabled}
            checked={draft.filterable}
            onChange={(event) => setDraft({ ...draft, filterable: event.target.checked })}
          />
          Фильтр каталога
        </label>
        <label>
          <input
            type="checkbox"
            disabled={storefrontDisabled}
            checked={draft.showOnPdp}
            onChange={(event) => setDraft({ ...draft, showOnPdp: event.target.checked })}
          />
          На странице товара
        </label>
        <label>
          <input
            type="checkbox"
            disabled={storefrontDisabled}
            checked={draft.keyCharacteristic}
            onChange={(event) =>
              setDraft({
                ...draft,
                keyCharacteristic: event.target.checked,
                summaryPosition: event.target.checked ? draft.summaryPosition || 10 : undefined,
              })
            }
          />
          Основные сведения
        </label>
        <label>
          <input
            type="checkbox"
            disabled={storefrontDisabled}
            checked={draft.badge}
            onChange={(event) => setDraft({ ...draft, badge: event.target.checked })}
          />
          Плашка
        </label>
        <label>
          <input
            type="checkbox"
            disabled={storefrontDisabled}
            checked={draft.showInCharacteristics}
            onChange={(event) =>
              setDraft({ ...draft, showInCharacteristics: event.target.checked })
            }
          />
          Таблица характеристик
        </label>
        <label>
          <input
            type="checkbox"
            checked={draft.excluded}
            onChange={(event) =>
              setDraft({
                ...draft,
                excluded: event.target.checked,
                required: false,
                filterable: false,
                showOnPdp: false,
                keyCharacteristic: false,
                badge: false,
                showInCharacteristics: false,
              })
            }
          />
          Не использовать
        </label>
        <NumericInput
          className="tiny-number"
          value={draft.sortOrder}
          onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
          aria-label="Порядок"
        />
        {!item.inherited && item.sourceCategoryId === categoryId && (
          <button className="text-button" onClick={reset}>
            Вернуть наследование
          </button>
        )}
        <button className="admin-action" onClick={save}>
          {item.inherited ? "Переопределить" : "Сохранить"}
        </button>
      </div>
    </div>
  );
}

export function AttributeManager({
  categories,
  onError,
  initialView = "definitions",
}: {
  categories: Category[];
  onError: (value: string) => void;
  initialView?: "definitions" | "filters";
}) {
  const [view, setView] = useState<"definitions" | "schema" | "filters">(initialView);
  const [definitions, setDefinitions] = useState<AttributeDefinition[]>([]);
  const [categoryId, setCategoryId] = useState<number | null>(categories[0]?.id || null);
  const selectedCategoryId = categoryId ?? categories[0]?.id ?? null;
  const [effective, setEffective] = useState<EffectiveAttribute[]>([]);
  const [editing, setEditing] = useState<AttributeDefinition | "new" | null>(null);
  const [addAttributeId, setAddAttributeId] = useState("");
  const [filters, setFilters] = useState<CatalogFilter[]>([]);
  const [filterDraft, setFilterDraft] = useState({
    title: "",
    code: "",
    attributeId: "",
    displayMode: "select",
    categoryId: "",
  });
  const loadDefinitions = useCallback(
    () =>
      api<{ attributes: AttributeDefinition[] }>("/api/v1/admin/attributes")
        .then((data) => setDefinitions(data.attributes))
        .catch((error) => onError(error.message)),
    [onError],
  );
  const loadEffective = useCallback(() => {
    if (!selectedCategoryId) return Promise.resolve();
    return api<{ attributes: EffectiveAttribute[] }>(
      `/api/v1/admin/categories/${selectedCategoryId}/effective-attributes`,
    )
      .then((data) => setEffective(data.attributes))
      .catch((error) => onError(error.message));
  }, [selectedCategoryId, onError]);
  const loadFilters = useCallback(
    () =>
      api<{ filters: CatalogFilter[] }>("/api/v1/admin/catalog-filters")
        .then((data) => setFilters(data.filters))
        .catch((error) => onError(error.message)),
    [onError],
  );
  useEffect(() => {
    void loadDefinitions();
    void loadFilters();
  }, [loadDefinitions, loadFilters]);
  useEffect(() => {
    void loadEffective();
  }, [loadEffective]);
  const available = definitions.filter(
    (definition) => !effective.some((item) => item.id === definition.id),
  );
  const assign = async () => {
    if (!selectedCategoryId || !addAttributeId) return;
    try {
      const definition = definitions.find((item) => item.id === Number(addAttributeId));
      const customer = definition?.audience === "customer";
      await api(`/api/v1/admin/categories/${selectedCategoryId}/attributes/${addAttributeId}`, {
        method: "PUT",
        body: JSON.stringify({
          attributeId: Number(addAttributeId),
          showOnPdp: customer,
          showInCharacteristics: customer,
          sortOrder: (effective.length + 1) * 10,
        }),
      });
      setAddAttributeId("");
      loadEffective();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const archive = async (definition: AttributeDefinition) => {
    if (!window.confirm(`Архивировать атрибут «${definition.name}»?`)) return;
    try {
      await api(`/api/v1/admin/attributes/${definition.id}`, { method: "DELETE" });
      loadDefinitions();
      loadEffective();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const addFilter = async () => {
    try {
      await api("/api/v1/admin/catalog-filters", {
        method: "POST",
        body: JSON.stringify({
          title: filterDraft.title,
          code: filterDraft.code || `filter-${filterDraft.attributeId}`,
          attributeId: Number(filterDraft.attributeId),
          categoryId: filterDraft.categoryId ? Number(filterDraft.categoryId) : null,
          displayMode: filterDraft.displayMode,
          sortOrder: (filters.length + 1) * 10,
          active: true,
        }),
      });
      setFilterDraft({
        title: "",
        code: "",
        attributeId: "",
        displayMode: "select",
        categoryId: "",
      });
      loadFilters();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  return (
    <section className="admin-pim">
      <header className="pim-header">
        <div>
          <h2>{view === "filters" ? "Фильтры витрины" : "Характеристики"}</h2>
          <p>Справочник общих характеристик и настройки каждой категории.</p>
        </div>
        {view === "definitions" && (
          <button className="admin-primary" onClick={() => setEditing("new")}>
            Добавить характеристику
          </button>
        )}
      </header>
      <nav className="catalog-work-views" aria-label="Настройка характеристик">
        {view !== "filters" && (
          <>
            <button
              className={view === "definitions" ? "active" : ""}
              onClick={() => setView("definitions")}
            >
              Справочник
            </button>
            <button className={view === "schema" ? "active" : ""} onClick={() => setView("schema")}>
              Характеристики категории
            </button>
          </>
        )}
      </nav>
      {editing && (
        <AttributeDefinitionEditor
          value={editing === "new" ? undefined : editing}
          onCancel={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            loadDefinitions();
            loadEffective();
          }}
          onError={onError}
        />
      )}
      <div className="pim-columns">
        {view === "definitions" && (
          <div className="pim-definitions">
            <h3>Справочник характеристик</h3>
            {definitions.map((definition) => (
              <div className={!definition.active ? "muted" : ""} key={definition.id}>
                <button onClick={() => setEditing(definition)}>
                  <strong>{definition.name}</strong>
                  <small>{definition.unit || "Без единицы измерения"}</small>
                  <small>
                    {definition.scope === "variant" ? "Размер" : "Товар"} ·{" "}
                    {definition.audience === "technical" ? "технический" : "клиентский"}
                  </small>
                </button>
                <button className="text-button danger" onClick={() => archive(definition)}>
                  Архив
                </button>
              </div>
            ))}
          </div>
        )}
        {view === "schema" && (
          <div className="pim-schema">
            <h3>Характеристики категории</h3>
            <select
              value={selectedCategoryId || ""}
              onChange={(event) => setCategoryId(Number(event.target.value) || null)}
            >
              {categories.map((category) => (
                <option value={category.id} key={category.id}>
                  {categoryPath(categories, category.id)}
                </option>
              ))}
            </select>
            <div className="pim-add">
              <select
                value={addAttributeId}
                onChange={(event) => setAddAttributeId(event.target.value)}
              >
                <option value="">Добавить характеристику…</option>
                {available
                  .filter((item) => item.active)
                  .map((item) => (
                    <option value={item.id} key={item.id}>
                      {item.name}
                      {item.unit ? ` · ${item.unit}` : ""} ·{" "}
                      {item.scope === "variant" ? "Размер" : "Товар"}
                    </option>
                  ))}
              </select>
              <button disabled={!addAttributeId} onClick={assign}>
                Назначить
              </button>
            </div>
            {effective.map((item) => (
              <AttributeAssignmentRow
                item={item}
                categoryId={selectedCategoryId!}
                onSaved={loadEffective}
                onError={onError}
                key={`${selectedCategoryId}:${item.id}:${item.inherited}:${item.sourceCategoryId ?? "local"}:${item.required}:${item.filterable}:${item.showOnPdp}:${item.keyCharacteristic}:${item.badge}:${item.sortOrder}:${item.summaryPosition ?? ""}:${item.showInCharacteristics}:${item.excluded}`}
              />
            ))}
          </div>
        )}
      </div>
      {view === "filters" && (
        <div className="pim-filters">
          <h3>Фильтры витрины</h3>
          <div className="admin-toolbar pim-filter-toolbar">
            <input
              value={filterDraft.title}
              onChange={(event) => setFilterDraft({ ...filterDraft, title: event.target.value })}
              placeholder="Название фильтра"
            />
            <select
              value={filterDraft.attributeId}
              onChange={(event) => {
                const attribute = definitions.find(
                  (item) => String(item.id) === event.target.value,
                );
                setFilterDraft({
                  ...filterDraft,
                  attributeId: event.target.value,
                  displayMode: attribute?.dataType === "number" ? "range" : "select",
                });
              }}
            >
              <option value="">Характеристика</option>
              {definitions
                .filter((item) => item.active && item.audience === "customer")
                .map((item) => (
                  <option value={item.id} key={item.id}>
                    {item.name}
                    {item.unit ? ` · ${item.unit}` : ""} ·{" "}
                    {item.scope === "variant" ? "Размер" : "Товар"}
                  </option>
                ))}
            </select>
            <select
              value={filterDraft.categoryId}
              onChange={(event) =>
                setFilterDraft({ ...filterDraft, categoryId: event.target.value })
              }
            >
              <option value="">Все категории</option>
              {categories.map((category) => (
                <option value={category.id} key={category.id}>
                  {categoryPath(categories, category.id)}
                </option>
              ))}
            </select>
            <select
              value={filterDraft.displayMode}
              onChange={(event) =>
                setFilterDraft({ ...filterDraft, displayMode: event.target.value })
              }
            >
              {definitions.find((item) => String(item.id) === filterDraft.attributeId)?.dataType ===
              "number" ? (
                <option value="range">Диапазон</option>
              ) : (
                <>
                  <option value="select">Список</option>
                  <option value="chips">Чипы</option>
                </>
              )}
            </select>
            <button disabled={!filterDraft.title || !filterDraft.attributeId} onClick={addFilter}>
              Добавить
            </button>
          </div>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>Фильтр</th>
                  <th>Атрибут</th>
                  <th>Вид</th>
                  <th>Статус</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {filters.map((filter) => (
                  <tr key={filter.id}>
                    <td>{filter.title}</td>
                    <td>
                      {definitions.find((x) => x.id === filter.attributeId)?.name ||
                        filter.attributeCode}
                    </td>
                    <td>
                      {
                        { select: "Список", chips: "Кнопки выбора", range: "Диапазон" }[
                          filter.displayMode
                        ]
                      }
                    </td>
                    <td>{filter.active ? "Включён" : "Выключен"}</td>
                    <td>
                      <button
                        className="text-button danger"
                        onClick={async () => {
                          try {
                            await api(`/api/v1/admin/catalog-filters/${filter.id}`, {
                              method: "DELETE",
                            });
                            loadFilters();
                          } catch (error) {
                            onError((error as Error).message);
                          }
                        }}
                      >
                        Удалить
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  );
}

function DynamicValue({
  attribute,
  value,
  onChange,
}: {
  attribute: EffectiveAttribute;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
  if (attribute.code === "package_weight_grams") {
    const kilograms = value == null || value === "" ? "" : String(Number(value) / 1000);
    return (
      <label>
        Вес упаковки, кг{attribute.required ? " *" : ""}
        <NumericInput
          min="0.01"
          step="0.01"
          inputMode="decimal"
          value={kilograms}
          onChange={(event) =>
            onChange(
              event.target.value === ""
                ? null
                : Math.round(Number(event.target.value.replace(",", ".")) * 1000),
            )
          }
        />
      </label>
    );
  }
  if (attribute.dataType === "boolean")
    return (
      <label>
        {title}
        <select
          value={value == null ? "" : value === true ? "true" : "false"}
          onChange={(event) =>
            onChange(event.target.value === "" ? null : event.target.value === "true")
          }
        >
          <option value="">Не указано</option>
          <option value="true">Да</option>
          <option value="false">Нет</option>
        </select>
      </label>
    );
  if (attribute.dataType === "enum") {
    const current = String(value ?? "");
    const options = attribute.options.filter((item) => item.active);
    const unknown = current && !options.some((item) => item.code === current);
    return (
      <label>
        {title}
        <select value={current} onChange={(event) => onChange(event.target.value || null)}>
          <option value="">Не указано</option>
          {unknown && <option value={current}>{`Неизвестное значение: ${current}`}</option>}
          {options.map((item) => (
            <option value={item.code} key={item.code}>
              {item.label}
            </option>
          ))}
        </select>
        {unknown && (
          <small className="admin-inline-error">Значение сохранено до явной замены.</small>
        )}
      </label>
    );
  }
  if (attribute.dataType === "multi_enum") {
    const selected = Array.isArray(value) ? value.map(String) : [];
    const options = attribute.options.filter((item) => item.active);
    const unknown = selected.filter((code) => !options.some((item) => item.code === code));
    return (
      <fieldset className="attribute-multi">
        <legend>{title}</legend>
        {options.map((item) => (
          <label key={item.code}>
            <input
              type="checkbox"
              checked={selected.includes(item.code)}
              onChange={(event) =>
                onChange(
                  event.target.checked
                    ? [...selected, item.code]
                    : selected.filter((code) => code !== item.code),
                )
              }
            />
            {item.label}
          </label>
        ))}
        {unknown.length > 0 && (
          <small className="admin-inline-error">
            Старые значения сохранены: {unknown.join(", ")}
          </small>
        )}
      </fieldset>
    );
  }
  const isPackageSize = /^package_(length|width|height)_cm$/.test(attribute.code);
  return (
    <label>
      {title}
      <NumericInput
        type={attribute.dataType === "number" ? "number" : "text"}
        min={attribute.dataType === "number" ? (isPackageSize ? "0.1" : "0") : undefined}
        step={attribute.dataType === "number" ? "0.1" : undefined}
        inputMode={attribute.dataType === "number" ? "decimal" : undefined}
        value={value == null ? "" : String(value)}
        onChange={(event) =>
          onChange(
            event.target.value === ""
              ? null
              : attribute.dataType === "number"
                ? Number(event.target.value.replace(",", "."))
                : event.target.value,
          )
        }
      />
    </label>
  );
}

function attributeValueMissing(value: unknown) {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0)
  );
}

export function VariantsEditor({
  owner = false,
  productId,
  categoryId,
  variants,
  onChange,
  dirty,
  onReload,
  onError,
  selectedId,
  onSelect,
}: {
  selectedId: number | null;
  onSelect: (id: number | null) => void;
  owner?: boolean;
  productId: number;
  categoryId?: number;
  variants: AdminVariant[];
  onChange: (items: AdminVariant[]) => void;
  dirty: boolean;
  onReload: () => void;
  onError: (value: string) => void;
}) {
  const [schema, setSchema] = useState<EffectiveAttribute[]>([]);
  const setSelectedId = onSelect;
  const [showArchived, setShowArchived] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    if (!categoryId)
      return () => {
        active = false;
      };
    api<{ attributes: EffectiveAttribute[] }>(
      `/api/v1/admin/categories/${categoryId}/effective-attributes`,
    )
      .then((data) => {
        if (active)
          setSchema(
            (data.attributes || []).filter((x) => x.scope === "variant" && x.active && !x.excluded),
          );
      })
      .catch((error) => {
        if (active) setError(error.message);
      });
    return () => {
      active = false;
    };
  }, [categoryId]);
  const visible = variants.filter((x) => showArchived || !x.archived);
  const draft = visible.find((x) => x.id === selectedId) || visible[0];
  const update = (patch: Partial<AdminVariant>) => {
    if (draft) onChange(variants.map((x) => (x.id === draft.id ? { ...x, ...patch } : x)));
  };
  const add = (copy?: AdminVariant) => {
    const id = Math.min(0, ...variants.map((x) => x.id)) - 1;
    const next: AdminVariant = {
      id,
      productId,
      sku: "После сохранения",
      label: copy ? `${copy.label} — копия` : "Новый размер",
      price: copy?.price || 0,
      stock: 0,
      wholesaleMinQty: copy?.wholesaleMinQty || 1,
      active: false,
      archived: false,
      attributes: { ...copy?.attributes },
      externalIds: [],
      images: [],
      managedFields: [],
    };
    onChange([...variants, next]);
    setSelectedId(id);
  };
  const remove = async () => {
    if (!draft) return;
    if (draft.id < 0) {
      onChange(variants.filter((x) => x.id !== draft.id));
      setSelectedId(null);
      return;
    }
    if (dirty) {
      onError("Сначала сохраните изменения карточки. Удаление размера выполняется отдельно.");
      return;
    }
    if (
      !window.confirm(
        `Удалить размер «${draft.label}», артикул ${draft.sku}? Проданный размер сервер защитит от удаления.`,
      )
    )
      return;
    try {
      await api(`/api/v1/admin/variants/${draft.id}`, { method: "DELETE" });
      setSelectedId(null);
      onReload();
    } catch (error) {
      onError((error as Error).message);
    }
  };
  const available = (categoryId ? schema : []).filter(
    (x) => owner || x.audience === "customer" || /^package_/.test(x.code),
  );
  const packaging = available.filter((x) => /^package_/.test(x.code));
  const dimensions = available.filter((x) => !/^package_/.test(x.code));
  const missing = draft
    ? available.filter(
        (x) =>
          (x.required || x.keyCharacteristic) && attributeValueMissing(draft.attributes[x.code]),
      )
    : [];
  const field = (attribute: EffectiveAttribute) => (
    <DynamicValue
      key={attribute.id}
      attribute={attribute}
      value={draft?.attributes[attribute.code]}
      onChange={(value) =>
        update({ attributes: { ...draft!.attributes, [attribute.code]: value as never } })
      }
    />
  );
  const length = Number(draft?.attributes.package_length_cm || 0),
    width = Number(draft?.attributes.package_width_cm || 0),
    height = Number(draft?.attributes.package_height_cm || 0);
  const grams = Number(draft?.attributes.package_weight_grams || 0);
  return (
    <section className="variant-editor wide">
      <div className="variant-editor-head">
        <div>
          <h3>Размеры и цены</h3>
          <p>
            Каждый размер имеет свой артикул, цену и упаковку. Правки сохраняются общей кнопкой
            карточки.
          </p>
        </div>
        {owner && (
          <button type="button" className="admin-primary" onClick={() => add()}>
            Добавить размер
          </button>
        )}
      </div>
      {error && (
        <p className="admin-inline-error" role="alert">
          {error}
        </p>
      )}
      <label className="admin-checkbox">
        <input
          type="checkbox"
          checked={showArchived}
          onChange={(e) => setShowArchived(e.target.checked)}
        />
        Показать архивные размеры
      </label>
      <div className="catalog-variant-workspace">
        <nav className="catalog-variant-list" aria-label="Размеры товара">
          {visible.map((v) => (
            <button
              type="button"
              key={v.id}
              className={draft?.id === v.id ? "active" : ""}
              onClick={() => setSelectedId(v.id)}
            >
              <strong>{v.label}</strong>
              <span>
                {money.format(v.price)} · {v.stock} шт.
              </span>
              <small>
                {v.id < 0 ? "Новый размер" : `Артикул ${v.sku}`} ·{" "}
                {v.archived ? "Архив" : v.active ? "В продаже" : "Выключен"}
              </small>
            </button>
          ))}
        </nav>
        {draft ? (
          <div className="variant-detail" key={draft.id}>
            <div className="variant-summary">
              <strong>{draft.id < 0 ? "Новый размер" : `Артикул ${draft.sku}`}</strong>
              <span>
                {missing.length
                  ? `Заполнить: ${missing.map((x) => x.name).join(", ")}`
                  : "Замечаний по характеристикам размера нет"}
              </span>
            </div>
            <div className="admin-form-grid">
              <label className="wide">
                Название размера
                <input
                  aria-label="Название размера"
                  value={draft.label}
                  onChange={(e) => update({ label: e.target.value })}
                  placeholder="Горшок 12 см · высота 35 см"
                />
              </label>
              <label>
                Цена на сайте, ₽
                <NumericInput
                  aria-label="Цена на сайте, ₽"
                  min="0"
                  step="0.01"
                  disabled={!owner}
                  value={draft.price}
                  onChange={(e) => update({ price: Number(e.target.value) })}
                />
              </label>
              <label>
                Остаток из СБИС
                <input value={`${draft.stock} шт.`} disabled />
                <small>Остаток обновляет складской обмен. Цена и ОВХ хранятся на сайте.</small>
              </label>
              <label>
                Оптовый минимум, шт.
                <NumericInput
                  min="1"
                  step="1"
                  disabled={!owner}
                  value={draft.wholesaleMinQty}
                  onChange={(e) => update({ wholesaleMinQty: Number(e.target.value) })}
                />
              </label>
              <label className="admin-checkbox">
                <input
                  type="checkbox"
                  disabled={!owner}
                  checked={draft.active}
                  onChange={(e) =>
                    update({
                      active: e.target.checked,
                      archived: e.target.checked ? false : draft.archived,
                    })
                  }
                />
                Размер в продаже
              </label>
              {dimensions.length > 0 && (
                <>
                  <h4 className="wide">Характеристики размера</h4>
                  {dimensions.map(field)}
                </>
              )}
              <h4 className="wide">ОВХ · упаковка для отправки</h4>
              <p className="wide admin-hint">
                Измерьте упакованный товар: длина × ширина × высота в сантиметрах, полный вес в
                килограммах.
              </p>
              {packaging.map(field)}
              {!packaging.length && (
                <p className="wide admin-hint">Выберите категорию товара, чтобы заполнить ОВХ.</p>
              )}
              {length > 0 && width > 0 && height > 0 && (
                <div className="wide catalog-package-summary">
                  <strong>
                    Объём: {((length * width * height) / 1000).toLocaleString("ru-RU")} л
                  </strong>
                  <span>
                    Объёмный вес:{" "}
                    {((length * width * height) / 5000).toLocaleString("ru-RU", {
                      maximumFractionDigits: 2,
                    })}{" "}
                    кг
                  </span>
                  {grams > 0 && (
                    <span>
                      Расчётный вес одной упаковки:{" "}
                      {Math.max(grams / 1000, (length * width * height) / 5000).toLocaleString(
                        "ru-RU",
                        { maximumFractionDigits: 2 },
                      )}{" "}
                      кг
                    </span>
                  )}
                </div>
              )}
              <details className="wide catalog-variant-integrations">
                <summary>Связи площадок и артикулы ({draft.externalIds.length})</summary>
                <p className="admin-hint">
                  Связи СБИС защищены. Артикулы площадок можно изменить здесь; результат публикации
                  проверяется отдельно.
                </p>
                {draft.externalIds.map((mapping, index) => {
                  const protectedMapping = !owner || ["ficusin", "saby"].includes(mapping.provider);
                  return (
                    <div className="external-mapping-row" key={index}>
                      <label>
                        Площадка
                        <select
                          disabled={protectedMapping}
                          value={mapping.provider}
                          onChange={(e) =>
                            update({
                              externalIds: draft.externalIds.map((x, i) =>
                                i === index
                                  ? {
                                      ...x,
                                      provider: e.target.value,
                                      type: e.target.value === "ozon" ? "offer_id" : "vendor_code",
                                    }
                                  : x,
                              ),
                            })
                          }
                        >
                          <option value="wildberries">Wildberries</option>
                          <option value="ozon">Ozon</option>
                          {!["wildberries", "ozon"].includes(mapping.provider) && (
                            <option value={mapping.provider}>
                              {mapping.provider === "saby" ? "СБИС" : "Системная связь"}
                            </option>
                          )}
                        </select>
                      </label>
                      <label>
                        Артикул
                        <input
                          aria-label={`Артикул ${index + 1}`}
                          disabled={protectedMapping}
                          value={mapping.externalId}
                          onChange={(e) =>
                            update({
                              externalIds: draft.externalIds.map((x, i) =>
                                i === index ? { ...x, externalId: e.target.value } : x,
                              ),
                            })
                          }
                        />
                      </label>
                      {!protectedMapping && (
                        <button
                          type="button"
                          onClick={() =>
                            update({ externalIds: draft.externalIds.filter((_, i) => i !== index) })
                          }
                        >
                          Убрать связь
                        </button>
                      )}
                    </div>
                  );
                })}
                {owner && (
                  <button
                    type="button"
                    className="admin-action"
                    onClick={() =>
                      update({
                        externalIds: [
                          ...draft.externalIds,
                          { provider: "wildberries", type: "vendor_code", externalId: "" },
                        ],
                      })
                    }
                  >
                    Добавить артикул площадки
                  </button>
                )}
              </details>
              {owner && draft.id > 0 && (
                <VariantPriceProposals variantId={draft.id} onError={onError} />
              )}
              <h4 className="wide">Фотографии этого размера</h4>
              {draft.id < 0 ? (
                <p className="wide admin-hint">
                  Сохраните карточку, затем загрузите фото нового размера.
                </p>
              ) : (
                <div className="wide">
                  <p className="admin-hint">
                    Загрузка и удаление фото применяются сразу, отдельно от текстовых правок.
                  </p>
                  <VariantMediaManager
                    canDelete={owner}
                    variantId={draft.id}
                    sku={draft.sku}
                    onChanged={() => {}}
                    onError={onError}
                  />
                </div>
              )}
            </div>
            {owner && (
              <div className="catalog-variant-actions">
                <button type="button" className="admin-action" onClick={() => add(draft)}>
                  Копировать как новый размер
                </button>
                {draft.id > 0 && (
                  <button
                    type="button"
                    className="text-button"
                    onClick={() => update({ archived: !draft.archived, active: false })}
                  >
                    {draft.archived ? "Вернуть из архива" : "В архив после сохранения"}
                  </button>
                )}
                <button
                  type="button"
                  className="text-button danger"
                  disabled={draft.id > 0 && dirty}
                  onClick={() => void remove()}
                >
                  {draft.id < 0 ? "Убрать новый размер" : "Удалить размер отдельно…"}
                </button>
              </div>
            )}
          </div>
        ) : (
          <p className="editor-empty">Размеров пока нет. Добавьте первый размер.</p>
        )}
      </div>
    </section>
  );
}
