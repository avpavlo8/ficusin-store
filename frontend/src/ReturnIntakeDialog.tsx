import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, Dialog } from "./adminShared";
import { downloadReturnLabels, returnMoney } from "./returnHelpers";
import { CatalogSearch } from "./CatalogSearch";
import type { MarketplaceReturn, ReturnIntake, ReturnIntakeData, ReturnIntakeRow, ReturnProduct } from "./adminTypes";

const rowConditions = { ready: "В продажу", restoring: "На восстановление", dead: "Списать" } as const;
const newRow = (): ReturnIntakeRow => ({ key: crypto.randomUUID(), variantId: 0, name: "", sku: "", search: "", quantity: 1, condition: "ready", comment: "" });
const dataJSON = (data?: ReturnIntakeData) => JSON.stringify(data && { channel: data.channel, channelComment: data.channelComment, rows: data.rows.map(row => ({key: row.key, variantId: row.variantId, name: row.name, sku: row.sku, search: row.search, quantity: row.quantity, condition: row.condition, comment: row.comment, discountPercent: row.discountPercent || 0})) });
const receiptLabels: Record<string, string> = { none: "", queued: "Создаём поступление в СБИС…", checking: "Проверяем поступление в СБИС…", draft_created: "Поступление создано в СБИС", posted: "Поступление проведено в СБИС", failed: "Не удалось создать поступление", correction_required: "Нужна корректировка в СБИС" };

export function ReturnIntakeDialog({ onClose, onCreated, canReceipt, intakeID }: { onClose: () => void; onCreated: (items: MarketplaceReturn[]) => void; canReceipt: boolean; intakeID?: string }) {
  const [products, setProducts] = useState<ReturnProduct[]>([]);
  const [intake, setIntake] = useState<ReturnIntake>();
  const [data, setData] = useState<ReturnIntakeData>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [error, setError] = useState("");
  const [saveState, setSaveState] = useState<"clean" | "dirty" | "saving" | "error">("clean");
  const [busy, setBusy] = useState(false);
  const dataRef = useRef<ReturnIntakeData>(undefined);
  const intakeRef = useRef<ReturnIntake>(undefined);
  const savedJSON = useRef("");
  const inFlight = useRef<Promise<ReturnIntake> | undefined>(undefined);
  const loaded = useRef(false);

  const load = useCallback(async () => {
    setLoading(true); setLoadError("");
    try {
      const [catalogue, draft] = await Promise.all([api<{ items: ReturnProduct[] }>("/api/v1/admin/returns/products"), api<{ intake: ReturnIntake | null }>(intakeID ? `/api/v1/admin/returns/intakes/${intakeID}` : "/api/v1/admin/returns/intake-draft")]);
      const next = draft.intake || { id: crypto.randomUUID(), revision: 0, data: { channel: "ozon", channelComment: "", rows: [newRow()] }, status: "draft", receiptStatus: "none", items: [] } satisfies ReturnIntake;
      setProducts(catalogue.items); setIntake(next); setData(next.data);
      intakeRef.current = next; dataRef.current = next.data; savedJSON.current = dataJSON(next.data); loaded.current = true;
    } catch (cause) { setLoadError(cause instanceof Error ? cause.message : "Не удалось загрузить каталог"); }
    finally { setLoading(false); }
  }, [intakeID]);
  useEffect(() => { void load(); }, [load]);

  // Only one request writes at a time. Edits made during it are saved next;
  // the older response can update the revision, never the user's newer data.
  const flush = useCallback(async (): Promise<ReturnIntake> => {
    while (inFlight.current) await inFlight.current;
    const current = intakeRef.current, snapshot = dataRef.current;
    if (!current || !snapshot) throw new Error("Форма ещё загружается");
    const json = dataJSON(snapshot);
    if (current.status === "saved" || json === savedJSON.current) return current;
    setSaveState("saving"); setError("");
    const applied = (result: ReturnIntake) => {
      intakeRef.current = result; setIntake(result); savedJSON.current = json;
      setSaveState(dataJSON(dataRef.current) === json ? "clean" : "dirty");
      return result;
    };
    const pending = api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}`, { method: "PUT", body: JSON.stringify({ revision: current.revision, data: snapshot }) }).then(({ intake: result }) => applied(result)).catch(async cause => {
      // The write may have succeeded even when its response was lost. Verify
      // the exact snapshot before using a newer server revision for next edits.
      try {
        const { intake: confirmed } = await api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}`);
        if (confirmed.status === "draft" && dataJSON(confirmed.data) === json) return applied(confirmed);
      } catch { /* The original write error is the useful message. */ }
      setSaveState("error"); setError(cause instanceof Error ? cause.message : "Не удалось сохранить черновик"); throw cause;
    });
    inFlight.current = pending;
    try { return await pending; } finally { if (inFlight.current === pending) inFlight.current = undefined; }
  }, []);

  const update = (next: ReturnIntakeData) => { dataRef.current = next; setData(next); setSaveState("dirty"); setError(""); };
  const updateRow = (key: string, patch: Partial<ReturnIntakeRow>) => { if (dataRef.current) update({ ...dataRef.current, rows: dataRef.current.rows.map(row => row.key === key ? { ...row, ...patch } : row) }); };
  useEffect(() => {
    if (!data || intake?.status === "saved" || dataJSON(data) === savedJSON.current) return;
    const timer = window.setTimeout(() => { void flush().catch(() => {}); }, 700);
    return () => window.clearTimeout(timer);
  }, [data, intake?.status, flush]);
  useEffect(() => {
    const dirty = () => dataJSON(dataRef.current) !== savedJSON.current || Boolean(inFlight.current);
    const beforeUnload = (event: BeforeUnloadEvent) => { if (loaded.current && dirty()) event.preventDefault(); };
    const hide = () => { if (loaded.current && document.visibilityState === "hidden" && dirty()) void flush().catch(() => {}); };
    window.addEventListener("beforeunload", beforeUnload); document.addEventListener("visibilitychange", hide);
    return () => {
      window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("visibilitychange", hide);
      if (loaded.current && dirty()) void flush().catch(() => {});
    };
  }, [flush]);
  useEffect(() => {
    if (intake?.status !== "saved" || !["queued", "checking"].includes(intake.receiptStatus)) return;
    const timer = window.setInterval(() => {
      api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${intake.id}`).then(({ intake: next }) => { intakeRef.current = next; setIntake(next); setError(""); }).catch(cause => setError(cause instanceof Error ? cause.message : "Не удалось проверить поступление"));
    }, 3000);
    return () => window.clearInterval(timer);
  }, [intake?.id, intake?.status, intake?.receiptStatus]);

  const catalogue = useMemo(() => products.map(product => ({ id: String(product.variantId), name: product.name, latin: product.label, price: product.price, catalogSection: "plants" })), [products]);
  const close = async () => {
    if (busy) return;
    if (!loaded.current) { onClose(); return; }
    setBusy(true);
    try { await flush(); if (dataJSON(dataRef.current) !== savedJSON.current) await flush(); const current = intakeRef.current; if (current?.status === "saved") onCreated(current.items); else onClose(); }
    catch { /* Keep the form open with its inline error and unsaved values. */ }
    finally { setBusy(false); }
  };
  const accept = async (receipt: boolean, labels = false) => {
    if (!data || busy) return;
    if (!data.rows.length || data.rows.some(row => !row.variantId || !Number.isInteger(row.quantity) || row.quantity < 1 || row.quantity > 100)) { setError("Выберите растение из каталога и укажите количество от 1 до 100 в каждой строке."); return; }
    setBusy(true); setError("");
    try {
      // A pristine new form has no server row yet. Complete forms necessarily
      // contain edits, which flush persists before this final operation.
      let current = await flush();
      if (dataJSON(dataRef.current) !== savedJSON.current) current = await flush();
      const { intake: next } = await api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}/${receipt ? "receipt" : "save"}`, { method: "POST", body: JSON.stringify({ revision: current.revision }) });
      intakeRef.current = next; setIntake(next);
      if (labels) await downloadReturnLabels(next.items.filter(item => item.condition === "restoring").map(item => item.id));
      else if (!receipt) onCreated(next.items);
    } catch (cause) {
      try {
        const current = intakeRef.current;
        if (current) {
          const { intake: confirmed } = await api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}`);
          if (confirmed.status === "saved" && (!receipt || confirmed.receiptStatus !== "none")) {
            intakeRef.current = confirmed; setIntake(confirmed);
            if (!receipt) onCreated(confirmed.items);
            return;
          }
        }
      } catch { /* Preserve the form and report the original error. */ }
      setError(cause instanceof Error ? cause.message : "Не удалось сохранить возврат");
    }
    finally { setBusy(false); }
  };
  const locked = busy || intake?.status === "saved";
  const readyQuantity = intake?.status === "saved" ? intake.items.filter(item => item.condition === "ready" && item.receiptStatus === "none").length : data?.rows.filter(row => row.condition === "ready" && row.variantId).reduce((sum, row) => sum + row.quantity, 0) || 0;

  return <Dialog title={intake?.status === "saved" ? `Возврат №${intake.documentNumber || ""}${intake.createdAt ? ` · ${new Date(intake.createdAt).toLocaleDateString("ru-RU")}` : ""}` : "Принять возврат"} onClose={() => void close()} className="return-create-dialog">
    {loading ? <p className="returns-empty">Загружаем каталог и черновик…</p> : loadError ? <div className="return-intake-error" role="alert"><p>{loadError}</p><button onClick={() => void load()}>Попробовать снова</button></div> : data && <form onSubmit={event => { event.preventDefault(); void accept(false); }}>
      <div className="return-intake-top"><label>Канал<select aria-label="Канал" value={data.channel} disabled={locked} onChange={event => update({ ...data, channel: event.target.value as ReturnIntakeData["channel"] })}><option value="ozon">Озон</option><option value="wb">ВБ</option><option value="other">Другое</option></select></label>
        <span role="status" aria-live="polite" className={`return-autosave ${saveState}`}>{intake?.status === "saved" ? "Возврат сохранён" : saveState === "saving" ? "Сохраняется…" : saveState === "dirty" ? "Есть изменения…" : saveState === "error" ? "Не сохранено" : intake?.revision ? "Черновик сохранён" : "Автосохранение включено"}</span>
      </div>
      {data.channel === "other" && <label className="return-comment return-other-comment"><span>Комментарий к каналу</span><textarea aria-label="Комментарий к каналу" maxLength={4000} placeholder="Откуда пришёл возврат" value={data.channelComment} disabled={locked} onChange={event => update({ ...data, channelComment: event.target.value })} /></label>}
      <div className="return-plants-heading"><h3>{intake?.status === "saved" ? "Растения при приёмке" : "Растения"}</h3><span>{data.rows.reduce((sum, row) => sum + row.quantity, 0)} шт.</span></div>
      <table className="return-intake-table"><thead><tr><th>Название</th><th>Количество</th><th>Состояние</th><th>Комментарий</th><th aria-label="Удаление строки" /></tr></thead><tbody>
        {data.rows.map((row, index) => <tr key={row.key}>
          <td data-label="Название"><fieldset disabled={locked}><CatalogSearch className="return-plant-picker" placeholder="Выберите растение или введите часть названия" value={row.search} onChange={value => updateRow(row.key, { search: value, variantId: 0, name: "", sku: "" })} selection={{ products: catalogue, label: `Название растения ${index + 1}`, onSelect: product => { const selected = products.find(item => item.variantId === Number(product.id)); if (selected) updateRow(row.key, { variantId: selected.variantId, name: selected.name, sku: selected.sku, search: [selected.name, selected.label].filter(Boolean).join(" · ") }); } }} /></fieldset>{row.variantId > 0 && <small className="return-plant-sku">{returnMoney(intake?.items.find(item => item.variantId === row.variantId)?.price ?? products.find(item => item.variantId === row.variantId)?.price)}</small>}</td>
          <td data-label="Количество"><input type="number" aria-label={`Количество ${index + 1}`} min={1} max={100} step={1} value={row.quantity || ""} disabled={locked} onFocus={event => event.target.select()} onChange={event => updateRow(row.key, { quantity: Math.min(100, Math.max(0, Math.trunc(Number(event.target.value)))) })} /></td>
          <td data-label="Состояние"><select aria-label={`Состояние ${index + 1}`} value={row.condition} disabled={locked} onChange={event => updateRow(row.key, { condition: event.target.value as ReturnIntakeRow["condition"] })}>{Object.entries(rowConditions).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>{row.condition === "restoring" && <label className="return-row-discount">Скидка, %<input type="number" aria-label={`Скидка ${index + 1}`} min={0} max={100} value={row.discountPercent || 0} disabled={locked} onChange={event => updateRow(row.key, { discountPercent: Math.min(100, Math.max(0, Math.round(Number(event.target.value)))) })} /><small>{returnMoney((intake?.items.find(item => item.variantId === row.variantId)?.price ?? products.find(item => item.variantId === row.variantId)?.price ?? 0) * (100 - (row.discountPercent || 0)) / 100)}</small></label>}</td>
          <td data-label="Комментарий"><textarea aria-label={`Комментарий ${index + 1}`} maxLength={4000} value={row.comment} placeholder="По растению" disabled={locked} onChange={event => updateRow(row.key, { comment: event.target.value })} /></td>
          <td><button type="button" className="return-remove-row" aria-label={`Удалить растение ${index + 1}`} disabled={locked} onClick={() => update({ ...data, rows: data.rows.filter(item => item.key !== row.key) })}>×</button></td>
        </tr>)}
      </tbody></table>
      {!locked && <button type="button" className="return-add-row" disabled={data.rows.length >= 100} onClick={() => update({ ...data, rows: [...data.rows, newRow()] })}>＋ Добавить растение</button>}
      {error && <div className="return-intake-error" role="alert"><p>{error}</p>{saveState === "error" && <button type="button" disabled={busy} onClick={() => void flush().catch(() => {})}>Повторить сохранение</button>}</div>}
      {intake && intake.receiptStatus !== "none" && <div className="return-intake-receipt" role="status"><strong>{intake.receiptExternalUrl ? "Поступление создано в СБИС" : receiptLabels[intake.receiptStatus]}</strong>{intake.receiptStatus === "failed" && !intake.receiptExternalUrl && <p>Поступление не создано. Проверьте связь товаров с СБИС.</p>}{intake.receiptExternalUrl && <a className="returns-action" href={intake.receiptExternalUrl} target="_blank" rel="noreferrer">Открыть поступление ↗</a>}<p>Проведите документ в СБИС, чтобы растения появились в остатках.</p></div>}
      <footer>{readyQuantity > 0 && <div className="return-receipt-hint">В поступление попадут только растения «В продажу»{readyQuantity > 0 ? ` — ${readyQuantity} шт.` : "."}</div>}<div className="return-intake-actions">{(intake?.status === "saved" ? intake.items.some(item => item.condition === "restoring") : data.rows.some(row => row.condition === "restoring" && row.variantId)) && <button type="button" disabled={busy} onClick={() => void accept(false, true)}>Скачать этикетки</button>}{canReceipt && readyQuantity > 0 && <button type="button" className="return-receipt-button" disabled={busy || ["queued", "checking"].includes(intake?.receiptStatus || "")} onClick={() => void accept(true)}>Создать поступление</button>}<button className="primary" disabled={busy || !data.rows.length}>{busy ? "Сохраняем…" : "Сохранить"}</button></div></footer>
    </form>}
  </Dialog>;
}
