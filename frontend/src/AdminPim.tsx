import { useCallback, useEffect, useMemo, useState } from "react";
import { api, money } from "./adminShared";
import type { Category } from "./adminTypes";

export type AttributeOption = { id?: number; code: string; label: string; sortOrder: number; active: boolean };
export type AttributeDefinition = {
  id: number; code: string; name: string; description: string;
  dataType: "text" | "string" | "number" | "boolean" | "enum" | "multi_enum";
  unit: string; audience: "customer" | "technical"; scope: "product" | "variant";
  global: boolean; active: boolean; options: AttributeOption[];
};
export type EffectiveAttribute = AttributeDefinition & {
  required: boolean; filterable: boolean; showOnPdp: boolean; keyCharacteristic: boolean;
  badge: boolean; sortOrder: number; summaryPosition?: number; showInCharacteristics: boolean;
  excluded: boolean; inherited: boolean; sourceCategoryId?: number; sourceCategoryName: string;
};
export type VariantMapping = { provider: string; type: string; externalId: string };
export type AdminVariant = {
  id: number; productId: number; sku: string; label: string; price: number; stock: number;
  wholesaleMinQty: number; active: boolean; archived: boolean;
  attributes: Record<string, string | number | boolean | string[]>;
  externalIds: VariantMapping[]; images: string[];
};
export type CatalogFilter = {
  id: number; code: string; title: string; attributeId: number; attributeCode: string;
  categoryId?: number; displayMode: "select" | "chips" | "range"; sortOrder: number; active: boolean;
};

type DraftDefinition = Omit<AttributeDefinition, "id" | "options"> & { optionsText: string };
const emptyDefinition: DraftDefinition = {
  code: "", name: "", description: "", dataType: "enum", unit: "", audience: "customer",
  scope: "product", global: false, active: true, optionsText: "",
};

function optionText(options: AttributeOption[]) {
  return options.map((option) => `${option.code} | ${option.label}`).join("\n");
}
function parseOptions(text: string): AttributeOption[] {
  return text.split("\n").map((line) => line.trim()).filter(Boolean).map((line, index) => {
    const [rawCode, ...rest] = line.split("|");
    const code = rawCode.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, "_");
    const label = rest.join("|").trim() || rawCode.trim();
    return { code, label, sortOrder: (index + 1) * 10, active: true };
  });
}

function AttributeDefinitionEditor({ value, onSaved, onCancel, onError }: {
  value?: AttributeDefinition; onSaved: () => void; onCancel: () => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState<DraftDefinition>(() => value ? {
    code: value.code, name: value.name, description: value.description, dataType: value.dataType,
    unit: value.unit, audience: value.audience, scope: value.scope, global: value.global,
    active: value.active, optionsText: optionText(value.options),
  } : emptyDefinition);
  const save = async () => {
    try {
      const body = JSON.stringify({ ...draft, optionsText: undefined, options: ["enum", "multi_enum"].includes(draft.dataType) ? parseOptions(draft.optionsText) : [] });
      await api(value ? `/api/v1/admin/attributes/${value.id}` : "/api/v1/admin/attributes", { method: value ? "PATCH" : "POST", body });
      onSaved();
    } catch (error) { onError((error as Error).message); }
  };
  return <div className="admin-pim-editor">
    <div className="admin-form-grid">
      <label>Название<input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></label>
      <label>Код<input disabled={Boolean(value)} value={draft.code} onChange={(event) => setDraft({ ...draft, code: event.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, "_") })} /></label>
      <label>Тип<select value={draft.dataType} onChange={(event) => setDraft({ ...draft, dataType: event.target.value as DraftDefinition["dataType"] })}><option value="text">Текст</option><option value="number">Число</option><option value="boolean">Да / нет</option><option value="enum">Один вариант</option><option value="multi_enum">Несколько вариантов</option></select></label>
      <label>Единица<input value={draft.unit} onChange={(event) => setDraft({ ...draft, unit: event.target.value })} placeholder="см, г, мл" /></label>
      <label>Уровень<select value={draft.scope} onChange={(event) => setDraft({ ...draft, scope: event.target.value as DraftDefinition["scope"] })}><option value="product">PRODUCT — общая карточка</option><option value="variant">SKU — вариант</option></select></label>
      <label>Видимость<select value={draft.audience} onChange={(event) => setDraft({ ...draft, audience: event.target.value as DraftDefinition["audience"] })}><option value="customer">Клиентский</option><option value="technical">Технический / внутренний</option></select></label>
      <label className="wide">Описание<textarea rows={2} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} /></label>
      {["enum", "multi_enum"].includes(draft.dataType) && <label className="wide">Варианты — по одному на строку: <code>code | Название</code><textarea rows={6} value={draft.optionsText} onChange={(event) => setDraft({ ...draft, optionsText: event.target.value })} /></label>}
      <label className="admin-checkbox"><input type="checkbox" checked={draft.global} onChange={(event) => setDraft({ ...draft, global: event.target.checked })} />Глобальный атрибут</label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.active} onChange={(event) => setDraft({ ...draft, active: event.target.checked })} />Активен</label>
    </div>
    <div className="dialog-actions"><button onClick={onCancel}>Отмена</button><button className="primary" disabled={!draft.name.trim() || !draft.code.trim()} onClick={save}>Сохранить</button></div>
  </div>;
}

function AttributeAssignmentRow({ item, categoryId, onSaved, onError }: {
  item: EffectiveAttribute; categoryId: number; onSaved: () => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState(item);
  const save = async () => {
    try {
      await api(`/api/v1/admin/categories/${categoryId}/attributes/${item.id}`, { method: "PUT", body: JSON.stringify({
        attributeId: item.id, required: draft.required, filterable: draft.filterable,
        showOnPdp: draft.showOnPdp, keyCharacteristic: draft.keyCharacteristic, badge: draft.badge,
        sortOrder: draft.sortOrder, summaryPosition: draft.summaryPosition || null,
        showInCharacteristics: draft.showInCharacteristics, excluded: draft.excluded,
      }) });
      onSaved();
    } catch (error) { onError((error as Error).message); }
  };
  return <div className={`pim-assignment ${item.inherited ? "inherited" : "local"}`}>
    <div><strong>{item.name}</strong><code>{item.code}</code><small>{item.scope === "variant" ? "SKU" : "PRODUCT"} · {item.audience === "technical" ? "технический" : "клиентский"}{item.inherited ? ` · унаследовано от ${item.sourceCategoryName}` : " · локально"}</small></div>
    <div className="pim-flags">
      <label><input type="checkbox" checked={draft.required} onChange={(event) => setDraft({ ...draft, required: event.target.checked })} />обяз.</label>
      <label><input type="checkbox" checked={draft.filterable} onChange={(event) => setDraft({ ...draft, filterable: event.target.checked })} />фильтр</label>
      <label><input type="checkbox" checked={draft.showOnPdp} onChange={(event) => setDraft({ ...draft, showOnPdp: event.target.checked })} />PDP</label>
      <label><input type="checkbox" checked={draft.keyCharacteristic} onChange={(event) => setDraft({ ...draft, keyCharacteristic: event.target.checked, summaryPosition: event.target.checked ? (draft.summaryPosition || 10) : undefined })} />основная</label>
      <label><input type="checkbox" checked={draft.badge} onChange={(event) => setDraft({ ...draft, badge: event.target.checked })} />бейдж</label>
      <label><input type="checkbox" checked={draft.showInCharacteristics} onChange={(event) => setDraft({ ...draft, showInCharacteristics: event.target.checked })} />характеристики</label>
      <label><input type="checkbox" checked={draft.excluded} onChange={(event) => setDraft({ ...draft, excluded: event.target.checked })} />исключить</label>
      <input className="tiny-number" type="number" value={draft.sortOrder} onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })} aria-label="Порядок" />
      <button className="admin-action" onClick={save}>{item.inherited ? "Переопределить" : "Сохранить"}</button>
    </div>
  </div>;
}

export function AttributeManager({ categories, onError }: { categories: Category[]; onError: (value: string) => void }) {
  const [definitions, setDefinitions] = useState<AttributeDefinition[]>([]);
  const [categoryId, setCategoryId] = useState<number | null>(categories[0]?.id || null);
  const selectedCategoryId = categoryId ?? categories[0]?.id ?? null;
  const [effective, setEffective] = useState<EffectiveAttribute[]>([]);
  const [editing, setEditing] = useState<AttributeDefinition | "new" | null>(null);
  const [addAttributeId, setAddAttributeId] = useState("");
  const [filters, setFilters] = useState<CatalogFilter[]>([]);
  const [filterDraft, setFilterDraft] = useState({ title: "", code: "", attributeId: "", displayMode: "select", categoryId: "" });
  const loadDefinitions = useCallback(() => api<{ attributes: AttributeDefinition[] }>("/api/v1/admin/attributes").then((data) => setDefinitions(data.attributes)).catch((error) => onError(error.message)), [onError]);
  const loadEffective = useCallback(() => {
    if (!selectedCategoryId) return Promise.resolve();
    return api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${selectedCategoryId}/effective-attributes`).then((data) => setEffective(data.attributes)).catch((error) => onError(error.message));
  }, [selectedCategoryId, onError]);
  const loadFilters = useCallback(() => api<{ filters: CatalogFilter[] }>("/api/v1/admin/catalog-filters").then((data) => setFilters(data.filters)).catch((error) => onError(error.message)), [onError]);
  useEffect(() => { void loadDefinitions(); void loadFilters(); }, [loadDefinitions, loadFilters]);
  useEffect(() => { void loadEffective(); }, [loadEffective]);
  const available = definitions.filter((definition) => !effective.some((item) => item.id === definition.id));
  const assign = async () => {
    if (!selectedCategoryId || !addAttributeId) return;
    try {
      await api(`/api/v1/admin/categories/${selectedCategoryId}/attributes/${addAttributeId}`, { method: "PUT", body: JSON.stringify({ attributeId: Number(addAttributeId), showOnPdp: true, showInCharacteristics: true, sortOrder: (effective.length + 1) * 10 }) });
      setAddAttributeId(""); loadEffective();
    } catch (error) { onError((error as Error).message); }
  };
  const archive = async (definition: AttributeDefinition) => {
    if (!window.confirm(`Архивировать атрибут «${definition.name}»?`)) return;
    try { await api(`/api/v1/admin/attributes/${definition.id}`, { method: "DELETE" }); loadDefinitions(); loadEffective(); }
    catch (error) { onError((error as Error).message); }
  };
  const addFilter = async () => {
    try {
      await api("/api/v1/admin/catalog-filters", { method: "POST", body: JSON.stringify({
        title: filterDraft.title, code: filterDraft.code || filterDraft.title.toLowerCase().replace(/[^a-z0-9а-я]+/gi, "-").replace(/^-|-$/g, ""),
        attributeId: Number(filterDraft.attributeId), categoryId: filterDraft.categoryId ? Number(filterDraft.categoryId) : null,
        displayMode: filterDraft.displayMode, sortOrder: (filters.length + 1) * 10, active: true,
      }) });
      setFilterDraft({ title: "", code: "", attributeId: "", displayMode: "select", categoryId: "" }); loadFilters();
    } catch (error) { onError((error as Error).message); }
  };
  return <section className="admin-pim">
    <header className="pim-header"><div><p className="eyebrow">PIM</p><h2>Атрибуты и схема категорий</h2><p>Определения создаются один раз. Категория наследует настройки родителей; локальная строка перекрывает ближайшего предка.</p></div><button className="admin-primary" onClick={() => setEditing("new")}>Новый атрибут</button></header>
    {editing && <AttributeDefinitionEditor value={editing === "new" ? undefined : editing} onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); loadDefinitions(); loadEffective(); }} onError={onError} />}
    <div className="pim-columns">
      <div className="pim-definitions"><h3>Определения</h3>{definitions.map((definition) => <div className={!definition.active ? "muted" : ""} key={definition.id}><button onClick={() => setEditing(definition)}><strong>{definition.name}</strong><code>{definition.code}</code><small>{definition.scope === "variant" ? "SKU" : "PRODUCT"} · {definition.audience === "technical" ? "технический" : "клиентский"}</small></button><button className="text-button danger" onClick={() => archive(definition)}>Архив</button></div>)}</div>
      <div className="pim-schema"><h3>Схема категории</h3><select value={selectedCategoryId || ""} onChange={(event) => setCategoryId(Number(event.target.value) || null)}>{categories.map((category) => <option value={category.id} key={category.id}>{category.name}</option>)}</select><div className="pim-add"><select value={addAttributeId} onChange={(event) => setAddAttributeId(event.target.value)}><option value="">Добавить атрибут…</option>{available.filter((item) => item.active).map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select><button disabled={!addAttributeId} onClick={assign}>Назначить</button></div>{effective.map((item) => <AttributeAssignmentRow item={item} categoryId={selectedCategoryId!} onSaved={loadEffective} onError={onError} key={`${selectedCategoryId}:${item.id}:${item.inherited}:${item.sourceCategoryId ?? "local"}:${item.required}:${item.filterable}:${item.showOnPdp}:${item.keyCharacteristic}:${item.badge}:${item.sortOrder}:${item.summaryPosition ?? ""}:${item.showInCharacteristics}:${item.excluded}`} />)}</div>
    </div>
    <div className="pim-filters"><h3>Фильтры витрины</h3><div className="admin-toolbar"><input value={filterDraft.title} onChange={(event) => setFilterDraft({ ...filterDraft, title: event.target.value })} placeholder="Название фильтра" /><select value={filterDraft.attributeId} onChange={(event) => setFilterDraft({ ...filterDraft, attributeId: event.target.value })}><option value="">Атрибут</option>{definitions.filter((item) => item.active && item.audience === "customer").map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select><select value={filterDraft.displayMode} onChange={(event) => setFilterDraft({ ...filterDraft, displayMode: event.target.value })}><option value="select">Список</option><option value="chips">Чипы</option><option value="range">Диапазон</option></select><button disabled={!filterDraft.title || !filterDraft.attributeId} onClick={addFilter}>Добавить</button></div>
      <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Фильтр</th><th>Атрибут</th><th>Вид</th><th>Статус</th><th /></tr></thead><tbody>{filters.map((filter) => <tr key={filter.id}><td>{filter.title}</td><td><code>{filter.attributeCode}</code></td><td>{filter.displayMode}</td><td>{filter.active ? "Включён" : "Выключен"}</td><td><button className="text-button danger" onClick={async () => { try { await api(`/api/v1/admin/catalog-filters/${filter.id}`, { method: "DELETE" }); loadFilters(); } catch (error) { onError((error as Error).message); } }}>Удалить</button></td></tr>)}</tbody></table></div>
    </div>
  </section>;
}

function DynamicValue({ attribute, value, onChange }: { attribute: EffectiveAttribute; value: unknown; onChange: (value: unknown) => void }) {
  const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
  if (attribute.dataType === "boolean") return <label className="admin-checkbox"><input type="checkbox" checked={value === true} onChange={(event) => onChange(event.target.checked)} />{title}</label>;
  if (attribute.dataType === "enum") return <label>{title}<select value={String(value ?? "")} onChange={(event) => onChange(event.target.value || null)}><option value="">Не указано</option>{attribute.options.filter((item) => item.active).map((item) => <option value={item.code} key={item.code}>{item.label}</option>)}</select></label>;
  if (attribute.dataType === "multi_enum") {
    const selected = Array.isArray(value) ? value.map(String) : [];
    return <fieldset className="attribute-multi"><legend>{title}</legend>{attribute.options.filter((item) => item.active).map((item) => <label key={item.code}><input type="checkbox" checked={selected.includes(item.code)} onChange={(event) => onChange(event.target.checked ? [...selected, item.code] : selected.filter((code) => code !== item.code))} />{item.label}</label>)}</fieldset>;
  }
  return <label>{title}<input type={attribute.dataType === "number" ? "number" : "text"} value={value == null ? "" : String(value)} onChange={(event) => onChange(event.target.value === "" ? null : attribute.dataType === "number" ? Number(event.target.value) : event.target.value)} /></label>;
}

export function VariantsEditor({ productId, categoryId, onError }: { productId: number; categoryId?: number; onError: (value: string) => void }) {
  const [variants, setVariants] = useState<AdminVariant[]>([]);
  const [schema, setSchema] = useState<EffectiveAttribute[]>([]);
  const [selectedId, setSelectedId] = useState<number | "new" | null>(null);
  const empty = useMemo<AdminVariant>(() => ({ id: 0, productId, sku: "авто", label: "Новый вариант", price: 0, stock: 0, wholesaleMinQty: 1, active: true, archived: false, attributes: {}, externalIds: [], images: [] }), [productId]);
  const [draft, setDraft] = useState<AdminVariant>(empty);
  const load = useCallback(() => api<{ variants: AdminVariant[] }>(`/api/v1/admin/products/${productId}/variants`).then((data) => setVariants(data.variants || [])).catch((error) => onError(error.message)), [productId, onError]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { if (!categoryId) return; void api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setSchema(data.attributes.filter((item) => item.scope === "variant" && item.active && !item.excluded))).catch((error) => onError(error.message)); }, [categoryId, onError]);
  const select = (variant: AdminVariant | "new") => { setSelectedId(variant === "new" ? "new" : variant.id); setDraft(variant === "new" ? empty : structuredClone(variant)); };
  const save = async () => {
    try {
      const body = JSON.stringify({ label: draft.label, priceMinor: Math.round(draft.price * 100), stock: draft.stock, wholesaleMinQty: draft.wholesaleMinQty, active: draft.active, attributes: draft.attributes, externalIds: draft.externalIds });
      const result = await api<{ variant: AdminVariant }>(selectedId === "new" ? `/api/v1/admin/products/${productId}/variants` : `/api/v1/admin/variants/${draft.id}`, { method: selectedId === "new" ? "POST" : "PATCH", body });
      setSelectedId(result.variant.id); setDraft(result.variant); load();
    } catch (error) { onError((error as Error).message); }
  };
  const visibleSchema = categoryId ? schema : [];
  const variantAttributes = visibleSchema.filter((item) => item.audience === "customer");
  const technicalAttributes = visibleSchema.filter((item) => item.audience === "technical");
  const completeness = visibleSchema.length ? Math.round(visibleSchema.filter((item) => !item.required || draft.attributes[item.code] !== undefined && draft.attributes[item.code] !== null && draft.attributes[item.code] !== "").length / visibleSchema.length * 100) : 100;
  return <section className="variant-editor wide">
    <div className="variant-editor-head"><div><h3>Варианты / SKU</h3><p>Каждый размер — отдельная продаваемая единица. Артикул назначается автоматически и не меняется.</p></div><button type="button" className="admin-primary" onClick={() => select("new")}>Добавить SKU</button></div>
    <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>SKU</th><th>Вариант</th><th>Горшок</th><th>Высота</th><th>Цена</th><th>Остаток</th><th>Статус</th><th /></tr></thead><tbody>{variants.map((variant) => <tr className={selectedId === variant.id ? "selected" : ""} key={variant.id} onClick={() => select(variant)}><td><strong>{variant.sku}</strong></td><td>{variant.label}</td><td>{variant.attributes.pot_diameter_cm ?? "—"}</td><td>{variant.attributes.height_cm ?? "—"}</td><td>{money.format(variant.price)}</td><td>{variant.stock}</td><td>{variant.archived ? "Архив" : variant.active ? "Активен" : "Выключен"}</td><td><button type="button" className="text-button" onClick={async (event) => { event.stopPropagation(); try { const data = await api<{ variant: AdminVariant }>(`/api/v1/admin/variants/${variant.id}/copy`, { method: "POST" }); setDraft(data.variant); setSelectedId(data.variant.id); load(); } catch (error) { onError((error as Error).message); } }}>Копировать</button></td></tr>)}</tbody></table></div>
    {selectedId && <div className="variant-detail"><div className="variant-summary"><strong>SKU {draft.sku}</strong><span>Заполнение схемы: {completeness}%</span></div><div className="admin-form-grid">
      <label className="wide">Название варианта<input value={draft.label} onChange={(event) => setDraft({ ...draft, label: event.target.value })} placeholder="Ø12 / H35" /></label>
      <label>Цена, ₽<input type="number" min="0" value={draft.price} onChange={(event) => setDraft({ ...draft, price: Number(event.target.value) })} /></label>
      <label>Остаток<input type="number" min="0" value={draft.stock} onChange={(event) => setDraft({ ...draft, stock: Number(event.target.value) })} /></label>
      <label>Оптовый минимум<input type="number" min="1" value={draft.wholesaleMinQty} onChange={(event) => setDraft({ ...draft, wholesaleMinQty: Number(event.target.value) })} /></label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.active} onChange={(event) => setDraft({ ...draft, active: event.target.checked })} />Активен</label>
      {variantAttributes.length > 0 && <><h4 className="wide">Клиентские характеристики SKU</h4>{variantAttributes.map((attribute) => <DynamicValue key={attribute.id} attribute={attribute} value={draft.attributes[attribute.code]} onChange={(value) => setDraft({ ...draft, attributes: { ...draft.attributes, [attribute.code]: value as never } })} />)}</>}
      {technicalAttributes.length > 0 && <><h4 className="wide">Упаковка и логистика</h4>{technicalAttributes.map((attribute) => <DynamicValue key={attribute.id} attribute={attribute} value={draft.attributes[attribute.code]} onChange={(value) => setDraft({ ...draft, attributes: { ...draft.attributes, [attribute.code]: value as never } })} />)}</>}
      <h4 className="wide">Интеграции SKU</h4><div className="wide external-mappings">{draft.externalIds.map((mapping, index) => <div className="external-mapping-row" key={`${index}-${mapping.provider}`}><input value={mapping.provider} placeholder="saby / wildberries / ozon" onChange={(event) => setDraft({ ...draft, externalIds: draft.externalIds.map((item, itemIndex) => itemIndex === index ? { ...item, provider: event.target.value.toLowerCase() } : item) })} /><input value={mapping.type} placeholder="id / nmId / offerId" onChange={(event) => setDraft({ ...draft, externalIds: draft.externalIds.map((item, itemIndex) => itemIndex === index ? { ...item, type: event.target.value } : item) })} /><input value={mapping.externalId} placeholder="Значение" onChange={(event) => setDraft({ ...draft, externalIds: draft.externalIds.map((item, itemIndex) => itemIndex === index ? { ...item, externalId: event.target.value } : item) })} /><button type="button" onClick={() => setDraft({ ...draft, externalIds: draft.externalIds.filter((_, itemIndex) => itemIndex !== index) })}>Удалить</button></div>)}<button type="button" className="admin-action" onClick={() => setDraft({ ...draft, externalIds: [...draft.externalIds, { provider: "saby", type: "id", externalId: "" }] })}>Добавить связь</button></div>
      <h4 className="wide">Фото SKU</h4><div className="wide"><p className="admin-hint">{draft.images.length ? `${draft.images.length} фото привязано к этому SKU. На PDP они имеют приоритет над общими фото товара.` : "Собственных фото нет — PDP использует общую галерею товара."}</p></div>
    </div><div className="dialog-actions"><button type="button" className="danger" disabled={selectedId === "new"} onClick={async () => { if (selectedId === "new") return; try { await api(`/api/v1/admin/variants/${draft.id}/archive`, { method: "POST" }); setSelectedId(null); load(); } catch (error) { onError((error as Error).message); } }}>Архивировать</button><button type="button" className="danger" disabled={selectedId === "new"} onClick={async () => { if (selectedId === "new" || !window.confirm(`Удалить SKU ${draft.sku}? Это возможно только если он никогда не продавался.`)) return; try { await api(`/api/v1/admin/variants/${draft.id}`, { method: "DELETE" }); setSelectedId(null); load(); } catch (error) { onError((error as Error).message); } }}>Удалить</button><button type="button" className="primary" onClick={save}>Сохранить SKU</button></div></div>}
  </section>;
}
