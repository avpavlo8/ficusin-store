import { useCallback, useEffect, useMemo, useState } from "react";
import { api, PageHeading } from "./adminShared";
import { ReturnIntakeDialog } from "./ReturnIntakeDialog";
import { downloadReturnLabels, returnMoney, returnPhotoBase64 } from "./returnHelpers";
import type { MarketplaceReturn, ReturnIntake } from "./adminTypes";
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
  const [tab, setTab] = useState<MarketplaceReturn["condition"] | "all" | "documents">("inspection");
  const [channel, setChannel] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [intakeID, setIntakeID] = useState<string>();
  const [documents, setDocuments] = useState<ReturnIntake[]>([]);
  const [checked, setChecked] = useState<number[]>([]);
  const [busy, setBusy] = useState(false);
  const [includeDate, setIncludeDate] = useState(true);
  const refresh = useCallback(async () => {
    const [returns, docs] = await Promise.all([api<{items: MarketplaceReturn[]}>("/api/v1/admin/returns"), api<{items: ReturnIntake[]}>("/api/v1/admin/returns/documents")]);
    setItems(returns.items); setDocuments(docs.items);
  }, []);
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); try { await action(); await refresh(); setChecked([]); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось выполнить действие"); } finally { setBusy(false); } };
  const remove = (documentId?: string, all = false) => {
    if (!window.confirm(all ? "Удалить все записи и документы возвратов? Документы СБИС останутся в СБИС." : "Удалить выбранные записи возврата?")) return;
    void perform(() => api("/api/v1/admin/returns", { method: "DELETE", body: JSON.stringify({ ids: checked, documentId, all }) }));
  };

  useEffect(() => { let current = true; refresh().catch(error => { if(current) onError(error.message); }).finally(() => { if(current) setLoading(false); }); return () => {current=false}; }, [onError,refresh]);
  useEffect(() => {
    if(!items.some(item => ["queued","checking"].includes(item.receiptStatus))) return;
    const timer=window.setInterval(() => { void refresh().catch(error => onError(error.message)); },3000); return () => window.clearInterval(timer);
  },[items,refresh,onError]);
  const counts = useMemo(() => Object.fromEntries(Object.keys(conditions).map((key) => [key, items.filter((item) => item.condition === key).length])), [items]);
  const visible = useMemo(() => items.filter((item) => (tab === "all" || item.condition === tab) && (!channel || item.channel === channel) && (!query || `${item.productName} ${item.article || ""}`.toLocaleLowerCase("ru").includes(query.toLocaleLowerCase("ru")))), [items, tab, channel, query]);
  const selected = visible.find((item) => item.id === selectedID) || visible[0];

  const picked = visible.filter(item => checked.includes(item.id));
  return <div className="returns-workspace">
    <div className="returns-heading"><PageHeading eyebrow="Физический факт и документы" title="Возвраты растений" text="Сохраняйте документы приёмки, оценивайте растения и собирайте поступления." />{can("returns.edit") && <button className="primary returns-create" onClick={() => { setIntakeID(undefined); setCreating(true); }}>+ Принять возврат</button>}</div>
    <nav className="returns-tabs" aria-label="Состояния возвратов">
      {(Object.entries(conditions) as Array<[MarketplaceReturn["condition"], typeof conditions.inspection]>).map(([value, meta]) => <button key={value} className={tab === value ? "active" : ""} onClick={() => { setTab(value); setChecked([]); }}>{meta.label}<b>{counts[value] || 0}</b></button>)}
      <button className={tab === "all" ? "active" : ""} onClick={() => { setTab("all"); setChecked([]); }}>Все<b>{items.length}</b></button>
      <button className={tab === "documents" ? "active" : ""} onClick={() => { setTab("documents"); setChecked([]); }}>Документы<b>{documents.length}</b></button>
    </nav>
    {tab === "documents" ? <section className="return-documents">{documents.map(doc => <article key={doc.id}><button onClick={() => { setIntakeID(doc.id); setCreating(true); }}><strong>Возврат №{doc.documentNumber} · {doc.createdAt ? new Date(doc.createdAt).toLocaleDateString("ru-RU") : ""}</strong><span>{channels[doc.data.channel]} · {doc.data.rows.reduce((sum,row) => sum + row.quantity,0)} растений</span><small>{doc.data.rows.map(row => `${row.name} × ${row.quantity} · ${conditions[row.condition].label}`).join("; ")}</small></button>{can("returns.delete") && <button aria-label={`Удалить документ ${doc.documentNumber}`} className="return-delete" disabled={busy} onClick={() => remove(doc.id)}>×</button>}</article>)}{!documents.length && <p className="returns-empty">Сохранённые документы появятся здесь после приёмки.</p>}{can("returns.delete") && documents.length > 0 && <button className="return-delete" disabled={busy} onClick={() => remove(undefined, true)}>Удалить все записи раздела</button>}</section> : <>
    <div className="returns-filters"><label className="returns-search"><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название или артикул экземпляра" /></label><select aria-label="Канал возврата" value={channel} onChange={(event) => setChannel(event.target.value)}><option value="">Все каналы</option>{Object.entries(channels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
    <div className="return-bulk-actions"><label><input type="checkbox" aria-label="Выбрать все растения" checked={visible.length > 0 && picked.length === visible.length} onChange={event => setChecked(event.target.checked ? visible.map(item => item.id) : [])} /> Выбрать все</label>{picked.length > 0 && <span>Выбрано: {picked.length}</span>}
      {tab === "ready" && can("returns.receipt.create") && <button className="returns-action" disabled={busy || !picked.some(item => item.receiptStatus === "none")} onClick={() => void perform(async () => { await api("/api/v1/admin/returns/receipt-batch", { method: "POST", body: JSON.stringify({ ids: picked.map(item => item.id) }) }); setSelectedID(picked.find(item => item.receiptStatus === "none")?.id); })}>Создать поступление</button>}
      {tab === "restoring" && <><label><input type="checkbox" checked={includeDate} onChange={event => setIncludeDate(event.target.checked)} /> Печатать дату</label><button disabled={busy || !picked.length} onClick={() => void perform(() => downloadReturnLabels(picked.map(item => item.id), includeDate))}>Скачать этикетки</button><a href="/discounts" target="_blank" rel="noreferrer">Растения со скидкой ↗</a></>}
      {can("returns.delete") && <><button className="return-delete" disabled={busy || !picked.length} onClick={() => remove()}>Удалить выбранные</button><button className="return-delete" disabled={busy || !items.length} onClick={() => remove(undefined, true)}>Удалить все записи</button></>}
    </div>
    <div className="returns-layout">
      <section className="returns-list" aria-label="Список возвратов">
        {loading && <p className="returns-empty">Загружаем журнал…</p>}
        {!loading && visible.map((item) => <div className="return-unit" key={item.id}><input type="checkbox" aria-label={`Выбрать ${item.article || item.id}`} checked={checked.includes(item.id)} onChange={event => setChecked(current => event.target.checked ? [...current, item.id] : current.filter(id => id !== item.id))} /><button className={selected?.id === item.id ? "selected" : ""} onClick={() => setSelectedID(item.id)}>
          <span className={`return-state ${conditions[item.condition].className}`}>{conditions[item.condition].icon}</span><span className="return-card-main"><strong>{item.productName}{item.label && ` · ${item.label}`}</strong><small>{returnMoney(item.discountedPrice ?? item.price)} · {item.article}</small><small>{channels[item.channel]}{item.condition === "ready" && <> · {item.receiptExternalUrl ? "Поступление создано" : receiptLabels[item.receiptStatus]}</>}</small></span><span className="return-card-tail"><time>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "short" })}</time><em>{conditions[item.condition].label}</em></span>
        </button></div>)}
        {!loading && !visible.length && <p className="returns-empty">В этой группе возвратов нет.</p>}
      </section>
      <ReturnDetails key={selected?.id} item={selected} canEdit={can("returns.edit")} canReceipt={can("returns.receipt.create")} onSell={(id) => { void perform(async () => { await api("/api/v1/admin/returns/receipt-batch", { method: "POST", body: JSON.stringify({ ids: [id], sell: true }) }); setTab("ready"); setSelectedID(id); }); }} onSaved={(item) => { setItems(current => current.map(old => old.id === item.id ? item : old)); setSelectedID(item.id); void refresh().catch(error => onError(error.message)); }} onError={onError} />
    </div>
    </>}
    {creating && <ReturnIntakeDialog intakeID={intakeID} canReceipt={can("returns.receipt.create")} onClose={() => setCreating(false)} onCreated={(created) => { setCreating(false); setItems((current) => [...created, ...current.filter(item => !created.some(next => next.id === item.id))]); setSelectedID(created[0]?.id); setTab("documents"); void refresh().catch(error => onError(error.message)); }} />}
  </div>;
}

function ReturnDetails({ item, canEdit, canReceipt, onSaved, onSell, onError }: { item?: MarketplaceReturn; canEdit: boolean; canReceipt: boolean; onSaved: (item: MarketplaceReturn) => void; onSell: (id: number) => void; onError: (message: string) => void }) {
  const [discount, setDiscount] = useState(item?.discountPercent || 0);
  const [comment, setComment] = useState(item?.comment || ""); const [busy, setBusy] = useState(false);
  if (!item) return <aside className="return-detail returns-empty">Выберите возврат слева.</aside>;
  const save = async (condition = item.condition) => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}`, { method: "PATCH", body: JSON.stringify({ condition, comment, discountPercent: discount }) }); onSaved(result.item); return true; } catch (error) { onError(error instanceof Error ? error.message : "Не удалось сохранить оценку"); return false; } finally { setBusy(false); } };
  const receipt = async () => { setBusy(true); try { const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/receipt`, { method: "POST" }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось создать поступление"); } finally { setBusy(false); } };
  const photo = async (file?: File) => { if (!file) return; setBusy(true); try { const data = await returnPhotoBase64(file); const result = await api<{ item: MarketplaceReturn }>(`/api/v1/admin/returns/${item.id}/photos`, { method: "POST", body: JSON.stringify({ contentType: "image/jpeg", data }) }); onSaved(result.item); } catch (error) { onError(error instanceof Error ? error.message : "Не удалось добавить фото"); } finally { setBusy(false); } };
  return <aside className="return-detail">
    <header><div><span className={`return-state ${conditions[item.condition].className}`}>{conditions[item.condition].icon}</span><p>Возврат №{item.id}</p><h2>{item.productName}{item.label && ` · ${item.label}`}</h2><small>{item.article} · {returnMoney(item.price)}</small></div><span className={`return-finance ${item.financialStatus}`}>{item.financialStatus === "linked" ? "Продажа связана" : "Нужна финансовая сверка"}</span></header>
    <dl><div><dt>Канал</dt><dd>{channels[item.channel]}{item.channelComment && <small className="return-channel-comment">{item.channelComment}</small>}</dd></div><div><dt>Получено</dt><dd>{new Date(`${item.returnedAt}T00:00:00`).toLocaleDateString("ru-RU")}</dd></div><div><dt>Цена экземпляра</dt><dd>{returnMoney(item.discountedPrice ?? item.price)}</dd></div><div><dt>Себестоимость</dt><dd>{item.unitCost == null ? "Неизвестна" : `${item.unitCost.toLocaleString("ru-RU")} ₽`}</dd></div></dl>
    <section><h3>Оценка растения</h3><div className="return-condition-grid">{(Object.entries(conditions) as Array<[MarketplaceReturn["condition"], typeof conditions.inspection]>).map(([value, meta]) => <button key={value} disabled={!canEdit || busy} className={`${item.condition === value ? "active" : ""} ${meta.className}`} onClick={() => void save(value)}><b>{meta.icon}</b><span><strong>{meta.label}</strong><small>{meta.note}</small></span></button>)}</div></section>
    {item.condition === "restoring" && <section><label className="return-comment"><span>Скидка для этого экземпляра, %</span><input type="number" aria-label="Скидка экземпляра" min={0} max={100} value={discount} disabled={!canEdit || busy} onChange={event => setDiscount(Math.max(0, Math.min(100, Math.round(Number(event.target.value)))))} /><strong>На этикетке: {returnMoney((item.price || 0) * (100 - discount) / 100)}</strong></label><p>Скидка в кассе указывается вручную.</p>{canReceipt && canEdit && <button className="returns-action" disabled={busy || item.receiptStatus !== "none"} onClick={() => void save().then(saved => { if (saved) onSell(item.id); })}>Продать</button>}</section>}
    <section><h3>Фото осмотра <small>{(item.photoIds || []).length}/6</small></h3><div className="return-photos">{(item.photoIds || []).map((id) => <img key={id} src={`/api/v1/admin/return-photos/${id}`} alt="Фото возвращённого растения" />)}{canEdit && (item.photoIds || []).length < 6 && <label><input type="file" accept="image/jpeg,image/png,image/webp" onChange={event => { void photo(event.target.files?.[0]); event.target.value = ""; }} disabled={busy} /><b>＋</b><span>Добавить фото</span></label>}</div></section>
    <section><label className="return-comment"><span>Комментарий осмотра</span><textarea value={comment} onChange={(event) => setComment(event.target.value)} disabled={!canEdit || busy} placeholder="Что произошло с растением и что делать дальше" /></label>{canEdit && <button className="return-save" disabled={busy || comment === item.comment && discount === (item.discountPercent || 0)} onClick={() => void save()}>Сохранить оценку</button>}</section>
    {item.condition === "ready" && <section className="return-receipt"><div><h3>Поступление в СБИС</h3><p>{item.receiptExternalUrl ? "Поступление создано" : receiptLabels[item.receiptStatus]}</p><small>Проведите поступление в СБИС перед продажей.</small>{item.receiptStatus === "correction_required" && <small>Состояние изменилось. Проверьте состав документа в СБИС.</small>}</div>{item.receiptExternalUrl ? <a className="returns-action" href={item.receiptExternalUrl} target="_blank" rel="noreferrer">Открыть поступление ↗</a> : canReceipt && <button className="returns-action" disabled={busy || ["failed","correction_required"].includes(item.receiptStatus)} onClick={() => void (item.receiptStatus === "none" ? receipt() : api<{item:MarketplaceReturn}>(`/api/v1/admin/returns/${item.id}`).then(result => onSaved(result.item)))}>{["queued", "checking"].includes(item.receiptStatus) ? "Проверить операцию" : "Создать поступление"}</button>}</section>}
    {(item.history || []).length > 1 && <details className="return-history"><summary>История оценки · {(item.history || []).length}</summary>{(item.history || []).map((entry, index) => <p key={`${entry.createdAt}-${index}`}><time>{new Date(entry.createdAt).toLocaleString("ru-RU")}</time><b>{conditions[entry.to as keyof typeof conditions]?.label || entry.to}</b>{entry.comment && <span>{entry.comment}</span>}</p>)}</details>}
  </aside>;
}
