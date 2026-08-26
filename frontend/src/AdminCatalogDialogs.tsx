import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CategoryPicker } from "./AdminCatalog";
import { Dialog, api, money, sabyFieldLabels } from "./adminShared";
import { aiCandidates, applyAICandidates, displayValue, isPlantProduct, valueMissing, type AICandidate } from "./adminCatalogLogic";
import { VariantsEditor } from "./AdminPim";
import { ProductMediaManager } from "./ProductMediaManager";
import type { Category, CategoryAttribute, ImportEntry, Product } from "./adminTypes";

export function AttributeFields({ schema, values, onChange, onGenerate, busy }: {
  schema: CategoryAttribute[]; values: Record<string, unknown>;
  onChange: (code: string, value: string | number | boolean | string[] | null) => void;
  onGenerate?: (code: string) => void; busy?: string | null;
}) {
  const field = (attribute: CategoryAttribute) => {
    const value = values[attribute.code];
    const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
    const ai = onGenerate && <button className="ai-field-button" type="button" disabled={Boolean(busy)} onClick={() => onGenerate(attribute.code)} title={`Сгенерировать: ${attribute.name}`}>{busy === attribute.code ? "…" : "✦"}</button>;
    if (attribute.dataType === "boolean") return <label className={`attribute-toggle ${value === true ? "selected" : ""}`} key={attribute.code}><input type="checkbox" checked={value === true} onChange={(event) => onChange(attribute.code, event.target.checked)} /><span>{title}</span>{ai}</label>;
    if (attribute.dataType === "enum") { const current = String(value ?? ""); const unknown = current && !attribute.options.includes(current); return <label className="attribute-control" key={attribute.code}><span>{title}{ai}</span><select aria-label={title} required={attribute.required} value={current} onChange={(event) => onChange(attribute.code, event.target.value || null)}><option value="">Не указано</option>{unknown && <option value={current}>{`Неизвестное значение: ${current}`}</option>}{attribute.options.map((option) => <option key={option} value={option}>{attribute.optionLabels?.[option] || option}</option>)}</select>{unknown && <small className="admin-inline-error">Старое значение сохранено. Выберите вариант из матрицы, чтобы заменить его.</small>}</label>; }
    if (attribute.dataType === "multi_enum") {
      const selected = Array.isArray(value) ? value.map(String) : [];
      const unknown = selected.filter((option) => !attribute.options.includes(option));
      return <fieldset className="attribute-multi" key={attribute.code}><legend>{title}{ai}</legend><div className="attribute-choice-list">{attribute.options.map((option) => <label className={selected.includes(option) ? "selected" : ""} key={option}><input type="checkbox" checked={selected.includes(option)} onChange={(event) => onChange(attribute.code, event.target.checked ? [...selected, option] : selected.filter((item) => item !== option))} />{attribute.optionLabels?.[option] || option}</label>)}</div>{unknown.length > 0 && <small className="admin-inline-error">Неизвестные старые значения сохранены: {unknown.join(", ")}</small>}</fieldset>;
    }
    return <label className="attribute-control" key={attribute.code}><span>{title}{ai}</span><input aria-label={title} type={attribute.dataType === "number" ? "number" : "text"} min={attribute.dataType === "number" ? 0 : undefined} required={attribute.required} value={value == null ? "" : String(value)} onChange={(event) => onChange(attribute.code, event.target.value === "" ? null : attribute.dataType === "number" ? Number(event.target.value) : event.target.value)} /></label>;
  };
  const simple = schema.filter((item) => item.dataType !== "boolean" && item.dataType !== "multi_enum");
  const choices = schema.filter((item) => item.dataType === "multi_enum");
  const toggles = schema.filter((item) => item.dataType === "boolean");
  return <div className="attribute-groups">
    {simple.length > 0 && <div className="attribute-group"><h4>Значения</h4><div className="attribute-control-grid">{simple.map(field)}</div></div>}
    {choices.length > 0 && <div className="attribute-group"><h4>Подходящие варианты</h4>{choices.map(field)}</div>}
    {toggles.length > 0 && <div className="attribute-group"><h4>Особенности</h4><div className="attribute-toggle-grid">{toggles.map(field)}</div></div>}
  </div>;
}

function validAttributes(schema: CategoryAttribute[], values: Record<string, unknown>) {
  const normalized: Record<string, unknown> = {};
  for (const item of schema) {
    const value = values[item.code];
    if (value == null || value === "") continue;
    if (item.dataType === "boolean" && typeof value === "boolean") normalized[item.code] = value;
    else if (item.dataType === "number" && Number.isFinite(Number(value))) normalized[item.code] = Number(value);
    else if (item.dataType === "enum" && typeof value === "string" && item.options.includes(value)) normalized[item.code] = value;
    else if (item.dataType === "multi_enum" && Array.isArray(value)) {
      const selected = [...new Set(value.map(String).filter((option) => item.options.includes(option)))];
      if (selected.length) normalized[item.code] = selected;
    } else if (item.dataType === "text" && typeof value === "string" && value.trim()) normalized[item.code] = value.trim();
  }
  return normalized;
}

function changedAttributes(schema: CategoryAttribute[], values: Record<string, unknown>, original: Record<string, unknown>) {
  const result: Record<string, unknown> = {};
  for (const item of schema) {
    if (JSON.stringify(values[item.code]) === JSON.stringify(original[item.code])) continue;
    if (valueMissing(values[item.code])) result[item.code] = null;
    else Object.assign(result, validAttributes([item], values));
  }
  return result;
}

function missingRequired(schema: CategoryAttribute[], values: Record<string, unknown>) {
  return schema.filter((item) => item.required && (values[item.code] == null || values[item.code] === "" || (Array.isArray(values[item.code]) && (values[item.code] as unknown[]).length === 0))).map((item) => item.name);
}

type ProductEditorSection = "main" | "attributes" | "care" | "variants" | "photos" | "sync";
type ProductAIMode = "description" | "attributes" | "care";
type AIDraft = { name?:string; latinName?:string; shortDescription?:string; description?:string; careInstructions?:string; attributes?:Record<string,unknown>; passport?:Product["passport"]; warnings?:string[] };
const passportFields = [
  ["origin","Происхождение"],["lighting","Освещение"],["watering","Полив"],["humidity","Влажность"],
  ["temperature","Температура"],["soil","Грунт"],["fertilizer","Удобрение"],["repotting","Пересадка"],
  ["careDifficulty","Сложность ухода"],["growthRate","Скорость роста"],["matureSize","Взрослый размер"],
  ["toxicity","Токсичность"],["problems","Типичные проблемы и решения"],["pests","Вредители"],
] as const;

export function ProductDialog({ product, onClose, onSaved, onError, hasNext = false }: { product: Product; onClose: () => void; onSaved: (value: Product, openNext: boolean) => void; onError: (value: string) => void; hasNext?: boolean }) {
  const [form, setForm] = useState(product);
  const [section, setSection] = useState<ProductEditorSection>("main");
  const [categories, setCategories] = useState<Category[]>([]);
  const [schema, setSchema] = useState<CategoryAttribute[]>([]);
  const [aiBusy,setAIBusy]=useState<ProductAIMode|null>(null);
  const [aiFieldBusy,setAIFieldBusy]=useState<string|null>(null);
  const [aiCoverBusy,setAICoverBusy]=useState(false);
  const [saving,setSaving]=useState(false);
  const [saveError,setSaveError]=useState("");
  const [saveNotice,setSaveNotice]=useState("");
  const [aiPreview,setAIPreview]=useState<{ candidates: AICandidate[]; selected: Set<string> }|null>(null);
  const aiRequest=useRef(false);
  useEffect(() => { api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setCategories(data.categories)).catch((error) => onError(error.message)); }, [onError]);
  useEffect(() => { let active = true; if (form.categoryId) api<{ attributes: CategoryAttribute[] }>(`/api/v1/admin/categories/${form.categoryId}/attributes`).then((data) => { if (active) setSchema(data.attributes || []); }).catch((error) => onError(error.message)); return () => { active = false; }; }, [form.categoryId, onError]);
  const plant = isPlantProduct(form, categories);
  const dirty = useMemo(() => JSON.stringify(form) !== JSON.stringify(product), [form, product]);
  useEffect(() => { const protect = (event: BeforeUnloadEvent) => { if (dirty) { event.preventDefault(); event.returnValue = ""; } }; window.addEventListener("beforeunload", protect); return () => window.removeEventListener("beforeunload", protect); }, [dirty]);
  const close = () => { if (!dirty || window.confirm("Закрыть карточку и потерять несохранённые изменения?")) onClose(); };
  const save = async (openNext = false) => {
    const missing = missingRequired(schema, form.attributes || {});
    if (form.status === "published" && missing.length) { const message=`Заполните обязательные характеристики: ${missing.join(", ")}`;setSaveError(message);setSection("attributes");return; }
    setSaving(true);setSaveError("");setSaveNotice("");
    try {
      const body: Record<string, unknown> = {};
      const copyIfChanged = (key: keyof Product) => { if (JSON.stringify(form[key]) !== JSON.stringify(product[key])) body[key] = form[key]; };
      (["name","shortDescription","description","status","featured","image","catalogSection","categoryId","sabyFields"] as Array<keyof Product>).forEach(copyIfChanged);
      if (product.categoryId && !form.categoryId) { delete body.categoryId; body.clearCategory = true; }
      if (plant) (["latinName","careInstructions","passport","importantWarnings"] as Array<keyof Product>).forEach(copyIfChanged);
      const attributes = changedAttributes(schema.filter((item) => item.scope === "product"), form.attributes || {}, product.attributes || {});
      if (Object.keys(attributes).length) body.attributes = attributes;
      const result = await api<{ product: Product }>(`/api/v1/admin/products/${product.id}`, { method: "PATCH", body: JSON.stringify(body) });
      setForm(result.product); setSaveNotice("Сохранено"); onSaved(result.product, openNext);
    } catch (error) { const message=(error as Error).message;setSaveError(message);onError(message);setSaving(false); }
    finally { setSaving(false); }
  };
  const showProposal = useCallback((proposal: Record<string, unknown>, field?: string) => { const normalized={...proposal,attributes:validAttributes(schema.filter((item)=>item.scope==="product"),proposal.attributes&&typeof proposal.attributes==="object"?proposal.attributes as Record<string,unknown>: {})}; const candidates = aiCandidates(form, normalized, schema, field); setAIPreview({ candidates, selected: new Set(candidates.filter((item) => !item.conflict).map((item) => item.path)) }); }, [form, schema]);
  const generateAI=async(mode:ProductAIMode,field?:string)=>{if(aiRequest.current||aiBusy||aiFieldBusy||aiCoverBusy)return;aiRequest.current=true;setAIBusy(field?null:mode);setAIFieldBusy(field||null);try{const result=await api<{proposal:AIDraft}>(`/api/v1/admin/products/${product.id}/ai-draft`,{method:"POST",body:JSON.stringify({mode})});showProposal(result.proposal as Record<string, unknown>,field);}catch(error){onError((error as Error).message);}finally{aiRequest.current=false;setAIBusy(null);setAIFieldBusy(null);}};
  const aiField=(mode:ProductAIMode,field:string,label="Сгенерировать поле")=><button className="ai-field-button" type="button" disabled={Boolean(aiBusy)||Boolean(aiFieldBusy)||aiCoverBusy} onClick={()=>generateAI(mode,field)} title={label}>{aiFieldBusy===field?"…":"✦"}</button>;
  const sectionAI=(mode:ProductAIMode,label:string)=><button className="ai-section-button" type="button" disabled={Boolean(aiBusy)||Boolean(aiFieldBusy)||aiCoverBusy} onClick={()=>generateAI(mode)}>{aiBusy===mode?"✦ Генерируем…":`✦ ${label}`}</button>;
  const generateCover=async()=>{if(!plant||aiCoverBusy)return;const identity=[form.name,form.latinName].filter(Boolean).join(" / ");const prompt=`Photorealistic premium e-commerce product photo of one botanically accurate ${identity}. The complete plant and pot are fully visible and centered, with generous margins. Simple matte warm ivory or light beige ceramic pot, no logo. Seamless warm creamy beige studio background (#F4EBDD), soft diffused daylight from upper left, subtle natural floor shadow. Clean quiet Ficusin catalogue style, realistic leaf texture and true cultivar colors. No room interior, no furniture, no props, no text, no label, no badge, no hands, no people, no extra plants, no decorative stones. Square 1:1 composition.`;setAICoverBusy(true);try{const result=await api<{url:string}>(`/api/v1/admin/products/${product.id}/ai-cover`,{method:"POST",body:JSON.stringify({prompt})});showProposal({image:result.url});}catch(error){onError((error as Error).message);}finally{setAICoverBusy(false);}};
  const sections: Array<{id:ProductEditorSection;label:string}> = [
    {id:"main",label:"Основное"},{id:"attributes",label:"Характеристики"},...(plant?[{id:"care" as ProductEditorSection,label:"Уход и FAQ"}]:[]),
    {id:"variants",label:"Варианты"},{id:"photos",label:"Фотографии"},{id:"sync",label:"SEO / публикация"},
  ];
  const changeCategory = async (categoryId?: number) => { if (categoryId === form.categoryId) return; let nextSchema: CategoryAttribute[] = []; if (categoryId) { try { nextSchema = (await api<{attributes:CategoryAttribute[]}>(`/api/v1/admin/categories/${categoryId}/attributes`)).attributes || []; } catch (error) { onError((error as Error).message); return; } } const visible = new Set(nextSchema.map((item)=>item.code)); const hidden = schema.filter((item)=>!visible.has(item.code)&&!valueMissing(form.attributes?.[item.code])).map((item)=>item.name); if (hidden.length && !window.confirm(`После смены категории перестанут отображаться: ${hidden.join(", ")}. Значения не удалятся. Продолжить?`)) return; const rootSlug = (()=>{let c=categories.find((item)=>item.id===categoryId);while(c?.parentId)c=categories.find((item)=>item.id===c?.parentId);return c?.slug;})(); setSchema(nextSchema); setForm({...form,categoryId,catalogSection:rootSlug||form.catalogSection}); if(rootSlug!=="plants"&&section==="care")setSection("attributes"); };
  const applyPreview = () => { if (!aiPreview) return; const conflicts = aiPreview.candidates.filter((item)=>item.conflict&&aiPreview.selected.has(item.path)); if (conflicts.length && !window.confirm(`Заменить вручную заполненные поля: ${conflicts.map((item)=>item.label).join(", ")}?`)) return; setForm(applyAICandidates(form,aiPreview.candidates,aiPreview.selected)); setAIPreview(null); };
  return <Dialog title="Редактирование товара" onClose={close} className="product-editor-dialog">
    <div className="product-editor-shell">
      <aside className="product-editor-aside">
        <div className="product-editor-cover">{form.image ? <img src={form.image} alt="" /> : <span>Нет обложки</span>}</div>
        <div className="product-editor-identity"><strong>{form.name || "Без названия"}</strong><small>{form.sabyCode || `Товар ${form.id}`}</small></div>
        <nav className="product-editor-nav" aria-label="Разделы карточки">{sections.map((item)=><button key={item.id} type="button" className={section===item.id?"active":""} onClick={()=>setSection(item.id)}><span>{item.label}</span>{item.id==="attributes"&&schema.length>0&&<small>{schema.filter(x=>x.scope==="product"&&x.audience==="customer").length}</small>}</button>)}</nav>
      </aside>
      <main className="product-editor-content">
        {section==="main"&&<section className="editor-section"><header><div><p>Карточка</p><h3>Название и описание</h3></div>{sectionAI("description","Сгенерировать раздел")}</header><div className="admin-form-grid product-form">
          <label className="wide"><span>Название {aiField("description","name")}</span><input aria-label="Название" value={form.name} onChange={(event)=>setForm({...form,name:event.target.value})}/><small>Размер горшка лучше хранить в варианте, а не в названии.</small></label>
          {plant&&<label><span>Латинское название {aiField("description","latinName")}</span><input aria-label="Латинское название" value={form.latinName} onChange={(event)=>setForm({...form,latinName:event.target.value})}/></label>}
          <label className="wide"><span>Короткое описание {aiField("description","shortDescription")}</span><textarea aria-label="Короткое описание" rows={3} value={form.shortDescription} onChange={(event)=>setForm({...form,shortDescription:event.target.value})}/><small>1–2 предложения для верхней части карточки.</small></label>
          <label className="wide"><span>Описание {aiField("description","description")}</span><textarea aria-label="Описание" rows={9} value={form.description} onChange={(event)=>setForm({...form,description:event.target.value})}/></label>
          <div className="wide editor-cover-field"><div className="editor-cover-preview">{form.image?<img src={form.image} alt="Текущая обложка"/>:<span>Основного фото нет</span>}</div><div><strong>{form.image?"Основное фото выбрано":"Статус: без фото"}</strong><small>Загрузить или выбрать обычное фото можно во вкладке «Фотографии».</small>{plant&&<button type="button" className="ai-section-button cover-generate" disabled={Boolean(aiBusy)||Boolean(aiFieldBusy)||aiCoverBusy} onClick={generateCover}>{aiCoverBusy?"✦ Создаём preview…":"✦ Предложить AI-обложку"}</button>}</div></div>
        </div></section>}
        {section==="attributes"&&<section className="editor-section"><header><div><p>Каталог</p><h3>Категория и характеристики</h3></div>{sectionAI("attributes","Сгенерировать раздел")}</header><div className="admin-form-grid product-form">
          <div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={form.categoryId} onChange={(categoryId)=>void changeCategory(categoryId)}/></div>
          <AttributeFields schema={schema.filter((item)=>item.scope==="product"&&item.audience==="customer")} values={form.attributes||{}} onChange={(code,value)=>setForm((current)=>({...current,attributes:{...current.attributes,[code]:value as never}}))} onGenerate={(code)=>generateAI("attributes",code)} busy={aiFieldBusy}/>
          {schema.filter((item)=>item.scope==="product"&&item.audience==="customer").length===0&&<p className="wide editor-empty">Выберите категорию — здесь появятся только подходящие ей характеристики.</p>}
        </div></section>}
        {section==="care"&&<section className="editor-section"><header><div><p>Контент</p><h3>Уход, паспорт и FAQ</h3></div>{sectionAI("care","Сгенерировать раздел")}</header><div className="care-editor-preview"><div><p>Так инструкция выглядит для покупателя</p><h4>{form.name||"Название растения"}</h4><span>{form.careInstructions||"Сгенерируйте или напишите персональное введение по уходу."}</span></div>{[["light.webp","Свет",form.passport?.lighting],["watering.webp","Полив",form.passport?.watering],["humidity.webp","Влажность",form.passport?.humidity],["repotting.webp","Пересадка",form.passport?.repotting]].map(([image,title,text])=><article key={image}><img src={`/images/care/${image}`} alt=""/><div><strong>{title}</strong><span>{text||"Добавьте рекомендацию"}</span></div></article>)}</div><div className="admin-form-grid product-form passport-admin">
          <label className="wide"><span>Введение в инструкцию по уходу {aiField("care","careInstructions")}</span><textarea rows={6} value={form.careInstructions} onChange={(event)=>setForm({...form,careInstructions:event.target.value})}/></label>
          {passportFields.map(([key,label])=><label key={key} className={key==="problems"?"wide":""}><span>{label} {aiField("care",key)}</span><textarea rows={key==="problems"?4:2} value={form.passport?.[key]||""} onChange={(event)=>setForm({...form,passport:{...form.passport,[key]:event.target.value}})}/></label>)}
          <label className="wide"><span>FAQ {aiField("care","faq")}</span><small>Одна строка: вопрос | ответ</small><textarea rows={7} value={(form.passport?.faq||[]).map((item)=>`${item.question} | ${item.answer}`).join("\n")} onChange={(event)=>setForm({...form,passport:{...form.passport,faq:event.target.value.split("\n").map((line)=>line.split("|").map((part)=>part.trim())).filter((parts)=>parts.length>1&&parts[0]&&parts[1]).map(([question,answer])=>({question,answer}))}})}/></label>
          <label className="wide"><span>Важные предупреждения {aiField("care","warnings")}</span><small>До четырёх, каждое с новой строки</small><textarea rows={4} value={(form.importantWarnings||[]).join("\n")} onChange={(event)=>setForm({...form,importantWarnings:event.target.value.split("\n").map((item)=>item.trim()).filter(Boolean).slice(0,4)})}/></label>
        </div></section>}
        {section==="variants"&&<section className="editor-section"><header><div><p>Продажа</p><h3>Размеры и SKU</h3></div><span>Цена, остаток и габариты каждого варианта</span></header><VariantsEditor productId={product.id} categoryId={form.categoryId} onError={onError}/></section>}
        {section==="photos"&&<section className="editor-section"><header><div><p>Медиа</p><h3>Фотографии товара</h3></div><span>Общая галерея PRODUCT; фото SKU остаются внутри варианта</span></header><ProductMediaManager productId={product.id} onError={onError}/></section>}
        {section==="sync"&&<section className="editor-section"><header><div><p>Управление</p><h3>Публикация и синхронизация</h3></div><span>Что показываем и что разрешаем менять СБИС</span></header><div className="admin-form-grid product-form">
          <label>Статус<select value={form.status} onChange={(event)=>setForm({...form,status:event.target.value})}><option value="draft">Черновик</option><option value="published">Опубликован</option><option value="archived">Архив</option></select></label>
          <label className="admin-checkbox editor-featured"><input type="checkbox" checked={form.featured} onChange={(event)=>setForm({...form,featured:event.target.checked})}/>Поднимать в начало каталога</label>
          {form.sabyId&&<div className="wide admin-field"><span className="admin-field-label">Автоматически брать из СБИС</span><p className="editor-field-note">Отмеченные поля СБИС сможет перезаписывать. Контент AI и ручные правки лучше не отмечать.</p><div className="sync-options">{Object.entries(sabyFieldLabels).map(([field,label])=><label key={field}><input type="checkbox" checked={form.sabyFields.includes(field)} onChange={(event)=>setForm({...form,sabyFields:event.target.checked?[...form.sabyFields,field]:form.sabyFields.filter((item)=>item!==field)})}/><span><strong>{label}</strong></span></label>)}</div></div>}
        </div></section>}
      </main>
    </div>
    <footer className="product-editor-footer"><span className={saveError?"editor-save-error":""}>{saveError||saveNotice||"Изменения не попадут на сайт, пока вы не нажмёте «Сохранить»."}</span><div className="dialog-actions"><button disabled={saving} onClick={close}>Закрыть</button><button disabled={saving||!dirty} onClick={()=>void save(false)}>{saving?"Сохраняем…":"Сохранить"}</button><button className="primary" disabled={saving||!dirty||!hasNext} onClick={()=>void save(true)}>Сохранить и открыть следующий незаполненный</button></div></footer>
    {aiPreview&&createPortal(<><button className="admin-dialog-backdrop confirm-backdrop" aria-label="Отменить предложения AI" onClick={()=>setAIPreview(null)}/><section className="admin-dialog ai-diff-dialog" role="dialog" aria-modal="true"><header><h2>Предложения AI</h2><button onClick={()=>setAIPreview(null)} aria-label="Закрыть">×</button></header><p>По умолчанию выбраны только пустые поля. Непустые поля требуют отдельного выбора и подтверждения.</p><div className="ai-diff-list">{aiPreview.candidates.map((item)=><label key={item.path} className={item.conflict?"conflict":""}><input type="checkbox" checked={aiPreview.selected.has(item.path)} onChange={(event)=>setAIPreview((current)=>{if(!current)return current;const selected=new Set(current.selected);if(event.target.checked)selected.add(item.path);else selected.delete(item.path);return {...current,selected};})}/><span><strong>{item.label}</strong><small>Сейчас: {displayValue(item.current)}</small><small>AI: {displayValue(item.proposed)}</small>{item.conflict&&<em>Замена заполненного поля</em>}</span></label>)}</div><div className="dialog-actions"><button onClick={()=>setAIPreview(null)}>Отмена</button><button className="primary" disabled={aiPreview.selected.size===0} onClick={applyPreview}>Применить выбранное в форму</button></div></section></>,document.body)}
  </Dialog>;
}

// Карточка, заведённая здесь, с СБИС не связана вовсе: ни цена, ни остаток
// оттуда не придут, пока товар не импортируют по коду.
export function NewProductDialog({ onClose, onCreated, onError }: { onClose: () => void; onCreated: () => void; onError: (value: string) => void }) {
  const [form, setForm] = useState({ name: "", latinName: "", shortDescription: "", description: "", image: "", price: 0, stock: 0, catalogSection: "plants", heightCm: "", potDiameterCm: "", packageLengthCm: "", packageWidthCm: "", packageHeightCm: "", packageWeightGrams: "" });
  const [categoryId, setCategoryId] = useState<number | undefined>(undefined);
  const [categories, setCategories] = useState<Category[]>([]);
  const [schema, setSchema] = useState<CategoryAttribute[]>([]);
  const [attributes, setAttributes] = useState<Record<string, string | number | boolean | string[] | null>>({});
  const [saving, setSaving] = useState(false);
  useEffect(() => { api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setCategories(data.categories)).catch((error) => onError(error.message)); }, [onError]);
  useEffect(() => { let active = true; if (categoryId) api<{ attributes: CategoryAttribute[] }>(`/api/v1/admin/categories/${categoryId}/attributes`).then((data) => { if (active) setSchema(data.attributes || []); }).catch((error) => onError(error.message)); return () => { active = false; }; }, [categoryId, onError]);
  const plant = isPlantProduct({ categoryId, catalogSection: form.catalogSection }, categories);
  const save = async () => {
    const missing = missingRequired(schema, attributes);
    if (missing.length) { onError(`Заполните обязательные характеристики: ${missing.join(", ")}`); return; }
    setSaving(true);
    try {
      await api("/api/v1/admin/products", { method: "POST", body: JSON.stringify({
        name: form.name, latinName: plant ? form.latinName : "", shortDescription: form.shortDescription,
        description: form.description, image: form.image, catalogSection: form.catalogSection,
        categoryId, priceMinor: Math.round(form.price * 100), stock: form.stock,
        heightCm: form.heightCm === "" ? null : Number(form.heightCm), potDiameterCm: form.potDiameterCm === "" ? null : Number(form.potDiameterCm),
        packageLengthCm: form.packageLengthCm === "" ? null : Number(form.packageLengthCm), packageWidthCm: form.packageWidthCm === "" ? null : Number(form.packageWidthCm),
        packageHeightCm: form.packageHeightCm === "" ? null : Number(form.packageHeightCm), packageWeightGrams: form.packageWeightGrams === "" ? null : Number(form.packageWeightGrams),
        attributes: validAttributes(schema, attributes),
      }) });
      onCreated();
    } catch (error) { onError((error as Error).message); setSaving(false); }
  };
  return <Dialog title="Новый товар" onClose={onClose}><div className="admin-form-grid product-form">
    <h3 className="product-form-heading wide">Карточка товара</h3>
    <label className="wide">Название<input autoFocus value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="Фикус Бенджамина" /></label>
    {plant&&<label>Латинское название<input value={form.latinName} onChange={(event) => setForm({ ...form, latinName: event.target.value })} /></label>}
    <label className="wide">Короткое описание<textarea rows={2} value={form.shortDescription} onChange={(event) => setForm({ ...form, shortDescription: event.target.value })} /></label>
    <label className="wide">Описание<textarea rows={5} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
    <label className="wide">URL фотографии<input value={form.image} onChange={(event) => setForm({ ...form, image: event.target.value })} /></label>
    <div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={categoryId} onChange={(next) => { setSchema([]); setCategoryId(next);let category=categories.find((item)=>item.id===next);while(category?.parentId)category=categories.find((item)=>item.id===category?.parentId);if(category)setForm((current)=>({...current,catalogSection:category!.slug})); }} /></div>
    <h3 className="product-form-heading wide">Клиентские характеристики</h3>
    <AttributeFields schema={schema.filter((item) => item.audience === "customer")} values={attributes} onChange={(code, value) => setAttributes((current) => ({ ...current, [code]: value }))} />
    <h3 className="product-form-heading wide">Продажа</h3>
    <label>Цена, ₽<input type="number" min="0" value={form.price} onChange={(event) => setForm({ ...form, price: Number(event.target.value) })} /></label>
    <label>Остаток, шт.<input type="number" min="0" value={form.stock} onChange={(event) => setForm({ ...form, stock: Number(event.target.value) })} /></label>
    <h3 className="product-form-heading wide">Упаковка и технические характеристики</h3>
    <AttributeFields schema={schema.filter((item) => item.audience === "technical")} values={attributes} onChange={(code, value) => setAttributes((current) => ({ ...current, [code]: value }))} />
  </div><p className="admin-hint">Товар появится на витрине сразу. Остатком такого товара распоряжаетесь вы: СБИС о нём ничего не знает.</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={saving || form.name.trim() === ""} onClick={save}>Создать</button></div></Dialog>;
}

// Импорт ищет по справочнику, который приносит обмен, а не ходит в СБИС в
// момент нажатия: список из сотни кодов разбирается мгновенно. Обратная
// сторона — товар, заведённый в СБИС пять минут назад, приедет со следующим
// обменом.
export function ImportDialog({ onClose, onImported, onError }: { onClose: () => void; onImported: () => void; onError: (value: string) => void }) {
  const [codes, setCodes] = useState("");
  const [categoryId, setCategoryId] = useState<number | undefined>(undefined);
  const [categories, setCategories] = useState<Category[]>([]);
  const [preview, setPreview] = useState<ImportEntry[] | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => { api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setCategories(data.categories)).catch((error) => onError(error.message)); }, [onError]);
  const send = async (dryRun: boolean) => {
    setBusy(true);
    try {
      const result = await api<{ created: number; entries: ImportEntry[] }>("/api/v1/admin/products/import", {
        method: "POST", body: JSON.stringify({ codes: [codes], categoryId, dryRun }),
      });
      if (dryRun) { setPreview(result.entries); } else { onImported(); return; }
    } catch (error) { onError((error as Error).message); }
    setBusy(false);
  };
  const found = preview ? preview.filter((entry) => entry.status === "new").length : 0;
  const labels: Record<string, string> = { new: "Черновик", exists: "Уже есть", missing: "Не найден" };
  return <Dialog title="Импорт товаров из СБИС" onClose={onClose}>
    <label className="wide">Коды товаров<textarea rows={6} value={codes} onChange={(event) => { setCodes(event.target.value); setPreview(null); }} placeholder="X1150532&#10;X1150533" /></label>
    <p className="admin-hint">Вставьте коды из СБИС — через запятую, пробел или с новой строки. Подойдёт также артикул или штрихкод. Новые позиции создаются черновиками и не попадут на витрину, пока вы не приведёте название и варианты в порядок.</p>
    <div className="admin-field"><span className="admin-field-label">Раздел каталога</span><CategoryPicker categories={categories} value={categoryId} onChange={setCategoryId} /></div>
    {preview && <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Код</th><th>Товар</th><th>Цена</th><th>Остаток</th><th>Что будет</th></tr></thead><tbody>{preview.map((entry) => <tr key={entry.code}><td>{entry.code}</td><td>{entry.name || "—"}</td><td>{entry.name ? money.format(entry.price) : "—"}</td><td>{entry.name ? entry.stock : "—"}</td><td><span className={"admin-pill " + entry.status}>{labels[entry.status] || entry.status}</span></td></tr>)}</tbody></table></div>}
    {preview && found === 0 && <p className="admin-hint">Заводить нечего: ни одного нового товара в списке нет.</p>}
    <div className="dialog-actions"><button onClick={onClose}>Отмена</button><button disabled={busy || codes.trim() === ""} onClick={() => send(true)}>Проверить</button><button className="primary" disabled={busy || !preview || found === 0} onClick={() => send(false)}>Создать черновики {found > 0 ? found : ""}</button></div>
  </Dialog>;
}

export function MergeProductsDialog({ products, onClose, onMerged, onError }: { products: Product[]; onClose: () => void; onMerged: () => void; onError: (value: string) => void }) {
  const [targetId, setTargetId] = useState(products[0]?.id || 0);
  const [busy, setBusy] = useState(false);
  const target = products.find((item) => item.id === targetId);
  const sources = products.filter((item) => item.id !== targetId);
  const merge = async () => { setBusy(true); try { await api("/api/v1/admin/products/merge", { method: "POST", body: JSON.stringify({ targetProductId: targetId, sourceProductIds: sources.map((item) => item.id) }) }); onMerged(); } catch (error) { onError((error as Error).message); setBusy(false); } };
  return <Dialog title="Объединить размеры в одну карточку" onClose={onClose}><p>Выберите PRODUCT — его название, категория и описание останутся основными. SKU, остатки, цены, фотографии и связи СБИС остальных черновиков переедут в него.</p><label className="wide">Основная карточка<select value={targetId} onChange={(event) => setTargetId(Number(event.target.value))}>{products.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.sabyCode || item.sku}</option>)}</select></label><div className="admin-merge-preview"><strong>Будет одна карточка: {target?.name}</strong><span>Вариантов после объединения: не менее {products.length}</span><small>Поглощаемые черновики: {sources.map((item) => item.name).join(", ")}</small></div><p className="admin-hint">Объединяются только черновики без заказов и отзывов. Все SKU и коды СБИС сохраняются.</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={busy || sources.length === 0} onClick={merge}>Объединить {products.length} карточки</button></div></Dialog>;
}

export function SyncDialog({ count, onClose, onSync }: { count: number; onClose: () => void; onSync: (fields: string[]) => void }) {
  const options = [{ id: "name", label: "Название" }, { id: "photo", label: "Фото" }, { id: "price", label: "Цена" }, { id: "description", label: "Описание" }];
  const [fields, setFields] = useState(["price"]);
  return <Dialog title={`Подтянуть из СБИС: ${count} ${count === 1 ? "товар" : "товаров"}`} onClose={onClose}><p>Выбранные поля заменятся тем, что лежит в справочнике СБИС. Это разовое действие: дальше товар снова ведёте вы.</p><div className="sync-options">{options.map((option) => <label key={option.id}><input type="checkbox" checked={fields.includes(option.id)} onChange={(event) => setFields((current) => event.target.checked ? [...current, option.id] : current.filter((field) => field !== option.id))} /><span><strong>{option.label}</strong></span></label>)}</div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={fields.length === 0} onClick={() => onSync(fields)}>Синхронизировать</button></div></Dialog>;
}
