import { useEffect, useMemo, useState } from "react";
import { api, Dialog, PageHeading } from "./adminShared";
import type { MarketplaceReturn, ReturnProduct } from "./adminTypes";
import "./styles/admin-returns.css";

const conditions = {
  inspection: { label: "На осмотре", note: "Нужно оценить состояние", icon: "◌" },
  ready: { label: "Готово к продаже", note: "Можно вернуть в учёт СБИС", icon: "✓" },
  restoring: { label: "На восстановлении", note: "Вернуться к оценке позже", icon: "↻" },
  dead: { label: "Списано", note: "Себестоимость зафиксирована как потеря", icon: "×" },
} as const;
const channels: Record<string, string> = { wb: "Wildberries", ozon: "Ozon", avito: "Avito", saby: "Розница СБИС" };
const receiptLabels: Record<string, string> = { none: "Документ не создан", queued: "Создание поставлено в очередь", checking: "Проверяем СБИС", draft_created: "Черновик создан в СБИС", posted: "Поступление проведено", failed: "Не удалось создать документ", correction_required: "Нужна корректировка" };

export function AdminReturns({ can, onError }: { can: (permission: string) => boolean; onError: (message: string) => void }) {
  const [items, setItems] = useState<MarketplaceReturn[]>([]);
  const [selectedID, setSelectedID] = useState<number>();
  const [tab, setTab] = useState<MarketplaceReturn["condition"] | "all">("inspection");
  const [channel, setChannel] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  useEffect(() => { let current = true; api<{ items: MarketplaceReturn[] }>("/api/v1/admin/returns").then((result) => { if (current) { setItems(result.items); setSelectedID(result.items[0]?.id); } }).catch((error) => { if (current) onError(error instanceof Error ? error.message : "Не удалось загрузить возвраты"); }).finally(() => { if (current) setLoading(false); }); return () => { current = false; }; }, [onError]);
  const counts = useMemo(() => Object.fromEntries(Object.keys(conditions).map((key) => [key, items.filter((item) => item.condition === key).length])), [items]);
  const visible = useMemo(() => items.filter((item) => (tab === "all" || item.condition === tab) && (!channel || item.channel === channel) && (!query || `${item.productName} ${item.sku} ${item.sourceShipmentId} ${item.sourceReturnId}`.toLocaleLowerCase("ru").includes(query.toLocaleLowerCase("ru")))), [items, tab, channel, query]);
  const selected = visible.find((item) => item.id === selectedID) || visible[0];

  return <div className="returns-workspace">
    <div className="returns-heading"><PageHeading eyebrow="Физический факт и документы" title="Возвраты растений" text="Каждое растение проходит собственный осмотр. Финансовая связь и поступление СБИС видны отдельно." />{can("returns.edit") && <button className="primary returns-create" onClick={() => setCreating(true)}>+ Принять возврат</button>}</div>
    <nav className="returns-tabs" aria-label="Состояния возвратов">
      {(Object.entries(conditions) as Array<[MarketplaceReturn["condition"], typeof conditions.inspection]>).map(([value, meta]) => <button key={value} className={tab === value ? "active" : ""} onClick={() => setTab(value)}>{meta.label}<b>{counts[value] || 0}</b></button>)}
      <button className={tab === "all" ? "active" : ""} onClick={() => setTab("all")}>Все<b>{items.length}</b></button>
    </nav>
    <div className="returns-filters"><label className="returns-search"><span>⌕</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Найти товар, отправление или возврат" /></label><select aria-label="Канал возврата" value={channel} onChange={(event) => setChannel(event.target.value)}><option value="">Все каналы</option>{Object.entries(channels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
    <div className="returns-layout">
      <section className="returns-list" aria-label="Список возвратов">
        {loading && <p className="returns-empty">Загружаем журнал…</p>}
        {!loading && visible.map((item) => <button key={item.id} className={selected?.id === item.id ? "selected" : ""} onClick={() => setSelectedID(item.id)}>
          <span className={`return-state state-${item.condition}`}>{conditions[item.condition].icon}</span><span className="return-card-main"><strong>{item.productName}</strong><small>{item.sku} · {channels[item.channel]}</small><small>{item.sourceShipmentId || `Возврат ${item.sourceReturnId}`}</small></span><span className="return-card-tail"><time>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "short" })}</time><em>{conditions[item.condition].label}</em></span>
        </button>)}
        {!loading && !visible.length && <p className="returns-empty">В этой группе возвратов нет.</p>}
      </section>
      <ReturnDetails key={selected?.id} item={selected} canEdit={can("returns.edit")} canReceipt={can("returns.receipt")} onSaved={(item) => { setItems((current) => current.map((old) => old.id === item.id ? item : old)); setSelectedID(item.id); }} onError={onError} />
    </div>
    {creating && <CreateReturn onClose={() => setCreating(false)} onCreated={(created) => { setCreating(false); setItems((current) => [...created, ...current]); setSelectedID(created[0]?.id); setTab("all"); }} onError={onError} />}
  </div>;
}

function ReturnDetails({ item, canEdit, canReceipt, onSaved, onError }: { item?: MarketplaceReturn; canEdit: boolean; canReceipt: boolean; onSaved: (item: MarketplaceReturn) => void; onError: (message: string) => void }) {
  const [comment, setComment] = useState(item?.comment || ""); const [busy, setBusy] = useState(false);
  if (!item) return <aside className="return-detail returns-empty">Выберите возврат слева.</aside>;
  const save = async (condition = item.condition) => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}`, { method: "PATCH", body: JSON.stringify({ condition, comment }) }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось сохранить оценку"); } finally { setBusy(false); } };
  const receipt = async () => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/receipt`, { method: "POST" }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось создать поступление"); } finally { setBusy(false); } };
  const photo = async (file?: File) => { if (!file) return; setBusy(true); try { const data = await fileBase64(file); const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/photos`, { method: "POST", body: JSON.stringify({ contentType: file.type, data }) }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось добавить фото"); } finally { setBusy(false); } };
  return <aside className="return-detail">
    <header><div><span className={`return-state state-${item.condition}`}>{conditions[item.condition].icon}</span><p>Возврат №{item.id}</p><h2>{item.productName}</h2><small>{item.sku} · растение {item.sourceUnitIndex}</small></div><span className={`return-finance ${item.financialStatus}`}>{item.financialStatus === "linked" ? "Продажа связана" : "Нужна финансовая сверка"}</span></header>
    <dl><div><dt>Канал</dt><dd>{channels[item.channel]}</dd></div><div><dt>Получено</dt><dd>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU")}</dd></div><div><dt>Исходное отправление</dt><dd>{item.sourceShipmentId || "Не указано"}</dd></div><div><dt>Себестоимость</dt><dd>{item.unitCost == null ? "Неизвестна" : `${item.unitCost.toLocaleString("ru-RU")} ₽`}</dd></div></dl>
    <section><h3>Оценка растения</h3><div className="return-condition-grid">{(Object.entries(conditions) as Array<[MarketplaceReturn["condition"], typeof conditions.inspection]>).map(([value, meta]) => <button key={value} disabled={!canEdit || busy || (item.condition !== "inspection" && item.condition !== "restoring" && item.condition !== value) || (item.receiptStatus === "posted" && item.condition !== value)} className={item.condition === value ? "active" : ""} onClick={() => void save(value)}><b>{meta.icon}</b><span><strong>{meta.label}</strong><small>{meta.note}</small></span></button>)}</div></section>
    <section><h3>Фото осмотра <small>{item.photoIds.length}/6</small></h3><div className="return-photos">{item.photoIds.map((id) => <img key={id} src={`/api/v1/admin/return-photos/${id}`} alt="Фото возвращённого растения" />)}{canEdit && item.photoIds.length < 6 && <label><input type="file" accept="image/jpeg,image/png,image/webp" onChange={(event) => void photo(event.target.files?.[0])} disabled={busy} /><b>＋</b><span>Добавить фото</span></label>}</div></section>
    <section><label className="return-comment"><span>Комментарий осмотра</span><textarea value={comment} onChange={(event) => setComment(event.target.value)} disabled={!canEdit || busy} placeholder="Что произошло с растением и что делать дальше" /></label>{canEdit && <button className="return-save" disabled={busy || comment === item.comment} onClick={() => void save()}>Сохранить комментарий</button>}</section>
    <section className="return-receipt"><div><h3>Поступление в СБИС</h3><p>{receiptLabels[item.receiptStatus]}</p><small>Остаток изменится только после ручного проведения документа в СБИС.</small></div>{item.receiptExternalUrl && <a href={item.receiptExternalUrl} target="_blank" rel="noreferrer">Открыть документ ↗</a>}{canReceipt && item.condition === "ready" && !["posted", "draft_created"].includes(item.receiptStatus) && <button disabled={busy} onClick={() => void receipt()}>{["queued", "checking"].includes(item.receiptStatus) ? "Проверить операцию" : "Создать черновик поступления"}</button>}</section>
    {item.history.length > 1 && <details className="return-history"><summary>История оценки · {item.history.length}</summary>{item.history.map((entry, index) => <p key={`${entry.createdAt}-${index}`}><time>{new Date(entry.createdAt).toLocaleString("ru-RU")}</time><b>{conditions[entry.to as keyof typeof conditions]?.label || entry.to}</b>{entry.comment && <span>{entry.comment}</span>}</p>)}</details>}
  </aside>;
}

function CreateReturn({ onClose, onCreated, onError }: { onClose: () => void; onCreated: (items: MarketplaceReturn[]) => void; onError: (message: string) => void }) {
  const [products, setProducts] = useState<ReturnProduct[]>([]); const [variantID, setVariantID] = useState(0); const [channel, setChannel] = useState("wb"); const [returnID, setReturnID] = useState(""); const [shipmentID, setShipmentID] = useState(""); const [quantity, setQuantity] = useState(1); const [states, setStates] = useState<MarketplaceReturn["condition"][]>(["inspection"]); const [comment, setComment] = useState(""); const [busy, setBusy] = useState(false);
  useEffect(() => { api<{ items: ReturnProduct[] }>("/api/v1/admin/returns/products").then((result) => { setProducts(result.items); setVariantID(result.items[0]?.variantId || 0); }).catch((error) => onError(error.message)); }, [onError]);
  const resize = (value: number) => { const next = Math.max(1, Math.min(20, value)); setQuantity(next); setStates((current) => Array.from({ length: next }, (_, index) => current[index] || "inspection")); };
  const submit = async (event: React.FormEvent) => { event.preventDefault(); setBusy(true); try { const result = await api<{ items: MarketplaceReturn[] }>("/api/v1/admin/returns", { method: "POST", body: JSON.stringify({ channel, sourceReturnId: returnID, sourceShipmentId: shipmentID, variantId: variantID, quantity, returnedAt: new Date().toISOString().slice(0, 10), conditions: states, comment }) }); onCreated(result.items); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось принять возврат"); } finally { setBusy(false); } };
  return <Dialog title="Принять возврат" onClose={onClose} className="return-create-dialog"><form onSubmit={submit}><p className="return-dialog-note">Одна единица станет одной записью осмотра. Исходный номер объединит растения одного отправления.</p><div className="return-form-grid"><label>Канал<select value={channel} onChange={(event) => setChannel(event.target.value)}>{Object.entries(channels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label><label>Номер возврата<input value={returnID} onChange={(event) => setReturnID(event.target.value)} placeholder="Можно оставить пустым" /></label><label className="wide">Исходное отправление<input value={shipmentID} onChange={(event) => setShipmentID(event.target.value)} placeholder="Номер заказа или отправления" /></label><label className="wide">Растение<select value={variantID} onChange={(event) => setVariantID(Number(event.target.value))} required><option value={0}>Выберите растение</option>{products.map((product) => <option value={product.variantId} key={product.variantId}>{product.name} · {product.sku}</option>)}</select></label><label>Количество<input type="number" min={1} max={20} value={quantity} onChange={(event) => resize(Number(event.target.value))} /></label></div><fieldset><legend>Состояние каждого растения</legend>{states.map((state, index) => <label key={index}><span>Растение {index + 1}</span><select value={state} onChange={(event) => setStates((current) => current.map((value, itemIndex) => itemIndex === index ? event.target.value as MarketplaceReturn["condition"] : value))}>{Object.entries(conditions).map(([value, meta]) => <option key={value} value={value}>{meta.label}</option>)}</select></label>)}</fieldset><label className="return-comment"><span>Общий комментарий</span><textarea value={comment} onChange={(event) => setComment(event.target.value)} /></label><footer><button type="button" onClick={onClose}>Отмена</button><button className="primary" disabled={busy || !variantID}>{busy ? "Сохраняем…" : `Принять ${quantity} шт.`}</button></footer></form></Dialog>;
}

function fileBase64(file: File) { return new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onerror = () => reject(new Error("Не удалось прочитать фото")); reader.onload = () => resolve(String(reader.result).split(",")[1] || ""); reader.readAsDataURL(file); }); }
