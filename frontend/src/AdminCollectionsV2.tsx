import { useCallback, useEffect, useMemo, useState } from "react";
import { PageHeading, api } from "./adminShared";
import type { Product } from "./adminTypes";

type Rule = { attribute: string; operator: "eq" | "neq" | "in" | "contains" | "gte" | "lte" | "exists"; value?: unknown };
type Definition = {
  id: number; slug: string; title: string; note: string; sortOrder: number; active: boolean;
  coverUrl: string; mode: "manual" | "dynamic"; rules: Rule[]; products: number[];
};
type Attribute = {
  id: number; code: string; name: string; dataType: string; unit: string; active: boolean;
  options: Array<{ code: string; label: string; active: boolean }>;
};

function NewCollectionDraft({ sortOrder, onCreated, onCancel, onError }: {
  sortOrder: number; onCreated: (value: Definition) => void; onCancel: () => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState({ title: "", slug: "", note: "", coverUrl: "", active: false });
  const [file, setFile] = useState<File | null>(null);
  const [saving, setSaving] = useState(false);
  const preview = useMemo(() => file ? URL.createObjectURL(file) : draft.coverUrl, [file, draft.coverUrl]);
  useEffect(() => () => { if (preview.startsWith("blob:")) URL.revokeObjectURL(preview); }, [preview]);
  const create = async () => {
    setSaving(true);
    try {
      const body = new FormData();
      Object.entries({ ...draft, sortOrder: String(sortOrder), mode: "manual", rules: "[]" }).forEach(([key, value]) => body.append(key, String(value)));
      if (file) body.append("file", file);
      const result = await api<{ collection: Definition }>("/api/v1/admin/collection-definitions/with-cover", { method: "POST", body });
      onCreated(result.collection);
    } catch (error) { onError((error as Error).message); }
    finally { setSaving(false); }
  };
  return <section className="collection-create-card" aria-label="Новая подборка">
    <header><div><p className="eyebrow">Новая подборка</p><h3>Название и обложка</h3></div><button type="button" className="text-button" onClick={onCancel}>Закрыть</button></header>
    <p className="admin-hint">Запись появится только после сохранения. Название и обложка обязательны.</p>
    <div className="admin-form-grid">
      <label>Название<input autoFocus value={draft.title} onChange={(event) => { const title=event.target.value;setDraft((current)=>({ ...current,title,slug: current.slug || title.toLowerCase().replace(/[^a-z0-9а-яё]+/gi,"-").replace(/[а-яё]/gi,"").replace(/^-|-$/g,"") })); }} /></label>
      <label>Slug<input value={draft.slug} placeholder="green-office" onChange={(event) => setDraft({ ...draft, slug: event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-") })} /></label>
      <label className="wide">Подпись<input value={draft.note} onChange={(event) => setDraft({ ...draft, note: event.target.value })} /></label>
      <div className="wide collection-cover-editor">
        <div className="collection-cover-preview" style={preview ? { backgroundImage: `url('${preview}')` } : undefined}><span>{preview ? draft.title || "Новая подборка" : "Добавьте обложку"}</span></div>
        <div><strong>Обложка подборки</strong><p>JPEG, PNG или GIF до 12 МБ. Можно указать готовый адрес.</p><label className="admin-upload-button"><input type="file" accept="image/jpeg,image/png,image/gif" onChange={(event) => setFile(event.target.files?.[0] || null)} /><span>{file ? file.name : "Выбрать файл"}</span></label><label>Адрес изображения<input value={draft.coverUrl} placeholder="https://… или /assets/…" onChange={(event) => { setFile(null);setDraft({ ...draft, coverUrl: event.target.value }); }} /></label></div>
      </div>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.active} onChange={(event) => setDraft({ ...draft, active: event.target.checked })} />Показывать после создания</label>
    </div>
    <div className="dialog-actions"><button type="button" onClick={onCancel}>Отмена</button><button type="button" className="primary" disabled={saving || !draft.title.trim() || !draft.slug.trim() || (!file && !draft.coverUrl.trim())} onClick={() => void create()}>{saving ? "Создаём…" : "Создать подборку"}</button></div>
  </section>;
}

function valueForInput(rule: Rule) {
  if (Array.isArray(rule.value)) return rule.value.join(", ");
  if (rule.value == null) return "";
  return String(rule.value);
}

function parseRuleValue(attribute: Attribute | undefined, operator: Rule["operator"], raw: string): unknown {
  if (operator === "exists") return undefined;
  if (operator === "in") return raw.split(",").map((item) => item.trim()).filter(Boolean);
  if (attribute?.dataType === "number" && raw.trim() !== "") return Number(raw);
  if (attribute?.dataType === "boolean") return raw === "true";
  return raw.trim();
}

function operatorsFor(attribute?: Attribute): Array<{ value: Rule["operator"]; label: string }> {
  const common: Array<{ value: Rule["operator"]; label: string }> = [{ value: "eq", label: "равно" }, { value: "neq", label: "не равно" }, { value: "exists", label: "заполнено" }];
  if (!attribute) return common;
  if (attribute.dataType === "number") return [...common, { value: "in", label: "одно из" }, { value: "gte", label: "не меньше" }, { value: "lte", label: "не больше" }];
  if (attribute.dataType === "multi_enum") return [...common, { value: "in", label: "одно из" }, { value: "contains", label: "содержит" }];
  if (attribute.dataType === "enum" || attribute.dataType === "boolean") return [...common, { value: "in", label: "одно из" }];
  return [...common, { value: "in", label: "одно из" }, { value: "contains", label: "содержит" }];
}

function CollectionEditor({ owner, item, attributes, products, onSaved, onDeleted, onError }: {
  owner: boolean; item: Definition; attributes: Attribute[]; products: Product[];
  onSaved: (value: Definition) => void; onDeleted: (id: number) => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState<Definition>(() => structuredClone(item));
  const [query, setQuery] = useState("");
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const shown = useMemo(() => products.filter((product) => `${product.name} ${product.latinName} ${product.sku}`.toLowerCase().includes(query.toLowerCase())), [products, query]);

  const save = async () => {
    setSaving(true);
    try {
      const result = await api<{ collection: Definition }>(`/api/v1/admin/collection-definitions/${item.id}`, {
        method: "PUT",
        body: JSON.stringify({ slug: draft.slug, title: draft.title, note: draft.note, coverUrl: draft.coverUrl, sortOrder: draft.sortOrder, active: draft.active, mode: draft.mode, rules: draft.mode === "dynamic" ? draft.rules : [] }),
      });
      setDraft(structuredClone(result.collection));
      onSaved(result.collection);
    } catch (error) { onError((error as Error).message); }
    finally { setSaving(false); }
  };

  const uploadCover = async (file?: File) => {
    if (!file) return;
    setUploading(true);
    try {
      const body = new FormData(); body.append("file", file);
      const result = await api<{ collection: Definition }>(`/api/v1/admin/collection-definitions/${item.id}/cover`, { method: "POST", body });
      setDraft(structuredClone(result.collection)); onSaved(result.collection);
    } catch (error) { onError((error as Error).message); }
    finally { setUploading(false); }
  };

  const toggleProduct = async (productId: number) => {
    if (draft.mode !== "manual") return;
    const next = draft.products.includes(productId) ? draft.products.filter((id) => id !== productId) : [...draft.products, productId];
    try {
      const result = await api<{ collections: Array<{ id: number; products: number[] }> }>(`/api/v1/admin/collections/${item.id}`, { method: "PATCH", body: JSON.stringify({ products: next }) });
      const updated = result.collections.find((collection) => collection.id === item.id);
      const saved = { ...draft, products: updated?.products || next };
      setDraft(saved);
      onSaved(saved);
    } catch (error) { onError((error as Error).message); }
  };

  const remove = async () => {
    if (!window.confirm(`Удалить подборку «${draft.title}»?`)) return;
    try {
      await api(`/api/v1/admin/collection-definitions/${item.id}`, { method: "DELETE" });
      onDeleted(item.id);
    } catch (error) { onError((error as Error).message); }
  };

  const updateRule = (index: number, patch: Partial<Rule>) => setDraft((current) => ({ ...current, rules: current.rules.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, ...patch } : rule) }));

  return <div className="admin-collection-body collection-editor-v2">
    <div className="admin-form-grid">
      <label>Название<input value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
      <label>Slug<input value={draft.slug} onChange={(event) => setDraft({ ...draft, slug: event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-") })} /></label>
      <label className="wide">Подпись<input value={draft.note} onChange={(event) => setDraft({ ...draft, note: event.target.value })} /></label>
      <label>Режим<select value={draft.mode} onChange={(event) => setDraft({ ...draft, mode: event.target.value as Definition["mode"], rules: event.target.value === "dynamic" && draft.rules.length === 0 ? [{ attribute: attributes[0]?.code || "", operator: "eq", value: "" }] : draft.rules })}><option value="manual">Вручную</option><option value="dynamic">По правилам</option></select></label>
      <label>Порядок<input type="number" value={draft.sortOrder} onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })} /></label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.active} onChange={(event) => setDraft({ ...draft, active: event.target.checked })} />Показывать на витрине</label>
      <div className="wide collection-cover-editor">
        <div className="collection-cover-preview" style={draft.coverUrl ? { backgroundImage: `url('${draft.coverUrl}')` } : undefined}><span>{draft.coverUrl ? draft.title : "Нет обложки"}</span></div>
        <div><strong>Обложка подборки</strong><p>Лучше горизонтальное изображение 1600×900 или крупнее.</p><label className="admin-upload-button"><input type="file" accept="image/jpeg,image/png,image/gif" disabled={uploading} onChange={(event) => { void uploadCover(event.target.files?.[0]); event.currentTarget.value = ""; }} /><span>{uploading ? "Загружаем…" : "Загрузить обложку"}</span></label><label>Или адрес изображения<input value={draft.coverUrl} placeholder="https://… или /assets/…" onChange={(event) => setDraft({ ...draft, coverUrl: event.target.value })} /></label></div>
      </div>
    </div>

    {draft.mode === "dynamic" ? <div className="collection-rules">
      <div className="variant-editor-head"><div><h4>Правила</h4><p>Все строки применяются одновременно (AND). Совпадение считается по PRODUCT/SKU-атрибутам PIM.</p></div><button type="button" className="admin-action" onClick={() => setDraft({ ...draft, rules: [...draft.rules, { attribute: attributes[0]?.code || "", operator: "eq", value: "" }] })}>Добавить правило</button></div>
      {draft.rules.map((rule, index) => {
        const attribute = attributes.find((candidate) => candidate.code === rule.attribute);
        return <div className="collection-rule-row" key={`${index}-${rule.attribute}`}>
          <select value={rule.attribute} onChange={(event) => updateRule(index, { attribute: event.target.value, operator: "eq", value: "" })}>{attributes.filter((candidate) => candidate.active).map((candidate) => <option value={candidate.code} key={candidate.id}>{candidate.name} · {candidate.code}</option>)}</select>
          <select value={rule.operator} onChange={(event) => updateRule(index, { operator: event.target.value as Rule["operator"], value: "" })}>{operatorsFor(attribute).map((operator) => <option value={operator.value} key={operator.value}>{operator.label}</option>)}</select>
          {rule.operator !== "exists" && (attribute?.dataType === "enum" && ["eq", "neq"].includes(rule.operator) ? <select value={valueForInput(rule)} onChange={(event) => updateRule(index, { value: event.target.value })}><option value="">Не выбрано</option>{attribute.options.filter((option) => option.active).map((option) => <option value={option.code} key={option.code}>{option.label}</option>)}</select> : <input value={valueForInput(rule)} placeholder={rule.operator === "in" ? "значение1, значение2" : attribute?.unit || "Значение"} onChange={(event) => updateRule(index, { value: parseRuleValue(attribute, rule.operator, event.target.value) })} />)}
          <button type="button" className="text-button danger" onClick={() => setDraft({ ...draft, rules: draft.rules.filter((_, ruleIndex) => ruleIndex !== index) })}>Удалить</button>
        </div>;
      })}
      <div className="admin-collection-preview"><strong>Сейчас подходит: {draft.products.length}</strong><span>{products.filter((product) => draft.products.includes(product.id)).slice(0, 12).map((product) => product.name).join(", ") || "После сохранения здесь появится превью"}</span></div>
    </div> : <div className="collection-manual-products">
      <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Найти товар" />
      <div className="admin-collection-list">{shown.map((product) => <label key={product.id}><input type="checkbox" checked={draft.products.includes(product.id)} onChange={() => void toggleProduct(product.id)} /><span>{product.name}</span><small>{product.stock > 0 ? `${product.stock} шт.` : "под заказ"}</small></label>)}</div>
    </div>}

    <div className="dialog-actions"><button type="button" className="danger" disabled={!owner} onClick={() => void remove()}>Удалить подборку</button><button type="button" className="primary" disabled={saving || !draft.title.trim() || !draft.slug.trim() || (draft.mode === "dynamic" && draft.rules.length === 0)} onClick={() => void save()}>{saving ? "Сохраняем…" : "Сохранить"}</button></div>
  </div>;
}

export function CollectionsV2({ owner, onError }: { owner: boolean; onError: (value: string) => void }) {
  const [collections, setCollections] = useState<Definition[]>([]);
  const [products, setProducts] = useState<Product[]>([]);
  const [attributes, setAttributes] = useState<Attribute[]>([]);
  const [opened, setOpened] = useState<number | null>(null);
  const [creating, setCreating] = useState(false);
  const [dragged, setDragged] = useState<number | null>(null);
  const load = useCallback(() => Promise.all([
    api<{ collections?: Definition[] }>("/api/v1/admin/collection-definitions").then((data) => setCollections(data.collections || [])),
    api<{ products?: Product[] }>("/api/v1/admin/products").then((data) => setProducts(data.products || [])),
    api<{ attributes?: Attribute[] }>("/api/v1/admin/attributes").then((data) => setAttributes(data.attributes || [])),
  ]).catch((error) => onError((error as Error).message)), [onError]);
  useEffect(() => { void load(); }, [load]);

  const persistOrder = async (next: Definition[]) => {
    const previous=collections;setCollections(next);
    try {
      const result = await api<{ collections: Definition[] }>("/api/v1/admin/collection-definitions/order", { method: "PUT", body: JSON.stringify({ ids: next.map((item) => item.id) }) });
      setCollections(result.collections);
    } catch (error) { setCollections(previous);onError((error as Error).message); }
  };
  const reorder = async (id: number, direction: -1 | 1) => {
    const index = collections.findIndex((item) => item.id === id); const target = index + direction;
    if (index < 0 || target < 0 || target >= collections.length) return;
    const next = [...collections]; [next[index], next[target]] = [next[target], next[index]];await persistOrder(next);
  };
  const dropBefore = async (targetId:number) => {
    if(!dragged||dragged===targetId)return;const moved=collections.find((item)=>item.id===dragged);if(!moved)return;const next=collections.filter((item)=>item.id!==dragged);next.splice(next.findIndex((item)=>item.id===targetId),0,moved);setDragged(null);await persistOrder(next);
  };

  return <><PageHeading eyebrow="Витрина" title="Подборки" text="Ручные списки или автоматические подборки по PIM-атрибутам" />
    <div className="admin-toolbar"><button className="admin-primary" disabled={creating} onClick={() => setCreating(true)}>Новая подборка</button><span>{collections.length} подборок</span></div>
    {creating && <NewCollectionDraft sortOrder={(collections.length + 1) * 10} onCancel={() => setCreating(false)} onError={onError} onCreated={(collection) => { setCollections((current) => [...current, collection]);setOpened(collection.id);setCreating(false); }} />}
    <div className="admin-collections">{collections.map((collection, index) => <div key={collection.id} className={`admin-collection${dragged===collection.id?" dragging":""}`} draggable onDragStart={()=>setDragged(collection.id)} onDragEnd={()=>setDragged(null)} onDragOver={(event)=>event.preventDefault()} onDrop={()=>void dropBefore(collection.id)}>
      <div className="admin-collection-row"><div className="collection-order-buttons" aria-label={`Порядок: ${collection.title}`}><button type="button" disabled={index===0} aria-label={`Поднять ${collection.title}`} onClick={() => void reorder(collection.id,-1)}>↑</button><button type="button" disabled={index===collections.length-1} aria-label={`Опустить ${collection.title}`} onClick={() => void reorder(collection.id,1)}>↓</button></div><button className="admin-collection-head" onClick={() => setOpened(opened === collection.id ? null : collection.id)}>{collection.coverUrl && <span className="admin-collection-thumb" style={{ backgroundImage: `url('${collection.coverUrl}')` }} />}<span className="admin-collection-copy"><b>{collection.title}</b><small>{collection.mode === "dynamic" ? "Автоматически по правилам" : collection.note || "Ручной список"}</small></span><span className="admin-collection-count">{collection.products.length} товаров</span></button></div>
      {opened === collection.id && <CollectionEditor owner={owner} item={collection} attributes={attributes} products={products} onSaved={(saved) => setCollections((current) => current.map((candidate) => candidate.id === saved.id ? saved : candidate))} onDeleted={(id) => { setCollections((current) => current.filter((candidate) => candidate.id !== id)); setOpened(null); }} onError={onError} />}
    </div>)}{!collections.length && <p className="admin-hint">Подборок пока нет.</p>}</div>
  </>;
}
