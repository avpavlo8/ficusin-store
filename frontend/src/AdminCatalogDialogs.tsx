import { useEffect, useState } from "react";
import { CategoryPicker } from "./AdminCatalog";
import { Dialog, api, money, sabyFieldLabels } from "./adminShared";
import type { Category, CategoryAttribute, ImportEntry, Product } from "./adminTypes";

const attributeOptionLabels: Record<string, string> = {
  sunny: "Яркий свет", diffused: "Рассеянный свет", low_light: "Полутень",
  frequent: "Частый", moderate: "Умеренный", rare: "Редкий",
  low: "Низкая", medium: "Средняя", high: "Высокая",
  easy: "Лёгкий", demanding: "Требовательный", non_toxic: "Нетоксично",
  toxic: "Токсично", unknown: "Не проверено", safe: "Безопасно", caution: "С осторожностью",
  bathroom: "Ванная", bedroom: "Спальня", office: "Офис", nursery: "Детская",
  living_room: "Гостиная", kitchen: "Кухня", upright: "Вертикальная",
  bushy: "Кустовая", trailing: "Ампельная", climbing: "Вьющаяся", rosette: "Розетка",
};

function AttributeFields({ schema, values, onChange }: {
  schema: CategoryAttribute[]; values: Record<string, unknown>;
  onChange: (code: string, value: string | number | boolean | string[] | null) => void;
}) {
  return <>{schema.map((attribute) => {
    const value = values[attribute.code];
    const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
    if (attribute.dataType === "boolean") return <label className="admin-checkbox" key={attribute.code}><input type="checkbox" checked={value === true} onChange={(event) => onChange(attribute.code, event.target.checked)} />{title}</label>;
    if (attribute.dataType === "enum") return <label key={attribute.code}>{title}<select required={attribute.required} value={String(value ?? "")} onChange={(event) => onChange(attribute.code, event.target.value || null)}><option value="">Не указано</option>{attribute.options.map((option) => <option key={option} value={option}>{attributeOptionLabels[option] || option}</option>)}</select></label>;
    if (attribute.dataType === "multi_enum") {
      const selected = Array.isArray(value) ? value.map(String) : [];
      return <fieldset className="attribute-multi" key={attribute.code}><legend>{title}</legend>{attribute.options.map((option) => <label key={option}><input type="checkbox" checked={selected.includes(option)} onChange={(event) => onChange(attribute.code, event.target.checked ? [...selected, option] : selected.filter((item) => item !== option))} />{attributeOptionLabels[option] || option}</label>)}</fieldset>;
    }
    return <label key={attribute.code}>{title}<input type={attribute.dataType === "number" ? "number" : "text"} min={attribute.dataType === "number" ? 0 : undefined} required={attribute.required} value={value == null ? "" : String(value)} onChange={(event) => onChange(attribute.code, event.target.value === "" ? null : attribute.dataType === "number" ? Number(event.target.value) : event.target.value)} /></label>;
  })}</>;
}

export function ProductDialog({ product, onClose, onSaved, onError }: { product: Product; onClose: () => void; onSaved: (value: Product) => void; onError: (value: string) => void }) {
  const [form, setForm] = useState(product);
  const [categories, setCategories] = useState<Category[]>([]);
  const [schema, setSchema] = useState<CategoryAttribute[]>([]);
  useEffect(() => { api<{ categories: Category[] }>("/api/v1/admin/categories").then((data) => setCategories(data.categories)).catch((error) => onError(error.message)); }, [onError]);
  useEffect(() => { if (form.categoryId) api<{ attributes: CategoryAttribute[] }>(`/api/v1/admin/categories/${form.categoryId}/attributes`).then((data) => setSchema(data.attributes)).catch((error) => onError(error.message)); }, [form.categoryId, onError]);
  const save = async () => {
    try {
      const result = await api<{ product: Product }>(`/api/v1/admin/products/${product.id}`, { method: "PATCH", body: JSON.stringify({
        name: form.name, latinName: form.latinName, shortDescription: form.shortDescription,
        description: form.description, careInstructions: form.careInstructions, status: form.status,
        featured: form.featured, image: form.image, priceMinor: Math.round(form.price * 100),
        variantLabel: form.variantLabel, heightCm: form.heightCm, potDiameterCm: form.potDiameterCm,
        packageLengthCm: form.packageLengthCm, packageWidthCm: form.packageWidthCm,
        packageHeightCm: form.packageHeightCm, packageWeightGrams: form.packageWeightGrams,
        wholesaleMinQty: form.wholesaleMinQty, catalogSection: form.catalogSection, categoryId: form.categoryId,
        plantKind: form.plantKind || "", lightLevel: form.lightLevel || "", watering: form.watering || "",
        heightClass: form.heightClass || "", careLevel: form.careLevel || "", placement: form.placement || "",
        petSafety: form.petSafety || "", growthHabit: form.growthHabit || "",
        passport: form.passport, importantWarnings: form.importantWarnings,
        sabyFields: form.sabyFields,
        attributes: form.attributes,
        ...(form.sabyFields.includes("stock") ? {} : { stock: form.stock }),
      }) }); onSaved(result.product);
    } catch (error) { onError((error as Error).message); }
  };
  return <Dialog title="Редактирование товара" onClose={onClose}><div className="admin-form-grid product-form">
    <h3 className="product-form-heading wide">Карточка товара</h3>
    <label className="wide">Название<input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></label>
    <label>Латинское название<input value={form.latinName} onChange={(event) => setForm({ ...form, latinName: event.target.value })} /></label>
    <label className="wide">Короткое описание<textarea rows={2} value={form.shortDescription} onChange={(event) => setForm({ ...form, shortDescription: event.target.value })} /></label>
    <label className="wide">Описание<textarea rows={5} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
    <label className="wide">Уход<textarea rows={4} value={form.careInstructions} onChange={(event) => setForm({ ...form, careInstructions: event.target.value })} /></label>
    <fieldset className="wide passport-admin"><legend>Паспорт растения</legend><p className="admin-hint">Поля публикуются как самостоятельный SEO-раздел по адресу карточки с якорем #plant-passport.</p>{([['origin','Происхождение'],['lighting','Освещение'],['watering','Полив'],['humidity','Влажность'],['temperature','Температура'],['soil','Грунт'],['fertilizer','Удобрение'],['repotting','Пересадка'],['careDifficulty','Сложность ухода'],['growthRate','Скорость роста'],['matureSize','Взрослый размер'],['toxicity','Токсичность'],['problems','Типичные проблемы и решения'],['pests','Вредители']] as const).map(([key,label]) => <label key={key}>{label}<textarea rows={key === 'problems' ? 4 : 2} value={form.passport?.[key] || ''} onChange={(event) => setForm({...form, passport: {...form.passport, [key]: event.target.value}})} /></label>)}<label>FAQ (вопрос | ответ, одна пара на строку)<textarea rows={5} value={(form.passport?.faq || []).map((item) => `${item.question} | ${item.answer}`).join('\n')} onChange={(event) => setForm({...form, passport: {...form.passport, faq: event.target.value.split('\n').map((line) => line.split('|').map((part) => part.trim())).filter((parts) => parts.length > 1 && parts[0] && parts[1]).map(([question,answer]) => ({question,answer}))}})} /></label></fieldset>
    <label className="wide">Важные предупреждения (по одному на строку)<textarea rows={3} value={(form.importantWarnings || []).join('\n')} onChange={(event) => setForm({...form, importantWarnings: event.target.value.split('\n').map((item) => item.trim()).filter(Boolean).slice(0, 4)})} /></label>
    <label className="wide">URL фотографии<input value={form.image} onChange={(event) => setForm({ ...form, image: event.target.value })} /></label>
    <h3 className="product-form-heading wide">Категория и характеристики</h3>
    <div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={form.categoryId} onChange={(categoryId) => setForm({ ...form, categoryId })} /></div>
    <AttributeFields schema={schema.filter((item) => item.audience === "customer")} values={form.attributes || {}} onChange={(code, value) => setForm((current) => ({ ...current, attributes: { ...current.attributes, [code]: value as never } }))} />
    <h3 className="product-form-heading wide">Продажа</h3>
    <label>Цена, ₽<input type="number" min="0" value={form.price} onChange={(event) => setForm({ ...form, price: Number(event.target.value) })} /></label>
    <label>Остаток, шт.<input type="number" min="0" disabled={form.sabyFields.includes("stock")} value={form.stock} onChange={(event) => setForm({ ...form, stock: Number(event.target.value) })} /></label>
    <label>Оптовый минимум<input type="number" min="1" value={form.wholesaleMinQty} onChange={(event) => setForm({ ...form, wholesaleMinQty: Number(event.target.value) })} /></label>
    <h3 className="product-form-heading wide">Упаковка и доставка</h3>
    <AttributeFields schema={schema.filter((item) => item.audience === "technical")} values={form.attributes || {}} onChange={(code, value) => setForm((current) => ({ ...current, attributes: { ...current.attributes, [code]: value as never } }))} />
    <h3 className="product-form-heading wide">Интеграции</h3>
    <div className="wide integration-mapping"><strong>Ficusin SKU: {form.sku}</strong>{form.externalIds?.length ? form.externalIds.map((mapping) => <small key={`${mapping.provider}:${mapping.type}:${mapping.externalId}`}>{mapping.provider.toUpperCase()} · {mapping.type}: {mapping.externalId}</small>) : <small>Внешних идентификаторов нет</small>}</div>
    {form.sabyId && <div className="wide admin-field"><span className="admin-field-label">Что берём из СБИС</span><div className="sync-options">{Object.entries(sabyFieldLabels).map(([field, label]) => <label key={field}><input type="checkbox" checked={form.sabyFields.includes(field)} onChange={(event) => setForm({ ...form, sabyFields: event.target.checked ? [...form.sabyFields, field] : form.sabyFields.filter((item) => item !== field) })} /><span><strong>{label}</strong></span></label>)}</div></div>}
    <label>Статус<select value={form.status} onChange={(event) => setForm({ ...form, status: event.target.value })}><option value="draft">Черновик</option><option value="published">Опубликован</option><option value="archived">Архив</option></select></label>
    <label className="admin-checkbox"><input type="checkbox" checked={form.featured} onChange={(event) => setForm({ ...form, featured: event.target.checked })} />Поднимать в начало каталога</label>
  </div><p className="admin-hint">Габариты упаковки определяют стоимость доставки СДЭК: из коробок всех позиций заказа складывается одна общая. Пока поля пусты, товар считается как коробка 40×25×25 см, 1,5 кг.</p><p className="admin-hint">Карточка ваша: обмен с СБИС меняет только те поля, что отмечены выше. Остальное берётся оттуда лишь по кнопке «Подтянуть из СБИС» и только один раз.</p><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" onClick={save}>Сохранить</button></div></Dialog>;
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
  useEffect(() => { if (categoryId) api<{ attributes: CategoryAttribute[] }>(`/api/v1/admin/categories/${categoryId}/attributes`).then((data) => setSchema(data.attributes)).catch((error) => onError(error.message)); }, [categoryId, onError]);
  const save = async () => {
    setSaving(true);
    try {
      await api("/api/v1/admin/products", { method: "POST", body: JSON.stringify({
        name: form.name, latinName: form.latinName, shortDescription: form.shortDescription,
        description: form.description, image: form.image, catalogSection: form.catalogSection,
        categoryId, priceMinor: Math.round(form.price * 100), stock: form.stock,
        heightCm: form.heightCm === "" ? null : Number(form.heightCm), potDiameterCm: form.potDiameterCm === "" ? null : Number(form.potDiameterCm),
        packageLengthCm: form.packageLengthCm === "" ? null : Number(form.packageLengthCm), packageWidthCm: form.packageWidthCm === "" ? null : Number(form.packageWidthCm),
        packageHeightCm: form.packageHeightCm === "" ? null : Number(form.packageHeightCm), packageWeightGrams: form.packageWeightGrams === "" ? null : Number(form.packageWeightGrams),
        attributes,
      }) });
      onCreated();
    } catch (error) { onError((error as Error).message); setSaving(false); }
  };
  return <Dialog title="Новый товар" onClose={onClose}><div className="admin-form-grid product-form">
    <h3 className="product-form-heading wide">Карточка товара</h3>
    <label className="wide">Название<input autoFocus value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="Фикус Бенджамина" /></label>
    <label>Латинское название<input value={form.latinName} onChange={(event) => setForm({ ...form, latinName: event.target.value })} /></label>
    <label>Раздел<select value={form.catalogSection} onChange={(event) => setForm({ ...form, catalogSection: event.target.value })}><option value="plants">Растения</option><option value="pots">Кашпо и горшки</option><option value="soil">Грунт</option><option value="fertilizer">Удобрения</option><option value="accessories">Аксессуары</option></select></label>
    <label className="wide">Короткое описание<textarea rows={2} value={form.shortDescription} onChange={(event) => setForm({ ...form, shortDescription: event.target.value })} /></label>
    <label className="wide">Описание<textarea rows={5} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
    <label className="wide">URL фотографии<input value={form.image} onChange={(event) => setForm({ ...form, image: event.target.value })} /></label>
    <div className="wide admin-field"><span className="admin-field-label">Категория</span><CategoryPicker categories={categories} value={categoryId} onChange={setCategoryId} /></div>
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
  const labels: Record<string, string> = { new: "Заведём", exists: "Уже есть", missing: "Не найден" };
  return <Dialog title="Импорт товаров из СБИС" onClose={onClose}>
    <label className="wide">Коды товаров<textarea rows={6} value={codes} onChange={(event) => { setCodes(event.target.value); setPreview(null); }} placeholder="X1150532&#10;X1150533" /></label>
    <p className="admin-hint">Вставьте коды из СБИС — через запятую, пробел или с новой строки, как удобно. Подойдёт и артикул, и штрихкод.</p>
    <div className="admin-field"><span className="admin-field-label">Раздел каталога</span><CategoryPicker categories={categories} value={categoryId} onChange={setCategoryId} /></div>
    {preview && <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Код</th><th>Товар</th><th>Цена</th><th>Остаток</th><th>Что будет</th></tr></thead><tbody>{preview.map((entry) => <tr key={entry.code}><td>{entry.code}</td><td>{entry.name || "—"}</td><td>{entry.name ? money.format(entry.price) : "—"}</td><td>{entry.name ? entry.stock : "—"}</td><td><span className={"admin-pill " + entry.status}>{labels[entry.status] || entry.status}</span></td></tr>)}</tbody></table></div>}
    {preview && found === 0 && <p className="admin-hint">Заводить нечего: ни одного нового товара в списке нет.</p>}
    <div className="dialog-actions"><button onClick={onClose}>Отмена</button><button disabled={busy || codes.trim() === ""} onClick={() => send(true)}>Проверить</button><button className="primary" disabled={busy || !preview || found === 0} onClick={() => send(false)}>Завести {found > 0 ? found : ""}</button></div>
  </Dialog>;
}

export function SyncDialog({ count, onClose, onSync }: { count: number; onClose: () => void; onSync: (fields: string[]) => void }) {
  const options = [{ id: "name", label: "Название" }, { id: "photo", label: "Фото" }, { id: "price", label: "Цена" }, { id: "description", label: "Описание" }];
  const [fields, setFields] = useState(["price"]);
  return <Dialog title={`Подтянуть из СБИС: ${count} ${count === 1 ? "товар" : "товаров"}`} onClose={onClose}><p>Выбранные поля заменятся тем, что лежит в справочнике СБИС. Это разовое действие: дальше товар снова ведёте вы.</p><div className="sync-options">{options.map((option) => <label key={option.id}><input type="checkbox" checked={fields.includes(option.id)} onChange={(event) => setFields((current) => event.target.checked ? [...current, option.id] : current.filter((field) => field !== option.id))} /><span><strong>{option.label}</strong></span></label>)}</div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={fields.length === 0} onClick={() => onSync(fields)}>Синхронизировать</button></div></Dialog>;
}
