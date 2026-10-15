import { useEffect, useMemo, useRef, useState } from "react";
import { CategoryPicker } from "./AdminCatalog";
import { Dialog, api, money } from "./adminShared";
import { channelLabels, mappingLabels, isPlantProduct, valueMissing } from "./adminCatalogLogic";
import { VariantsEditor, type AdminVariant } from "./AdminPim";
import { ProductMediaManager } from "./ProductMediaManager";
import type { Category, CategoryAttribute, ImportEntry, Product } from "./adminTypes";

export function AttributeFields({ schema, values, onChange }: {
  schema: CategoryAttribute[]; values: Record<string, unknown>;
  onChange: (code: string, value: string | number | boolean | string[] | null) => void;
}) {
  const field = (attribute: CategoryAttribute) => {
    const value = values[attribute.code];
    const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
    if (attribute.dataType === "boolean") return <label className={`attribute-toggle ${value === true ? "selected" : ""}`} key={attribute.code}><input type="checkbox" checked={value === true} onChange={(event) => onChange(attribute.code, event.target.checked)} /><span>{title}</span></label>;
    if (attribute.dataType === "enum") { const current = String(value ?? ""); const unknown = current && !attribute.options.includes(current); return <label className="attribute-control" key={attribute.code}><span>{title}</span><select aria-label={title} required={attribute.required} value={current} onChange={(event) => onChange(attribute.code, event.target.value || null)}><option value="">Не указано</option>{unknown && <option value={current}>{`Неизвестное значение: ${current}`}</option>}{attribute.options.map((option) => <option key={option} value={option}>{attribute.optionLabels?.[option] || option}</option>)}</select>{unknown && <small className="admin-inline-error">Старое значение сохранено. Выберите вариант из матрицы, чтобы заменить его.</small>}</label>; }
    if (attribute.dataType === "multi_enum") {
      const selected = Array.isArray(value) ? value.map(String) : [];
      const unknown = selected.filter((option) => !attribute.options.includes(option));
      return <fieldset className="attribute-multi" key={attribute.code}><legend>{title}</legend><div className="attribute-choice-list">{attribute.options.map((option) => <label className={selected.includes(option) ? "selected" : ""} key={option}><input type="checkbox" checked={selected.includes(option)} onChange={(event) => onChange(attribute.code, event.target.checked ? [...selected, option] : selected.filter((item) => item !== option))} />{attribute.optionLabels?.[option] || option}</label>)}</div>{unknown.length > 0 && <small className="admin-inline-error">Неизвестные старые значения сохранены: {unknown.join(", ")}</small>}</fieldset>;
    }
    return <label className="attribute-control" key={attribute.code}><span>{title}</span><input aria-label={title} type={attribute.dataType === "number" ? "number" : "text"} min={attribute.dataType === "number" ? 0 : undefined} required={attribute.required} value={value == null ? "" : String(value)} onChange={(event) => onChange(attribute.code, event.target.value === "" ? null : attribute.dataType === "number" ? Number(event.target.value) : event.target.value)} /></label>;
  };
  const group = (attribute: CategoryAttribute) => {
    if (/^(package_|shipping_)/.test(attribute.code)) return "Упаковка и логистика";
    if (/(height|width|length|diameter|pot|size)/.test(attribute.code)) return "Размеры растения";
    if (/(light|water|care|humidity|temperature|soil|pet|toxic|growth)/.test(attribute.code)) return "Уход";
    return "Характеристики";
  };
  const groups = ["Уход", "Размеры растения", "Упаковка и логистика", "Характеристики"].map((title) => ({ title, items: schema.filter((item) => group(item) === title) })).filter((item) => item.items.length > 0);
  return <div className="attribute-groups wide">
    {groups.map(({ title, items }) => <div className="attribute-group" key={title}><h4>{title}</h4><div className="attribute-control-grid">{items.map(field)}</div></div>)}
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

export type ProductEditorSection = "main" | "attributes" | "care" | "variants" | "relationships" | "photos";
type ProductRelationships = { mappings:Array<{variantId:number;sku:string;provider:string;type:string;externalId:string;status:string}>;suppliers:Array<{id:number;name:string;article:string;availability:string}>;sales:Array<{channel:string;units:number;gross:number}> };

function ProductRelationshipsPanel({ productId, owner, onEdit, onError }: { productId:number;owner:boolean;onEdit:()=>void;onError:(value:string)=>void }) {
  const [data,setData]=useState<ProductRelationships|null>(null);
  useEffect(()=>{let active=true;api<{relationships:ProductRelationships}>(`/api/v1/admin/products/${productId}/relationships`).then((result)=>{if(active)setData(result.relationships)}).catch((error)=>onError(error.message));return()=>{active=false};},[productId,onError]);
  if(!data)return <p className="editor-empty">Загружаем связи…</p>;
  const activeMappings=data.mappings.filter((item)=>item.status==="active"),legacyMappings=data.mappings.filter((item)=>item.status!=="active");
  const groups=Object.entries(activeMappings.filter(item=>item.provider!=="ficusin").reduce<Record<string,typeof activeMappings>>((groups,item)=>{const key=`${item.provider}:${item.variantId}`;(groups[key] ||= []).push(item);return groups},{}));
  return <div className="product-relationships">
    <div className="relationship-summary"><article><small>Связи размеров с системами</small><strong>{groups.length}</strong></article><article><small>Поставщики</small><strong>{data.suppliers.length}</strong></article><article><small>Продажи за 60 дней</small><strong>{data.sales.reduce((sum,item)=>sum+item.units,0)}</strong></article></div>
    <section><header><div><h4>СБИС и каналы продаж</h4><p>Каждая строка привязана к конкретному SKU. Отсутствующий необязательный канал не считается ошибкой.</p></div>{owner&&<button type="button" className="admin-action" onClick={onEdit}>Изменить связи SKU</button>}</header>{groups.length?<div className="relationship-list">{groups.map(([key,items])=><article key={key}><span className="channel-mark">{channelLabels[items![0].provider]||"Другая система"}</span><div><strong>Артикул {items![0].sku}</strong><span>Связь подтверждена в справочнике</span><small>Публикация и доставка цены здесь не проверяются.</small><details><summary>Идентификаторы ({items!.length})</summary>{items!.map(item=><p key={`${item.type}:${item.externalId}`}>{mappingLabels[item.type]||"Идентификатор"}: <code>{item.externalId}</code></p>)}</details></div></article>)}</div>:<p className="admin-hint">Внешних связей пока нет.</p>}{legacyMappings.length>0&&<details><summary>История прежних связей ({legacyMappings.length})</summary><div className="relationship-list legacy">{legacyMappings.map((item)=><article key={`${item.variantId}:${item.provider}:${item.type}:${item.externalId}`}><span className="channel-mark">{item.provider}</span><div><strong>{item.externalId}</strong><small>SKU {item.sku} · сохранено для истории</small></div></article>)}</div></details>}</section>
    <section><header><div><h4>Поставщики</h4><p>У одного товара может быть несколько поставщиков и отдельные предложения для разных горшков.</p></div></header>{data.suppliers.length?<div className="relationship-list">{data.suppliers.map((item)=><article key={`${item.id}:${item.article}`}><span className="channel-mark supplier">П</span><div><strong>{item.name}</strong><small>{item.article||"Артикул поставщика не указан"}</small></div><b>{({unknown:"Наличие не проверено",available:"Есть у поставщика",unavailable:"Нет у поставщика"} as Record<string,string>)[item.availability]||"Требует проверки"}</b></article>)}</div>:<p className="admin-hint">Связанные предложения поставщиков ещё не подтверждены.</p>}</section>
    <section><header><div><h4>Продажи по каналам</h4><p>Сводка за последние 60 дней; непривязанные продажи остаются в очереди сверки.</p></div></header>{data.sales.length?<div className="relationship-sales">{data.sales.map((item)=><article key={item.channel}><b>{channelLabels[item.channel]||item.channel}</b><strong>{item.units} шт.</strong><span>{money.format(item.gross)}</span></article>)}</div>:<p className="admin-hint">За период связанных продаж нет.</p>}</section>
  </div>;
}
const passportFields = [
  ["origin","Происхождение"],["lighting","Освещение"],["watering","Полив"],["humidity","Влажность"],
  ["temperature","Температура"],["soil","Грунт"],["fertilizer","Удобрение"],["repotting","Пересадка"],
  ["careDifficulty","Сложность ухода"],["growthRate","Скорость роста"],["matureSize","Взрослый размер"],
  ["toxicity","Токсичность"],["problems","Типичные проблемы и решения"],["pests","Вредители"],
] as const;

function ProductCollections({productId,onError}:{productId:number;onError:(value:string)=>void}) {
 const [collections,setCollections]=useState<Array<{id:number;title:string;mode:string;active:boolean;products:number[]}>>([]);
 useEffect(()=>{let active=true;api<{collections:typeof collections}>("/api/v1/admin/collection-definitions").then(data=>{if(active)setCollections(data.collections||[])}).catch(error=>onError(error.message));return()=>{active=false}},[productId,onError]);
 const included=collections.filter(x=>x.products.includes(productId));
 return <section className="catalog-memberships"><h4>Входит в подборки</h4><p className="admin-hint">Подтверждённый состав сохранённой карточки. Подборки по правилам пересчитываются после сохранения характеристик.</p>{included.length?<ul>{included.map(x=><li key={x.id}><a href={`/admin?section=collections&collection=${x.id}`}>{x.title}</a><span>{x.mode==="dynamic"?"По характеристикам":"Вручную"} · {x.active?"На витрине":"Скрыта"}</span></li>)}</ul>:<p>Пока ни в одной подборке.</p>}</section>;
}

export function ProductDialog({owner=false,product,onClose,onSaved,onError,hasNext=false,initialSection="main",onSectionChange,onDirtyChange}: {
 owner?:boolean;product:Product;onClose:()=>void;onSaved:(value:Product,openNext:boolean)=>void;onError:(value:string)=>void;hasNext?:boolean;initialSection?:ProductEditorSection;onSectionChange?:(section:ProductEditorSection)=>void;onDirtyChange?:(dirty:boolean)=>void;
}) {
 const [form,setForm]=useState(product);
 const [section,setSection]=useState<ProductEditorSection>(initialSection);
 const [categories,setCategories]=useState<Category[]>([]);
 const [schema,setSchema]=useState<CategoryAttribute[]>([]);
 const [variants,setVariants]=useState<AdminVariant[]>([]);
 const originalVariants=useRef<AdminVariant[]>([]);
 const [loaded,setLoaded]=useState(false);
 const [loadError,setLoadError]=useState("");
 const [saving,setSaving]=useState(false);
 const [saveError,setSaveError]=useState("");
 const [saveNotice,setSaveNotice]=useState("");
 const [faqText,setFaqText]=useState((product.passport?.faq||[]).map(x=>`${x.question} | ${x.answer}`).join("\n"));
 const [warningText,setWarningText]=useState((product.importantWarnings||[]).join("\n"));
 const formRef=useRef<HTMLFormElement>(null);
 const loadVariants=async()=>{const data=await api<{variants:AdminVariant[]}>(`/api/v1/admin/products/${product.id}/variants`);originalVariants.current=structuredClone(data.variants||[]);setVariants(data.variants||[]);setLoaded(true)};
 useEffect(()=>{let active=true;Promise.all([api<{categories:Category[]}>("/api/v1/admin/categories"),api<{variants:AdminVariant[]}>(`/api/v1/admin/products/${product.id}/variants`)]).then(([cats,rows])=>{if(active){setCategories(cats.categories||[]);originalVariants.current=structuredClone(rows.variants||[]);setVariants(rows.variants||[]);setLoaded(true)}}).catch(error=>{if(active)setLoadError(error.message)});return()=>{active=false}},[product.id]);
 useEffect(()=>{let active=true;if(!form.categoryId){setSchema([]);return()=>{active=false}}api<{attributes:CategoryAttribute[]}>(`/api/v1/admin/categories/${form.categoryId}/attributes`).then(data=>{if(active)setSchema(data.attributes||[])}).catch(error=>{if(active)setLoadError(error.message)});return()=>{active=false}},[form.categoryId]);
 const plant=isPlantProduct(form,categories);
 const dirty=useMemo(()=>JSON.stringify(form)!==JSON.stringify(product)||JSON.stringify(variants)!==JSON.stringify(originalVariants.current)||faqText!==(product.passport?.faq||[]).map(x=>`${x.question} | ${x.answer}`).join("\n")||warningText!==(product.importantWarnings||[]).join("\n"),[form,product,variants,faqText,warningText]);
 useEffect(()=>{const protect=(event:BeforeUnloadEvent)=>{if(dirty){event.preventDefault();event.returnValue=""}};window.addEventListener("beforeunload",protect);return()=>window.removeEventListener("beforeunload",protect)},[dirty]);
 useEffect(()=>{onDirtyChange?.(dirty);return()=>onDirtyChange?.(false)},[dirty,onDirtyChange]);
 const close=()=>{if(saving)return;if(!dirty||window.confirm("Закрыть карточку и отказаться от несохранённых правок товара и размеров?"))onClose()};
 const changeSection=(next:ProductEditorSection)=>{setSection(next);onSectionChange?.(next)};
 const fail=(message:string,next:ProductEditorSection,field?:string)=>{setSaveError(message);changeSection(next);requestAnimationFrame(()=>{const controls=Array.from(formRef.current?.querySelectorAll<HTMLElement>("input,textarea,select")||[]);(controls.find(x=>x.getAttribute("aria-label")===field)||controls.find(x=>x.getClientRects().length))?.focus()})};
 const save=async(openNext=false)=>{
  if(saving||!loaded)return;
  if(!form.name.trim()){fail("Укажите название товара","main","Название");return}
  const customerSchema=schema.filter(x=>x.scope==="product"&&x.audience==="customer");
  const missing=missingRequired(customerSchema,form.attributes||{});
  if(form.status==="published"&&missing.length){fail(`Заполните: ${missing.join(", ")}`,"attributes",`${missing[0]} *`);return}
  if(form.status==="published"&&!form.categoryId){fail("Выберите категорию товара","attributes");return}
  const faq=faqText.split("\n").filter(x=>x.trim()).map(line=>{const split=line.indexOf("|");return{question:line.slice(0,split).trim(),answer:line.slice(split+1).trim(),valid:split>0&&!!line.slice(split+1).trim()}});
  if(plant&&faq.some(x=>!x.valid)){fail("В каждой строке вопросов укажите: вопрос | ответ","care","Вопросы и ответы");return}
  const warnings=warningText.split("\n").map(x=>x.trim()).filter(Boolean);
  if(warnings.length>4){fail("Оставьте не более четырёх важных предупреждений","care","Важные предупреждения");return}
  const edits=[];
  for(const variant of variants){
   const original=originalVariants.current.find(x=>x.id===variant.id);
   if(JSON.stringify(variant)===JSON.stringify(original))continue;
   if(!variant.label.trim()||!Number.isFinite(variant.price)||variant.price<0||(!Number.isInteger(variant.wholesaleMinQty)||variant.wholesaleMinQty<1)){fail(`Проверьте название, цену и минимум размера «${variant.label}»`,"variants");return}
   const attributes:Record<string,unknown>={};
   for(const code of new Set([...Object.keys(original?.attributes||{}),...Object.keys(variant.attributes)])){
    const value=variant.attributes[code];if(JSON.stringify(value)===JSON.stringify(original?.attributes[code]))continue;
    if(/^package_/.test(code)&&value!=null&&value!==""&&(!Number.isFinite(Number(value))||Number(value)<=0)){fail(`ОВХ размера «${variant.label}» должны быть больше нуля`,"variants");return}
    attributes[code]=valueMissing(value)?null:value;
   }
   edits.push(owner?{id:variant.id,label:variant.label,priceMinor:Math.round(variant.price*100),wholesaleMinQty:variant.wholesaleMinQty,active:variant.active,archived:variant.archived,attributes,externalIds:variant.externalIds}:{id:variant.id,label:variant.label,attributes});
  }
  setSaving(true);setSaveError("");setSaveNotice("");
  try{
   const body:Record<string,unknown>={};
   const copy=(key:keyof Product)=>{if(JSON.stringify(form[key])!==JSON.stringify(product[key]))body[key]=form[key]};
   (["name","description","status","featured","catalogSection","categoryId"] as Array<keyof Product>).forEach(copy);
   if(product.categoryId&&!form.categoryId){delete body.categoryId;body.clearCategory=true}
   if(plant){(["latinName","careInstructions"] as Array<keyof Product>).forEach(copy);const passport={...form.passport,faq:faq.map(({question,answer})=>({question,answer}))};if(JSON.stringify(passport)!==JSON.stringify(product.passport))body.passport=passport;if(JSON.stringify(warnings)!==JSON.stringify(product.importantWarnings))body.importantWarnings=warnings}
   const attributes=changedAttributes(customerSchema,form.attributes||{},product.attributes||{});if(Object.keys(attributes).length)body.attributes=attributes;
   if(edits.length)body.variants=edits;
   const result=await api<{product:Product}>(`/api/v1/admin/products/${product.id}`,{method:"PATCH",body:JSON.stringify(body)});
   setForm(result.product);setFaqText((result.product.passport?.faq||[]).map(x=>`${x.question} | ${x.answer}`).join("\n"));setWarningText((result.product.importantWarnings||[]).join("\n"));setSaveNotice("Товар и размеры сохранены");onSaved(result.product,openNext);
   if(!openNext)await loadVariants();
  }catch(error){setSaveError((error as Error).message);onError((error as Error).message)}finally{setSaving(false)}
 };
 const sections:Array<{id:ProductEditorSection;label:string}>=[{id:"main",label:"Основное"},{id:"attributes",label:"Характеристики"},{id:"variants",label:"Размеры и цены"},{id:"photos",label:"Фотографии"},...(plant?[{id:"care" as ProductEditorSection,label:"Уход и FAQ"}]:[]),{id:"relationships",label:"Связи и продажи"}];
 const changeCategory=async(categoryId?:number)=>{
  if(categoryId===form.categoryId)return;
  try{const next=categoryId?(await api<{attributes:CategoryAttribute[]}>(`/api/v1/admin/categories/${categoryId}/attributes`)).attributes||[]:[];
   const hidden=schema.filter(x=>!next.some(n=>n.code===x.code)&&!valueMissing(form.attributes?.[x.code])).map(x=>x.name);
   if(hidden.length&&!window.confirm(`В новой категории не отображаются: ${hidden.join(", ")}. Сохранённые значения останутся в истории. Продолжить?`))return;
   let root=categories.find(x=>x.id===categoryId);const seen=new Set<number>();while(root?.parentId&&!seen.has(root.id)){seen.add(root.id);root=categories.find(x=>x.id===root?.parentId)}
   setSchema(next);setForm({...form,categoryId,catalogSection:root?.slug||form.catalogSection});
  }catch(error){onError((error as Error).message)}
 };
 return <Dialog title={form.name||"Новый товар"} onClose={close} className="product-editor-dialog">
  <div className="catalog-editor-topline"><span>Артикул {product.sku} · {product.status==="published"?"Опубликован":product.status==="archived"?"Архив":"Черновик"}</span><a href={`/product/${product.slug}`} target="_blank" rel="noreferrer">На сайте ↗</a></div>
  {loadError&&<div className="admin-inline-error" role="alert">{loadError}<button type="button" onClick={()=>{setLoadError("");void api<{categories:Category[]}>("/api/v1/admin/categories").then(data=>setCategories(data.categories||[])).catch(error=>setLoadError(error.message));if(!loaded)void loadVariants().catch(error=>setLoadError(error.message));if(form.categoryId)void api<{attributes:CategoryAttribute[]}>(`/api/v1/admin/categories/${form.categoryId}/attributes`).then(data=>setSchema(data.attributes||[])).catch(error=>setLoadError(error.message))}}>Повторить загрузку карточки</button></div>}
  <div className="product-editor-shell"><aside className="product-editor-aside"><div className="product-editor-cover">{form.image?<img src={form.image} alt=""/>:<span>Нет фото</span>}</div><nav className="product-editor-nav" aria-label="Разделы карточки">{sections.map(x=><button type="button" className={section===x.id?"active":""} aria-current={section===x.id?"page":undefined} key={x.id} onClick={()=>changeSection(x.id)}>{x.label}</button>)}</nav><p className="catalog-source-note">Остаток — из СБИС.<br/>Цена, фото и контент — на сайте.</p></aside>
  <form className="product-editor-content" ref={formRef} onSubmit={event=>{event.preventDefault();void save()}}>
   <fieldset className="catalog-editor-fields" disabled={saving}>
   {section==="main"&&<section className="editor-section"><header><div><p>Карточка товара</p><h3>Название, описание и статус</h3></div></header><div className="admin-form-grid product-form">
    <label className="wide">Название<input aria-label="Название" value={form.name} onChange={e=>setForm({...form,name:e.target.value})}/><small>Размер горшка укажите в разделе «Размеры и цены».</small></label>
    {plant&&<label className="wide">Латинское название<input aria-label="Латинское название" value={form.latinName} onChange={e=>setForm({...form,latinName:e.target.value})}/></label>}
    <label className="wide">Описание<textarea aria-label="Описание" rows={8} value={form.description} onChange={e=>setForm({...form,description:e.target.value})}/></label>
    <label>Статус товара<select aria-label="Статус товара" value={form.status} onChange={e=>setForm({...form,status:e.target.value})}><option value="draft">Черновик</option><option value="published">Опубликован</option>{form.status==="archived"&&<option value="archived">Архив</option>}</select><small>Черновик скрыт от покупателей. Публикация проходит проверку обязательных полей.</small></label>
    <div className="catalog-main-photo"><strong>{form.image?"Основное фото выбрано":"Добавьте фото товара"}</strong><button type="button" className="admin-action" onClick={()=>changeSection("photos")}>Открыть фотографии</button></div>
    <details className="wide"><summary>Дополнительные действия</summary><label className="admin-checkbox"><input type="checkbox" checked={form.featured} onChange={e=>setForm({...form,featured:e.target.checked})}/>Выделять товар в каталоге</label>{owner&&<button type="button" className="text-button" onClick={()=>setForm({...form,status:form.status==="archived"?"draft":"archived"})}>{form.status==="archived"?"Вернуть в черновики":"Переместить в архив после сохранения"}</button>}</details>
   </div></section>}
   {section==="attributes"&&<section className="editor-section"><header><div><p>Каталог</p><h3>Характеристики и подборки</h3></div></header><div className="admin-form-grid product-form"><div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={form.categoryId} onChange={id=>void changeCategory(id)}/></div><AttributeFields schema={schema.filter(x=>x.scope==="product"&&x.audience==="customer")} values={form.attributes||{}} onChange={(code,value)=>setForm(current=>({...current,attributes:{...current.attributes,[code]:value as never}}))}/>{!schema.filter(x=>x.scope==="product"&&x.audience==="customer").length&&<p className="editor-empty wide">Выберите категорию — появятся подходящие характеристики.</p>}</div><ProductCollections productId={product.id} onError={onError}/></section>}
   {section==="care"&&plant&&<section className="editor-section"><header><div><p>Рекомендации для покупателя</p><h3>Уход и вопросы</h3></div></header><div className="admin-form-grid product-form passport-admin"><label className="wide">Введение в уход<textarea aria-label="Введение в уход" rows={4} value={form.careInstructions} onChange={e=>setForm({...form,careInstructions:e.target.value})}/></label>{passportFields.map(([key,label])=><label key={key} className={key==="problems"?"wide":""}>{label}<textarea aria-label={label} rows={key==="problems"?4:3} value={form.passport?.[key]||""} onChange={e=>setForm({...form,passport:{...form.passport,[key]:e.target.value}})}/></label>)}<label className="wide">Вопросы и ответы<small>Каждая строка: вопрос | ответ</small><textarea aria-label="Вопросы и ответы" rows={6} value={faqText} onChange={e=>setFaqText(e.target.value)}/></label><label className="wide">Важные предупреждения<small>До четырёх, каждое с новой строки</small><textarea aria-label="Важные предупреждения" rows={4} value={warningText} onChange={e=>setWarningText(e.target.value)}/></label></div></section>}
   {section==="variants"&&<VariantsEditor owner={owner} productId={product.id} categoryId={form.categoryId} variants={variants} onChange={setVariants} dirty={dirty} onReload={()=>void loadVariants().catch(error=>onError(error.message))} onError={onError}/>}
   {section==="relationships"&&<section className="editor-section"><header><h3>Связи и продажи</h3></header><ProductRelationshipsPanel productId={product.id} owner={owner} onEdit={()=>changeSection("variants")} onError={onError}/></section>}
   {section==="photos"&&<section className="editor-section"><header><h3>Фотографии товара</h3></header><p className="admin-hint">Фото общей карточки. Загрузка, смена основного фото и удаление применяются сразу.</p><ProductMediaManager canDelete={owner} productId={product.id} onChanged={image=>{setForm(current=>({...current,image}));onSaved({...product,image},false);setSaveNotice("Фотографии обновлены на сервере")}} onError={onError}/></section>}
   </fieldset>
  </form></div>
  <footer className="product-editor-footer"><div role={saveError?"alert":"status"} aria-live="polite" className={saveError?"editor-save-error":""}>{saveError||saveNotice||(dirty?"Есть несохранённые правки товара и размеров":"Все изменения сохранены")}</div><div className="dialog-actions"><button type="button" disabled={saving} onClick={close}>Закрыть</button>{hasNext&&<button type="button" className="catalog-save-next" disabled={saving||!loaded||!dirty} onClick={()=>void save(true)}>Сохранить и следующий</button>}<button type="button" className="primary" disabled={saving||!loaded||!dirty} onClick={()=>void save()}>{saving?"Сохраняем…":"Сохранить изменения"}</button></div></footer>
 </Dialog>;
}

// Карточку сначала заводят на сайте; продавать из наличия можно после
// привязки SKU и подтверждённого обновления остатка из СБИС.
export function NewProductDialog({onClose,onCreated,onError}:{onClose:()=>void;onCreated:(product:Product)=>void;onError:(value:string)=>void}){
 const [name,setName]=useState("");const [categoryId,setCategoryId]=useState<number>();const [categories,setCategories]=useState<Category[]>([]);const [saving,setSaving]=useState(false);const [error,setError]=useState("");
 useEffect(()=>{api<{categories:Category[]}>("/api/v1/admin/categories").then(data=>setCategories(data.categories||[])).catch(error=>onError(error.message))},[onError]);
 const create=async()=>{if(!name.trim()||saving)return;setSaving(true);try{let root=categories.find(x=>x.id===categoryId);const seen=new Set<number>();while(root?.parentId&&!seen.has(root.id)){seen.add(root.id);root=categories.find(x=>x.id===root?.parentId)}const data=await api<{product:Product}>("/api/v1/admin/products",{method:"POST",body:JSON.stringify({name:name.trim(),categoryId,catalogSection:root?.slug||"plants",priceMinor:0})});onCreated(data.product)}catch(error){setError((error as Error).message);setSaving(false)}};
 return <Dialog title="Добавить товар" onClose={()=>{if(!saving&&(!name.trim()||window.confirm("Отменить создание товара?")))onClose()}}><form onSubmit={event=>{event.preventDefault();void create()}}><p>Начните с названия и категории. Затем откроется единый редактор: фото, размеры, ОВХ, уход и публикация.</p><div className="admin-form-grid"><label className="wide">Название товара<input autoFocus required aria-label="Название товара" value={name} onChange={e=>setName(e.target.value)} placeholder="Например, Аглаонема Мария"/></label><div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={categoryId} onChange={setCategoryId}/></div></div><p className="admin-hint">Создадим скрытый черновик без остатка. Цена и контент заполняются на сайте, остаток появится после связи со СБИС.</p>{error&&<p role="alert" className="admin-inline-error">{error}</p>}<div className="dialog-actions"><button type="button" disabled={saving} onClick={onClose}>Отмена</button><button type="submit" className="primary" disabled={saving||!name.trim()}>{saving?"Создаём…":"Создать черновик и продолжить"}</button></div></form></Dialog>;
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
  return <Dialog title="Объединить размеры в одну карточку" onClose={onClose}><p>Выберите основную карточку — его название, категория и описание останутся основными. Размеры, остатки, цены, фотографии и связи СБИС остальных черновиков переедут в него.</p><label className="wide">Основная карточка<select value={targetId} onChange={(event) => setTargetId(Number(event.target.value))}>{products.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.sabyCode || item.sku}</option>)}</select></label><div className="admin-merge-preview"><strong>Будет одна карточка: {target?.name}</strong><span>Вариантов после объединения: не менее {products.length}</span><small>Поглощаемые черновики: {sources.map((item) => item.name).join(", ")}</small></div><p className="admin-hint">Объединяются только черновики без заказов и отзывов. Все SKU и коды СБИС сохраняются.</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={busy || sources.length === 0} onClick={merge}>Объединить {products.length} карточки</button></div></Dialog>;
}

type PublishedMergePreview = { ready: boolean; targetProductCode?: string; sourceProductCodes?: string[]; skus: string[]; blockers: string[] };

export function PublishedMergePreviewDialog({ products, onClose, onMerged, onError }: { products: Product[]; onClose: () => void; onMerged: () => void; onError: (value: string) => void }) {
  const [targetId, setTargetId] = useState(products[0]?.id || 0);
  const [expectedSKUs, setExpectedSKUs] = useState("");
  const [mergeSizes,setMergeSizes]=useState<AdminVariant[]>([]);
  const [sizeError,setSizeError]=useState("");
  useEffect(()=>{let active=true;Promise.all(products.map(product=>api<{variants:AdminVariant[]}>(`/api/v1/admin/products/${product.id}/variants`))).then(rows=>{if(active){const variants=rows.flatMap(x=>x.variants||[]).filter(x=>!x.archived);setMergeSizes(variants);setExpectedSKUs(variants.map(x=>x.sku).join(", "))}}).catch(error=>{if(active)setSizeError(error.message)});return()=>{active=false}},[products]);
  const [reviewedIdentity, setReviewedIdentity] = useState(products[0]?.name || "");
  const [preview, setPreview] = useState<PublishedMergePreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const requestBody = () => ({
    targetProductId: targetId,
    sourceProductIds: products.filter((item) => item.id !== targetId).map((item) => item.id),
    expectedSkus: expectedSKUs.split(/[\s,;]+/).map((value) => value.trim()).filter(Boolean),
    reviewedIdentity: reviewedIdentity.trim(),
  });
  const check = async () => {
    setBusy(true); setPreview(null); setConfirmation("");
    try {
      const result = await api<PublishedMergePreview>("/api/v1/admin/products/merge-published/preview", {
        method: "POST",
        body: JSON.stringify(requestBody()),
      });
      setPreview(result);
    } catch (error) { onError((error as Error).message); }
    finally { setBusy(false); }
  };
  const merge = async () => {
    if (!preview?.ready || confirmation !== `ОБЪЕДИНИТЬ ${preview.targetProductCode}`) return;
    setBusy(true);
    try {
      await api("/api/v1/admin/products/merge-published", { method: "POST", body: JSON.stringify(requestBody()) });
      onMerged();
    } catch (error) { setPreview(null); setConfirmation(""); onError((error as Error).message); }
    finally { setBusy(false); }
  };
  return <Dialog title="Проверка и объединение опубликованных карточек" onClose={onClose}>
    <p>Проверка только читает базу. Выберите основную карточку. Все её размеры и размеры исходных карточек загружаются автоматически. После проверки товары останутся без изменений.</p>
    <ul>{products.map((item) => <li key={item.id}>{item.name} · внутренний ID {item.id} · адрес {item.slug} · {item.sku} · {item.latinName || "латинское название не заполнено"}</li>)}</ul>
    <label className="wide">Основная карточка<select value={targetId} onChange={(event) => { setTargetId(Number(event.target.value)); setReviewedIdentity(products.find((item) => item.id === Number(event.target.value))?.name || ""); setPreview(null); }}>{products.map((item) => <option key={item.id} value={item.id}>{item.name} · ID {item.id} · {item.slug}</option>)}</select></label>
    <label className="wide">Подтверждённый вид или сорт<input value={reviewedIdentity} onChange={(event) => { setReviewedIdentity(event.target.value); setPreview(null); }} /></label>
    <section><h3>Размеры, участвующие в проверке</h3>{sizeError?<p className="admin-inline-error" role="alert">{sizeError}</p>:!mergeSizes.length?<p>Загружаем размеры…</p>:<ul>{mergeSizes.map(x=><li key={x.id}>{products.find(p=>p.id===x.productId)?.name} · {x.label} · артикул {x.sku}</li>)}</ul>}</section>
    {preview && <div className="admin-merge-preview"><strong>{preview.ready ? "Предварительные проверки пройдены" : "Объединение заблокировано"}</strong><span>Основная карточка: {preview.targetProductCode || "—"}; исходные: {preview.sourceProductCodes?.join(", ") || "—"}</span>{preview.blockers.map((blocker) => <p className="admin-inline-error" key={blocker}>{blocker}</p>)}{preview.ready && <small>Результат может устареть. Перед фактическим объединением сервер повторит все проверки.</small>}</div>}
    {preview?.ready && <label className="wide">Для объединения введите «ОБЪЕДИНИТЬ {preview.targetProductCode}»<input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" /></label>}
    <div className="dialog-actions"><button onClick={onClose}>Закрыть</button><button disabled={busy || !!sizeError || !reviewedIdentity.trim() || !expectedSKUs.trim()} onClick={() => void check()}>{busy ? "Подождите…" : "Проверить без изменений"}</button>{preview?.ready && <button className="admin-danger" disabled={busy || confirmation !== `ОБЪЕДИНИТЬ ${preview.targetProductCode}`} onClick={() => void merge()}>Объединить карточки</button>}</div>
  </Dialog>;
}
