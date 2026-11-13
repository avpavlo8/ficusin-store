import { NumericInput } from "./NumericInput";
import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";
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
