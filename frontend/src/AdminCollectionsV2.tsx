import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { PageHeading, api } from "./adminShared";
import { catalogSlug, normalizeCatalogQuery, type RegisterCatalogLeave } from "./adminCatalogLogic";
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

type RegisterGuard = (check:()=>boolean)=>()=>void;
function useCollectionGuard(dirty:boolean,busy:boolean,register:RegisterGuard) {
 useEffect(()=>register(()=>!busy&&(!dirty||window.confirm("В подборке есть несохранённые правки. Отказаться от них?"))),[dirty,busy,register]);
 useEffect(()=>{const protect=(event:BeforeUnloadEvent)=>{if(dirty||busy){event.preventDefault();event.returnValue=""}};window.addEventListener("beforeunload",protect);return()=>window.removeEventListener("beforeunload",protect)},[dirty,busy]);
}

function NewCollectionDraft({ sortOrder, onCreated, onCancel, onError, registerGuard }: {
 registerGuard:RegisterGuard;
  sortOrder: number; onCreated: (value: Definition) => void; onCancel: () => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState({ title: "", slug: "", note: "", coverUrl: "", active: false });
  const [slugEdited,setSlugEdited]=useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [saving, setSaving] = useState(false);
  useCollectionGuard(Boolean(draft.title||draft.slug||draft.note||draft.coverUrl||file),saving,registerGuard);
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
      <label>Название<input autoFocus value={draft.title} onChange={(event) => { const title=event.target.value;setDraft((current)=>({ ...current,title,slug: slugEdited?current.slug:catalogSlug(title) })); }} /></label>
      <label>Адрес подборки<input aria-label="Адрес подборки" aria-describedby="new-collection-address-help" value={draft.slug} placeholder="green-office" onChange={(event) => {setSlugEdited(true);setDraft({ ...draft, slug: event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-") })}} /><small id="new-collection-address-help">Латиницей, например: dlya-ofisa. Создаётся из названия.</small></label>
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
  if (attribute?.dataType === "boolean") return raw === ""?undefined:raw === "true";
  return raw;
}

function operatorsFor(attribute?: Attribute): Array<{ value: Rule["operator"]; label: string }> {
  const common: Array<{ value: Rule["operator"]; label: string }> = [{ value: "eq", label: "равно" }, { value: "neq", label: "не равно" }, { value: "exists", label: "заполнено" }];
  if (!attribute) return common;
  if (attribute.dataType === "number") return [...common, { value: "in", label: "одно из" }, { value: "gte", label: "не меньше" }, { value: "lte", label: "не больше" }];
  if (attribute.dataType === "multi_enum") return [...common, { value: "in", label: "одно из" }, { value: "contains", label: "содержит" }];
  if (attribute.dataType === "enum" || attribute.dataType === "boolean") return [...common, { value: "in", label: "одно из" }];
  return [...common, { value: "in", label: "одно из" }, { value: "contains", label: "содержит" }];
}

function ruleSummary(rule:Rule,attributes:Attribute[]){
 const attribute=attributes.find(x=>x.code===rule.attribute);const operator=operatorsFor(attribute).find(x=>x.value===rule.operator)?.label||rule.operator;
 const values=Array.isArray(rule.value)?rule.value:[rule.value];const value=values.map(x=>typeof x==="boolean"?(x?"Да":"Нет"):attribute?.options.find(o=>o.code===String(x))?.label||String(x??"")).join(", ");
 return `${attribute?.name||"Неизвестная характеристика"} ${operator}${rule.operator==="exists"?"":` ${value}${attribute?.unit?` ${attribute.unit}`:""}`}`;
}

function RuleValue({rule,attribute,index,onChange}:{rule:Rule;attribute?:Attribute;index:number;onChange:(value:unknown)=>void}) {
 const label=`Значение условия ${index+1}`;
 const options=attribute?.dataType==="boolean"?[{code:"true",label:"Да",active:true},{code:"false",label:"Нет",active:true}]:attribute?.options.filter(x=>x.active)||[];
 const choices=["enum","multi_enum","boolean"].includes(attribute?.dataType||"");
 const typed=(value:string)=>attribute?.dataType==="boolean"?value==="true":value;
 if(choices&&rule.operator==="in") {
  const selected=Array.isArray(rule.value)?rule.value.map(String):[];
  return <fieldset className="collection-rule-choices"><legend>{label}</legend>{options.map(option=><label key={option.code}><input type="checkbox" checked={selected.includes(option.code)} onChange={event=>onChange(event.target.checked?[...selected,option.code].map(typed):selected.filter(x=>x!==option.code).map(typed))}/>{option.label}</label>)}</fieldset>;
 }
 if(choices)return <select aria-label={label} value={valueForInput(rule)} onChange={event=>onChange(event.target.value===""?undefined:typed(event.target.value))}><option value="">Не выбрано</option>{rule.value!=null&&!options.some(x=>x.code===String(rule.value))&&<option value={String(rule.value)}>Неизвестное значение: {String(rule.value)}</option>}{options.map(option=><option value={option.code} key={option.code}>{option.label}</option>)}</select>;
 return <input aria-label={label} value={valueForInput(rule)} placeholder={rule.operator==="in"?"Числа через запятую":attribute?.unit||"Значение"} onChange={event=>onChange(rule.operator==="in"?event.target.value.split(","):parseRuleValue(attribute,rule.operator,event.target.value))}/>;
}

function CollectionEditor({ owner, item, attributes, products, onSaved, onDeleted, onError, registerGuard }: {
 registerGuard:RegisterGuard;
  owner: boolean; item: Definition; attributes: Attribute[]; products: Product[];
  onSaved: (value: Definition) => void; onDeleted: (id: number) => void; onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState<Definition>(() => structuredClone(item));
  const [query, setQuery] = useState("");
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [saveError,setSaveError]=useState("");
  const [notice,setNotice]=useState("");
  const [preview,setPreview]=useState<{rules:string;products:number[];error?:string}|null>(null);
  const normalizedRules=useMemo(()=>draft.rules.map(rule=>({...rule,value:Array.isArray(rule.value)?rule.value.map(value=>typeof value==="string"?value.trim():value).filter(value=>value!==""):rule.value})),[draft.rules]);
  const rulesValid=normalizedRules.every(rule=>{
   const attribute=attributes.find(x=>x.code===rule.attribute&&x.active);
   if(!attribute)return false;if(rule.operator==="exists")return true;
   const values=Array.isArray(rule.value)?rule.value:[rule.value];
   return values.length>0&&values.every(value=>value!=null&&value!==""&&(attribute.dataType!=="number"||Number.isFinite(Number(value)))&&(!["enum","multi_enum"].includes(attribute.dataType)||attribute.options.some(option=>option.active&&option.code===String(value))));
  });
  const ruleKey=JSON.stringify(normalizedRules);
  useEffect(()=>{if(draft.mode!=="dynamic")return;let active=true;const timer=window.setTimeout(()=>{api<{products:number[]}>("/api/v1/admin/collection-definitions/preview",{method:"POST",body:JSON.stringify({rules:normalizedRules})}).then(data=>{if(active)setPreview({rules:ruleKey,products:data.products||[]})}).catch(error=>{if(active)setPreview({rules:ruleKey,products:[],error:error.message})})},300);return()=>{active=false;window.clearTimeout(timer)}},[draft.mode,normalizedRules,ruleKey]);
  const previewIds=preview?.rules===ruleKey&&!preview.error?preview.products:null;
  const dirty=JSON.stringify(draft)!==JSON.stringify(item);
  useCollectionGuard(dirty,saving||uploading,registerGuard);
  const shown = useMemo(() => products.filter((product) => normalizeCatalogQuery(`${product.name} ${product.latinName} ${product.sku}`).includes(normalizeCatalogQuery(query))), [products, query]);

  const save = async () => {
    if(saving||uploading)return;
    setSaving(true);setSaveError("");setNotice("");
    try {
      const result = await api<{ collection: Definition }>(`/api/v1/admin/collection-definitions/${item.id}`, {
        method: "PUT",
        body: JSON.stringify({ slug: draft.slug, title: draft.title, note: draft.note, coverUrl: draft.coverUrl, sortOrder: draft.sortOrder, active: draft.active, mode: draft.mode, rules: draft.mode === "dynamic" ? normalizedRules : [], products:draft.mode==="manual"?draft.products:undefined }),
      });
      setDraft(structuredClone(result.collection));
      onSaved(result.collection);setNotice("Подборка и состав сохранены");
    } catch (error) { setSaveError((error as Error).message);onError((error as Error).message); }
    finally { setSaving(false); }
  };

  const uploadCover = async (file?: File) => {
    if (!file) return;
    setUploading(true);
    try {
      const body = new FormData(); body.append("file", file);
      const result = await api<{ collection: Definition }>(`/api/v1/admin/collection-definitions/${item.id}/cover`, { method: "POST", body });
      setDraft(current=>({...current,coverUrl:result.collection.coverUrl})); onSaved({...item,coverUrl:result.collection.coverUrl});
    } catch (error) { onError((error as Error).message); }
    finally { setUploading(false); }
  };

  const toggleProduct = (productId:number) => {
    if(draft.mode!=="manual")return;
    setNotice("");setDraft(current=>({...current,products:current.products.includes(productId)?current.products.filter(id=>id!==productId):[...current.products,productId]}));
  };

  const remove = async () => {
    if (!window.confirm(`Удалить подборку «${draft.title}»?`)) return;
    try {
      await api(`/api/v1/admin/collection-definitions/${item.id}`, { method: "DELETE" });
      onDeleted(item.id);
    } catch (error) { onError((error as Error).message); }
  };

  const updateRule = (index: number, patch: Partial<Rule>) => setDraft((current) => ({ ...current, rules: current.rules.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, ...patch } : rule) }));

  return <div className="admin-collection-body collection-editor-v2"><fieldset className="collection-editor-fields" disabled={saving||uploading}>
    <div className="admin-form-grid">
      <label>Название<input value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
      <label>Адрес подборки<input value={draft.slug} onChange={(event) => setDraft({ ...draft, slug: event.target.value.toLowerCase().replace(/[^a-z0-9-]+/g, "-") })} /></label>
      <label className="wide">Подпись<input value={draft.note} onChange={(event) => setDraft({ ...draft, note: event.target.value })} /></label>
      <label>Режим<select value={draft.mode} onChange={(event) => setDraft({ ...draft, mode: event.target.value as Definition["mode"], rules: event.target.value === "dynamic" && draft.rules.length === 0 ? [{ attribute: attributes[0]?.code || "", operator: "eq", value: "" }] : draft.rules })}><option value="manual">Вручную</option><option value="dynamic">По правилам</option></select></label>
      <label>Порядок<input type="number" value={draft.sortOrder} onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })} /></label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.active} onChange={(event) => setDraft({ ...draft, active: event.target.checked })} />Показывать на витрине</label>
      <div className="wide collection-cover-editor">
        <div className="collection-cover-preview" style={draft.coverUrl ? { backgroundImage: `url('${draft.coverUrl}')` } : undefined}><span>{draft.coverUrl ? draft.title : "Нет обложки"}</span></div>
        <div><strong>Обложка подборки</strong><p>Горизонтальное изображение 1600×900 или крупнее. Загрузка файла применяется сразу.</p><label className="admin-upload-button"><input type="file" accept="image/jpeg,image/png,image/gif" disabled={uploading} onChange={(event) => { void uploadCover(event.target.files?.[0]); event.currentTarget.value = ""; }} /><span>{uploading ? "Загружаем…" : "Загрузить обложку"}</span></label><label>Или адрес изображения<input value={draft.coverUrl} placeholder="https://… или /assets/…" onChange={(event) => setDraft({ ...draft, coverUrl: event.target.value })} /></label></div>
      </div>
    </div>

    {draft.mode === "dynamic" ? <div className="collection-rules">
      <div className="variant-editor-head"><div><h4>Правила</h4><p>Товар должен соответствовать всем условиям. Проверяются общие характеристики и активные размеры; подборка включает карточку целиком.</p></div><button type="button" className="admin-action" onClick={() => setDraft({ ...draft, rules: [...draft.rules, { attribute: attributes[0]?.code || "", operator: "eq", value: "" }] })}>Добавить правило</button></div>
      {draft.rules.map((rule, index) => {
        const attribute = attributes.find((candidate) => candidate.code === rule.attribute);
        return <div className="collection-rule-row" key={`${index}-${rule.attribute}`}>
          <select aria-label={`Характеристика условия ${index+1}`} value={rule.attribute} onChange={(event) => updateRule(index, { attribute: event.target.value, operator: "eq", value: "" })}>{attributes.filter((candidate) => candidate.active).map((candidate) => <option value={candidate.code} key={candidate.id}>{candidate.name}{candidate.unit?` · ${candidate.unit}`:""}</option>)}</select>
          <select aria-label={`Сравнение условия ${index+1}`} value={rule.operator} onChange={(event) => updateRule(index, { operator: event.target.value as Rule["operator"], value: "" })}>{operatorsFor(attribute).map((operator) => <option value={operator.value} key={operator.value}>{operator.label}</option>)}</select>
          {rule.operator !== "exists" && <RuleValue rule={rule} attribute={attribute} index={index} onChange={value=>updateRule(index,{value})}/> }
          <button type="button" className="text-button danger" aria-label={`Удалить условие ${index+1}`} onClick={() => setDraft({ ...draft, rules: draft.rules.filter((_, ruleIndex) => ruleIndex !== index) })}>Удалить</button>
        </div>;
      })}
      {!rulesValid&&<p className="admin-hint">Выберите характеристику и заполните значения каждого условия.</p>}<div className="admin-collection-preview"><strong>{previewIds?`Подходит: ${previewIds.length} товаров`:preview?.rules===ruleKey&&preview.error?"Не удалось проверить условия":"Проверяем условия…"}</strong>{preview?.rules===ruleKey&&preview.error&&<p role="alert">{preview.error}</p>}{previewIds&&<><p>После сохранения: добавится {previewIds.filter(id=>!item.products.includes(id)).length}, исключится {item.products.filter(id=>!previewIds.includes(id)).length}. Предпросмотр ничего не сохраняет.</p><div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар</th><th>Артикул / размер</th><th>Статус</th><th>Совпадение</th></tr></thead><tbody>{products.filter(product=>previewIds.includes(product.id)).map(product=><tr key={product.id}><td><a href={`/admin?section=products&product=${product.id}&tab=attributes`}>{product.name}</a></td><td>{product.sku} · {product.variantLabel}{product.variantCount&&product.variantCount>1?` · всего размеров: ${product.variantCount}`:""}<a href={`/admin?section=products&product=${product.id}&tab=variants`}>Проверить размеры</a></td><td>{product.status==="published"?"Опубликован":product.status==="archived"?"Архив":"Черновик"}</td><td>{draft.rules.map((rule,index)=><small key={index}>{ruleSummary(rule,attributes)}</small>)}</td></tr>)}</tbody></table></div>{!previewIds.length&&<p>Ни один товар не соответствует всем условиям.</p>}</>}</div>
    </div> : <div className="collection-manual-products">
      <p className="admin-hint">Выбрано: {draft.products.length}. Состав и настройки сохраняются вместе кнопкой «Сохранить».</p><input aria-label="Найти товар в подборку" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Найти товар" />
      <div className="admin-collection-list">{shown.map((product) => <label key={product.id}><input type="checkbox" checked={draft.products.includes(product.id)} onChange={() => toggleProduct(product.id)} /><span>{product.name}<small>Артикул {product.sku} · {product.variantLabel}</small></span><small>{(product.totalStock??product.stock) > 0 ? `${product.totalStock??product.stock} шт.` : "Нет в наличии"}</small></label>)}</div>{!shown.length&&<p role="status">Товары не найдены. Измените запрос.</p>}
    </div>}

    </fieldset><div className="collection-save-status" role={saveError?"alert":"status"}>{saveError||(dirty?"Есть несохранённые изменения":notice||"Все изменения сохранены")}</div><div className="dialog-actions"><button type="button" className="danger" disabled={!owner||saving||uploading} onClick={() => void remove()}>Удалить подборку</button><button type="button" className="primary" disabled={saving || uploading || !dirty || !draft.title.trim() || !draft.slug.trim() || (draft.mode === "dynamic" && (draft.rules.length === 0||!rulesValid))} onClick={() => void save()}>{saving ? "Сохраняем…" : "Сохранить"}</button></div>
  </div>;
}

export function CollectionsV2({ owner, onError, registerBeforeLeave }: { owner: boolean; onError: (value: string) => void;registerBeforeLeave?:RegisterCatalogLeave }) {
  const guard=useRef<()=>boolean>(()=>true);
  const currentURL=useRef(location.href);
  const registerGuard=useCallback<RegisterGuard>(check=>{guard.current=check;return()=>{if(guard.current===check)guard.current=()=>true}},[]);
  const allowLeave=useCallback(()=>{const allowed=guard.current();if(!allowed&&location.href!==currentURL.current)history.pushState(null,"",currentURL.current);return allowed},[]);
  useEffect(()=>registerBeforeLeave?.(async()=>allowLeave()),[registerBeforeLeave,allowLeave]);
  const [collections, setCollections] = useState<Definition[]>([]);
  const [products, setProducts] = useState<Product[]>([]);
  const [attributes, setAttributes] = useState<Attribute[]>([]);
  const [opened, setOpened] = useState<number | null>(()=>Number(new URLSearchParams(location.search).get("collection"))||null);
  const [creating, setCreating] = useState(false);
  const changeOpened=(id:number|null)=>{if(!allowLeave())return;setOpened(id);setCreating(false);const url=new URL(location.href);if(id)url.searchParams.set("collection",String(id));else url.searchParams.delete("collection");history.replaceState(null,"",url.pathname+url.search);currentURL.current=location.href};
  useEffect(()=>{const read=()=>{if(new URLSearchParams(location.search).get("section")!=="collections")return;if(allowLeave()){setOpened(Number(new URLSearchParams(location.search).get("collection"))||null);setCreating(false);currentURL.current=location.href}};window.addEventListener("popstate",read);return()=>window.removeEventListener("popstate",read)},[allowLeave]);
  const [ordering,setOrdering]=useState(false);
  const [loading,setLoading]=useState(true);
  const [loadError,setLoadError]=useState("");
  const [dragged, setDragged] = useState<number | null>(null);
  const load = useCallback(() => {setLoading(true);setLoadError("");return Promise.all([
    api<{ collections?: Definition[] }>("/api/v1/admin/collection-definitions").then((data) => setCollections(data.collections || [])),
    api<{ products?: Product[] }>("/api/v1/admin/products").then((data) => setProducts(data.products || [])),
    api<{ attributes?: Attribute[] }>("/api/v1/admin/attributes").then((data) => setAttributes(data.attributes || [])),
  ]).catch((error) => {setLoadError((error as Error).message);onError((error as Error).message)}).finally(()=>setLoading(false))}, [onError]);
  useEffect(() => { void load(); }, [load]);

  const persistOrder = async (next: Definition[]) => {
    if(ordering)return;setOrdering(true);const previous=collections;setCollections(next);
    try {
      const result = await api<{ collections: Definition[] }>("/api/v1/admin/collection-definitions/order", { method: "PUT", body: JSON.stringify({ ids: next.map((item) => item.id) }) });
      setCollections(result.collections);
    } catch (error) { setCollections(previous);onError((error as Error).message); }finally{setOrdering(false)}
  };
  const reorder = async (id: number, direction: -1 | 1) => {
    const index = collections.findIndex((item) => item.id === id); const target = index + direction;
    if (index < 0 || target < 0 || target >= collections.length) return;
    const next = [...collections]; [next[index], next[target]] = [next[target], next[index]];await persistOrder(next);
  };
  const dropBefore = async (targetId:number) => {
    if(!dragged||dragged===targetId)return;const moved=collections.find((item)=>item.id===dragged);if(!moved)return;const next=collections.filter((item)=>item.id!==dragged);next.splice(next.findIndex((item)=>item.id===targetId),0,moved);setDragged(null);await persistOrder(next);
  };

  return <><PageHeading eyebrow="Витрина" title="Подборки" text="Ручной состав или автоматический подбор по характеристикам" />
    <div className="admin-toolbar"><button className="admin-primary" disabled={creating||ordering||loading} onClick={() => {if(allowLeave()){setOpened(null);setCreating(true)}}}>Новая подборка</button><span>{collections.length} подборок</span></div>
    {creating && <NewCollectionDraft registerGuard={registerGuard} sortOrder={(collections.length + 1) * 10} onCancel={() => {if(allowLeave())setCreating(false)}} onError={onError} onCreated={(collection) => { setCollections((current) => [...current, collection]);setOpened(collection.id);setCreating(false); }} />}
    <div className="admin-collections">{collections.map((collection, index) => <div key={collection.id} className={`admin-collection${dragged===collection.id?" dragging":""}`} draggable={false} onDragStart={()=>setDragged(collection.id)} onDragEnd={()=>setDragged(null)} onDragOver={(event)=>event.preventDefault()} onDrop={()=>void dropBefore(collection.id)}>
      <div className="admin-collection-row"><div className="collection-order-buttons" draggable={!opened&&!creating&&!ordering} aria-label={`Порядок: ${collection.title}`}><button type="button" disabled={index===0||ordering||!!opened||creating} aria-label={`Поднять ${collection.title}`} onClick={() => void reorder(collection.id,-1)}>↑</button><button type="button" disabled={index===collections.length-1||ordering||!!opened||creating} aria-label={`Опустить ${collection.title}`} onClick={() => void reorder(collection.id,1)}>↓</button></div><button className="admin-collection-head" aria-expanded={opened===collection.id} onClick={() => changeOpened(opened === collection.id ? null : collection.id)}>{collection.coverUrl && <span className="admin-collection-thumb" style={{ backgroundImage: `url('${collection.coverUrl}')` }} />}<span className="admin-collection-copy"><b>{collection.title}</b><small>{collection.active?"На витрине":"Скрыта"} · {collection.mode === "dynamic" ? collection.rules.map(rule=>ruleSummary(rule,attributes)).join("; ") : collection.note || "Ручной список"}</small></span><span className="admin-collection-count">{collection.products.length} товаров</span></button></div>
      {opened === collection.id && <CollectionEditor registerGuard={registerGuard} owner={owner} item={collection} attributes={attributes} products={products} onSaved={(saved) => setCollections((current) => current.map((candidate) => candidate.id === saved.id ? saved : candidate))} onDeleted={(id) => { setCollections((current) => current.filter((candidate) => candidate.id !== id)); setOpened(null); }} onError={onError} />}
    </div>)}{loading?<p role="status">Загружаем подборки…</p>:loadError?<div role="alert"><p>{loadError}</p><button onClick={()=>void load()}>Повторить загрузку</button></div>:!collections.length&&<p className="admin-hint">Подборок пока нет.</p>}</div>
  </>;
}
