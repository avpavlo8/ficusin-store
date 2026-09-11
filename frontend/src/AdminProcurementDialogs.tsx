import { useCallback, useEffect, useState } from "react";
import { normalizeProcurementOrderDetail } from "./AdminProcurement";
import { ConfirmDialog, api, money } from "./adminShared";
import type { NomenclatureCandidate, ProcurementActionBatch, ProcurementActionItem, ProcurementAlias, ProcurementOrder, ProcurementOrderDetail, ProcurementOrderLine, ProcurementRecommendation, ProcurementSupplier, ProcurementSettings } from "./adminTypes";

export function ProcurementPlanDialog({ suppliers, recommendations, settings, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; recommendations: ProcurementRecommendation[]; settings: ProcurementSettings; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  type NumericDraft = string;
  type DraftLine = { id: string; sabyId: string; sabyName: string; article: string; category: string; recommendedQty: number; packageCount: NumericDraft; unitsPerPackage: NumericDraft; expectedUnitPrice: NumericDraft; potDiameterCm: NumericDraft; heightCm: NumericDraft; loadUnit: string };
  type SavedPlan = { supplierId: number; exchangeRate: NumericDraft; deliveryToMoscowRub: NumericDraft; deliveryToRyazanRub: NumericDraft; items: DraftLine[] };
  const draftStorageKey = "ficusin:procurement-plan-draft:v1";
  const preferred = suppliers.find((item) => item.kind === "international") || suppliers[0];
  const makeLine = (item?: ProcurementRecommendation): DraftLine => ({ id: item?.sabyId || `new-${Date.now()}-${Math.random()}`, sabyId: item?.sabyId || "", sabyName: item?.name || "", article: item?.supplierArticle || "", category: item?.dutchName || "", recommendedQty: item?.suggestedQty || 0, packageCount: "1", unitsPerPackage: String(item?.orderMultiple || 1), expectedUnitPrice: item?.lastUnitPrice?.toString() || "", potDiameterCm: item?.potDiameterCm?.toString() || "", heightCm: item?.heightCm?.toString() || "", loadUnit: "shelf" });
  const [savedPlan] = useState<SavedPlan | null>(() => { try { const value = window.localStorage.getItem(draftStorageKey); return value ? JSON.parse(value) as SavedPlan : null; } catch { return null; } });
  const recommendationsFor = (id: number) => recommendations.filter((item) => item.supplierId === id);
  const savedSupplierId = suppliers.some((item) => item.id === savedPlan?.supplierId) ? savedPlan!.supplierId : preferred?.id || 0;
  const [supplierId, setSupplierId] = useState(savedSupplierId); const [saving, setSaving] = useState(false);
  const [exchangeRate, setExchangeRate] = useState<NumericDraft>(savedPlan?.exchangeRate || String(settings.defaultExchangeRate)); const [deliveryToMoscowRub, setDeliveryToMoscowRub] = useState<NumericDraft>(savedPlan?.deliveryToMoscowRub || ""); const [deliveryToRyazanRub, setDeliveryToRyazanRub] = useState<NumericDraft>(savedPlan?.deliveryToRyazanRub || "");
  const [sabyQuery, setSabyQuery] = useState(""); const [sabyResults, setSabyResults] = useState<NomenclatureCandidate[]>([]); const [searchingSaby, setSearchingSaby] = useState(false);
  const isEmptyLine = (item: DraftLine) => !item.sabyId && !item.sabyName.trim() && !item.article.trim() && !item.category.trim() && !item.expectedUnitPrice && !item.potDiameterCm && !item.heightCm;
  const [items, setItems] = useState<DraftLine[]>(() => savedPlan?.items?.length ? savedPlan.items : [makeLine()]);
  useEffect(() => { try { window.localStorage.setItem(draftStorageKey, JSON.stringify({ supplierId, exchangeRate, deliveryToMoscowRub, deliveryToRyazanRub, items } satisfies SavedPlan)); } catch { /* The page remains usable when browser storage is unavailable. */ } }, [deliveryToMoscowRub, deliveryToRyazanRub, exchangeRate, items, supplierId]);
  const availableRecommendations = recommendationsFor(supplierId).filter((item) => !items.some((line) => line.sabyId === item.sabyId));
  const updateItem = (id: string, patch: Partial<DraftLine>) => setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item));
  const draftNumber = (value: string): NumericDraft => value;
  const addRecommendation = (recommendation: ProcurementRecommendation) => setItems((current) => current.length === 1 && isEmptyLine(current[0]) ? [makeLine(recommendation)] : [...current, makeLine(recommendation)]);
  const addSabyProduct = (candidate: NomenclatureCandidate) => {
    const recommendation = recommendationsFor(supplierId).find((item) => item.sabyId === candidate.sabyId);
    const line = recommendation ? makeLine(recommendation) : { ...makeLine(), id: candidate.sabyId, sabyId: candidate.sabyId, sabyName: candidate.name };
    setItems((current) => current.length === 1 && isEmptyLine(current[0]) ? [line] : [...current, line]);
    setSabyQuery(""); setSabyResults([]);
  };
  const sorted = items;
  const sortItems = () => setItems((current) => [...current].sort((a, b) => a.category.localeCompare(b.category, "ru") || a.sabyName.localeCompare(b.sabyName, "ru")));
  const selected = sorted.filter((item) => !isEmptyLine(item));
  const totalUnits = selected.reduce((sum, item) => sum + Number(item.packageCount) * Number(item.unitsPerPackage), 0);
  const missingFields = (item: DraftLine) => {
    const missing: string[] = [];
    if (!item.category.trim()) missing.push("категория");
    if (!Number.isInteger(Number(item.packageCount)) || Number(item.packageCount) <= 0) missing.push("упаковки");
    if (!Number.isInteger(Number(item.unitsPerPackage)) || Number(item.unitsPerPackage) <= 0) missing.push("штук в упаковке");
    if (Number(item.packageCount) * Number(item.unitsPerPackage) > 1000000) missing.push("количество");
    if (!(Number(item.expectedUnitPrice) > 0)) missing.push("цена");
    if (!(Number(item.potDiameterCm) > 0)) missing.push("горшок");
    if (!(Number(item.heightCm) > 0)) missing.push("высота");
    return missing;
  };
  const invalidItems = selected.filter((item) => missingFields(item).length > 0);
  const valid = selected.length > 0 && Number(exchangeRate) > 0 && Number(deliveryToMoscowRub) >= 0 && Number(deliveryToRyazanRub) >= 0 && selected.every((item) =>
    item.category.trim() && Number.isInteger(Number(item.packageCount)) && Number(item.packageCount) > 0 &&
    Number.isInteger(Number(item.unitsPerPackage)) && Number(item.unitsPerPackage) > 0 && Number(item.packageCount) * Number(item.unitsPerPackage) <= 1000000 &&
    Number(item.expectedUnitPrice) > 0 && Number(item.potDiameterCm) > 0 && Number(item.heightCm) > 0);
  const totalPurchaseEUR = selected.reduce((sum, item) => sum + Number(item.expectedUnitPrice || 0) * Number(item.packageCount || 0) * Number(item.unitsPerPackage || 0), 0);
  const requestBody = JSON.stringify({ supplierId, orderNumber: "",
    costs: { exchangeRate: Number(exchangeRate), deliveryToMoscowRub: Number(deliveryToMoscowRub), deliveryToRyazanRub: Number(deliveryToRyazanRub) },
    items: selected.map((item) => ({ sabyId: item.sabyId, rawName: item.category, category: item.category, supplierArticle: item.article,
      packageCount: Number(item.packageCount), unitsPerPackage: Number(item.unitsPerPackage),
      quantity: Number(item.packageCount) * Number(item.unitsPerPackage), expectedUnitPrice: Number(item.expectedUnitPrice),
      potDiameterCm: item.potDiameterCm === "" ? null : Number(item.potDiameterCm), heightCm: item.heightCm === "" ? null : Number(item.heightCm), loadUnit: item.loadUnit })) });
  type PreviewLine = { purchase: number; cost: number; retail: number };
  const [preview, setPreview] = useState<{ key: string; lines: PreviewLine[]; error?: string } | null>(null);
  useEffect(() => {
    if (!valid) return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void api<{ lines: PreviewLine[] }>("/api/v1/admin/procurement/plans/preview", { method: "POST", body: requestBody })
        .then((result) => { if (!cancelled) setPreview({ key: requestBody, lines: result.lines }); })
        .catch(() => { if (!cancelled) setPreview({ key: requestBody, lines: [], error: "Не удалось рассчитать. Проверьте подключение и данные." }); });
    }, 300);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [requestBody, valid]);
  useEffect(() => {
    if (sabyQuery.trim().length < 2) { setSabyResults([]); setSearchingSaby(false); return; }
    let cancelled = false;
    const controller = new AbortController();
    let requestTimeout = 0;
    const timer = window.setTimeout(() => {
      setSearchingSaby(true);
      requestTimeout = window.setTimeout(() => controller.abort(), 8000);
      void api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(sabyQuery.trim())}`, { signal: controller.signal })
        .then((result) => { if (!cancelled) setSabyResults(result.items.filter((candidate) => !items.some((line) => line.sabyId === candidate.sabyId))); })
        .catch((error) => { if (!cancelled) onError((error as Error).name === "AbortError" ? "Поиск занял слишком много времени. Попробуйте точный код СБИС." : (error as Error).message); })
        .finally(() => { window.clearTimeout(requestTimeout); if (!cancelled) setSearchingSaby(false); });
    }, 250);
    return () => { cancelled = true; window.clearTimeout(timer); window.clearTimeout(requestTimeout); controller.abort(); };
  }, [items, onError, sabyQuery]);
  const save = async () => {
    if (!valid || saving) return;
    setSaving(true);
    try {
      const result = await api<{ order: ProcurementOrder }>("/api/v1/admin/procurement/plans", { method: "POST", body: requestBody });
      if (!result.order?.id) throw new Error("Сервер не вернул номер заказа. Проверьте список закупок перед повторным сохранением.");
      window.localStorage.removeItem(draftStorageKey);
      onSaved();
    } catch (error) { onError((error as Error).message); }
    finally { setSaving(false); }
  };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-plan-dialog procurement-plan-fullscreen" role="dialog" aria-modal="true" aria-labelledby="plan-title"><header><div><p className="eyebrow">Закупка в Голландии</p><h2 id="plan-title">Новый заказ поставщику</h2></div><button className="procurement-plan-close" onClick={onClose} aria-label="Закрыть">×</button></header>
    <div className="procurement-plan-settings"><label>Поставщик<select value={supplierId} onChange={(event) => { if (selected.length && !window.confirm("Сменить поставщика и очистить текущий список?")) return; setSupplierId(Number(event.target.value)); setItems([makeLine()]); }}>{suppliers.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select></label><label>Курс, ₽/€<input type="number" min="0" step="0.01" value={exchangeRate} onChange={(event) => setExchangeRate(draftNumber(event.target.value))} /></label><label>До Москвы, ₽<input type="number" min="0" value={deliveryToMoscowRub} placeholder="0" onChange={(event) => setDeliveryToMoscowRub(draftNumber(event.target.value))} /></label><label>Москва → Рязань, ₽<input type="number" min="0" value={deliveryToRyazanRub} placeholder="0" onChange={(event) => setDeliveryToRyazanRub(draftNumber(event.target.value))} /></label></div>
    <div className="procurement-plan-tools"><button onClick={() => setItems((current) => [...current, makeLine()])}>+ Новая строка</button><button onClick={sortItems}>↕ По категории</button><small>Черновик сохраняется автоматически</small><span><strong>{selected.length} позиции</strong> · {totalUnits} шт.</span></div>
    <div className="admin-table-wrap procurement-plan-table-wrap"><table className="admin-table procurement-plan-table"><thead><tr><th>Категория</th><th>Товар СБИС</th><th>Артикул</th><th>Горшок · высота</th><th>Упаковки · штук</th><th>Цена, €</th><th>Себестоимость</th><th></th></tr></thead><tbody>{sorted.map((item) => { const units = Number(item.packageCount) * Number(item.unitsPerPackage); const selectedIndex = selected.indexOf(item); const lineMissing = missingFields(item); const estimate = selectedIndex >= 0 && valid && preview?.key === requestBody ? preview.lines[selectedIndex] : undefined; return <tr key={item.id}><td><input value={item.category} onChange={(event) => updateItem(item.id, { category: event.target.value })} placeholder="Категория" /></td><td><strong>{item.sabyName || "Новая позиция"}</strong>{item.recommendedQty > 0 && <small className="procurement-recommended">Рекомендовано: {item.recommendedQty} шт.</small>}</td><td><input value={item.article} onChange={(event) => updateItem(item.id, { article: event.target.value })} placeholder="Артикул" /></td><td><div className="procurement-size-inputs"><input aria-label="Горшок, см" type="number" min="0" value={item.potDiameterCm} placeholder="P, см" onChange={(event) => updateItem(item.id, { potDiameterCm: draftNumber(event.target.value) })} /><input aria-label="Высота, см" type="number" min="0" value={item.heightCm} placeholder="H, см" onChange={(event) => updateItem(item.id, { heightCm: draftNumber(event.target.value) })} /></div></td><td><div className="procurement-quantity-inputs"><input aria-label="Количество упаковок" type="number" min="1" value={item.packageCount} placeholder="Кол-во" onChange={(event) => updateItem(item.id, { packageCount: draftNumber(event.target.value) })} /><span>×</span><input aria-label="Штук в упаковке" type="number" min="1" value={item.unitsPerPackage} placeholder="Кратность" onChange={(event) => updateItem(item.id, { unitsPerPackage: draftNumber(event.target.value) })} /></div><small>{units || 0} {units === 1 ? "штука" : "штук"}</small></td><td><input aria-label="Цена в евро" className="procurement-price-input" type="number" min="0" step="0.01" value={item.expectedUnitPrice} placeholder="0,00" onChange={(event) => updateItem(item.id, { expectedUnitPrice: draftNumber(event.target.value) })} /></td><td>{estimate ? <><strong>{money.format(estimate.cost)} / шт.</strong><small>Розница ≈ {money.format(estimate.retail)}</small></> : <small>{lineMissing.length ? `Не заполнено: ${lineMissing.join(", ")}` : valid ? (preview?.key === requestBody && preview.error ? preview.error : "Рассчитываем…") : "Проверьте другие строки"}</small>}</td><td><button className="table-action danger" aria-label={`Удалить ${item.sabyName || "позицию"}`} onClick={() => setItems((current) => { const remaining = current.filter((line) => line.id !== item.id); return remaining.length ? remaining : [makeLine()]; })}>×</button></td></tr>; })}</tbody></table></div>
    <p className="admin-hint">Предварительный расчёт: доставка до Москвы распределяется по объёму растений, до Рязани — по высоте. Остатки и цены не меняются.</p><div className="dialog-actions procurement-plan-footer"><div className="procurement-plan-total"><span>Сумма закупки</span><strong>{totalPurchaseEUR.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} €</strong><small>{invalidItems.length ? `Нужно проверить строк: ${invalidItems.length}` : `${totalUnits} шт.`}</small></div><button onClick={onClose}>Отмена</button><button className="primary" disabled={!supplierId || !valid || saving} onClick={save}>{saving ? "Считаем…" : "Создать и рассчитать →"}</button></div>
    <aside className="procurement-recommendation-drawer" aria-label="Рекомендации к закупке"><header><div><p className="eyebrow">Что добавить</p><h3>Рекомендации</h3><small>Продажи за {settings.recommendationDays} дней · запас на {settings.targetCoverDays} дней</small></div></header><label className="procurement-saby-search"><span>Добавить товар из СБИС</span><input value={sabyQuery} onChange={(event) => setSabyQuery(event.target.value)} placeholder="Название, код или артикул" /><small>Поиск по сохранённому каталогу СБИС. Точный код и связь с инвойсами показываются первыми.</small></label><div className="procurement-recommendation-list">{sabyQuery.trim().length >= 2 ? searchingSaby ? <div className="procurement-plan-empty"><span>Ищем в каталоге СБИС…</span></div> : sabyResults.length ? sabyResults.map((item) => <article key={item.sabyId} className="procurement-saby-result"><div><div className="procurement-saby-title"><strong>{item.name}</strong>{item.supplierLinked && <span>Инвойсы Голландии</span>}</div><b className="procurement-saby-code">{item.code || item.article || item.sabyId}</b><div className="procurement-recommendation-metrics"><span>Остаток {item.balance}</span></div><small className="procurement-channel-sales">Продажи и рекомендация подтянутся после добавления товара.</small></div><button className="admin-primary" onClick={() => addSabyProduct(item)}>+ Добавить</button></article>) : <div className="procurement-plan-empty"><strong>Товар не найден</strong><span>Проверьте название или обновите каталог СБИС.</span></div> : availableRecommendations.length ? availableRecommendations.map((item) => <article key={item.sabyId}><div><strong>{item.name}</strong><div className="procurement-recommendation-metrics"><span>Рекомендовано {item.suggestedQty}</span><span>Остаток {item.balance}</span><span>Продано {item.totalSales}</span></div></div><button className="admin-primary" onClick={() => addRecommendation(item)}>+ Добавить</button></article>) : <div className="procurement-plan-empty"><strong>Все рекомендации добавлены</strong><span>Найдите другой товар через поиск СБИС.</span></div>}</div></aside>
  </div></>;
}

export function ProcurementRequestDialog({ onClose, onSaved, onError }: { onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [kind, setKind] = useState("customer_order"); const [name, setName] = useState(""); const [sabyId, setSabyId] = useState(""); const [quantity, setQuantity] = useState(1); const [notes, setNotes] = useState(""); const [saving, setSaving] = useState(false); const [candidates, setCandidates] = useState<NomenclatureCandidate[]>([]);
  useEffect(() => { if (name.trim().length < 2 || sabyId) { setCandidates([]); return; } const timer = window.setTimeout(() => api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(name.trim())}`).then((result) => setCandidates(result.items.slice(0, 6))).catch(() => setCandidates([])), 250); return () => window.clearTimeout(timer); }, [name, sabyId]);
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/requests", { method: "POST", body: JSON.stringify({ kind, requestedName: name, sabyId, quantity, notes }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="request-title"><header><h2 id="request-title">Добавить к закупке</h2><button onClick={onClose} aria-label="Закрыть">×</button></header><div className="admin-form-grid">
    <label>Тип<select value={kind} onChange={(event) => setKind(event.target.value)}><option value="customer_order">Заказ клиента</option><option value="staff_recommendation">Рекомендация магазина</option></select></label>
    <label>Количество<input type="number" min="1" value={quantity} onChange={(event) => setQuantity(Number(event.target.value))} /></label>
    <label className="wide">Название товара<input value={name} onChange={(event) => { setName(event.target.value); setSabyId(""); }} placeholder="Начните вводить название" /></label>
    {sabyId ? <div className="wide procurement-selected-product"><strong>Связано с товаром СБИС</strong><span>{sabyId}</span><button onClick={() => setSabyId("")}>Изменить</button></div> : candidates.length > 0 && <div className="wide procurement-request-candidates">{candidates.map((item) => <button key={item.sabyId} onClick={() => { setSabyId(item.sabyId); setName(item.name); }}><strong>{item.name}</strong><small>{item.code || item.article}</small></button>)}</div>}
    <label className="wide">Комментарий<textarea value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="Клиент, срок, цвет или другая важная деталь" /></label>
  </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={!name.trim() || quantity < 1 || saving} onClick={save}>Добавить</button></div></div></>;
}

export function ProcurementOrderDetailDialog({ orderId, onClose, onSaved, onError }: { orderId: number; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [detail, setDetail] = useState<ProcurementOrderDetail | null>(null); const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [priceChannels, setPriceChannels] = useState<Record<string, boolean>>({ saby_price: false, site: false, wb: false, ozon: false });
  const [costs, setCosts] = useState({ exchangeRate: 0, deliveryToMoscowRub: 0, deliveryToRyazanRub: 0 });
  const load = useCallback(() => api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${orderId}`).then((item) => { setDetail(normalizeProcurementOrderDetail(item)); setCosts({ exchangeRate: item.costs.exchangeRate || (item.order.currency === "RUB" ? 1 : 0), deliveryToMoscowRub: item.costs.deliveryToMoscowRub || item.costs.trolleyCostRub * item.validation.trolleyCount, deliveryToRyazanRub: item.costs.deliveryToRyazanRub }); }).catch((error) => onError((error as Error).message)), [orderId, onError]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    if (!detail?.batches.some((batch) => batch.status === "processing")) return;
    const timer = window.setTimeout(() => void load(), 4000);
    return () => window.clearTimeout(timer);
  }, [detail, load]);
  const calculate = async () => { setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${orderId}/calculate`, { method: "POST", body: JSON.stringify(costs) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const downloadSabyPrices = async () => {
    const response = await fetch(`/api/v1/admin/procurement/orders/${orderId}/saby-prices.xlsx`, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) {
      const result = await response.json().catch(() => ({})) as { error?: string };
      throw new Error(result.error || "Не удалось сформировать XLSX для Saby");
    }
    const blob = await response.blob(); const url = URL.createObjectURL(blob);
    const link = document.createElement("a"); link.href = url;
    link.download = response.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] || `saby-prices-${orderId}.xlsx`;
    document.body.appendChild(link); link.click(); link.remove(); URL.revokeObjectURL(url);
  };
  const prepare = async (kind: "receipt" | "prices") => {
    const selected = kind === "prices" ? Object.entries(priceChannels).filter(([, enabled]) => enabled).map(([channel]) => channel) : [];
    const apiChannels = selected.filter((channel) => channel !== "saby_price");
    setSaving(true);
    try {
      if (selected.includes("saby_price")) await downloadSabyPrices();
      if (kind === "receipt" || apiChannels.length > 0) await api(`/api/v1/admin/procurement/orders/${orderId}/batches`, { method: "POST", body: JSON.stringify({ kind, channels: apiChannels }) });
      await load();
    } catch (error) { onError((error as Error).message); } finally { setSaving(false); }
  };
  const approve = async (batch: ProcurementActionBatch) => { if (!window.confirm(batch.kind === "prices" ? "Подтвердить этот список и подготовить изменения только в выбранных каналах?" : "Подтвердить состав поступления?")) return; setSaving(true); try { await api(`/api/v1/admin/procurement/batches/${batch.id}/approve`, { method: "POST" }); await load(); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const retry = async (batch: ProcurementActionBatch) => { setSaving(true); try { await api(`/api/v1/admin/procurement/batches/${batch.id}/retry`, { method: "POST" }); await load(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const editLine = async (line: ProcurementOrderLine) => {
    const pot = window.prompt("Диаметр горшка, см", line.potDiameterCm?.toString() || ""); if (pot == null) return;
    const height = window.prompt("Высота растения, см", line.heightCm?.toString() || ""); if (height == null) return;
    const loadUnit = window.prompt("Телега / коробка", line.loadUnit || ""); if (loadUnit == null) return;
    setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ potDiameterCm: Number(pot), heightCm: Number(height), loadUnit }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); }
  };
  const acceptMismatch = async (line: ProcurementOrderLine) => { const note = window.prompt("Почему расхождение допустимо? Это попадёт в журнал проверки.", line.comparisonNote || ""); if (!note?.trim()) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ acceptComparison: true, comparisonNote: note }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const setStatus = async (status: "received" | "cancelled" | "review") => { if (!window.confirm(status === "received" ? "Поступление уже проведено в СБИС? Закрыть закупку и выполнить клиентские заявки?" : status === "cancelled" ? "Отменить закупку? Заявки вернутся в открытые." : "Вернуть закупку на проверку? Черновики действий будут отменены.")) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${orderId}/status`, { method: "PATCH", body: JSON.stringify({ status }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const remove = async () => { if (!window.confirm("Удалить отменённую закупку вместе с загруженным PDF? После этого тот же файл можно будет загрузить заново.")) return; setSaving(true); try { await api(`/api/v1/admin/procurement/orders/${orderId}`, { method: "DELETE" }); onSaved(); onClose(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const uploadInvoice = async (file: File | null) => {
    if (!file || !detail) return;
    setUploading(true);
    try {
      const body = new FormData(); body.append("supplierId", String(detail.order.supplierId)); body.append("orderId", String(orderId)); body.append("file", file);
      const result = await api<{ duplicate: boolean; order: ProcurementOrder }>("/api/v1/admin/procurement/documents", { method: "POST", body });
      if (result.duplicate) throw new Error(`Этот PDF уже загружен в закупку ${result.order.orderNumber || `№${result.order.id}`}.`);
      await load(); onSaved();
    } catch (error) { onError((error as Error).message); }
    finally { setUploading(false); }
  };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-order-dialog" role="dialog" aria-modal="true" aria-labelledby="order-detail-title"><header><div><p className="eyebrow">Закупка</p><h2 id="order-detail-title">{detail?.order.orderNumber || `№${orderId}`}</h2></div><button onClick={onClose} aria-label="Закрыть">×</button></header>
    {!detail ? <div className="procurement-zero">Загружаем строки…</div> : <div className="procurement-order-body">
      <div className="procurement-costs">{detail.order.currency !== "RUB" && <><label>Курс оплаты<input type="number" step="0.01" value={costs.exchangeRate} onChange={(event) => setCosts({ ...costs, exchangeRate: Number(event.target.value) })} /></label><label>Голландия → Москва, весь инвойс, ₽<input type="number" step="0.01" value={costs.deliveryToMoscowRub} onChange={(event) => setCosts({ ...costs, deliveryToMoscowRub: Number(event.target.value) })} /></label><label>Москва → Рязань, весь инвойс, ₽<input type="number" value={costs.deliveryToRyazanRub} onChange={(event) => setCosts({ ...costs, deliveryToRyazanRub: Number(event.target.value) })} /></label></>}<label className="secondary-button procurement-inline-upload">{uploading ? "Разбираем PDF…" : "Загрузить инвойс в эту закупку"}<input type="file" accept="application/pdf,.pdf" disabled={uploading} onChange={(event) => void uploadInvoice(event.target.files?.[0] || null)} /></label><button className="admin-primary" disabled={saving || !detail.validation.canCalculate || costs.exchangeRate <= 0} onClick={calculate}>{saving ? "Считаем…" : "Рассчитать"}</button></div>
      {detail.order.currency !== "RUB" ? <p className="admin-hint procurement-note">Парсер нашёл телег: {detail.validation.trolleyCount}. Доставка до Москвы будет разделена между ними автоматически.</p> : <p className="admin-hint procurement-note">Российская закупка рассчитывается сразу в рублях, без курса и голландской логистики.</p>}
      <section className={detail.validation.blockers?.length ? "procurement-checklist blocked" : "procurement-checklist ready"}><strong>{detail.validation.blockers?.length ? "Что нужно сделать дальше" : "Проверки пройдены"}</strong>{detail.validation.blockers?.length ? <ul>{detail.validation.blockers.map((blocker) => <li key={blocker}>{blocker === "Не загружен инвойс или счёт" ? "Загрузите PDF-инвойс кнопкой выше — он будет привязан именно к этой закупке" : blocker}</li>)}</ul> : <p>Инвойс, сопоставление, размеры и расхождения проверены.</p>}<small>Красные строки означают расхождение с инвойсом или отсутствие связи с товаром СБИС; сохранённые данные закупки при этом не теряются.</small><small>Телег: {detail.validation.trolleyCount} · распределено {money.format(detail.validation.allocatedTrolleyRub)} из {money.format(detail.validation.expectedTrolleyRub)} · Москва → Рязань {money.format(detail.validation.allocatedRyazanRub)} из {money.format(detail.validation.expectedRyazanRub)}</small></section>
      <div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>Товар</th><th>Телега / размер</th><th>Заказ / инвойс</th><th>Цена заказ / инвойс</th><th>Доставка</th><th>Себестоимость</th><th>СБИС сейчас</th><th>Новая розница</th><th>WB / Ozon</th><th></th></tr></thead><tbody>{detail.lines.map((line) => <tr key={line.id} className={line.comparisonMismatch && !line.comparisonAccepted ? "procurement-row-mismatch" : ""}><td><strong>{line.sabyName || line.rawName}</strong><small>{line.supplierArticle}</small></td><td>{line.loadUnit || "—"}<small>{[line.potDiameterCm && `D${line.potDiameterCm}`, line.heightCm && `${line.heightCm} см`].filter(Boolean).join(" · ") || "Размер не заполнен"}</small></td><td>{line.orderedQuantity || "—"} / {line.invoicedQuantity ?? "—"}{line.comparisonAccepted && <small className="procurement-ok">Расхождение принято</small>}</td><td>{line.expectedUnitPrice ? line.expectedUnitPrice.toFixed(2) : "—"} / {line.invoicedQuantity == null ? "—" : line.unitPrice.toFixed(2)} {detail.order.currency}</td><td>{line.trolleyDeliveryUnitRub == null ? "—" : money.format((line.trolleyDeliveryUnitRub || 0) + (line.ryazanDeliveryUnitRub || 0))}</td><td>{line.unitCostRub == null ? "—" : money.format(line.unitCostRub)}</td><td>{money.format(line.currentRetailRub)}</td><td className={line.priceChangeNeeded ? "procurement-price-change" : ""}>{line.proposedRetailRub ? money.format(line.proposedRetailRub) : "—"}</td><td>{line.proposedMarketplaceRub ? money.format(line.proposedMarketplaceRub) : "—"}</td><td><div className="procurement-inline-actions"><button onClick={() => void editLine(line)}>Размеры</button>{line.comparisonMismatch && !line.comparisonAccepted && <button onClick={() => void acceptMismatch(line)}>Принять расхождение</button>}</div></td></tr>)}</tbody></table></div>
      {detail.order.status === "ready_to_receive" && <><fieldset className="procurement-price-channels"><legend>Где подготовить изменение цен</legend>{[["saby_price", "СБИС — скачать XLSX"], ["site", "Сайт"], ["wb", "Wildberries"], ["ozon", "Ozon"]].map(([channel, label]) => <label key={channel}><input type="checkbox" checked={priceChannels[channel]} onChange={(event) => setPriceChannels({ ...priceChannels, [channel]: event.target.checked })} />{label}</label>)}</fieldset><p className="admin-hint procurement-note">Для Saby будет скачан официальный формат «Код / Цена». Загрузите его в «Склад → Документы → Из файла» и проверьте документ перед проведением.</p><div className="procurement-batch-buttons"><button onClick={() => void setStatus("review")} disabled={saving}>Вернуть на проверку</button><button onClick={() => void prepare("receipt")} disabled={saving || !detail.validation.canPrepareActions}>Подготовить поступление СБИС</button><button className="admin-primary" onClick={() => void prepare("prices")} disabled={saving || !detail.validation.canPrepareActions || !Object.values(priceChannels).some(Boolean)}>Подготовить изменение цен</button></div></>}
      {detail.batches.map((batch) => <section className="procurement-batch" key={batch.id}><div className="admin-block-heading"><div><p className="eyebrow">{batch.kind === "prices" ? "Цены" : "Поступление"}</p><h3>{batch.kind === "prices" ? "Изменения по выбранным каналам" : "Поступление СБИС"}</h3></div><span className="admin-pill">{batch.kind === "receipt" && batch.status === "draft" ? "Подготовлено" : batchStatusLabel(batch.status)}</span></div><div className="procurement-batch-list">{batchDisplayRows(batch).map((row) => <article key={row.key}><div className="procurement-batch-product"><strong>{row.productName}</strong>{row.productCode && <small>Главный код: {row.productCode}</small>}</div><div><small>Канал</small><span>{channelLabel(row.item.channel)}</span>{(row.item.displayArticle || row.item.externalArticle) && !row.item.previewLines?.length && <small>{row.item.displayArticle || row.item.externalArticle}</small>}</div><div><small>{row.quantity == null ? "Было → станет" : "Остаток: было → станет"}</small><span>{row.quantity == null ? `${row.oldValue == null ? "Не получено" : money.format(row.oldValue)} → ${money.format(row.newValue)}` : `${row.oldBalance ?? 0} → ${row.newBalance ?? row.quantity}`}</span>{row.quantity != null && <small>Поступление: +{row.quantity}</small>}{row.item.compareAtValue && row.item.compareAtValue > row.newValue ? <small>до скидки {money.format(row.item.compareAtValue)}</small> : null}</div>{row.showStatus && <div className="procurement-action-status">{actionStatus(row.item)}</div>}</article>)}</div>{batch.status === "draft" && <div className="dialog-actions"><button className="primary" disabled={saving || !batch.items.length} onClick={() => void approve(batch)}>{batch.kind === "receipt" ? "Создать поступление в СБИС" : `Подтвердить ${batchDisplayRows(batch).length} строк`}</button></div>}{batch.items.some((item) => item.status === "failed") && <div className="dialog-actions"><button disabled={saving} onClick={() => void retry(batch)}>Повторить ошибки</button></div>}</section>)}
      {!['received', 'cancelled'].includes(detail.order.status) && <div className="procurement-order-final"><button className="text-button danger" disabled={saving} onClick={() => void setStatus("cancelled")}>Отменить закупку</button>{detail.order.status === "ready_to_receive" && detail.batches.some((batch) => batch.kind === "receipt" && batch.items.some((item) => item.channel === "saby_receipt" && item.status === "completed")) && <button className="admin-primary" disabled={saving} onClick={() => void setStatus("received")}>Поступление проведено — закрыть</button>}</div>}
      {detail.order.status === "cancelled" && <div className="procurement-order-final"><button className="text-button danger" disabled={saving} onClick={() => void remove()}>Удалить закупку и PDF</button><small>После удаления этот документ можно загрузить заново.</small></div>}
    </div>}
  </div></>;
}

export const channelLabel = (value: string) => ({ site: "Сайт", saby_price: "СБИС — цена", saby_receipt: "СБИС — поступление", wb: "Wildberries", ozon: "Ozon" }[value] || value);

const batchDisplayRows = (batch: ProcurementActionBatch) => batch.items.flatMap((item) => item.previewLines?.length
  ? item.previewLines.map((line, index) => ({ key: `${item.id}-${line.sabyId}`, item, productName: line.name, productCode: [line.code, `ID ${line.sabyId}`].filter(Boolean).join(" · "), oldValue: line.oldPrice, newValue: line.newPrice || 0, quantity: line.quantity, oldBalance: line.oldBalance, newBalance: line.newBalance, showStatus: index === 0 }))
  : [{ key: String(item.id), item, productName: item.productName, productCode: item.productCode, oldValue: item.oldValue, newValue: item.newValue, quantity: item.quantity, oldBalance: undefined, newBalance: undefined, showStatus: true }]);

export const batchStatusLabel = (value: string) => ({ draft: "Черновик", processing: "Выполняется", completed: "Выполнено", partially_completed: "Выполнено частично", failed: "Ошибка", cancelled: "Отменено" }[value] || value);

export const actionStatus = (item: ProcurementActionItem) => {
  const label = item.channel === "saby_receipt" && item.status === "draft" ? "Подготовлено" : (({ draft: "Черновик", queued: "В очереди", processing: "Отправляется", completed: "Выполнено", failed: "Ошибка", skipped: "Пропущено", not_configured: "API не подключён" } as Record<string, string>)[item.status] || item.status);
  return <><span className={item.status === "completed" ? "procurement-ok" : item.status === "failed" || item.status === "not_configured" || item.status === "skipped" ? "procurement-warning" : ""}>{label}</span>{item.externalUrl && <a href={item.externalUrl} target="_blank" rel="noreferrer"><small>{item.channel === "saby_receipt" ? "Открыть поступление в СБИС" : "Открыть документ в СБИС"}</small></a>}{item.errorMessage && <small>{item.errorMessage}</small>}</>;
};

export function ProcurementMatchDialog({ alias, onClose, onSaved, onError }: { alias: ProcurementAlias; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [query, setQuery] = useState(alias.rawName); const [items, setItems] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false); const [saving, setSaving] = useState(false); const [refreshing, setRefreshing] = useState(false);
  useEffect(() => {
    if (query.trim().length < 2) { setItems([]); return; }
    const timer = window.setTimeout(() => { setSearching(true); api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`)
      .then((result) => setItems(result.items)).catch((error) => onError((error as Error).message)).finally(() => setSearching(false)); }, 250);
    return () => window.clearTimeout(timer);
  }, [query, onError]);
  const resolve = async (matchStatus: "confirmed" | "new_product" | "ignored", sabyId = "") => { setSaving(true); try {
    await api(`/api/v1/admin/procurement/aliases/${alias.id}`, { method: "PATCH", body: JSON.stringify({ matchStatus, sabyId }) }); onSaved();
  } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
	const refreshSaby = async () => {
	  setRefreshing(true);
	  try {
		await api("/api/v1/admin/procurement/integrations/saby/catalog", { method: "POST" });
		const result = await api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`);
		setItems(result.items);
	  } catch (error) { onError((error as Error).message); }
	  finally { setRefreshing(false); }
	};
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-match-dialog" role="dialog" aria-modal="true" aria-labelledby="match-title"><header><div><p className="eyebrow">{alias.supplierName}</p><h2 id="match-title">Сопоставить товар</h2></div><button onClick={onClose} aria-label="Закрыть">×</button></header>
    <div className="procurement-match-source"><strong>{alias.rawName}</strong><span>{[alias.supplierArticle && `Артикул ${alias.supplierArticle}`, alias.potDiameterCm && `D${alias.potDiameterCm}`, alias.heightCm && `${alias.heightCm} см`].filter(Boolean).join(" · ") || "Размер не указан"}</span></div>
    <label className="procurement-match-search">Поиск по единому справочнику товаров<input value={query} onChange={(event) => setQuery(event.target.value)} autoFocus placeholder="Название или код X…" /></label>
    <div className="procurement-candidates">{searching ? <div className="procurement-zero"><span>Ищем в справочнике…</span></div> : items.length ? items.map((item) => <article key={item.variantId}><div><strong>{item.name}</strong><span>{[item.code, item.article].filter(Boolean).join(" · ")}</span><small>Остаток СБИС: {item.balance} · {money.format(item.price)}</small></div><button disabled={saving} onClick={() => void resolve("confirmed", item.sabyId)}>Выбрать</button></article>) : <div className="procurement-zero"><strong>Кандидаты не найдены</strong><span>Импортируйте новую карточку СБИС или измените запрос.</span></div>}</div>
	<p className="admin-hint">Если карточку только что создали в СБИС, обновите каталог и импортируйте её в наш справочник.</p>
    <div className="dialog-actions procurement-match-actions"><button onClick={() => void resolve("ignored")} disabled={saving || refreshing}>Игнорировать строку</button><button onClick={() => void resolve("new_product")} disabled={saving || refreshing}>Это новый товар</button><button onClick={() => void refreshSaby()} disabled={saving || refreshing}>{refreshing ? "Обновляем СБИС…" : "Обновить из СБИС"}</button><button className="primary" onClick={onClose}>Отмена</button></div>
  </div></>;
}

export function ProcurementUploadDialog({ suppliers, orders, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; orders: ProcurementOrder[]; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0); const [orderId, setOrderId] = useState(0);
  const [file, setFile] = useState<File | null>(null); const [saving, setSaving] = useState(false);
  const availableOrders = orders.filter((item) => item.supplierId === supplierId && !["received", "cancelled"].includes(item.status));
  const changeSupplier = (value: number) => { setSupplierId(value); setOrderId(0); };
  const save = async () => { if (!file || !supplierId) return; setSaving(true); try {
    const body = new FormData(); body.append("supplierId", String(supplierId)); if (orderId) body.append("orderId", String(orderId)); body.append("file", file);
    const result = await api<{ duplicate: boolean; order: ProcurementOrder }>("/api/v1/admin/procurement/documents", { method: "POST", body });
    if (result.duplicate) { onError(`Этот PDF уже загружен в закупку ${result.order.orderNumber || `№${result.order.id}`} (${result.order.status === "cancelled" ? "отменена — откройте её и удалите" : "откройте существующую закупку"}).`); return; }
    onSaved();
  } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="upload-title"><header><h2 id="upload-title">Загрузить документ</h2><button onClick={onClose} aria-label="Закрыть">×</button></header>
    <div className="admin-form-grid"><label className="wide">Поставщик<select value={supplierId} onChange={(event) => changeSupplier(Number(event.target.value))}>{suppliers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label className="wide">Связать с закупкой<select value={orderId} onChange={(event) => setOrderId(Number(event.target.value))}><option value={0}>Создать закупку из документа</option>{availableOrders.map((item) => <option key={item.id} value={item.id}>{item.orderNumber || `Черновик №${item.id}`}</option>)}</select></label>
      <label className="wide procurement-file">PDF-файл<input type="file" accept="application/pdf,.pdf" onChange={(event) => setFile(event.target.files?.[0] || null)} /><span>{file ? `${file.name} · ${(file.size / 1024 / 1024).toFixed(2)} МБ` : "До 20 МБ"}</span></label>
      <p className="admin-hint wide">Поддерживаются packing list PL-FG 267 и российские счета на оплату. Повторная загрузка того же файла не создаст дубль.</p>
    </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={!file || !supplierId || saving} onClick={save}>{saving ? "Разбираем PDF…" : "Загрузить и разобрать"}</button></div></div></>;
}

export function SupplierDialog({ suppliers, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [name, setName] = useState(""); const [kind, setKind] = useState<"international" | "domestic">("international");
  const [taxId, setTaxId] = useState(""); const [kpp, setKpp] = useState("");
  const [countryCode, setCountryCode] = useState("NL"); const [currency, setCurrency] = useState<"EUR" | "USD" | "RUB">("EUR");
  const [saving, setSaving] = useState(false); const [deletingId, setDeletingId] = useState(0);
  const [deleteCandidate, setDeleteCandidate] = useState<ProcurementSupplier | null>(null);
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/suppliers", { method: "POST", body: JSON.stringify({ name, kind, countryCode, taxId, kpp, defaultCurrency: currency }) }); setName(""); setTaxId(""); setKpp(""); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const remove = async (supplier: ProcurementSupplier) => {
    setDeletingId(supplier.id);
    try { await api(`/api/v1/admin/procurement/suppliers/${supplier.id}`, { method: "DELETE" }); setDeleteCandidate(null); onSaved(); }
    catch (error) { onError((error as Error).message); }
    finally { setDeletingId(0); }
  };
  const changeKind = (value: "international" | "domestic") => { setKind(value); if (value === "domestic") { setCountryCode("RU"); setCurrency("RUB"); } else { setCountryCode("NL"); setCurrency("EUR"); setKpp(""); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="supplier-title"><header><h2 id="supplier-title">Поставщики</h2><button onClick={onClose} aria-label="Закрыть">×</button></header>
    {suppliers.length > 0 && <div className="admin-table-wrap"><table className="admin-table procurement-suppliers-table"><thead><tr><th>Поставщик</th><th>Тип</th><th className="supplier-action-column">Действие</th></tr></thead><tbody>{suppliers.map((supplier) => <tr key={supplier.id}><td><strong>{supplier.name}</strong><small>{supplier.countryCode || "Страна не указана"} · {supplier.defaultCurrency}{supplier.taxId ? ` · ИНН ${supplier.taxId}` : ""}{supplier.kpp ? ` · КПП ${supplier.kpp}` : ""}</small></td><td>{supplier.kind === "international" ? "Иностранный" : "Российский"}</td><td className="supplier-action-column"><button className="table-action danger supplier-delete-button" disabled={deletingId > 0} onClick={() => setDeleteCandidate(supplier)}>{deletingId === supplier.id ? "Удаляем…" : "Удалить"}</button></td></tr>)}</tbody></table></div>}
    <h3>Добавить поставщика</h3>
    <div className="admin-form-grid"><label className="wide">Название<input value={name} onChange={(event) => setName(event.target.value)} autoFocus /></label>
      <label>ИНН<input inputMode="numeric" value={taxId} onChange={(event) => setTaxId(event.target.value.replace(/\D/g, "").slice(0, 12))} placeholder="Для выбора в СБИС" /></label>
      {kind === "domestic" && <label>КПП<input inputMode="numeric" value={kpp} onChange={(event) => setKpp(event.target.value.replace(/\D/g, "").slice(0, 9))} placeholder="9 цифр" /></label>}
      <label>Тип<select value={kind} onChange={(event) => changeKind(event.target.value as "international" | "domestic")}><option value="international">Иностранный</option><option value="domestic">Российский</option></select></label>
      <label>Страна<input maxLength={2} value={countryCode} onChange={(event) => setCountryCode(event.target.value.toUpperCase())} /></label>
      <label>Валюта<select value={currency} onChange={(event) => setCurrency(event.target.value as "EUR" | "USD" | "RUB")}><option>EUR</option><option>USD</option><option>RUB</option></select></label>
      <p className="admin-hint wide">Название поставщика не используется для автоматического сопоставления растений. У каждого поставщика будет собственный набор ключей.</p>
    </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={!name.trim() || saving || (kind === "domestic" && taxId.length === 10 && kpp.length !== 9)} onClick={save}>{saving ? "Сохраняем…" : "Добавить"}</button></div></div>
    {deleteCandidate && <ConfirmDialog
      title="Удалить поставщика?"
      text={<>Поставщик <strong>«{deleteCandidate.name}»</strong>, его ключи и сопоставления будут удалены. Это действие нельзя отменить.</>}
      confirmLabel={deletingId === deleteCandidate.id ? "Удаляем…" : "Удалить"}
      busy={deletingId > 0}
      danger
      onCancel={() => setDeleteCandidate(null)}
      onConfirm={() => void remove(deleteCandidate)}
    />}
  </>;
}

export function ProcurementOrderDialog({ suppliers, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0); const selected = suppliers.find((item) => item.id === supplierId);
  const [orderNumber, setOrderNumber] = useState(""); const [sourceKind, setSourceKind] = useState("manual"); const [notes, setNotes] = useState(""); const [saving, setSaving] = useState(false);
  const save = async () => { if (!selected) return; setSaving(true); try { await api("/api/v1/admin/procurement/orders", { method: "POST", body: JSON.stringify({ supplierId, orderNumber, sourceKind, currency: selected.defaultCurrency, notes }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="procurement-title"><header><h2 id="procurement-title">Новая закупка</h2><button onClick={onClose} aria-label="Закрыть">×</button></header>
    <div className="admin-form-grid"><label className="wide">Поставщик<select value={supplierId} onChange={(event) => setSupplierId(Number(event.target.value))}>{suppliers.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.defaultCurrency}</option>)}</select></label>
      <label>Номер заказа<input value={orderNumber} onChange={(event) => setOrderNumber(event.target.value)} placeholder="Можно заполнить позже" /></label>
      <label>Основание<select value={sourceKind} onChange={(event) => setSourceKind(event.target.value)}><option value="manual">Ручная закупка</option><option value="recommendation">Рекомендации системы</option><option value="invoice">Инвойс</option><option value="payment_invoice">Счёт на оплату</option></select></label>
      <label className="wide">Комментарий<textarea rows={3} value={notes} onChange={(event) => setNotes(event.target.value)} /></label>
      <p className="admin-hint wide">Создаётся только черновик. Отправки в СБИС и изменения цен не будет.</p>
    </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={!supplierId || saving} onClick={save}>{saving ? "Создаём…" : "Создать черновик"}</button></div></div></>;
}
