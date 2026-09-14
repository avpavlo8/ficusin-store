import { useState } from "react";
import { api, money } from "./adminShared";

type Item = { id:number;productName:string;unitPrice:number;quantity:number;packageLengthCm:number;packageWidthCm:number;packageHeightCm:number;packageWeightGrams:number };
type Offer = {id:number;status:string;deliveryFee:number;total:number;expiresAt?:string;items:Array<{orderItemId:number;productName:string;quantity:number}>;boxes:Array<unknown>};

export function AdminShipmentOffers({orderId,items,offers,deliveryMethod,deliveryFee,cdekTariffCode,busy,readOnly=false,setBusy,onCreated,onError}:{
  orderId:number;items:Item[];offers:Offer[];deliveryMethod:string;deliveryFee:number;cdekTariffCode?:number;busy:boolean;readOnly?:boolean;
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
  const send=async(id:number)=>{setBusy(true);try{await api(`/api/v1/admin/shipment-offers/${id}/send`,{method:"POST"});await onCreated();}catch(error){onError((error as Error).message);}finally{setBusy(false);}};
  const active=offers.some((offer)=>["draft","packaging_required","notifying","offered","payment_pending"].includes(offer.status));
  const labels:Record<string,string>={draft:"Черновик",packaging_required:"Нужно распределить коробки",notifying:"Уведомление отправляется",offered:"Ожидает оплаты",payment_pending:"Платёж проверяется",paid:"Оплачено",shipping:"Передаём в СДЭК",shipped:"Передано в СДЭК",ready:"Готово к выдаче",completed:"Получено",expired:"Срок истёк",stale:"Устарело",cancelled:"Отменено"};
  return <section className="admin-block admin-shipment-offers">
    <div className="admin-block-heading"><div><strong>Частичные отправки</strong><small>Каждая отправка хранит свой состав, цену, коробки и срок оплаты</small></div></div>
    {!readOnly&&!active&&<div className="admin-shipment-builder">
    <p>Выберите только те растения, которые уже приехали. Для каждой единицы создаётся отдельная коробка по габаритам карточки товара.</p>
    {items.map((item)=><label key={item.id}><span>{item.productName}<small>{money.format(item.unitPrice)} · в заказе {item.quantity} шт.</small></span><input aria-label={`В отправку ${item.productName}`} type="number" min="0" max={item.quantity} value={quantities[item.id]??0} onChange={(event)=>setQuantities((current)=>({...current,[item.id]:Math.max(0,Math.min(item.quantity,Number(event.target.value)||0))}))}/></label>)}
    <label><span>Доставка этой отправки<small>После отправки предложения сумма фиксируется</small></span><input aria-label="Доставка частичной отправки" type="number" min="0" step="1" value={fee} onChange={(event)=>setFee(Math.max(0,Number(event.target.value)||0))}/></label>
    <button type="button" className="admin-action" disabled={busy} onClick={()=>void create()}>Подготовить отправку</button>
    </div>}
    {!offers.length&&<p>Предложений отправки пока нет.</p>}
    {offers.map((offer)=><article className="admin-shipment-offer" key={offer.id}>
      <div><strong>Отправка №{offer.id}</strong><small>{labels[offer.status]||offer.status}</small></div>
      <div>{offer.items.map(item=><span key={item.orderItemId}>{item.productName} · {item.quantity} шт.</span>)}</div>
      <div><span>{offer.boxes.length} кор. · доставка {money.format(offer.deliveryFee)}</span><strong>{money.format(offer.total)}</strong></div>
      {offer.expiresAt&&<small>Оплатить до {new Date(offer.expiresAt).toLocaleString("ru-RU")}</small>}
      {offer.status==="draft"&&!readOnly&&<button type="button" className="admin-action" disabled={busy} onClick={()=>void send(offer.id)}>Уведомить клиента</button>}
    </article>)}
    <small>Повторная отправка уведомления не продлевает 48 часов. Истечение частичной отправки не отменяет остальные позиции заказа.</small>
  </section>;
}
