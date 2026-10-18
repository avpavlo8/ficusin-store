import { useState } from "react";
import { useProcurementDialog, useProcurementError } from "./procurementUX";
import type { ProcurementOrderLine } from "./adminTypes";

export function ProcurementInvoicePairDialog({ plannedLine, candidates, currency, saving, onClose, onPick, onPickMany, onError }: { plannedLine: ProcurementOrderLine; candidates: ProcurementOrderLine[]; currency: string; saving: boolean; onClose: () => void; onPick: (line: ProcurementOrderLine) => Promise<void>; onPickMany: (lines: ProcurementOrderLine[]) => Promise<void>; onError: (message: string) => void }) {
  const {error,errorRef,notify}=useProcurementError(onError);
  const close=()=>{if(!saving)onClose();};
  const dialogRef=useProcurementDialog(close);
  const [query,setQuery]=useState("");
  const [selected,setSelected]=useState<number[]>([]);
  const selectedLines=candidates.filter(line=>selected.includes(line.id));
  const addedUnits=selectedLines.reduce((sum,line)=>sum+(line.invoicedQuantity || 0),0);
  const finalUnits=(plannedLine.invoicedQuantity || 0)+addedUnits;
  const visible=candidates.filter(line=>(line.reconciliationStatus==="added" || plannedLine.invoicedQuantity!=null) && `${line.invoiceRawName} ${line.rawName} ${line.sabyName} ${line.invoiceSupplierArticle}`.toLocaleLowerCase("ru").includes(query.toLocaleLowerCase("ru")));
  const choose=async(line:ProcurementOrderLine)=>{try{if(line.reconciliationStatus==="added")await onPickMany([line]);else await onPick(line);}catch(error){notify((error as Error).message);}};
  const chooseMany=async()=>{try{await onPickMany(selectedLines);}catch(error){notify((error as Error).message);}};
  return <><button tabIndex={-1} className="admin-dialog-backdrop procurement-invoice-pair-backdrop" aria-label="Закрыть выбор строки инвойса" disabled={saving} onClick={close}/>
    <div ref={dialogRef} className="admin-dialog procurement-invoice-pair-dialog" role="dialog" aria-modal="true" aria-labelledby="invoice-pair-title" aria-describedby="invoice-pair-description">
      <header><h2 id="invoice-pair-title">Выбрать строки инвойса</h2><button aria-label="Закрыть выбор строки инвойса" disabled={saving} onClick={close}>×</button></header>
      <div className="procurement-invoice-pair-body"><strong>Для «{plannedLine.sabyName || plannedLine.rawName}»</strong>
        <p id="invoice-pair-description">Отметьте одну или несколько свободных строк — их количество сложится с уже привязанным. Названия поставщика сохранятся для следующих документов. Строку другого товара можно поменять местами с текущей.</p>
        <p>План: <strong>{plannedLine.orderedQuantity} шт.</strong> · уже в инвойсе: <strong>{plannedLine.invoicedQuantity || 0} шт.</strong></p>
        <input aria-label="Поиск строки инвойса" placeholder="Название или артикул в инвойсе" value={query} onChange={event=>setQuery(event.target.value)}/>
        {error && <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert"><strong>Не удалось сопоставить строки</strong><span>{error}</span></div>}
        <div className="admin-table-wrap"><table className="admin-table procurement-invoice-candidates"><thead><tr><th>Выбор</th><th>Строка инвойса</th><th>Сейчас привязана к</th><th>Размер · упаковка</th><th>Количество</th><th>Цена</th><th></th></tr></thead><tbody>{visible.map(line=><tr key={line.id}>
          <td data-label="Выбрать">{line.reconciliationStatus==="added" && <input type="checkbox" disabled={saving} aria-label={`Добавить строку ${line.id}: ${line.invoiceRawName || line.rawName}`} checked={selected.includes(line.id)} onChange={event=>setSelected(current=>event.target.checked?[...current,line.id]:current.filter(id=>id!==line.id))}/>}</td>
          <td data-label="Строка инвойса"><strong>{line.invoiceRawName || line.rawName}</strong>{line.invoiceSupplierArticle && <small>{line.invoiceSupplierArticle}</small>}</td>
          <td data-label="Сейчас привязана к">{line.reconciliationStatus==="added" ? "Не сопоставлена" : line.sabyName || line.rawName}</td>
          <td data-label="Размер · упаковка">{[line.potDiameterCm && `D${line.potDiameterCm}`,line.heightCm && `${line.heightCm} см`].filter(Boolean).join(" · ") || "—"}<small>{line.unitsPerPackage && line.packageCount ? `${line.unitsPerPackage} × ${line.packageCount} (шт. × мест)` : "Упаковка не указана"}</small></td>
          <td data-label="Количество">{line.invoicedQuantity ?? "—"}</td><td data-label="Цена">{line.unitPrice.toLocaleString("ru-RU",{minimumFractionDigits:2,maximumFractionDigits:4})} {currency}</td>
          <td><button disabled={saving} onClick={()=>void choose(line)}>{line.reconciliationStatus==="added"?"Сопоставить":"Поменять местами"}</button></td>
        </tr>)}</tbody></table></div>{visible.length===0 && <p>Строки не найдены. Измените поиск.</p>}
      </div>
      <div className="dialog-actions"><span aria-live="polite">Выбрано: {selectedLines.length} · +{addedUnits} шт. · итог {finalUnits} / {plannedLine.orderedQuantity} шт.{selectedLines.length>0 && finalUnits!==plannedLine.orderedQuantity && <small>Количество отличается от плана — потребуется принять расхождение.</small>}</span><button disabled={saving} onClick={close}>Отмена</button><button className="primary" disabled={saving || !selectedLines.length} onClick={()=>void chooseMany()}>{saving?"Сохраняем…":"Сопоставить выбранные"}</button></div>
    </div></>;
}
