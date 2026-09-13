import { useState } from "react";
import { api, money } from "./adminShared";

type Item = { id:number;productName:string;unitPrice:number;quantity:number;packageLengthCm:number;packageWidthCm:number;packageHeightCm:number;packageWeightGrams:number };

export function AdminShipmentOfferBuilder({orderId,items,deliveryMethod,deliveryFee,cdekTariffCode,busy,setBusy,onCreated,onError}:{
  orderId:number;items:Item[];deliveryMethod:string;deliveryFee:number;cdekTariffCode?:number;busy:boolean;
  setBusy:(value:boolean)=>void;onCreated:()=>Promise<void>;onError:(message:string)=>void;
}) {
  const [quantities,setQuantities]=useState<Record<number,number>>(()=>Object.fromEntries(items.map((item)=>[item.id,0])));
  const [fee,setFee]=useState(deliveryFee);
  const create=async()=>{
    const selected=items.flatMap((item)=>{const quantity=Math.max(0,Math.min(item.quantity,quantities[item.id]??0));return quantity>0?[{orderItemId:item.id,quantity}]:[];});
    if(!selected.length){onError("Выберите растения для этой отправки");return;}
    const boxes=selected.flatMap((line)=>{const item=items.find((candidate)=>candidate.id===line.orderItemId)!;return Array.from({length:line.quantity},()=>({lengthCm:item.packageLengthCm,widthCm:item.packageWidthCm,heightCm:item.packageHeightCm,weightGrams:item.packageWeightGrams,contents:[{orderItemId:item.id,quantity:1}]}));});
    setBusy(true);try{await api(`/api/v1/admin/orders/${orderId}/shipment-offers`,{method:"POST",body:JSON.stringify({items:selected,boxes,deliveryFee:fee,cdekTariffCode:deliveryMethod==="cdek"?cdekTariffCode:undefined})});await onCreated();}catch(error){onError((error as Error).message);}finally{setBusy(false);}
  };
  return <div className="admin-shipment-builder">
    <p>Выберите только те растения, которые уже приехали. Для каждой единицы создаётся отдельная коробка по габаритам карточки товара.</p>
    {items.map((item)=><label key={item.id}><span>{item.productName}<small>{money.format(item.unitPrice)} · в заказе {item.quantity} шт.</small></span><input aria-label={`В отправку ${item.productName}`} type="number" min="0" max={item.quantity} value={quantities[item.id]??0} onChange={(event)=>setQuantities((current)=>({...current,[item.id]:Math.max(0,Math.min(item.quantity,Number(event.target.value)||0))}))}/></label>)}
    <label><span>Доставка этой отправки<small>После отправки предложения сумма фиксируется</small></span><input aria-label="Доставка частичной отправки" type="number" min="0" step="1" value={fee} onChange={(event)=>setFee(Math.max(0,Number(event.target.value)||0))}/></label>
    <button type="button" className="admin-action" disabled={busy} onClick={()=>void create()}>Подготовить отправку</button>
  </div>;
}
