import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";
import type { NomenclatureCandidate, ProcurementProduct, ProcurementRequest, ProcurementSettings, ProcurementSupplier } from "./adminTypes";

export const availabilityLabel = (value: string) => ({ available: "Есть", check: "Проверить", temporarily_unavailable: "Временно нет", discontinued: "Снят с продажи", unknown: "Неизвестно" }[value] || value);

export const salesChannelLabel = (value: string) => ({ site: "Сайт", saby: "СБИС / магазин", wb: "Wildberries", ozon: "Ozon" }[value] || value);

export const salesSyncLabel = (value: string) => ({ pending: "Ожидает первой загрузки", running: "Обновляется", ok: "Актуально", error: "Ошибка", disabled: "Не подключено" }[value] || value);

export const recommendationStatusLabel = (value: string) => ({ recommended: "К заказу", already_ordered: "Уже заказано", check_availability: "Проверить наличие", supplier_unavailable: "Нет у поставщика", excluded: "Не закупаем" }[value] || value);

export const recommendationEmptyTitle = (value: string) => ({ recommended: "Дефицита нет", already_ordered: "Товаров в пути нет", check_availability: "Проверять нечего", supplier_unavailable: "У поставщиков всё есть", excluded: "Ничего не снято с закупки" }[value] || "Список пуст");

export const recommendationEmptyText = (value: string) => value === "recommended" ? "Текущий остаток и товары в пути покрывают рассчитанный спрос." : value === "already_ordered" ? "Здесь появятся позиции из действующих закупок, чтобы не заказать их повторно." : value === "check_availability" ? "Отметьте кнопкой «Проверить наличие» то, чего у поставщика может не оказаться." : value === "supplier_unavailable" ? "Растения, которых у поставщика нет, уходят сюда и не мешают собирать заказ." : "Снятое с закупки решением магазина видно здесь и в заказ не попадает.";

export const integrationChannelLabel = (value: string) => ({ saby: "СБИС / Saby", wb: "Wildberries", ozon: "Ozon" }[value] || value);

export async function updateAvailability(supplierId: number, sabyId: string, status: string, reload: () => Promise<unknown>, onError: (message: string) => void) {
  const date = status === "check" ? new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10) : "";
  try { await api("/api/v1/admin/procurement/availability", { method: "PATCH", body: JSON.stringify({ supplierId, sabyId, status, checkAfter: date }) }); await reload(); }
  catch (error) { onError((error as Error).message); }
}

export async function setExclusion(sabyId: string, excluded: boolean, reason: string, reload: () => Promise<unknown>, onError: (message: string) => void) {
  const note = excluded ? reason.trim() : "";
  try { await api("/api/v1/admin/procurement/exclusions", { method: "PUT", body: JSON.stringify({ sabyId, excluded, reason: note }) }); await reload(); }
  catch (error) { onError((error as Error).message); }
}

export async function updateRequestStatus(item: ProcurementRequest, status: string, reload: () => Promise<unknown>, onError: (value: string) => void) {
  try {
    await api(`/api/v1/admin/procurement/requests/${item.id}`, { method: "PATCH", body: JSON.stringify({ sabyId: item.sabyId, requestedName: item.requestedName, quantity: item.quantity, status, notes: item.notes }) });
    await reload();
  } catch (error) { onError((error as Error).message); }
}

export function ProcurementProducts({ suppliers, onError }: { suppliers: ProcurementSupplier[]; onError: (value: string) => void }) {
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0); const [query, setQuery] = useState("");
  const [items, setItems] = useState<ProcurementProduct[]>([]); const [editing, setEditing] = useState<ProcurementProduct | null>(null);
  const [loading, setLoading] = useState(false);
  const load = useCallback(() => { setLoading(true); return api<{ items: ProcurementProduct[] }>(`/api/v1/admin/procurement/products?supplierId=${supplierId}&q=${encodeURIComponent(query)}`).then((result) => setItems(result.items)).catch((error) => onError((error as Error).message)).finally(() => setLoading(false)); }, [supplierId, query, onError]);
  useEffect(() => { const timer = window.setTimeout(() => void load(), 200); return () => window.clearTimeout(timer); }, [load]);
  return <section className="admin-block procurement-block"><div className="admin-block-heading"><div><p className="eyebrow">Канонические товары</p><h2>Справочник закупки</h2></div><span className="admin-pill">{items.length} товаров</span></div>
    <div className="admin-toolbar"><select value={supplierId} onChange={(event) => setSupplierId(Number(event.target.value))}>{suppliers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название, код или артикул" /><span>{loading ? "Загружаем…" : "Все ключи инвойса хранятся у товара"}</span></div>
    {items.length ? <div className="admin-table-wrap"><table className="admin-table procurement-directory"><thead><tr><th>Карточка Saby</th><th>Поставщик</th><th>Каналы</th><th>Сопоставления инвойса</th><th>Заказ</th><th>Наличие</th><th></th></tr></thead><tbody>{items.map((item) => <tr key={`${item.supplierId}-${item.sabyId}`}><td><strong>{item.name}</strong><small>Код Saby: {item.sabyCode || "—"}</small><small>ID карточки: {item.sabyId} · остаток {item.balance}</small></td><td><strong>{item.supplierArticle || item.hollandArticle || "Артикул не заполнен"}</strong><small>{item.supplierName}</small></td><td><small>WB nmID: {item.wbNmId || "—"}</small><small>WB артикул: {item.wbVendorCode || "—"}</small><small>Ozon: {item.ozonOfferId || "—"}</small></td><td>{item.aliases.slice(0, 3).map((alias, index) => <small key={item.aliasIds[index] || `${alias}-${index}`}>Из инвойса: {alias}</small>)}{item.aliases.length > 3 && <small>ещё {item.aliases.length - 3}</small>}</td><td><small>минимум {item.minimumOrderQty}</small><small>кратно {item.orderMultiple}</small></td><td>{availabilityLabel(item.availabilityStatus)}</td><td><button className="table-action" onClick={() => setEditing(item)}>Изменить</button></td></tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Товары не найдены</strong><span>Сопоставьте хотя бы одно название из инвойса с товаром СБИС.</span></div>}
    {editing && <ProcurementProductDialog item={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void load(); }} onError={onError} />}
  </section>;
}

export function ProcurementProductDialog({ item, onClose, onSaved, onError }: { item: ProcurementProduct; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [draft, setDraft] = useState(item); const [saving, setSaving] = useState(false); const [editingAlias, setEditingAlias] = useState<number | null>(null);
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/products", { method: "PUT", body: JSON.stringify({ sabyId: draft.sabyId, supplierId: draft.supplierId, supplierArticle: draft.supplierArticle, availabilityStatus: draft.availabilityStatus, checkAfter: draft.checkAfter, hollandArticle: draft.hollandArticle, wbNmId: draft.wbNmId || null, wbVendorCode: draft.wbVendorCode, ozonOfferId: draft.ozonOfferId, minimumOrderQty: draft.minimumOrderQty, orderMultiple: draft.orderMultiple }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="product-directory-title"><header><div><p className="eyebrow">{item.supplierName}</p><h2 id="product-directory-title">{item.name}</h2></div><button onClick={onClose} aria-label="Закрыть">×</button></header><div className="admin-form-grid">
    <label>Артикул поставщика<input value={draft.supplierArticle} onChange={(event) => setDraft({ ...draft, supplierArticle: event.target.value })} /></label><label>Артикул Голландии<input value={draft.hollandArticle} onChange={(event) => setDraft({ ...draft, hollandArticle: event.target.value })} /></label>
    <label>WB nmID<input type="number" value={draft.wbNmId || ""} onChange={(event) => setDraft({ ...draft, wbNmId: event.target.value ? Number(event.target.value) : undefined })} /></label><label>Артикул продавца WB<input value={draft.wbVendorCode} onChange={(event) => setDraft({ ...draft, wbVendorCode: event.target.value })} /></label><label>Ozon offer_id<input value={draft.ozonOfferId} onChange={(event) => setDraft({ ...draft, ozonOfferId: event.target.value })} /></label>
    <label>Минимум для заказа<input type="number" min="1" value={draft.minimumOrderQty} onChange={(event) => setDraft({ ...draft, minimumOrderQty: Number(event.target.value) })} /></label><label>Заказывать кратно<input type="number" min="1" value={draft.orderMultiple} onChange={(event) => setDraft({ ...draft, orderMultiple: Number(event.target.value) })} /></label>
    <label>Наличие<select value={draft.availabilityStatus} onChange={(event) => setDraft({ ...draft, availabilityStatus: event.target.value })}><option value="available">Есть</option><option value="check">Проверить</option><option value="temporarily_unavailable">Временно нет</option><option value="discontinued">Снят с продажи</option><option value="unknown">Неизвестно</option></select></label><label>Проверить после<input type="date" value={draft.checkAfter} onChange={(event) => setDraft({ ...draft, checkAfter: event.target.value })} /></label>
    <div className="wide admin-field"><span className="admin-field-label">Сопоставления: позиция инвойса → карточка Saby</span><p className="admin-hint">Код Saby <strong>{draft.sabyCode || "—"}</strong> — видимый код товара. Для документов и остатков используется ID карточки <strong>{draft.sabyId}</strong>.</p>{draft.aliases.map((alias, index) => <div className="procurement-alias-edit-row" key={draft.aliasIds[index] || `${alias}-${index}`}><div><small>Из инвойса</small><strong>{alias}</strong><small>Сейчас: {draft.name} · код {draft.sabyCode || "—"} · ID {draft.sabyId}</small></div>{draft.aliasIds[index] && <button type="button" className="table-action" onClick={() => setEditingAlias(index)}>Выбрать карточку Saby</button>}</div>)}</div>
    {editingAlias != null && draft.aliasIds[editingAlias] && <ProcurementAliasReassign aliasId={draft.aliasIds[editingAlias]} aliasName={draft.aliases[editingAlias]} currentSabyId={draft.sabyId} onCancel={() => setEditingAlias(null)} onSaved={onSaved} onError={onError} />}
  </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={saving} onClick={save}>{saving ? "Сохраняем…" : "Сохранить"}</button></div></div></>;
}

function ProcurementAliasReassign({ aliasId, aliasName, currentSabyId, onCancel, onSaved, onError }: { aliasId: number; aliasName: string; currentSabyId: string; onCancel: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [query, setQuery] = useState(aliasName); const [items, setItems] = useState<NomenclatureCandidate[]>([]); const [searching, setSearching] = useState(false); const [saving, setSaving] = useState(false);
  useEffect(() => { if (query.trim().length < 2) { setItems([]); return; } const timer = window.setTimeout(() => { setSearching(true); api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`).then((result) => setItems(result.items)).catch((error) => onError((error as Error).message)).finally(() => setSearching(false)); }, 250); return () => window.clearTimeout(timer); }, [query, onError]);
  const choose = async (sabyId: string) => { setSaving(true); try { await api(`/api/v1/admin/procurement/aliases/${aliasId}`, { method: "PATCH", body: JSON.stringify({ matchStatus: "confirmed", sabyId }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <section className="wide procurement-alias-reassign"><div className="admin-block-heading"><div><small>Позиция из инвойса</small><strong>{aliasName}</strong></div><button type="button" onClick={onCancel}>Отмена</button></div><label>Найти карточку по названию, коду Saby или числовому ID<input value={query} onChange={(event) => setQuery(event.target.value)} autoFocus /></label><p className="admin-hint">В списке показываются только действующие карточки с числовым ID — именно по нему Saby возвращает остаток и принимает строки поступления.</p><div className="procurement-candidates">{searching ? <span>Ищем…</span> : items.map((candidate) => <article key={candidate.sabyId}><div><strong>{candidate.name}</strong><span>Код Saby: {candidate.code || "—"} · ID карточки: {candidate.sabyId}</span><small>Остаток: {candidate.balance}</small></div><button type="button" disabled={saving || candidate.sabyId === currentSabyId} onClick={() => void choose(candidate.sabyId)}>{candidate.sabyId === currentSabyId ? "Сейчас выбрано" : "Привязать"}</button></article>)}</div></section>;
}

export function ProcurementSettingsPanel({ settings, onSaved, onError }: { settings: ProcurementSettings; onSaved: () => void; onError: (value: string) => void }) {
  const [draft, setDraft] = useState(settings); const [saving, setSaving] = useState(false);
  const number = (key: keyof ProcurementSettings, value: string) => setDraft((current) => ({ ...current, [key]: Number(value) }));
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/settings", { method: "PUT", body: JSON.stringify(draft) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <section className="admin-block procurement-block"><div className="admin-block-heading"><div><p className="eyebrow">Версия {settings.version}</p><h2>Формула цены</h2></div><button className="admin-primary" disabled={saving} onClick={save}>{saving ? "Сохраняем…" : "Сохранить новую версию"}</button></div>
    <p className="admin-hint procurement-note">Проценты вводятся как 5%, 46%, 8%. Уже рассчитанные поставки сохраняют снимок прежней версии.</p>
    <div className="procurement-settings-grid">
      <PercentField label="Возвраты" value={draft.returnLossRate} onChange={(value) => setDraft({ ...draft, returnLossRate: value })} />
      <PercentField label="Расходы маркетплейсов" value={draft.marketplaceCostRate} onChange={(value) => setDraft({ ...draft, marketplaceCostRate: value })} />
      <PercentField label="Налог" value={draft.taxRate} onChange={(value) => setDraft({ ...draft, taxRate: value })} />
      <PercentField label="Резерв" value={draft.reserveRate} onChange={(value) => setDraft({ ...draft, reserveRate: value })} />
      <label>Упаковка, ₽<input type="number" value={draft.packageRub} onChange={(event) => number("packageRub", event.target.value)} /></label>
      <label>Логистика маркетплейса, ₽ за см высоты<input type="number" step="0.5" min="0" value={draft.marketplaceLogisticsPerCm} onChange={(event) => number("marketplaceLogisticsPerCm", event.target.value)} /></label>
      <PercentField label="Менять цену при отклонении более" value={draft.priceChangeThreshold} onChange={(value) => setDraft({ ...draft, priceChangeThreshold: value })} />
      <PercentField label="Наценка на закупочную стоимость" value={draft.retailMarkupMultiplier - 1} onChange={(value) => setDraft({ ...draft, retailMarkupMultiplier: 1 + value })} />
      <PercentField label="Цена МП без скидки" value={draft.marketplaceStrikeMarkup} onChange={(value) => setDraft({ ...draft, marketplaceStrikeMarkup: value })} />
      <label>Период анализа продаж, дней<input type="number" value={draft.recommendationDays} onChange={(event) => number("recommendationDays", event.target.value)} /></label>
      <label>Закупаем запас на, дней<input type="number" value={draft.targetCoverDays} onChange={(event) => number("targetCoverDays", event.target.value)} /></label>
      <label className="admin-checkbox"><input type="checkbox" checked={draft.roundPrices} onChange={(event) => setDraft({ ...draft, roundPrices: event.target.checked })} />Округлять цены до ближайших 50 или 90</label>
    </div>
  </section>;
}

export function PercentField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label>{label}, %<input type="number" step="0.1" value={Math.round(value * 10000) / 100} onChange={(event) => onChange(Number(event.target.value) / 100)} /></label>;
}
