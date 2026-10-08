import { useCallback, useEffect, useRef, useState } from "react";
import { normalizeProcurementOrderDetail } from "./AdminProcurement";
import { ConfirmDialog, api, money } from "./adminShared";
import type { NomenclatureCandidate, ProcurementActionBatch, ProcurementActionItem, ProcurementAlias, ProcurementOrder, ProcurementOrderDetail, ProcurementOrderLine, ProcurementRecommendation, ProcurementSupplier, ProcurementSettings } from "./adminTypes";

const reconciliationLabel = (value: string) => ({ planned: "Ожидает инвойс", matched: "Совпадает", changed: "Изменено", missing: "Нет в инвойсе", added: "Добавлено поставщиком", excluded: "Исключено", superseded: "Прошлая версия" }[value] || value);
const decimalNumber = (value: string) => Number(value.trim().replace(",", "."));
const displayPrice = (value: string | number | null | undefined) => {
  const text = String(value ?? "").trim().replace(".", ",");
  return /^\d+(?:,\d*)?$/.test(text) ? text.replace(/,(\d*?)0*$/, (_, digits: string) => digits ? `,${digits}` : "") : text;
};

function sortPlanLines<T extends { category: string; expectedUnitPrice: string }>(lines: T[]): T[] {
  return [...lines].sort((a, b) => {
    const categoryA = a.category.trim(); const categoryB = b.category.trim();
    if (!categoryA && categoryB) return 1;
    if (categoryA && !categoryB) return -1;
    const categoryOrder = categoryA.localeCompare(categoryB, "ru");
    if (categoryOrder) return categoryOrder;
    const priceA = decimalNumber(a.expectedUnitPrice);
    const priceB = decimalNumber(b.expectedUnitPrice);
    const priceOrder = (priceA > 0 ? priceA : Infinity) - (priceB > 0 ? priceB : Infinity);
    return priceOrder || 0;
  });
}

export function ProcurementPlanDialog({ suppliers, recommendations, initialRecommendations, settings, draftId, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; recommendations: ProcurementRecommendation[]; initialRecommendations: ProcurementRecommendation[]; settings: ProcurementSettings; draftId?: number | null; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  type NumericDraft = string;
  type DraftLine = { id: string; sabyId: string; sabyName: string; article: string; category: string; recommendedQty: number; packageCount: NumericDraft; unitsPerPackage: NumericDraft; expectedUnitPrice: NumericDraft; potDiameterCm: NumericDraft; heightCm: NumericDraft; loadUnit: string; rowTouched?: boolean };
  type SavedPlan = { supplierId: number; exchangeRate: NumericDraft; deliveryToMoscowRub: NumericDraft; deliveryToRyazanRub: NumericDraft; items: DraftLine[] };
  type RemoteDraft = { id: number; title: string; payload: SavedPlan; updatedAt: string };
	type ProductDefault = { potDiameterCm: number | null; heightCm: number | null; category: string; article: string; unitPrice: number | null; unitsPerPackage: number };
	const preferred = suppliers.find((item) => item.id === initialRecommendations[0]?.supplierId) || suppliers.find((item) => item.kind === "international") || suppliers[0];
	const makeLine = (item?: ProcurementRecommendation): DraftLine => ({ id: item?.sabyId || `new-${Date.now()}-${Math.random()}`, sabyId: item?.sabyId || "", sabyName: item?.name || "", article: item?.supplierArticle || "", category: item?.dutchName || "", recommendedQty: item?.suggestedQty || 0, packageCount: item?.quantityKnown === false ? "" : "1", unitsPerPackage: String(item?.orderMultiple || 1), expectedUnitPrice: displayPrice(item?.lastUnitPrice), potDiameterCm: item?.potDiameterCm?.toString() || "", heightCm: item?.heightCm?.toString() || "", loadUnit: "shelf" });
  const recommendationsFor = (id: number) => recommendations.filter((item) => item.supplierId === id);
  const [supplierId, setSupplierId] = useState(preferred?.id || 0); const [saving, setSaving] = useState(false);
  const selectedSupplier = suppliers.find((item) => item.id === supplierId); const isDomestic = selectedSupplier?.kind === "domestic"; const purchaseCurrency = selectedSupplier?.defaultCurrency || (isDomestic ? "RUB" : "EUR"); const currencySymbol = purchaseCurrency === "RUB" ? "₽" : purchaseCurrency === "USD" ? "$" : "€";
  const [exchangeRate, setExchangeRate] = useState<NumericDraft>(String(settings.defaultExchangeRate)); const [deliveryToMoscowRub, setDeliveryToMoscowRub] = useState<NumericDraft>(""); const [deliveryToRyazanRub, setDeliveryToRyazanRub] = useState<NumericDraft>("");
  const [sabyQuery, setSabyQuery] = useState(""); const [sabyResults, setSabyResults] = useState<NomenclatureCandidate[]>([]); const [searchingSaby, setSearchingSaby] = useState(false);
  const [sabyBalances, setSabyBalances] = useState<Record<string, { balance: number; seenAt?: string; defaults: ProductDefault[] }>>({});
  const isEmptyLine = (item: DraftLine) => !item.sabyId && !item.sabyName.trim() && !item.article.trim() && !item.category.trim() && !item.expectedUnitPrice && !item.potDiameterCm && !item.heightCm;
  const [items, setItems] = useState<DraftLine[]>(() => initialRecommendations.length ? initialRecommendations.map(makeLine) : [makeLine()]);
  const [sortOrder, setSortOrder] = useState(() => sortPlanLines(items).map((item) => item.id));
  const sabyIDsKey = [...new Set(items.map((item) => item.sabyId).filter(Boolean))].sort().join("\u001f");
  useEffect(() => {
    if (!sabyIDsKey) { setSabyBalances({}); return; }
    let cancelled = false;
    void api<{ items: { sabyId: string; balance: number; seenAt?: string; defaults: ProductDefault[] }[] }>("/api/v1/admin/procurement/nomenclature/balances", { method: "POST", body: JSON.stringify({ supplierId, sabyIds: sabyIDsKey.split("\u001f") }) })
      .then((result) => { if (!cancelled) setSabyBalances(Object.fromEntries(result.items.map((item) => [item.sabyId, item]))); })
      .catch(() => { if (!cancelled) setSabyBalances({}); });
    return () => { cancelled = true; };
  }, [sabyIDsKey, supplierId]);
  useEffect(() => {
    setItems((current) => current.map((line) => {
      const defaults = sabyBalances[line.sabyId]?.defaults || [];
      if (defaults.length !== 1 || line.category || line.expectedUnitPrice || line.potDiameterCm || line.heightCm) return line;
      const value = defaults[0];
      return { ...line, category: value.category, article: value.article || line.article, expectedUnitPrice: displayPrice(value.unitPrice),
        potDiameterCm: value.potDiameterCm?.toString() || "", heightCm: value.heightCm?.toString() || "", unitsPerPackage: String(value.unitsPerPackage) };
    }));
  }, [sabyBalances]);
  const [remoteDraft, setRemoteDraft] = useState<RemoteDraft | null>(null);
  const [draftLoading, setDraftLoading] = useState(Boolean(draftId));
  const [title, setTitle] = useState(() => initialRecommendations.length ? `Закупка по рекомендациям · ${new Date().toLocaleDateString("ru-RU")}` : `Новая закупка · ${new Date().toLocaleDateString("ru-RU")}`);
  const [draftSaving, setDraftSaving] = useState(false);
  const [savedDraftSignature, setSavedDraftSignature] = useState("");
  const initialTitle = useRef(title);
  const remoteDraftRef = useRef<RemoteDraft | null>(null);
  const savedSignatureRef = useRef("");
  const failedSignatureRef = useRef("");
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  useEffect(() => { if (!draftId) return; void api<{ draft: RemoteDraft }>(`/api/v1/admin/procurement/plan-drafts/${draftId}`)
    .then(({ draft }) => { const saved = draft.payload; if (!saved?.items?.length) throw new Error("Черновик пуст или повреждён"); const normalizedItems = saved.items.map((item) => ({ ...item, expectedUnitPrice: displayPrice(item.expectedUnitPrice) })); const signature = JSON.stringify({ title: draft.title, payload: { supplierId: saved.supplierId, exchangeRate: saved.exchangeRate, deliveryToMoscowRub: saved.deliveryToMoscowRub, deliveryToRyazanRub: saved.deliveryToRyazanRub, items: normalizedItems } }); remoteDraftRef.current = draft; savedSignatureRef.current = signature; setRemoteDraft(draft); setTitle(draft.title); setSupplierId(saved.supplierId); setExchangeRate(saved.exchangeRate); setDeliveryToMoscowRub(saved.deliveryToMoscowRub); setDeliveryToRyazanRub(saved.deliveryToRyazanRub); setItems(normalizedItems); setSortOrder(sortPlanLines(normalizedItems).map((item) => item.id)); setSavedDraftSignature(signature); })
    .catch((error) => onError(`Не удалось открыть черновик: ${(error as Error).message}`)).finally(() => setDraftLoading(false)); }, [draftId, onError]);
  const currentDraft = (): SavedPlan => ({ supplierId, exchangeRate, deliveryToMoscowRub, deliveryToRyazanRub, items: items.map((item) => ({ ...item, expectedUnitPrice: displayPrice(item.expectedUnitPrice) })) });
  const draftSignature = JSON.stringify({ title: title.trim(), payload: currentDraft() });
  const persistDraft = useCallback((signature: string): Promise<boolean> => {
    if (draftLoading || (draftId && !remoteDraftRef.current)) return Promise.resolve(false);
    const queued = saveQueueRef.current.then(async () => {
      if (signature === savedSignatureRef.current) return true;
      const snapshot = JSON.parse(signature) as { title: string; payload: SavedPlan };
      if (!snapshot.title) return false;
      setDraftSaving(true);
      try {
        const existing = remoteDraftRef.current;
        const result = await api<{ draft: RemoteDraft }>(existing ? `/api/v1/admin/procurement/plan-drafts/${existing.id}` : "/api/v1/admin/procurement/plan-drafts", { method: existing ? "PUT" : "POST", body: signature });
        remoteDraftRef.current = result.draft;
        savedSignatureRef.current = signature;
        failedSignatureRef.current = "";
        setRemoteDraft(result.draft);
        setSavedDraftSignature(signature);
        return true;
      } catch (error) { failedSignatureRef.current = signature; onError(`Не удалось сохранить черновик: ${(error as Error).message}`); return false; }
      finally { setDraftSaving(false); }
    });
    saveQueueRef.current = queued;
    return queued;
  }, [draftId, draftLoading, onError]);
  const hasDraftContent = initialRecommendations.length > 0 || items.some((item) => !isEmptyLine(item)) || title !== initialTitle.current;
  const commitPlan = useCallback(() => {
    // Sorting after focus changes keeps the row in place while a price or category is edited.
    const normalizedItems = items.map((item) => ({ ...item, expectedUnitPrice: displayPrice(item.expectedUnitPrice) }));
    if (normalizedItems.some((item, index) => item.expectedUnitPrice !== items[index].expectedUnitPrice)) setItems(normalizedItems);
    window.setTimeout(() => setSortOrder(sortPlanLines(normalizedItems).map((item) => item.id)), 0);
    if (!draftLoading && !saving && hasDraftContent && draftSignature !== savedSignatureRef.current && draftSignature !== failedSignatureRef.current) {
      void persistDraft(draftSignature);
    }
  }, [draftLoading, draftSignature, hasDraftContent, items, persistDraft, saving]);
  useEffect(() => {
    window.addEventListener("blur", commitPlan);
    return () => window.removeEventListener("blur", commitPlan);
  }, [commitPlan]);
  const closePlan = async () => {
    if (hasDraftContent && draftSignature !== savedSignatureRef.current && !await persistDraft(draftSignature)) return;
    onClose();
  };
  const availableRecommendations = recommendationsFor(supplierId).filter((item) => !items.some((line) => line.sabyId === item.sabyId));
  const updateItem = (id: string, patch: Partial<DraftLine>) => setItems((current) => current.map((item) => {
    if (item.id !== id) return item;
    return { ...item, ...patch, rowTouched: true };
  }));
  const markLineTouched = (id: string) => setItems((current) => current.map((item) => item.id === id && !item.rowTouched ? { ...item, rowTouched: true } : item));
  const draftNumber = (value: string): NumericDraft => value;
  const addRecommendation = (recommendation: ProcurementRecommendation) => setItems((current) => current.length === 1 && isEmptyLine(current[0]) ? [makeLine(recommendation)] : [...current, makeLine(recommendation)]);
  const addSabyProduct = (candidate: NomenclatureCandidate) => {
    const recommendation = recommendationsFor(supplierId).find((item) => item.sabyId === candidate.sabyId);
    const line = recommendation ? makeLine(recommendation) : { ...makeLine(), id: candidate.sabyId, sabyId: candidate.sabyId, sabyName: candidate.name };
    setItems((current) => current.length === 1 && isEmptyLine(current[0]) ? [line] : [...current, line]);
    setSabyQuery(""); setSabyResults([]);
  };
  const sorted = [...items].sort((a, b) => (sortOrder.indexOf(a.id) + 1 || Infinity) - (sortOrder.indexOf(b.id) + 1 || Infinity));
  const selected = sorted.filter((item) => !isEmptyLine(item));
  const totalUnits = selected.reduce((sum, item) => sum + Number(item.packageCount) * Number(item.unitsPerPackage), 0);
  const missingFields = (item: DraftLine) => {
    const missing: string[] = [];
    if (!item.sabyId && !item.sabyName.trim()) missing.push("название");
    if (!item.category.trim()) missing.push("категория");
    if (!Number.isInteger(Number(item.packageCount)) || Number(item.packageCount) <= 0) missing.push("упаковки");
    if (!Number.isInteger(Number(item.unitsPerPackage)) || Number(item.unitsPerPackage) <= 0) missing.push("штук в упаковке");
    if (Number(item.packageCount) * Number(item.unitsPerPackage) > 1000000) missing.push("количество");
    if (!(decimalNumber(item.expectedUnitPrice) > 0)) missing.push("цена");
    if (!isDomestic && !(Number(item.potDiameterCm) > 0)) missing.push("горшок");
    if (!(Number(item.heightCm) > 0)) missing.push("высота");
    return missing;
  };
  const invalidItems = selected.filter((item) => missingFields(item).length > 0);
  const valid = selected.length > 0 && (isDomestic || Number(exchangeRate) > 0) && (isDomestic || Number(deliveryToMoscowRub) >= 0 && Number(deliveryToRyazanRub) >= 0) && selected.every((item) =>
    item.category.trim() && (item.sabyId || item.sabyName.trim()) && Number.isInteger(Number(item.packageCount)) && Number(item.packageCount) > 0 &&
    Number.isInteger(Number(item.unitsPerPackage)) && Number(item.unitsPerPackage) > 0 && Number(item.packageCount) * Number(item.unitsPerPackage) <= 1000000 &&
    decimalNumber(item.expectedUnitPrice) > 0 && Number(item.heightCm) > 0 && (isDomestic || Number(item.potDiameterCm) > 0));
  const totalPurchaseEUR = selected.reduce((sum, item) => sum + decimalNumber(item.expectedUnitPrice || "0") * Number(item.packageCount || 0) * Number(item.unitsPerPackage || 0), 0);
  const partialCost = (item: DraftLine) => {
    if (missingFields(item).length || (!isDomestic && !(Number(exchangeRate) > 0))) return null;
    const purchase = decimalNumber(item.expectedUnitPrice) * (isDomestic ? 1 : Number(exchangeRate));
    if (isDomestic) return purchase;
    const volume = Math.PI * (Number(item.potDiameterCm) / 2) ** 2 * Number(item.heightCm);
    return purchase + settings.trolleyCostRub * volume / (settings.trolleyVolumeCm3 * settings.trolleyFillRatio);
  };
  const requestBody = JSON.stringify({ supplierId, orderNumber: "",
    costs: { exchangeRate: isDomestic ? 1 : Number(exchangeRate), deliveryToMoscowRub: isDomestic ? 0 : Number(deliveryToMoscowRub), deliveryToRyazanRub: isDomestic ? 0 : Number(deliveryToRyazanRub) },
    items: selected.map((item) => ({ sabyId: item.sabyId, rawName: item.sabyName || item.category, category: item.category, supplierArticle: item.article,
      packageCount: Number(item.packageCount), unitsPerPackage: Number(item.unitsPerPackage),
      quantity: Number(item.packageCount) * Number(item.unitsPerPackage), expectedUnitPrice: decimalNumber(item.expectedUnitPrice),
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
      await saveQueueRef.current;
      if (hasDraftContent && draftSignature !== savedSignatureRef.current && !await persistDraft(draftSignature)) return;
      const result = await api<{ order: ProcurementOrder }>("/api/v1/admin/procurement/plans", { method: "POST", body: requestBody });
      if (!result.order?.id) throw new Error("Сервер не вернул номер заказа. Проверьте список закупок перед повторным сохранением.");
      if (remoteDraftRef.current) await api(`/api/v1/admin/procurement/plan-drafts/${remoteDraftRef.current.id}`, { method: "DELETE" }).catch(() => undefined);
      onSaved();
    } catch (error) { onError((error as Error).message); }
    finally { setSaving(false); }
  };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={() => void closePlan()} /><div className="admin-dialog procurement-plan-dialog procurement-plan-fullscreen" role="dialog" aria-modal="true" aria-labelledby="plan-title" onBlurCapture={(event) => { const row = (event.target as HTMLElement).closest("tr"); if (!row || !row.contains(event.relatedTarget as Node | null)) commitPlan(); }}><header><div><p className="eyebrow">{isDomestic ? "Российская закупка" : "Закупка в Голландии"}</p><h2 id="plan-title">Новый заказ поставщику</h2></div><button className="procurement-plan-close" onClick={() => void closePlan()} aria-label="Закрыть">×</button></header>
    <div className="procurement-plan-settings"><label>Поставщик<select value={supplierId} onChange={(event) => { if (selected.length && !window.confirm("Сменить поставщика и очистить текущий список?")) return; setSupplierId(Number(event.target.value)); setItems([makeLine()]); }}>{suppliers.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select></label>{!isDomestic && <><label>Курс, ₽/{currencySymbol}<input type="number" min="0" step="0.01" value={exchangeRate} onChange={(event) => setExchangeRate(draftNumber(event.target.value))} /></label><label>До Москвы, ₽<input type="number" min="0" value={deliveryToMoscowRub} placeholder="0" onChange={(event) => setDeliveryToMoscowRub(draftNumber(event.target.value))} /></label><label>Москва → Рязань, ₽<input type="number" min="0" value={deliveryToRyazanRub} placeholder="0" onChange={(event) => setDeliveryToRyazanRub(draftNumber(event.target.value))} /></label></>}</div>
    <div className="procurement-plan-tools">
      <label className="procurement-draft-title">Название плана<input value={title} maxLength={120} onChange={(event) => setTitle(event.target.value)} placeholder="Например, закупка для озеленения" /></label>
      <button onClick={() => setItems((current) => [...current, makeLine()])}>+ Новая строка</button>
      <span className="procurement-sort-label">Категория · цена ↑</span>
      {draftSignature === failedSignatureRef.current && <button onClick={() => void persistDraft(draftSignature)} disabled={draftSaving || draftLoading}>Повторить сохранение</button>}
      <small>{draftLoading ? "Загружаем черновик…" : draftSaving ? "Сохраняем для команды…" : remoteDraft && savedDraftSignature === draftSignature ? `Сохранено для команды ${new Date(remoteDraft.updatedAt).toLocaleString("ru-RU")}` : draftSignature === failedSignatureRef.current ? "Не удалось сохранить изменения" : hasDraftContent ? "Сохраним при выходе" : "План появится после заполнения"}</small>
      <span><strong>{selected.length} позиции</strong> · {totalUnits} шт.</span>
    </div>
    <div className="admin-table-wrap procurement-plan-table-wrap"><table className="admin-table procurement-plan-table"><thead><tr><th>Категория</th><th>Товар СБИС</th><th>Артикул</th><th>Горшок · высота</th><th>Упаковки · штук</th><th>Цена, {currencySymbol}</th><th>Себестоимость</th><th></th></tr></thead><tbody>{sorted.map((item) => { const units = Number(item.packageCount) * Number(item.unitsPerPackage); const selectedIndex = selected.indexOf(item); const lineMissing = missingFields(item); const estimate = selectedIndex >= 0 && valid && preview?.key === requestBody ? preview.lines[selectedIndex] : undefined; const preliminary = partialCost(item); return <tr key={item.id} className={item.rowTouched ? "procurement-row-touched" : undefined} onClick={() => markLineTouched(item.id)}><td><input value={item.category} onChange={(event) => updateItem(item.id, { category: event.target.value })} placeholder="Категория" /></td><td>{item.sabyId ? <strong>{item.sabyName}</strong> : <input aria-label="Название новой позиции" value={item.sabyName} onChange={(event) => updateItem(item.id, { sabyName: event.target.value })} placeholder="Название растения" />}<div className="procurement-product-meta">{item.recommendedQty > 0 && <small className="procurement-recommended">Рекомендовано: {item.recommendedQty} шт.</small>}{item.sabyId && <small className="procurement-stock-note">Остаток: {sabyBalances[item.sabyId] ? `${sabyBalances[item.sabyId].balance} шт.` : "—"}</small>}</div>{!item.potDiameterCm && !item.heightCm && (sabyBalances[item.sabyId]?.defaults?.length || 0) > 1 && <select aria-label={`Размер для ${item.sabyName}`} value="" onChange={(event) => { const value = sabyBalances[item.sabyId].defaults[Number(event.target.value)]; if (value) updateItem(item.id, { category: value.category, article: value.article || item.article, expectedUnitPrice: displayPrice(value.unitPrice), potDiameterCm: value.potDiameterCm?.toString() || "", heightCm: value.heightCm?.toString() || "", unitsPerPackage: String(value.unitsPerPackage) }); }}><option value="">Выберите размер</option>{sabyBalances[item.sabyId].defaults.map((value, index) => <option key={index} value={index}>{[value.potDiameterCm && `D${value.potDiameterCm}`, value.heightCm && `${value.heightCm} см`, value.unitPrice != null && `${value.unitPrice} ${selectedSupplier?.defaultCurrency || ""}`].filter(Boolean).join(" · ")}</option>)}</select>}</td><td><input value={item.article} onChange={(event) => updateItem(item.id, { article: event.target.value })} placeholder="Артикул" /></td><td><div className="procurement-size-inputs"><input aria-label="Горшок, см" type="text" inputMode="decimal" value={item.potDiameterCm} placeholder="P, см" onChange={(event) => updateItem(item.id, { potDiameterCm: draftNumber(event.target.value) })} /><input aria-label="Высота, см" type="text" inputMode="decimal" value={item.heightCm} placeholder="H, см" onChange={(event) => updateItem(item.id, { heightCm: draftNumber(event.target.value) })} /></div></td><td><div className="procurement-quantity-inputs"><input aria-label="Количество упаковок" type="text" inputMode="numeric" value={item.packageCount} placeholder="Кол-во" onChange={(event) => updateItem(item.id, { packageCount: draftNumber(event.target.value) })} /><span>×</span><input aria-label="Штук в упаковке" type="text" inputMode="numeric" value={item.unitsPerPackage} placeholder="Кратность" onChange={(event) => updateItem(item.id, { unitsPerPackage: draftNumber(event.target.value) })} /></div><small>{units || 0} {units === 1 ? "штука" : "штук"}</small></td><td><input aria-label={isDomestic ? "Цена в рублях" : "Цена в евро"} className="procurement-price-input" type="text" inputMode="decimal" value={item.expectedUnitPrice} placeholder="0,00" onChange={(event) => updateItem(item.id, { expectedUnitPrice: draftNumber(event.target.value) })} /></td><td className="procurement-cost-cell">{estimate ? <><strong>{money.format(estimate.cost)} / шт.</strong><small>Розница ≈ {money.format(estimate.retail)}</small></> : preliminary !== null ? <><strong>{money.format(preliminary)} / шт.</strong><small>{valid ? "Считаем…" : isDomestic ? "Оценка" : "Без доставки"}</small></> : <small>{lineMissing.length ? `Нет: ${lineMissing.join(", ")}` : "Укажите курс"}</small>}</td><td><button className="table-action danger" aria-label={`Удалить ${item.sabyName || "позицию"}`} onClick={() => setItems((current) => { const remaining = current.filter((line) => line.id !== item.id); return remaining.length ? remaining : [makeLine()]; })}>×</button></td></tr>; })}</tbody></table></div>
    <p className="admin-hint">{isDomestic ? "Российский план в рублях. Строки вводятся вручную." : "Расчёт доставки по объёму и высоте. Плановая цена сохраняется в закупке."}</p><div className="dialog-actions procurement-plan-footer"><div className="procurement-plan-total"><span>Сумма закупки</span><strong>{totalPurchaseEUR.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {currencySymbol}</strong><small>{invalidItems.length ? `Нужно проверить строк: ${invalidItems.length}` : `${totalUnits} шт.`}</small></div><button onClick={() => void closePlan()}>Закрыть</button><button className="primary" disabled={!supplierId || !valid || saving} onClick={save}>{saving ? "Считаем…" : "Закупка сделана →"}</button></div>
    <aside className="procurement-recommendation-drawer" aria-label="Рекомендации к закупке"><header><div><p className="eyebrow">Что добавить</p><h3>Рекомендации</h3></div></header><label className="procurement-saby-search"><span>Добавить товар из СБИС</span><input value={sabyQuery} onChange={(event) => setSabyQuery(event.target.value)} placeholder="Название, код или артикул" /><small>Каталог СБИС</small></label><div className="procurement-recommendation-list">{sabyQuery.trim().length >= 2 ? searchingSaby ? <div className="procurement-plan-empty"><span>Ищем в каталоге СБИС…</span></div> : sabyResults.length ? sabyResults.map((item) => <article key={item.sabyId} className="procurement-saby-result"><div><div className="procurement-saby-title"><strong>{item.name}</strong>{item.supplierLinked && <span>Инвойсы Голландии</span>}</div><b className="procurement-saby-code">{item.code || item.article || item.sabyId}</b><div className="procurement-recommendation-metrics"><span>Остаток {item.balance}</span></div><small className="procurement-channel-sales">Продажи и рекомендация подтянутся после добавления товара.</small></div><button className="admin-primary" onClick={() => addSabyProduct(item)}>+ Добавить</button></article>) : <div className="procurement-plan-empty"><strong>Товар не найден</strong><span>Проверьте название или обновите каталог СБИС.</span></div> : availableRecommendations.length ? availableRecommendations.map((item) => <article key={item.sabyId}><div><strong>{item.name}</strong><div className="procurement-recommendation-metrics"><span>Рекомендовано {item.suggestedQty}</span><span>Остаток {item.balance}</span><span>Продано {item.totalSales}</span></div></div><button className="admin-primary" onClick={() => addRecommendation(item)}>+ Добавить</button></article>) : null}</div></aside>
  </div></>;
}

export function ProcurementRequestDialog({ onClose, onSaved, onError }: { onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [kind, setKind] = useState("customer_order"); const [customerOrderId, setCustomerOrderId] = useState(""); const [name, setName] = useState(""); const [sabyId, setSabyId] = useState(""); const [quantity, setQuantity] = useState(1); const [notes, setNotes] = useState(""); const [saving, setSaving] = useState(false); const [candidates, setCandidates] = useState<NomenclatureCandidate[]>([]);
  useEffect(() => { if (name.trim().length < 2 || sabyId) { setCandidates([]); return; } const timer = window.setTimeout(() => api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(name.trim())}`).then((result) => setCandidates(result.items.slice(0, 6))).catch(() => setCandidates([])), 250); return () => window.clearTimeout(timer); }, [name, sabyId]);
  const save = async () => { setSaving(true); try { await api("/api/v1/admin/procurement/requests", { method: "POST", body: JSON.stringify({ kind, requestedName: name, sabyId, quantity, notes, source: "manual", customerOrderId: kind === "customer_order" && customerOrderId ? Number(customerOrderId) : null }) }); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="request-title"><header><h2 id="request-title">Добавить к закупке</h2><button onClick={onClose} aria-label="Закрыть">×</button></header><div className="admin-form-grid">
    <label>Тип<select value={kind} onChange={(event) => setKind(event.target.value)}><option value="customer_order">Заказ клиента</option><option value="staff_recommendation">Рекомендация магазина</option></select></label>
    <label>Количество<input type="number" min="1" value={quantity} onChange={(event) => setQuantity(Number(event.target.value))} /></label>
    {kind === "customer_order" && <label>Номер заказа в CRM<input type="number" min="1" value={customerOrderId} onChange={(event) => setCustomerOrderId(event.target.value)} placeholder="Необязательно" /></label>}
    <label className="wide">Название товара<input value={name} onChange={(event) => { setName(event.target.value); setSabyId(""); }} placeholder="Начните вводить название" /></label>
    {sabyId ? <div className="wide procurement-selected-product"><strong>Связано с товаром СБИС</strong><span>{sabyId}</span><button onClick={() => setSabyId("")}>Изменить</button></div> : candidates.length > 0 && <div className="wide procurement-request-candidates">{candidates.map((item) => <button key={item.sabyId} onClick={() => { setSabyId(item.sabyId); setName(item.name); }}><strong>{item.name}</strong><small>{item.code || item.article}</small></button>)}</div>}
    <label className="wide">Комментарий<textarea value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="Клиент, срок, цвет или другая важная деталь" /></label>
  </div><div className="dialog-actions"><button onClick={onClose}>Отмена</button><button className="primary" disabled={!name.trim() || quantity < 1 || saving} onClick={save}>Добавить</button></div></div></>;
}

export function ProcurementOrderDetailDialog({ orderId, canDelete, onClose, onSaved, onError }: { orderId: number; canDelete: boolean; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
  const [detail, setDetail] = useState<ProcurementOrderDetail | null>(null); const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [pairingLineID, setPairingLineID] = useState<number | null>(null);
  const [priceChannels, setPriceChannels] = useState<Record<string, boolean>>({ saby_price: false, site: false, wb: false, ozon: false, avito: false });
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
  const downloadReceivingPDF = async () => {
    const response = await fetch(`/api/v1/admin/procurement/orders/${orderId}/receiving.pdf`, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) {
      const result = await response.json().catch(() => ({})) as { error?: string };
      throw new Error(result.error || "Не удалось сформировать PDF приёмки");
    }
    const blob = await response.blob(); const url = URL.createObjectURL(blob);
    const link = document.createElement("a"); link.href = url;
    link.download = response.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] || `receiving-${orderId}.pdf`;
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
  const setInvoiceExcluded = async (line: ProcurementOrderLine, excluded: boolean) => { const reason = excluded ? window.prompt("Почему строка исключена? Причина сохранится в истории.", line.invoiceExclusionReason || "Поставщик не включил позицию") : ""; if (excluded && !reason?.trim()) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ invoiceExcluded: excluded, exclusionReason: reason || "" }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const acceptAllChanged = async () => { if (!detail) return; const lines = detail.lines.filter((line) => line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded); if (!lines.length || !window.confirm(`Принять все изменения по цене/количеству: ${lines.length}? Несопоставленные и отсутствующие позиции сюда не входят.`)) return; setSaving(true); try { await Promise.all(lines.map((line) => api(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ acceptComparison: true, comparisonNote: "Массово принято в сверке закупки" }) }))); await load(); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const matchInvoiceLine = async (plannedLine: ProcurementOrderLine, invoiceLine: ProcurementOrderLine) => { setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${plannedLine.id}`, { method: "PATCH", body: JSON.stringify({ invoiceLineId: invoiceLine.id }) }); setDetail(normalizeProcurementOrderDetail(item)); setPairingLineID(null); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const acceptAddedLine = async (line: ProcurementOrderLine) => { if (!window.confirm(`Добавить «${line.invoiceRawName || line.rawName}» в закупку как новую позицию поставщика?`)) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ acceptComparison: true, comparisonNote: "Подтверждено как новая позиция из инвойса" }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };
  const setStatus = async (status: "received" | "cancelled" | "review", force = false) => {
  const restoring = status === "review" && detail?.order.status === "cancelled";
  const confirmation = force
    ? "Принудительно завершить закупку? CRM не будет ждать подтверждения проведения из СБИС. Используйте это только если поступление уже действительно проведено."
    : status === "received"
      ? "Поступление уже проведено в СБИС? Закрыть закупку и выполнить клиентские заявки?"
      : status === "cancelled"
        ? "Отменить закупку? Заявки вернутся в открытые."
        : restoring
          ? "Вернуть отменённую закупку в работу? Она откроется на этапе проверки, а старые действия останутся в истории."
          : "Вернуть закупку на проверку? Активные действия будут отменены и останутся в истории.";
  if (!window.confirm(confirmation)) return;
  setSaving(true);
  try {
    const note = force ? "Закупка завершена вручную оператором без автоматического подтверждения СБИС" : "";
    const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${orderId}/status`, { method: "PATCH", body: JSON.stringify({ status, force, note }) });
    setDetail(normalizeProcurementOrderDetail(item));
    onSaved();
  } catch (error) { onError((error as Error).message); }
  finally { setSaving(false); }
};
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
  const pendingInvoiceLines = detail?.lines.filter((line) => line.reconciliationStatus === "added" && !line.comparisonAccepted && !line.invoiceExcluded) || [];
  const visibleLines = detail?.lines.filter((line) => line.reconciliationStatus !== "added" || line.comparisonAccepted || line.invoiceExcluded) || [];
  const changedLines = detail?.lines.filter((line) => line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded) || [];
  const pairingLine = pairingLineID == null ? null : detail?.lines.find((line) => line.id === pairingLineID) || null;
  const invoicePairCandidates = detail?.lines.filter((line) => line.id !== pairingLineID && !line.invoiceExcluded && line.reconciliationStatus !== "superseded" && line.invoicedQuantity != null && ((line.reconciliationStatus === "added" && !line.comparisonAccepted) || (line.orderedQuantity > 0 && ["matched", "changed"].includes(line.reconciliationStatus)))) || [];
  const renderBatch = (batch: ProcurementActionBatch) => <section className="procurement-batch" key={batch.id}><div className="admin-block-heading"><div><p className="eyebrow">{batch.kind === "prices" ? "Цены" : "Поступление"}</p><h3>{batch.kind === "prices" ? "Изменения по выбранным каналам" : "Поступление СБИС"}</h3></div><span className="admin-pill">{batch.kind === "receipt" && batch.status === "draft" ? "Подготовлено" : batchStatusLabel(batch.status)}</span></div><div className="procurement-batch-list">{batchDisplayRows(batch).map((row) => <article key={row.key}><div className="procurement-batch-product"><strong>{row.productName}</strong>{row.productCode && <small>Главный код: {row.productCode}</small>}</div><div><small>Канал</small><span>{channelLabel(row.item.channel)}</span>{(row.item.displayArticle || row.item.externalArticle) && !row.item.previewLines?.length && <small>{row.item.displayArticle || row.item.externalArticle}</small>}</div><div><small>{row.quantity == null ? "Было → станет" : "Остаток: было → станет"}</small><span>{row.quantity == null ? `${row.oldValue == null ? "Не получено" : money.format(row.oldValue)} → ${money.format(row.newValue)}` : `${row.oldBalance ?? 0} → ${row.newBalance ?? row.quantity}`}</span>{row.quantity != null && <small>Поступление: +{row.quantity}</small>}{row.item.compareAtValue && row.item.compareAtValue > row.newValue ? <small>до скидки {money.format(row.item.compareAtValue)}</small> : null}</div>{row.showStatus && <div className="procurement-action-status">{actionStatus(row.item)}</div>}</article>)}</div>{batch.status === "draft" && <div className="dialog-actions"><button className="primary" disabled={saving || !batch.items.length} onClick={() => void approve(batch)}>{batch.kind === "receipt" ? "Создать поступление в СБИС" : `Подтвердить ${batchDisplayRows(batch).length} строк`}</button></div>}{batch.items.some((item) => item.status === "failed") && <div className="dialog-actions"><button disabled={saving} onClick={() => void retry(batch)}>Повторить ошибки</button></div>}</section>;
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-order-dialog" role="dialog" aria-modal="true" aria-labelledby="order-detail-title"><header><div><p className="eyebrow">Закупка</p><h2 id="order-detail-title">{detail?.order.orderNumber || `№${orderId}`}</h2></div><button onClick={onClose} aria-label="Закрыть">×</button></header>
    {!detail ? <div className="procurement-zero">Загружаем строки…</div> : <div className="procurement-order-body">
      <div className="procurement-costs">{detail.order.currency !== "RUB" && <><label>Курс оплаты<input type="number" step="0.01" value={costs.exchangeRate} onChange={(event) => setCosts({ ...costs, exchangeRate: Number(event.target.value) })} /></label><label>Голландия → Москва, весь инвойс, ₽<input type="number" step="0.01" value={costs.deliveryToMoscowRub} onChange={(event) => setCosts({ ...costs, deliveryToMoscowRub: Number(event.target.value) })} /></label><label>Москва → Рязань, весь инвойс, ₽<input type="number" value={costs.deliveryToRyazanRub} onChange={(event) => setCosts({ ...costs, deliveryToRyazanRub: Number(event.target.value) })} /></label></>}<label className="secondary-button procurement-inline-upload">{uploading ? "Разбираем PDF…" : "Загрузить инвойс в эту закупку"}<input type="file" accept="application/pdf,.pdf" disabled={uploading} onChange={(event) => void uploadInvoice(event.target.files?.[0] || null)} /></label><button className="admin-primary" disabled={saving || !detail.validation.canCalculate || costs.exchangeRate <= 0} onClick={calculate}>{saving ? "Считаем…" : "Рассчитать"}</button></div>
      {detail.order.currency !== "RUB" && detail.costs.exchangeRate > 0 && detail.costs.currentDefaultExchangeRate > 0 && Math.abs(detail.costs.exchangeRate-detail.costs.currentDefaultExchangeRate) > .001 && <div className="procurement-rate-comparison"><span>В закупке сохранён курс {detail.costs.exchangeRate.toFixed(2)} ₽/€. Текущая настройка: {detail.costs.currentDefaultExchangeRate.toFixed(2)} ₽/€.</span><button onClick={() => setCosts({...costs,exchangeRate:detail.costs.currentDefaultExchangeRate})}>Подставить актуальный курс</button></div>}
      {detail.order.currency !== "RUB" ? <p className="admin-hint procurement-note">Телег в расчёте: {detail.validation.trolleyCount}. Доставка до Москвы будет разделена между ними автоматически.</p> : <p className="admin-hint procurement-note">Российская закупка рассчитывается сразу в рублях, без курса и голландской логистики.</p>}
      <section className={detail.validation.blockers?.length ? "procurement-checklist blocked" : "procurement-checklist ready"}><strong>{detail.validation.blockers?.length ? "Что нужно сделать дальше" : "Проверки пройдены"}</strong>{detail.validation.blockers?.length ? <ul>{detail.validation.blockers.map((blocker) => <li key={blocker}>{blocker === "Не загружен инвойс или счёт" ? "Загрузите PDF-инвойс кнопкой выше — он будет привязан именно к этой закупке" : blocker}</li>)}</ul> : <p>Инвойс, сопоставление, размеры и расхождения проверены.</p>}<small>Красные строки означают расхождение с инвойсом или отсутствие связи с товаром СБИС; сохранённые данные закупки при этом не теряются.</small><small>Телег: {detail.validation.trolleyCount} · распределено {money.format(detail.validation.allocatedTrolleyRub)} из {money.format(detail.validation.expectedTrolleyRub)} · Москва → Рязань {money.format(detail.validation.allocatedRyazanRub)} из {money.format(detail.validation.expectedRyazanRub)}</small></section>
      {changedLines.length > 0 && <div className="procurement-batch-buttons"><button disabled={saving} onClick={() => void acceptAllChanged()}>Принять все изменения ({changedLines.length})</button><small>Только изменения количества/цены. «Нет в инвойсе» и новые строки нужно разобрать отдельно.</small></div>}
      {pairingLine && <section className="procurement-checklist blocked"><strong>Выберите правильную строку инвойса для «{pairingLine.sabyName || pairingLine.rawName}»</strong><p>{pairingLine.reconciliationStatus === "missing" ? "После выбора строка инвойса перейдёт в исходную позицию закупки." : "Можно выбрать строку, которая сейчас привязана к другому товару: система поменяет две строки инвойса местами, сохранив товары СБИС и плановые параметры."}</p><div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>Строка инвойса</th><th>Сейчас привязана к</th><th>Размер · упаковка</th><th>Количество</th><th>Цена</th><th></th></tr></thead><tbody>{invoicePairCandidates.map((invoiceLine) => <tr key={invoiceLine.id}><td><strong>{invoiceLine.invoiceRawName || invoiceLine.rawName}</strong><small>{invoiceLine.invoiceSupplierArticle || "Артикул не указан"}</small></td><td>{invoiceLine.reconciliationStatus === "added" ? <span>Не сопоставлена</span> : <><strong>{invoiceLine.sabyName || invoiceLine.rawName}</strong><small>{invoiceLine.sabyId}</small></>}</td><td>{[invoiceLine.potDiameterCm && `D${invoiceLine.potDiameterCm}`, invoiceLine.heightCm && `${invoiceLine.heightCm} см`].filter(Boolean).join(" · ") || "—"}<small>{invoiceLine.unitsPerPackage && invoiceLine.packageCount ? `${invoiceLine.unitsPerPackage} × ${invoiceLine.packageCount} (шт. × мест)` : "Упаковка не указана"}</small></td><td>{invoiceLine.invoicedQuantity ?? "—"}</td><td>{invoiceLine.unitPrice.toFixed(2)} {detail.order.currency}</td><td><button disabled={saving} onClick={() => void matchInvoiceLine(pairingLine, invoiceLine)}>{invoiceLine.reconciliationStatus === "added" ? "Сопоставить" : "Поменять местами"}</button></td></tr>)}</tbody></table></div>{invoicePairCandidates.length === 0 && <div className="procurement-zero"><span>Других строк инвойса для выбора нет.</span></div>}<div className="dialog-actions"><button onClick={() => setPairingLineID(null)}>Отмена</button></div></section>}
      <div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>План</th><th>Инвойс</th><th>Сверка</th><th>Телега / размер</th><th>Упаковки</th><th>Количество</th><th>Цена план / факт</th><th>Себестоимость</th><th>Розница СБИС / новая</th><th></th></tr></thead><tbody>{visibleLines.map((line) => <tr key={line.id} className={line.comparisonMismatch && !line.comparisonAccepted ? "procurement-row-mismatch" : ""}><td><strong>{line.sabyName || line.rawName}</strong><small>{[line.supplierCategory,line.supplierArticle].filter(Boolean).join(" · ") || "Артикул не заполнен"}</small></td><td><strong>{line.invoiceRawName || (line.reconciliationStatus === "missing" ? "Позиция отсутствует" : "—")}</strong><small>{line.invoiceSupplierArticle || "Артикул в PDF не указан"}</small></td><td><span className={`procurement-reconciliation procurement-reconciliation-${line.reconciliationStatus}`}>{reconciliationLabel(line.reconciliationStatus)}</span>{line.invoiceExclusionReason && <small>{line.invoiceExclusionReason}</small>}{line.comparisonAccepted && <small className="procurement-ok">Расхождение принято</small>}</td><td>{line.loadUnit || "—"}<small>{[line.potDiameterCm && `D${line.potDiameterCm}`, line.heightCm && `${line.heightCm} см`].filter(Boolean).join(" · ") || "Размер не заполнен"}</small></td><td>{line.packageCount && line.unitsPerPackage ? `${line.packageCount} × ${line.unitsPerPackage}` : "—"}</td><td>{line.orderedQuantity || "—"} / {line.invoicedQuantity ?? "—"}</td><td>{line.expectedUnitPrice ? line.expectedUnitPrice.toFixed(2) : "—"} / {line.invoicedQuantity == null ? "—" : line.unitPrice.toFixed(2)} {detail.order.currency}</td><td>{line.unitCostRub == null ? "—" : money.format(line.unitCostRub)}<small>{line.currentUnitCostRub == null ? "Учётная себестоимость: неизвестно" : `Учётная себестоимость: ${money.format(line.currentUnitCostRub)} · ${line.currentUnitCostKind === "actual" ? "фактическая" : "оценочная"}`}</small></td><td className={line.priceChangeNeeded ? "procurement-retail-change" : ""}>{line.currentRetailRub > 0 ? money.format(line.currentRetailRub) : "—"} / <span className={line.priceChangeNeeded ? "procurement-price-new" : ""}>{line.proposedRetailRub == null ? "—" : money.format(line.proposedRetailRub)}</span><small>{line.priceChangeNeeded ? "СБИС сейчас / новая — требуется изменение" : "СБИС сейчас / новая по формуле"}</small></td><td><div className="procurement-inline-actions">{detail.order.status !== "ordered" && <button onClick={() => void editLine(line)}>Исправить размеры</button>}{line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded && <button onClick={() => void acceptMismatch(line)}>Принять расхождение</button>}{line.reconciliationStatus === "missing" && !line.invoiceExcluded && pendingInvoiceLines.length > 0 && <button onClick={() => setPairingLineID(line.id)}>Выбрать из инвойса</button>}{["matched","changed"].includes(line.reconciliationStatus) && !line.invoiceExcluded && line.invoicedQuantity != null && invoicePairCandidates.length > 0 && <button onClick={() => setPairingLineID(line.id)}>Сменить строку инвойса</button>}{["missing","added","changed"].includes(line.reconciliationStatus) && !line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, true)}>Исключить</button>}{line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, false)}>Вернуть в сверку</button>}</div></td></tr>)}</tbody></table></div>
      {pendingInvoiceLines.length > 0 && <section className="procurement-checklist blocked"><strong>Несопоставленные строки инвойса: {pendingInvoiceLines.length}</strong><p>Они пока не считаются частью закупки. Сопоставьте их с позицией «Нет в инвойсе» кнопкой выше либо подтвердите, что поставщик действительно добавил новую позицию.</p><div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>Инвойс</th><th>Количество</th><th>Цена</th><th>Связь с СБИС</th><th></th></tr></thead><tbody>{pendingInvoiceLines.map((line) => <tr key={line.id}><td><strong>{line.invoiceRawName || line.rawName}</strong><small>{line.invoiceSupplierArticle || "Артикул не указан"}</small></td><td>{line.invoicedQuantity ?? "—"}</td><td>{line.unitPrice.toFixed(2)} {detail.order.currency}</td><td>{line.sabyName || (line.matchStatus === "confirmed" ? line.sabyId : "Не сопоставлено")}</td><td><button disabled={saving} onClick={() => void acceptAddedLine(line)}>Это новая позиция</button></td></tr>)}</tbody></table></div></section>}
      {detail.order.status === "ready_to_receive" && <><div className="procurement-batch-buttons"><button className="admin-primary" disabled={saving || !detail.validation.canPrepareActions} onClick={() => void downloadReceivingPDF().catch((error) => onError((error as Error).message))}>Скачать PDF приёмки</button><small>Печатный лист: название СБИС, рассчитанная цена, количество, упаковка и размеры.</small></div><fieldset className="procurement-price-channels"><legend>Где подготовить изменение цен</legend>{[["saby_price", "СБИС — скачать XLSX"], ["site", "Сайт"], ["wb", "Wildberries"], ["ozon", "Ozon"], ["avito", "Авито"]].map(([channel, label]) => <label key={channel}><input type="checkbox" checked={priceChannels[channel]} onChange={(event) => setPriceChannels({ ...priceChannels, [channel]: event.target.checked })} />{label}</label>)}</fieldset><p className="admin-hint procurement-note">Для Saby будет скачан официальный формат «Код / Цена». Загрузите его в «Склад → Документы → Из файла» и проверьте документ перед проведением.</p><div className="procurement-batch-buttons"><button onClick={() => void setStatus("review")} disabled={saving}>Вернуть на проверку</button><button onClick={() => void prepare("receipt")} disabled={saving || !detail.validation.canPrepareActions}>Подготовить поступление СБИС</button><button className="admin-primary" onClick={() => void prepare("prices")} disabled={saving || !detail.validation.canPrepareActions || !Object.values(priceChannels).some(Boolean)}>Подготовить изменение цен</button></div></>}
      {detail.batches.filter((batch) => !["completed", "cancelled"].includes(batch.status)).map(renderBatch)}
{detail.batches.some((batch) => ["completed", "cancelled"].includes(batch.status)) && <details className="procurement-batch procurement-batch-history"><summary><strong>История поступлений и изменений цен</strong> · {detail.batches.filter((batch) => ["completed", "cancelled"].includes(batch.status)).length}</summary><div className="procurement-batch-history-list">{detail.batches.filter((batch) => ["completed", "cancelled"].includes(batch.status)).map(renderBatch)}</div></details>}
      {!['received', 'cancelled'].includes(detail.order.status) && <div className="procurement-order-final"><button className="text-button danger" disabled={saving} onClick={() => void setStatus("cancelled")}>Отменить закупку</button>{detail.order.status === "ready_to_receive" && detail.batches.some((batch) => batch.kind === "receipt" && batch.items.some((item) => item.channel === "saby_receipt" && item.status === "completed")) && <button className="admin-primary" disabled={saving} onClick={() => void setStatus("received")}>Проведение подтверждено — закрыть</button>}{detail.order.status === "ready_to_receive" && <><button className="secondary-button" disabled={saving} onClick={() => void setStatus("received", true)}>Завершить вручную</button><small>Ручное завершение не ждёт обратной проверки СБИС и сразу убирает закупку из «товара в пути».</small></>}</div>}
      {detail.order.status === "cancelled" && <div className="procurement-order-final"><button className="admin-primary" disabled={saving} onClick={() => void setStatus("review")}>Вернуть закупку в работу</button>{canDelete && <button className="text-button danger" disabled={saving} onClick={() => void remove()}>Удалить закупку и PDF</button>}<small>Восстановление вернёт закупку на проверку. Старые поступления и изменения цен останутся в свёрнутой истории.</small></div>}
    </div>}
  </div></>;
}

export const channelLabel = (value: string) => ({ site: "Сайт", saby_price: "СБИС — цена", saby_receipt: "СБИС — поступление", wb: "Wildberries", ozon: "Ozon", avito: "Авито" }[value] || value);

const batchDisplayRows = (batch: ProcurementActionBatch) => batch.items.flatMap((item) => item.previewLines?.length
  ? item.previewLines.map((line, index) => ({ key: `${item.id}-${line.sabyId}`, item, productName: line.name, productCode: [line.code, `ID ${line.sabyId}`].filter(Boolean).join(" · "), oldValue: line.oldPrice, newValue: line.newPrice || 0, quantity: line.quantity, oldBalance: line.oldBalance, newBalance: line.newBalance, showStatus: index === 0 }))
  : [{ key: String(item.id), item, productName: item.productName, productCode: item.productCode, oldValue: item.oldValue, newValue: item.newValue, quantity: item.quantity, oldBalance: undefined, newBalance: undefined, showStatus: true }]);

export const batchStatusLabel = (value: string) => ({ draft: "Черновик", processing: "Выполняется", completed: "Выполнено", partially_completed: "Выполнено частично", failed: "Ошибка", cancelled: "Отменено" }[value] || value);

export const actionStatus = (item: ProcurementActionItem) => {
  const label = item.channel === "saby_receipt" && item.status === "draft" ? "Подготовлено" : item.channel === "saby_receipt" && item.externalUrl && ["queued", "processing"].includes(item.status) ? "Черновик создан · ждёт проведения" : item.channel === "saby_receipt" && item.status === "completed" ? "Проведение подтверждено" : item.channel === "wb" && item.externalOperationId && ["queued", "processing"].includes(item.status) ? "Wildberries обрабатывает загрузку" : (({ draft: "Черновик", queued: "В очереди", processing: "Отправляется", completed: "Выполнено", failed: "Ошибка", skipped: "Пропущено", not_configured: "API не подключён" } as Record<string, string>)[item.status] || item.status);
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
    <div className="procurement-candidates">{searching ? <div className="procurement-zero"><span>Ищем в справочнике…</span></div> : items.length ? items.map((item) => <article key={item.sabyId}><div><strong>{item.name}</strong><span>{[item.code, item.article].filter(Boolean).join(" · ")}</span><small>Остаток СБИС: {item.balance} · {money.format(item.price)}</small></div><button disabled={saving} onClick={() => void resolve("confirmed", item.sabyId)}>Выбрать</button></article>) : <div className="procurement-zero"><strong>Кандидаты не найдены</strong><span>Импортируйте новую карточку СБИС или измените запрос.</span></div>}</div>
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

export function SupplierDialog({ suppliers, canDelete, onClose, onSaved, onError }: { suppliers: ProcurementSupplier[]; canDelete: boolean; onClose: () => void; onSaved: () => void; onError: (value: string) => void }) {
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
    {suppliers.length > 0 && <div className="admin-table-wrap"><table className="admin-table procurement-suppliers-table"><thead><tr><th>Поставщик</th><th>Тип</th>{canDelete && <th className="supplier-action-column">Действие</th>}</tr></thead><tbody>{suppliers.map((supplier) => <tr key={supplier.id}><td><strong>{supplier.name}</strong><small>{supplier.countryCode || "Страна не указана"} · {supplier.defaultCurrency}{supplier.taxId ? ` · ИНН ${supplier.taxId}` : ""}{supplier.kpp ? ` · КПП ${supplier.kpp}` : ""}</small></td><td>{supplier.kind === "international" ? "Иностранный" : "Российский"}</td>{canDelete && <td className="supplier-action-column"><button className="table-action danger supplier-delete-button" disabled={deletingId > 0} onClick={() => setDeleteCandidate(supplier)}>{deletingId === supplier.id ? "Удаляем…" : "Удалить"}</button></td>}</tr>)}</tbody></table></div>}
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
