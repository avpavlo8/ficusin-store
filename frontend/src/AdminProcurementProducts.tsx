import { useProcurementDialog, useProcurementError } from "./procurementUX";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { api, money } from "./adminShared";
import type { NomenclatureCandidate, ProcurementProduct, ProcurementSupplier, SabyCatalogFolder } from "./adminTypes";
import { availabilityLabel } from "./AdminProcurementPanels";

export function ProcurementProducts({ suppliers, onError }: { suppliers: ProcurementSupplier[]; onError: (value: string) => void }) {
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0);
  const [query, setQuery] = useState("");
  const [page,setPage]=useState(0);
  const [total,setTotal]=useState(0);
  const [folderCounts,setFolderCounts]=useState<Record<string,number>>({});
  const [selectedFolderId, setSelectedFolderId] = useState("");
  const [expandedFolders, setExpandedFolders] = useState<Record<string, boolean>>({});
  const [folders, setFolders] = useState<SabyCatalogFolder[]>([]);
  const [items, setItems] = useState<ProcurementProduct[]>([]);
  const [editing, setEditing] = useState<ProcurementProduct | null>(null);
  const [importing, setImporting] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");

  const load = useCallback(() => {
    setLoading(true);
    return api<{ items: ProcurementProduct[]; folders: SabyCatalogFolder[]; total?: number; folderCounts?: Record<string,number> }>(`/api/v1/admin/procurement/products?supplierId=${supplierId}&q=${encodeURIComponent(query)}&folder=${encodeURIComponent(selectedFolderId)}&limit=100&offset=${page * 100}`)
      .then((result) => {
        setItems(result.items || []);
        setTotal(result.total ?? result.items?.length ?? 0);setFolderCounts(result.folderCounts || {});
        setLoadError("");
        setFolders(result.folders || []);
        setExpandedFolders((current) => {
          if (Object.keys(current).length) return current;
          return Object.fromEntries((result.folders || []).filter((folder) => !folder.parentId).map((folder) => [folder.id, true]));
        });
      })
      .catch((error) => { setLoadError((error as Error).message); onError(`Каталог СБИС: ${(error as Error).message}`); })
      .finally(() => setLoading(false));
  }, [supplierId, query, onError, selectedFolderId, page]);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 200);
    return () => window.clearTimeout(timer);
  }, [load]);

  const folderIDs = new Set(folders.map((folder) => folder.id));
  const rootFolders = folders.filter((folder) => !folder.parentId || !folderIDs.has(folder.parentId));
  const childrenOf = (parentId: string) => folders.filter((folder) => folder.parentId === parentId);
  const visibleItems = items;
  const countInFolder = (folderId: string) => folderCounts[folderId] ?? 0;

  const renderFolder = (folder: SabyCatalogFolder, depth: number): ReactNode => {
    const children = childrenOf(folder.id);
    const expanded = expandedFolders[folder.id] !== false;
    return <div key={folder.id}>
      <button
        type="button"
        className={`procurement-folder-row ${selectedFolderId === folder.id ? "active" : ""}`}
        style={{ paddingLeft: `${10 + depth * 16}px` }}
        onClick={() => {
          setSelectedFolderId(folder.id);setPage(0);
          if (children.length) setExpandedFolders((current) => ({ ...current, [folder.id]: !expanded }));
        }}
      >
        <span className="procurement-folder-toggle">{children.length ? (expanded ? "▾" : "›") : "·"}</span>
        <span>{folder.name}</span>
        <small>{countInFolder(folder.id)}</small>
      </button>
      {children.length > 0 && expanded && children.map((child) => renderFolder(child, depth + 1))}
    </div>;
  };

  const importToSite = async (item: ProcurementProduct) => {
    if (!item.sabyCode) {
      onError("У товара нет кода СБИС для импорта на сайт");
      return;
    }
    setImporting(item.sabyId);
    try {
      await api("/api/v1/admin/products/import", {
        method: "POST",
        body: JSON.stringify({ codes: [item.sabyCode], dryRun: false }),
      });
      await load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setImporting("");
    }
  };

  return <section className="admin-block procurement-block">
    <div className="admin-block-heading"><div><p className="eyebrow">Зеркало Saby</p><h2>Товары и связи каналов</h2></div><span className="admin-pill">{total} товаров</span></div>
    <p className="admin-hint procurement-note">Поиск и папки работают по всему каталогу СБИС. По 100 товаров на странице.</p>
    {loadError && <div className="procurement-product-error" role="alert"><strong>Не удалось загрузить товары СБИС</strong><span>{loadError}</span><button onClick={() => void load()}>Повторить</button></div>}
    <div className="admin-toolbar procurement-mirror-toolbar">
      <select value={supplierId} onChange={(event) => {setSupplierId(Number(event.target.value));setPage(0);}}>{suppliers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select>
      <input value={query} onChange={(event) => {setQuery(event.target.value);setPage(0);}} placeholder="Поиск по названию, X-коду, ID или артикулу" />
      <span>{loading ? "Обновляем зеркало…" : query.trim() ? "Поиск по всему каталогу Saby" : "Папки синхронизируются из Saby"}</span>
    </div>

    <div className="procurement-catalog-mirror">
      <aside className="procurement-folder-tree" aria-label="Папки каталога Saby">
        <button type="button" className={`procurement-folder-row root ${selectedFolderId === "" ? "active" : ""}`} onClick={() => {setSelectedFolderId("");setPage(0);}}>
          <span className="procurement-folder-toggle">⌂</span><span>Весь каталог</span><small>{Object.values(folderCounts).reduce((sum,count)=>sum+count,0)}</small>
        </button>
        {rootFolders.map((folder) => renderFolder(folder, 0))}
        {!folders.length && !loading && <p className="admin-hint">Дерево появится после следующей синхронизации каталога Saby.</p>}
      </aside>

      <div className="procurement-mirror-content">
        {selectedFolderId && !query.trim() && <div className="procurement-folder-caption">
          <strong>{folders.find((folder) => folder.id === selectedFolderId)?.name || "Папка Saby"}</strong>
          <button type="button" onClick={() => setSelectedFolderId("")}>Показать весь каталог</button>
        </div>}
        {loading && !items.length ? <div className="procurement-zero" role="status"><strong>Загружаем каталог СБИС…</strong><span>Первая страница появится здесь.</span></div> : visibleItems.length ? <div className="admin-table-wrap"><table className="admin-table procurement-directory"><thead><tr><th>Товар Saby</th><th>СБИС</th><th>Сайт</th><th>WB / продажи</th><th>Ozon / продажи</th><th>Поставщик / закупка</th><th></th></tr></thead><tbody>{visibleItems.map((item) => <tr key={`${item.supplierId}-${item.sabyId}`}>
          <td><strong>{item.name}</strong><small>{item.sectionPath?.join(" / ") || "Корень каталога"}</small><small>Продажи: магазин {item.sabySales} · сайт {item.siteSales}</small></td>
          <td><strong>{item.sabyCode || "Код не заполнен"}</strong><details><summary>Технический ID</summary>{item.sabyId || "—"}</details><small>Артикул: {item.sabyArticle || "—"}</small><small>{item.currentPriceRub.toLocaleString("ru-RU")} ₽ · остаток {item.balance}</small></td>
          <td>{item.variantId > 0 ? <><strong>{item.siteStatus === "published" ? "Опубликован" : item.siteStatus === "draft" ? "Черновик" : item.siteStatus || "На сайте"}</strong></> : <><strong>Нет на сайте</strong><small>Карточка Saby уже доступна для импорта</small><button type="button" className="table-action" disabled={importing === item.sabyId} onClick={() => void importToSite(item)}>{importing === item.sabyId ? "Добавляем…" : "Создать черновик"}</button></>}</td>
          <td>{(item.wbArticles || []).map((article) => <small key={`wb-${article}`}>{article}</small>)}{!(item.wbArticles || []).length && <small>Не связан</small>}{item.wbVendorCode && !item.wbNmId && <small>Артикул сохранён, но WB nmID ещё не найден — обновите зеркало WB</small>}{item.wbNmId && <small>API-связь готова</small>}<strong>{item.wbSales} продаж</strong>{(item.wbLegacyArticles || []).map((article) => <small key={`wb-legacy-${article}`}>Архивный: {article}</small>)}</td>
          <td>{(item.ozonArticles || []).map((article) => <small key={`ozon-${article}`}>{article}</small>)}{!(item.ozonArticles || []).length && <small>Не связан</small>}<strong>{item.ozonSales} продаж</strong>{(item.ozonLegacyArticles || []).map((article) => <small key={`ozon-legacy-${article}`}>Архивный: {article}</small>)}</td>
          <td><strong>{item.supplierName}</strong><small>{item.supplierArticle || item.hollandArticle || "Артикул поставщика не заполнен"}</small><small>{availabilityLabel(item.availabilityStatus)}</small><small>{item.supplierCategory || item.aliases[0] || "Закупок пока нет"}{item.expectedUnitPrice ? ` · ${item.expectedUnitPrice.toLocaleString("ru-RU",{minimumFractionDigits:2,maximumFractionDigits:4})} €` : ""}</small>{item.suggestedMarketplaceRub != null ? <><strong>Цена МП: {money.format(item.suggestedMarketplaceRub)}</strong><small>{item.latestOrderNumber ? `по закупке ${item.latestOrderNumber}` : "по последней рассчитанной закупке"}{item.latestUnitCostRub != null ? ` · себестоимость ${money.format(item.latestUnitCostRub)}` : ""}</small>{item.suggestedMarketplaceStrikeRub != null && item.suggestedMarketplaceStrikeRub > item.suggestedMarketplaceRub && <small>До скидки: {money.format(item.suggestedMarketplaceStrikeRub)}</small>}</> : <small>Цена МП: расчёта ещё нет</small>}</td>
          <td><button className="table-action" onClick={() => setEditing(item)}>Настроить товар</button></td>
        </tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Товары не найдены</strong><span>{query.trim() ? "Измените строку поиска." : "В этой папке пока нет товаров."}</span></div>}
      </div>
    </div>
    <div className="procurement-filter-bar"><button disabled={page === 0 || loading} onClick={()=>setPage(page-1)}>Назад</button><span>{total ? `${page*100+1}–${Math.min((page+1)*100,total)} из ${total}` : "0 товаров"}</span><button disabled={(page+1)*100>=total || loading} onClick={()=>setPage(page+1)}>Далее</button></div>
    {editing && <ProcurementProductDialog item={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void load(); }} onError={onError} />}
  </section>;
}

export function ProcurementProductDialog({ item, onClose, onSaved, onError: externalError }: { item: ProcurementProduct; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const {error:dialogError,errorRef,notify:onError,clear:clearError}=useProcurementError(externalError);
  const dialogRef = useProcurementDialog(() => {if (!saving && (JSON.stringify(draft) === JSON.stringify(item) || window.confirm("Закрыть без сохранения изменений?"))) onClose();});
  const [draft, setDraft] = useState(item); const [saving, setSaving] = useState(false); const [editingAlias, setEditingAlias] = useState<number | null>(null);
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/products", { method: "PUT", body: JSON.stringify({ variantId: draft.variantId, sabyId: draft.sabyId, supplierId: draft.supplierId, supplierArticle: draft.supplierArticle, availabilityStatus: draft.availabilityStatus, checkAfter: draft.checkAfter, hollandArticle: draft.hollandArticle, wbNmId: draft.wbNmId || null, wbVendorCode: draft.wbVendorCode, ozonOfferId: draft.ozonOfferId, minimumOrderQty: draft.minimumOrderQty, orderMultiple: draft.orderMultiple }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button tabIndex={-1} className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="product-directory-title"><header><div><p className="eyebrow">{item.supplierName}</p><h2 id="product-directory-title">Настроить товар</h2><p>{item.name}</p></div><button onClick={onClose} aria-label="Закрыть">×</button></header>{dialogError && <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert"><strong>Не удалось сохранить товар</strong><span>{dialogError}</span><button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>×</button></div>}<div className="admin-form-grid">
    <h3 className="wide">Закупка у поставщика</h3><label>Артикул поставщика<input value={draft.supplierArticle} onChange={(event) => setDraft({ ...draft, supplierArticle: event.target.value })} /></label><label>Артикул Голландии<input value={draft.hollandArticle} onChange={(event) => setDraft({ ...draft, hollandArticle: event.target.value })} /></label>
    <h3 className="wide">Связи маркетплейсов</h3><label>Артикул продавца WB<input value={draft.wbVendorCode} onChange={(event) => setDraft({ ...draft, wbVendorCode: event.target.value })} /><small>Числовой nmID подтягивается из зеркала WB автоматически и пользователю не показывается.</small></label><label>Артикул Ozon<input value={draft.ozonOfferId} onChange={(event) => setDraft({ ...draft, ozonOfferId: event.target.value })} /></label>
    <h3 className="wide">Количество и наличие</h3><label>Минимум для заказа<input type="number" min="1" value={draft.minimumOrderQty} onChange={(event) => setDraft({ ...draft, minimumOrderQty: Number(event.target.value) })} /></label><label>Заказывать кратно<input type="number" min="1" value={draft.orderMultiple} onChange={(event) => setDraft({ ...draft, orderMultiple: Number(event.target.value) })} /></label>
    <label>Наличие<select value={draft.availabilityStatus} onChange={(event) => setDraft({ ...draft, availabilityStatus: event.target.value })}><option value="available">Есть</option><option value="check">Проверить</option><option value="temporarily_unavailable">Временно нет</option><option value="discontinued">Снят с продажи</option><option value="unknown">Неизвестно</option></select></label><label>Проверить после<input type="date" value={draft.checkAfter} onChange={(event) => setDraft({ ...draft, checkAfter: event.target.value })} /></label>
    <div className="wide admin-field"><span className="admin-field-label">Сопоставления: позиция инвойса → наш товар</span><p className="admin-hint">Главный видимый код <strong>{draft.sabyCode || "—"}</strong>. Внутренние идентификаторы СБИС и PostgreSQL используются системой автоматически.</p>{draft.aliases.map((alias, index) => <div className="procurement-alias-edit-row" key={draft.aliasIds[index] || `${alias}-${index}`}><div><small>Из инвойса</small><strong>{alias}</strong><small>Сейчас: {draft.name} · код {draft.sabyCode || "—"}</small></div>{draft.aliasIds[index] && <button type="button" className="table-action" onClick={() => setEditingAlias(index)}>Выбрать наш товар</button>}</div>)}</div>
    {editingAlias != null && draft.aliasIds[editingAlias] && <ProcurementAliasReassign aliasId={draft.aliasIds[editingAlias]} aliasName={draft.aliases[editingAlias]} currentSabyId={draft.sabyId} onCancel={() => setEditingAlias(null)} onSaved={onSaved} onError={onError} />}
  </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={saving} onClick={save}>{saving ? "Сохраняем…" : "Сохранить"}</button></div></div></>;
}

function ProcurementAliasReassign({ aliasId, aliasName, currentSabyId, onCancel, onSaved, onError }: { aliasId: number; aliasName: string; currentSabyId: string; onCancel: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [query, setQuery] = useState(aliasName); const [items, setItems] = useState<NomenclatureCandidate[]>([]); const [searching, setSearching] = useState(false); const [saving, setSaving] = useState(false);
  useEffect(() => { if (query.trim().length < 2) { setItems([]); return; } const timer = window.setTimeout(() => { setSearching(true); api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`).then((result) => setItems(result.items)).catch((error) => onError((error as Error).message)).finally(() => setSearching(false)); }, 250); return () => window.clearTimeout(timer); }, [query, onError]);
  const choose = async (sabyId: string) => { setSaving(true); try { await api(`/api/v1/admin/procurement/aliases/${aliasId}`, { method: "PATCH", body: JSON.stringify({ matchStatus: "confirmed", sabyId }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <section className="wide procurement-alias-reassign"><div className="admin-block-heading"><div><small>Позиция из инвойса</small><strong>{aliasName}</strong></div><button type="button" onClick={onCancel}>Отмена</button></div><label>Найти наш товар по названию или коду X…<input value={query} onChange={(event) => setQuery(event.target.value)} autoFocus /></label><p className="admin-hint">Список берётся из единого справочника PostgreSQL. Остаток рядом — последнее значение зеркала СБИС.</p><div className="procurement-candidates">{searching ? <span>Ищем…</span> : items.map((candidate) => <article key={candidate.variantId}><div><strong>{candidate.name}</strong><span>Главный код: {candidate.code || "—"}</span><small>Остаток: {candidate.balance}</small></div><button type="button" disabled={saving || candidate.sabyId === currentSabyId} onClick={() => void choose(candidate.sabyId)}>{candidate.sabyId === currentSabyId ? "Сейчас выбрано" : "Привязать"}</button></article>)}</div></section>;
}
