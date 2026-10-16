import { useState } from "react";
import { api } from "./adminShared";
import type { ProcurementOrderLine } from "./adminTypes";
import { useProcurementDialog, useProcurementError } from "./procurementUX";

export function ProcurementLineEditor({line,kind,currency,onClose,onSaved,onError}:{line:ProcurementOrderLine;kind:"dimensions"|"comparison"|"exclusion";currency:string;onClose:()=>void;onSaved:()=>void;onError:(message:string)=>void}) {
  const [pot,setPot]=useState(line.potDiameterCm?.toString() || "");
  const [height,setHeight]=useState(line.heightCm?.toString() || "");
  const [loadUnit,setLoadUnit]=useState(line.loadUnit || "");
  const [note,setNote]=useState(kind==="exclusion"?line.invoiceExclusionReason || "":line.comparisonNote || "");
  const [saving,setSaving]=useState(false);
  const {error,errorRef,notify}=useProcurementError(onError);
  const close=()=>{if(!saving)onClose();};
  const ref=useProcurementDialog(close);
  const reviewingDimensions=kind==="dimensions" && Boolean(line.dimensionWarning);
  const title=kind==="dimensions"?(reviewingDimensions?"Проверить размеры":"Размеры и размещение"):kind==="comparison"?"Принять расхождение":"Исключить позицию";
  const number=(value:string)=>Number(value.replace(",","."));
  const valid=kind==="dimensions"?Number.isFinite(number(height)) && Number.isFinite(number(pot)) && number(height)>0 && (currency==="RUB"?pot==="" || number(pot)>=0:number(pot)>0):Boolean(note.trim());
  const save=async()=>{
    if(!valid || saving)return;
    setSaving(true);
    const body=kind==="dimensions"?{potDiameterCm:pot===""?null:number(pot),heightCm:number(height),loadUnit:loadUnit.trim()}:kind==="comparison"?{acceptComparison:true,comparisonNote:note.trim()}:{invoiceExcluded:true,exclusionReason:note.trim()};
    try{await api(`/api/v1/admin/procurement/order-lines/${line.id}`,{method:"PATCH",body:JSON.stringify(body)});onSaved();}
    catch(error){notify((error as Error).message);}finally{setSaving(false);}
  };
  return <><button tabIndex={-1} className="admin-dialog-backdrop procurement-receiving-backdrop" aria-label="Закрыть изменение позиции" disabled={saving} onClick={close}/><div className="admin-dialog procurement-receiving-dialog" ref={ref} role="dialog" aria-modal="true" aria-labelledby="line-editor-title"><header><div><p className="eyebrow">{line.rawName}</p><h2 id="line-editor-title">{title}</h2></div><button aria-label="Закрыть изменение позиции" disabled={saving} onClick={close}>×</button></header><form onSubmit={event=>{event.preventDefault();void save();}}><div className="procurement-receiving-body">{kind==="dimensions"?<><p>Инвойс: {line.matchAlias?.potDiameterCm?`D${line.matchAlias.potDiameterCm}`:"D не указан"}, {line.matchAlias?.heightCm?`H${line.matchAlias.heightCm} см`:"H не указана"}. Карточка: {line.catalogueLabel || "размер не указан"}.</p>{reviewingDimensions && <p className="procurement-warning">Сверьте выбранный товар и размеры. Если товар выбран верно, подтвердите размеры из инвойса или укажите измеренные. Они будут использованы для расчёта доставки.</p>}{Boolean(line.matchAlias?.potDiameterCm && line.matchAlias?.heightCm) && <button type="button" disabled={saving} onClick={()=>{setPot(String(line.matchAlias!.potDiameterCm));setHeight(String(line.matchAlias!.heightCm));}}>Использовать размеры инвойса</button>}<div className="admin-form-grid"><label>Диаметр горшка, см<input disabled={saving} inputMode="decimal" value={pot} onChange={event=>setPot(event.target.value)} required={currency!=="RUB"}/></label><label>Высота растения, см<input disabled={saving} inputMode="decimal" value={height} onChange={event=>setHeight(event.target.value)} required/></label><label className="wide">Телега / коробка<input disabled={saving} value={loadUnit} onChange={event=>setLoadUnit(event.target.value)} placeholder="Например, 2 или полка"/></label></div></>:<><p>{kind==="comparison"?`План: ${line.orderedQuantity} шт. × ${line.expectedUnitPrice} ${currency}. Инвойс: ${line.invoicedQuantity} шт. × ${line.unitPrice} ${currency}.`:"Позиция не войдёт в поступление. Её можно вернуть в сверку."}</p><label>Причина<textarea disabled={saving} value={note} onChange={event=>setNote(event.target.value)} required rows={3}/></label></>}{error && <div ref={errorRef} className="procurement-dialog-error" tabIndex={-1} role="alert">{error}</div>}</div><div className="dialog-actions"><button type="button" disabled={saving} onClick={close}>Отмена</button><button className="primary" disabled={saving || !valid}>{saving?"Сохраняем…":kind==="dimensions"?(reviewingDimensions?"Подтвердить размеры":"Сохранить"):title}</button></div></form></div></>;
}
