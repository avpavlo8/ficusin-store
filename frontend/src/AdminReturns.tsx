import { useEffect, useMemo, useState } from "react";
import { api, PageHeading } from "./adminShared";
import { ReturnIntakeDialog } from "./ReturnIntakeDialog";
import type { MarketplaceReturn } from "./adminTypes";
import "./styles/admin-returns.css";

const conditions = {
  inspection: { label: "На осмотре", note: "Нужно оценить состояние", icon: "◌", className: "state-inspection" },
  ready: { label: "В продажу", note: "Можно вернуть в учёт СБИС", icon: "✓", className: "state-ready" },
  restoring: { label: "На восстановление", note: "Вернуться к оценке позже", icon: "↻", className: "state-restoring" },
  dead: { label: "Списать", note: "Себестоимость зафиксирована как потеря", icon: "×", className: "state-dead" },
} as const;
const channels: Record<string, string> = { wb: "ВБ", ozon: "Озон", other: "Другое", avito: "Avito", saby: "Розница СБИС" };
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
          <span className={`return-state ${conditions[item.condition].className}`}>{conditions[item.condition].icon}</span><span className="return-card-main"><strong>{item.productName}</strong><small>{item.sku} · {channels[item.channel]}</small><small>{item.sourceShipmentId || `Возврат ${item.sourceReturnId}`}</small></span><span className="return-card-tail"><time>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "short" })}</time><em>{conditions[item.condition].label}</em></span>
        </button>)}
        {!loading && !visible.length && <p className="returns-empty">В этой группе возвратов нет.</p>}
      </section>
      <ReturnDetails key={selected?.id} item={selected} canEdit={can("returns.edit")} canReceipt={can("returns.receipt.create")} onSaved={(item) => { setItems((current) => current.map((old) => old.id === item.id ? item : old)); setSelectedID(item.id); }} onError={onError} />
    </div>
    {creating && <ReturnIntakeDialog canReceipt={can("returns.receipt.create")} onClose={() => setCreating(false)} onCreated={(created) => { setCreating(false); setItems((current) => [...created, ...current.filter(item => !created.some(next => next.id === item.id))]); setSelectedID(created[0]?.id); setTab("all"); }} />}
  </div>;
}

function ReturnDetails({ item, canEdit, canReceipt, onSaved, onError }: { item?: MarketplaceReturn; canEdit: boolean; canReceipt: boolean; onSaved: (item: MarketplaceReturn) => void; onError: (message: string) => void }) {
  const [comment, setComment] = useState(item?.comment || ""); const [busy, setBusy] = useState(false);
  if (!item) return <aside className="return-detail returns-empty">Выберите возврат слева.</aside>;
  const save = async (condition = item.condition) => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}`, { method: "PATCH", body: JSON.stringify({ condition, comment }) }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось сохранить оценку"); } finally { setBusy(false); } };
  const receipt = async () => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/receipt`, { method: "POST" }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось создать поступление"); } finally { setBusy(false); } };
  const photo = async (file?: File) => { if (!file) return; setBusy(true); try { const data = await fileBase64(file); const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/photos`, { method: "POST", body: JSON.stringify({ contentType: file.type, data }) }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось добавить фото"); } finally { setBusy(false); } };
  return <aside className="return-detail">
    <header><div><span className={`return-state ${conditions[item.condition].className}`}>{conditions[item.condition].icon}</span><p>Возврат №{item.id}</p><h2>{item.productName}</h2><small>{item.sku} · растение {item.sourceUnitIndex}</small></div><span className={`return-finance ${item.financialStatus}`}>{item.financialStatus === "linked" ? "Продажа связана" : "Нужна финансовая сверка"}</span></header>
    <dl><div><dt>Канал</dt><dd>{channels[item.channel]}{item.channelComment && <small className="return-channel-comment">{item.channelComment}</small>}</dd></div><div><dt>Получено</dt><dd>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU")}</dd></div><div><dt>Исходное отправление</dt><dd>{item.sourceShipmentId || "Не указано"}</dd></div><div><dt>Себестоимость</dt><dd>{item.unitCost == null ? "Неизвестна" : `${item.unitCost.toLocaleString("ru-RU")} ₽`}</dd></div></dl>
    <section><h3>Оценка растения</h3><div className="return-condition-grid">{(Object.entries(conditions) as Array<[MarketplaceReturn["condition"], typeof conditions.inspection]>).map(([value, meta]) => <button key={value} disabled={!canEdit || busy || (item.condition !== "inspection" && item.condition !== "restoring" && item.condition !== value) || (item.receiptStatus === "posted" && item.condition !== value)} className={item.condition === value ? "active" : ""} onClick={() => void save(value)}><b>{meta.icon}</b><span><strong>{meta.label}</strong><small>{meta.note}</small></span></button>)}</div></section>
    <section><h3>Фото осмотра <small>{(item.photoIds || []).length}/6</small></h3><div className="return-photos">{(item.photoIds || []).map((id) => <img key={id} src={`/api/v1/admin/return-photos/${id}`} alt="Фото возвращённого растения" />)}{canEdit && (item.photoIds || []).length < 6 && <label><input type="file" accept="image/jpeg,image/png,image/webp" onChange={(event) => void photo(event.target.files?.[0])} disabled={busy} /><b>＋</b><span>Добавить фото</span></label>}</div></section>
    <section><label className="return-comment"><span>Комментарий осмотра</span><textarea value={comment} onChange={(event) => setComment(event.target.value)} disabled={!canEdit || busy} placeholder="Что произошло с растением и что делать дальше" /></label>{canEdit && <button className="return-save" disabled={busy || comment === item.comment} onClick={() => void save()}>Сохранить комментарий</button>}</section>
    <section className="return-receipt"><div><h3>Поступление в СБИС</h3><p>{receiptLabels[item.receiptStatus]}</p><small>Остаток изменится только после ручного проведения документа в СБИС.</small></div>{item.receiptExternalUrl && <a href={item.receiptExternalUrl} target="_blank" rel="noreferrer">Открыть документ ↗</a>}{canReceipt && item.condition === "ready" && !["posted", "draft_created"].includes(item.receiptStatus) && <button disabled={busy} onClick={() => void receipt()}>{["queued", "checking"].includes(item.receiptStatus) ? "Проверить операцию" : "Создать черновик поступления"}</button>}</section>
    {(item.history || []).length > 1 && <details className="return-history"><summary>История оценки · {(item.history || []).length}</summary>{(item.history || []).map((entry, index) => <p key={`${entry.createdAt}-${index}`}><time>{new Date(entry.createdAt).toLocaleString("ru-RU")}</time><b>{conditions[entry.to as keyof typeof conditions]?.label || entry.to}</b>{entry.comment && <span>{entry.comment}</span>}</p>)}</details>}
  </aside>;
}

function fileBase64(file: File) { return new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onerror = () => reject(new Error("Не удалось прочитать фото")); reader.onload = () => resolve(String(reader.result).split(",")[1] || ""); reader.readAsDataURL(file); }); }
