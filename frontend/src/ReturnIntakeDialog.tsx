import {ReturnReceiptBadge} from "./ReturnState";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, Dialog } from "./adminShared";
import { downloadReturnLabels, returnMoney } from "./returnHelpers";
import { plantCount, returnConditions, returnChannels, returnDate } from "./returnWorkspace";
import { CatalogSearch } from "./CatalogSearch";
import type { MarketplaceReturn, ReturnIntake, ReturnIntakeData, ReturnIntakeRow, ReturnProduct } from "./adminTypes";

const rowConditions = { ready: "В продажу", restoring: "На восстановление", dead: "Списать" } as const;
const newRow = (): ReturnIntakeRow => ({ key: crypto.randomUUID(), variantId: 0, name: "", sku: "", search: "", quantity: 1, condition: "ready", comment: "" });
const dataJSON = (data?: ReturnIntakeData) => JSON.stringify(data && { channel: data.channel, channelComment: data.channelComment, rows: data.rows.map(row => ({key: row.key, variantId: row.variantId, name: row.name, sku: row.sku, search: row.search, quantity: row.quantity, condition: row.condition, comment: row.comment, discountPercent: row.discountPercent || 0})) });
const receiptLabels: Record<string, string> = { none: "", queued: "Создаём поступление в СБИС…", checking: "Проверяем поступление в СБИС…", draft_created: "Поступление создано в СБИС", posted: "Поступление проведено в СБИС", failed: "Не удалось создать поступление", correction_required: "Нужна корректировка в СБИС" };

export function ReturnIntakeDialog({ onClose, onCreated, canReceipt, intakeID, onItem, registerFlush }: { onClose: () => void; onCreated: (items: MarketplaceReturn[]) => void; canReceipt: boolean; intakeID?: string; onItem?:(item:MarketplaceReturn)=>void; registerFlush?:(flush:()=>Promise<void>)=>()=>void }) {
  const [products, setProducts] = useState<ReturnProduct[]>([]);
  const [intake, setIntake] = useState<ReturnIntake>();
  const [data, setData] = useState<ReturnIntakeData>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [error, setError] = useState("");
  const [invalidRows,setInvalidRows]=useState<string[]>([]);
  const [includeDate,setIncludeDate]=useState(true);
  const formRef=useRef<HTMLFormElement>(null);
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

  useEffect(()=>registerFlush?.(async()=>{await flush();if(dataJSON(dataRef.current)!==savedJSON.current)await flush();}),[flush,registerFlush]);

  const update = (next: ReturnIntakeData) => { setInvalidRows([]); dataRef.current = next; setData(next); setSaveState("dirty"); setError(""); };
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
    if (!data.rows.length || data.rows.some(row => !row.variantId || !Number.isInteger(row.quantity) || row.quantity < 1 || row.quantity > 100)) { const invalid=data.rows.filter(row=>!row.variantId||!Number.isInteger(row.quantity)||row.quantity<1||row.quantity>100);setInvalidRows(invalid.map(row=>row.key));setError("Заполните выделенные строки: выберите растение и количество от 1 до 100.");window.setTimeout(()=>formRef.current?.querySelector<HTMLElement>('input[aria-invalid="true"],[aria-invalid="true"] input')?.focus(),0); return; }
    setBusy(true); setError("");
    try {
      // A pristine new form has no server row yet. Complete forms necessarily
      // contain edits, which flush persists before this final operation.
      let current = await flush();
      if (dataJSON(dataRef.current) !== savedJSON.current) current = await flush();
      const { intake: next } = await api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}/${receipt ? "receipt" : "save"}`, { method: "POST", body: JSON.stringify({ revision: current.revision }) });
      intakeRef.current = next; setIntake(next);
      if (labels) await downloadReturnLabels(next.items.filter(item => item.condition === "restoring").map(item => item.id),includeDate);
      else if (!receipt) onCreated(next.items);
    } catch (cause) {
      try {
        const current = intakeRef.current;
        if (current) {
          const { intake: confirmed } = await api<{ intake: ReturnIntake }>(`/api/v1/admin/returns/intakes/${current.id}`);
          if (confirmed.status === "saved" && (!receipt || confirmed.receiptStatus !== "none")) {
            intakeRef.current = confirmed; setIntake(confirmed);
            if (labels) setError(cause instanceof Error ? cause.message : "Не удалось скачать этикетки. Попробуйте ещё раз.");
            else if (!receipt) onCreated(confirmed.items);
            return;
          }
        }
      } catch { /* Preserve the form and report the original error. */ }
      setError(cause instanceof Error ? cause.message : "Не удалось сохранить возврат");
    }
    finally { setBusy(false); }
  };
  const eligibleNames = intake?.status === "saved" ? intake.items.filter(item => item.condition === "ready" && item.receiptStatus === "none").reduce((result, item) => {const name = [item.productName, item.label].filter(Boolean).join(" · ");result.set(name,(result.get(name) || 0)+1);return result;}, new Map<string, number>()) : undefined;
  const readyQuantity = intake?.status === "saved" ? intake.items.filter(item => item.condition === "ready" && item.receiptStatus === "none").length : data?.rows.filter(row => row.condition === "ready" && row.variantId).reduce((sum, row) => sum + row.quantity, 0) || 0;

  const isSaved=intake?.status==="saved";
  const quantity=data?.rows.filter(row=>row.variantId>0).reduce((sum,row)=>sum+row.quantity,0)||0;
  const unfilled=data?.rows.filter(row=>!row.variantId).length||0;
  const restoring=isSaved?intake.items.some(item=>item.condition==="restoring"):Boolean(data?.rows.some(row=>row.condition==="restoring"&&row.variantId));
  return <Dialog title={isSaved?`Документ приёмки №${intake.documentNumber||""}`:"Принять возврат"} onClose={()=>void close()} className="return-create-dialog">
   {loading?<p className="returns-empty" role="status">Загружаем каталог и черновик…</p>:loadError?<div className="return-intake-error" role="alert"><p>{loadError}</p><button type="button" onClick={()=>void load()}>Попробовать снова</button></div>:data&&<form ref={formRef} noValidate onSubmit={event=>{event.preventDefault();void accept(false);}}>
    <div className="return-intake-content">
     {isSaved?<div className="return-saved-summary"><span>{returnDate(intake.createdAt||"")} · {returnChannels[data.channel]}</span><span className="return-autosave">Документ сохранён</span>{data.channelComment&&<p>{data.channelComment}</p>}</div>:<><div className="return-intake-top"><label>Канал<select aria-label="Канал" value={data.channel} disabled={busy} onChange={event=>update({...data,channel:event.target.value as ReturnIntakeData["channel"]})}><option value="ozon">Озон</option><option value="wb">ВБ</option><option value="other">Другое</option></select></label><span role="status" aria-live="polite" className={`return-autosave ${saveState}`}>{saveState==="saving"?"Сохраняется…":saveState==="dirty"?"Есть изменения…":saveState==="error"?"Не сохранено":intake?.revision?"Черновик сохранён":"Автосохранение включено"}</span></div>{data.channel==="other"&&<label className="return-comment return-other-comment"><span>Откуда пришёл возврат</span><textarea aria-label="Комментарий к каналу" maxLength={4000} placeholder="Например: возврат покупателя в магазине" value={data.channelComment} disabled={busy} onChange={event=>update({...data,channelComment:event.target.value})}/></label>}<p className="return-field-note">Черновик сохраняется автоматически. Нажмите «Сохранить возврат», чтобы завершить приёмку.</p></>}
     <div className="return-plants-heading"><h3>{isSaved?"Состав при приёмке":"Растения"}</h3><span>{plantCount(quantity)}{unfilled>0?` · незаполненных строк: ${unfilled}`:""}</span></div>
     {isSaved?<><div className="return-saved-table-wrap"><table className="return-saved-table"><thead><tr><th>Растение</th><th>Количество</th><th>Оценка при приёмке</th><th>Комментарий</th></tr></thead><tbody>{data.rows.map(row=><tr key={row.key}><td><strong>{row.search||row.name}</strong><small>{returnMoney(intake.items.find(item=>item.variantId===row.variantId)?.price??products.find(item=>item.variantId===row.variantId)?.price)}</small></td><td>{row.quantity}</td><td>{rowConditions[row.condition]}</td><td>{row.comment||"—"}</td></tr>)}</tbody></table></div><div className="return-plants-heading"><h3>Текущее состояние экземпляров</h3></div><p className="return-field-note">Оценки меняются в карточках растений. Первоначальный состав документа сохраняется.</p><div className="return-document-units">{intake.items.map(item=><article key={item.id}><div><strong>{item.productName} {item.label}</strong><small>{item.article}</small><span>{returnConditions[item.condition].label}</span>{item.condition==="ready"&&<ReturnReceiptBadge item={item}/>}</div><button type="button" onClick={()=>onItem?.(item)}>Открыть экземпляр</button></article>)}</div></>:<><table className="return-intake-table"><thead><tr><th>Название</th><th>Количество</th><th>Состояние</th><th>Комментарий</th><th aria-label="Удаление строки"/></tr></thead><tbody>{data.rows.map((row,index)=><tr key={row.key} className={invalidRows.includes(row.key)?"return-row-invalid":""}>
      <td data-label="Название"><fieldset disabled={busy} aria-invalid={invalidRows.includes(row.key)&&!row.variantId} aria-describedby={invalidRows.includes(row.key)?`return-row-error-${index}`:undefined}><CatalogSearch className="return-plant-picker" placeholder="Выберите растение или введите часть названия" value={row.search} onChange={value=>updateRow(row.key,{search:value,variantId:0,name:"",sku:""})} selection={{products:catalogue,label:`Название растения ${index+1}`,onSelect:product=>{const selected=products.find(item=>item.variantId===Number(product.id));if(selected)updateRow(row.key,{variantId:selected.variantId,name:selected.name,sku:selected.sku,search:[selected.name,selected.label].filter(Boolean).join(" · ")});}}}/></fieldset>{row.variantId>0&&<small className="return-plant-sku">{returnMoney(products.find(item=>item.variantId===row.variantId)?.price)}</small>}{invalidRows.includes(row.key)&&<small className="return-inline-error" id={`return-row-error-${index}`}>{!row.variantId?"Выберите растение из каталога":"Укажите количество от 1 до 100"}</small>}</td>
      <td data-label="Количество"><input type="number" aria-label={`Количество ${index+1}`} aria-invalid={invalidRows.includes(row.key)&&(row.quantity<1||row.quantity>100)} min={1} max={100} step={1} value={row.quantity||""} disabled={busy} onFocus={event=>event.target.select()} onChange={event=>updateRow(row.key,{quantity:Math.min(100,Math.max(0,Math.trunc(Number(event.target.value))))})}/></td>
      <td data-label="Состояние"><select aria-label={`Состояние ${index+1}`} value={row.condition} disabled={busy} onChange={event=>updateRow(row.key,{condition:event.target.value as ReturnIntakeRow["condition"]})}>{Object.entries(rowConditions).map(([value,label])=><option key={value} value={value}>{label}</option>)}</select>{row.condition==="restoring"&&<label className="return-row-discount">Скидка, %<input type="number" aria-label={`Скидка ${index+1}`} min={0} max={100} step={1} value={row.discountPercent||0} disabled={busy} onFocus={event=>event.target.select()} onChange={event=>updateRow(row.key,{discountPercent:Math.min(100,Math.max(0,Math.round(Number(event.target.value))))})}/><strong>{returnMoney(Math.round(Math.round((products.find(item=>item.variantId===row.variantId)?.price||0)*100)*(100-(row.discountPercent||0))/100)/100)}</strong></label>}</td>
      <td data-label="Комментарий"><textarea aria-label={`Комментарий ${index+1}`} maxLength={4000} value={row.comment} placeholder="По растению" disabled={busy} onChange={event=>updateRow(row.key,{comment:event.target.value})}/></td>
      <td><button type="button" className="return-remove-row" aria-label={`Удалить растение ${index+1}`} disabled={busy} onClick={()=>update({...data,rows:data.rows.filter(item=>item.key!==row.key)})}>×</button></td>
     </tr>)}</tbody></table><button type="button" className="return-add-row" disabled={busy||data.rows.length>=100} onClick={()=>update({...data,rows:[...data.rows,newRow()]})}>+ Добавить растение</button></>}
     {intake&&intake.receiptStatus!=="none"&&<div className="return-intake-receipt" role="status"><strong>{intake.receiptExternalUrl?intake.receiptStatus==="posted"?"Поступление проведено":"Черновик поступления создан в СБИС":receiptLabels[intake.receiptStatus]}</strong>{intake.receiptStatus==="failed"&&!intake.receiptExternalUrl&&<p>Проверьте существующую операцию и связь товаров с СБИС перед повторным созданием.</p>}{intake.receiptExternalUrl&&<a className="returns-action" href={intake.receiptExternalUrl} target="_blank" rel="noreferrer">Открыть поступление ↗</a>}{intake.receiptStatus!=="posted"&&<p>После создания откройте и проведите документ в СБИС.</p>}</div>}
    </div>
    <footer>{error&&<div className="return-intake-error" role="alert"><p>{error}</p>{saveState==="error"&&<button type="button" disabled={busy} onClick={()=>void flush().catch(()=>{})}>Повторить сохранение</button>}</div>}{readyQuantity>0&&<div className="return-receipt-hint">В новое поступление: {plantCount(readyQuantity)}. Восстановление, списание и уже оформленные экземпляры исключены.{!isSaved&&<span> При создании поступления приёмка также будет сохранена.</span>}{eligibleNames&&<p>{[...eligibleNames].map(([name,count])=>`${name} × ${count}`).join("; ")}</p>}</div>}{restoring&&<label className="return-check"><input type="checkbox" checked={includeDate} onChange={event=>setIncludeDate(event.target.checked)}/><span>Печатать дату на этикетке</span></label>}<div className="return-intake-actions">{restoring&&<button type="button" disabled={busy} onClick={()=>void accept(false,true)}>{isSaved?"Скачать этикетки":"Сохранить и скачать этикетки"}</button>}{canReceipt&&readyQuantity>0&&<button type="button" className={`return-receipt-button ${isSaved?"returns-action":""}`} disabled={busy||["queued","checking"].includes(intake?.receiptStatus||"")} onClick={()=>void accept(true)}>Создать поступление</button>}{isSaved?<button type="button" disabled={busy} onClick={()=>void close()}>Закрыть документ</button>:<button type="submit" className="primary" disabled={busy||!data.rows.length}>{busy?"Сохраняем…":"Сохранить возврат"}</button>}</div></footer>
   </form>}
  </Dialog>;
}
