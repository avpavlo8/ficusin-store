import { useMemo, useState } from "react";
import { api, money } from "./adminShared";

type Item = { id:number;productName:string;unitPrice:number;quantity:number;packageLengthCm:number;packageWidthCm:number;packageHeightCm:number;packageWeightGrams:number };
type Offer = {id:number;status:string;deliveryFee:number;total:number;expiresAt?:string;items:Array<{orderItemId:number;productName:string;unitPrice:number;originalUnitPrice:number;quantity:number}>;boxes:Array<unknown>};
type Box = { key:number;lengthCm:number;widthCm:number;heightCm:number;weightGrams:number;contents:Record<number,number> };
type Quote = {tariffCode:number;tariffName:string;price:number;daysMin:number;daysMax:number};

let nextBoxKey=1;

export function AdminShipmentOffers({orderId,items,offers,deliveryMethod,deliveryFee,busy,readOnly,setBusy,onCreated,onError}:{
  orderId:number;items:Item[];offers:Offer[];deliveryMethod:string;deliveryFee:number;cdekTariffCode?:number;busy:boolean;readOnly:boolean;
  setBusy:(value:boolean)=>void;onCreated:()=>Promise<void>;onError:(message:string)=>void;
}) {
  const [quantities,setQuantities]=useState<Record<number,number>>(()=>Object.fromEntries(items.map((item)=>[item.id,0])));
  const [fee,setFee]=useState(deliveryFee);
  const [boxes,setBoxes]=useState<Box[]>([]);
  const [quotes,setQuotes]=useState<Quote[]>([]);
  const [tariffCode,setTariffCode]=useState<number>();
  const selected=useMemo(()=>items.flatMap((item)=>{const quantity=Math.max(0,Math.min(item.quantity,quantities[item.id]??0));return quantity>0?[{orderItemId:item.id,quantity}]:[];}),[items,quantities]);

  const resetPacking=()=>{setBoxes([]);setQuotes([]);setTariffCode(undefined);};
  const changeQuantity=(item:Item,value:number)=>{setQuantities((current)=>({...current,[item.id]:Math.max(0,Math.min(item.quantity,value||0))}));resetPacking();};
  const defaultBoxes=()=>{
    const result:Box[]=[];
    for(const line of selected){const item=items.find((candidate)=>candidate.id===line.orderItemId)!;for(let count=0;count<line.quantity;count++)result.push({key:nextBoxKey++,lengthCm:item.packageLengthCm,widthCm:item.packageWidthCm,heightCm:item.packageHeightCm,weightGrams:item.packageWeightGrams,contents:{[item.id]:1}});}
    setBoxes(result);setQuotes([]);setTariffCode(undefined);
  };
  const combineBoxes=()=>{
    if(!boxes.length)return;
    const normalized=boxes.map((box)=>({sides:[box.lengthCm,box.widthCm,box.heightCm].sort((a,b)=>b-a),weight:box.weightGrams}));
    const contents:Record<number,number>={};for(const box of boxes)for(const [id,quantity] of Object.entries(box.contents))contents[Number(id)]=(contents[Number(id)]||0)+quantity;
    setBoxes([{key:nextBoxKey++,lengthCm:Math.max(...normalized.map((box)=>box.sides[0])),widthCm:normalized.reduce((sum,box)=>sum+box.sides[1],0),heightCm:Math.max(...normalized.map((box)=>box.sides[2])),weightGrams:normalized.reduce((sum,box)=>sum+box.weight,0),contents}]);setQuotes([]);setTariffCode(undefined);
  };
  const updateBox=(key:number,update:Partial<Box>)=>{setBoxes((current)=>current.map((box)=>box.key===key?{...box,...update}:box));setQuotes([]);setTariffCode(undefined);};
  const payloadBoxes=boxes.map((box)=>({lengthCm:box.lengthCm,widthCm:box.widthCm,heightCm:box.heightCm,weightGrams:box.weightGrams,contents:Object.entries(box.contents).flatMap(([orderItemId,quantity])=>quantity>0?[{orderItemId:Number(orderItemId),quantity}]:[])}));
  const quote=async()=>{
    if(!selected.length||!boxes.length){onError("Сначала выберите растения и подготовьте коробки");return;}
    setBusy(true);try{const result=await api<{quotes:Quote[]}>(`/api/v1/admin/orders/${orderId}/shipment-offers/quote`,{method:"POST",body:JSON.stringify({items:selected,boxes:payloadBoxes})});setQuotes(result.quotes);const preferred=result.quotes[0];setTariffCode(preferred?.tariffCode);setFee(preferred?.price??0);}catch(error){onError((error as Error).message);}finally{setBusy(false);}
  };
  const create=async()=>{
    if(!selected.length){onError("Выберите растения для этой отправки");return;}
    if(!boxes.length){onError("Подготовьте фактические коробки");return;}
    if(deliveryMethod==="cdek"&&!tariffCode){onError("Пересчитайте доставку СДЭК после упаковки");return;}
    setBusy(true);try{await api(`/api/v1/admin/orders/${orderId}/shipment-offers`,{method:"POST",body:JSON.stringify({items:selected,boxes:payloadBoxes,deliveryFee:fee,cdekTariffCode:deliveryMethod==="cdek"?tariffCode:undefined})});await onCreated();}catch(error){onError((error as Error).message);}finally{setBusy(false);}
  };
  const send=async(id:number)=>{setBusy(true);try{await api(`/api/v1/admin/shipment-offers/${id}/send`,{method:"POST"});await onCreated();}catch(error){onError((error as Error).message);}finally{setBusy(false);}};
  const active=offers.some((offer)=>["draft","packaging_required","notifying","offered","payment_pending"].includes(offer.status));
  const labels:Record<string,string>={draft:"Черновик",packaging_required:"Нужно распределить коробки",notifying:"Уведомление отправляется",offered:"Ожидает оплаты",payment_pending:"Платёж проверяется",paid:"Оплачено",shipping:"Передаём в СДЭК",shipped:"Передано в СДЭК",ready:"Готово к выдаче",completed:"Получено",expired:"Срок истёк",stale:"Устарело",cancelled:"Отменено"};
  return <section className="admin-block admin-shipment-offers">
    <div className="admin-block-heading"><div><strong>Частичные отправки</strong><small>Каждая отправка хранит свой состав, цену, коробки и срок оплаты</small></div></div>
    {!readOnly&&!active&&<div className="admin-shipment-builder">
      <p>Выберите приехавшие растения, затем подтвердите, как менеджер фактически их упакует.</p>
      {items.map((item)=><label key={item.id}><span>{item.productName}<small>{money.format(item.unitPrice)} · в заказе {item.quantity} шт.</small></span><input aria-label={`В отправку ${item.productName}`} type="number" min="0" max={item.quantity} value={quantities[item.id]??0} onChange={(event)=>changeQuantity(item,Number(event.target.value))}/></label>)}
      {!!selected.length&&<div className="admin-shipment-pack-actions"><button type="button" onClick={defaultBoxes}>Отдельная коробка для каждого</button>{boxes.length>1&&<button type="button" onClick={combineBoxes}>Объединить в одну коробку</button>}<button type="button" onClick={()=>{setBoxes((current)=>[...current,{key:nextBoxKey++,lengthCm:0,widthCm:0,heightCm:0,weightGrams:0,contents:{}}]);setQuotes([]);setTariffCode(undefined);}}>Добавить коробку</button></div>}
      {boxes.map((box,index)=><article className="admin-shipment-box" key={box.key}>
        <div><strong>Коробка {index+1}</strong><button type="button" aria-label={`Удалить коробку ${index+1}`} onClick={()=>{setBoxes((current)=>current.filter((item)=>item.key!==box.key));setQuotes([]);setTariffCode(undefined);}}>Удалить</button></div>
        <div className="admin-shipment-dimensions">{([['lengthCm','Длина, см'],['widthCm','Ширина, см'],['heightCm','Высота, см'],['weightGrams','Вес, г']] as const).map(([field,label])=><label key={field}>{label}<input type="number" min="0" value={box[field]} onChange={(event)=>updateBox(box.key,{[field]:Math.max(0,Number(event.target.value)||0)})}/></label>)}</div>
        <div className="admin-shipment-box-contents">{selected.map((line)=>{const item=items.find((candidate)=>candidate.id===line.orderItemId)!;return <label key={item.id}>{item.productName}<input aria-label={`${item.productName} в коробке ${index+1}`} type="number" min="0" max={line.quantity} value={box.contents[item.id]||0} onChange={(event)=>updateBox(box.key,{contents:{...box.contents,[item.id]:Math.max(0,Math.min(line.quantity,Number(event.target.value)||0))}})}/></label>;})}</div>
      </article>)}
      {deliveryMethod==="cdek"?<div className="admin-shipment-quote"><button type="button" className="admin-action" disabled={busy||!boxes.length} onClick={()=>void quote()}>Пересчитать доставку СДЭК</button>{quotes.map((item)=><label key={item.tariffCode}><input type="radio" name="shipment-tariff" checked={tariffCode===item.tariffCode} onChange={()=>{setTariffCode(item.tariffCode);setFee(item.price);}}/><span><strong>{item.tariffName}</strong><small>{money.format(item.price)} · {item.daysMin}–{item.daysMax} дн.</small></span></label>)}</div>:<label><span>Доставка этой отправки<small>После отправки предложения сумма фиксируется</small></span><input aria-label="Доставка частичной отправки" type="number" min="0" step="1" value={fee} onChange={(event)=>setFee(Math.max(0,Number(event.target.value)||0))}/></label>}
      <button type="button" className="admin-action" disabled={busy||!boxes.length||(deliveryMethod==="cdek"&&!tariffCode)} onClick={()=>void create()}>Подготовить отправку</button>
    </div>}
    {!offers.length&&<p>Предложений отправки пока нет.</p>}
    {offers.map((offer)=><article className="admin-shipment-offer" key={offer.id}>
      <div><strong>Отправка №{offer.id}</strong><small>{labels[offer.status]||offer.status}</small></div>
      <div>{offer.items.map(item=><span key={item.orderItemId}>{item.productName} · {item.quantity} шт. · {money.format(item.unitPrice)}{item.originalUnitPrice!==item.unitPrice&&<small>При оформлении {money.format(item.originalUnitPrice)}</small>}</span>)}</div>
      <div><span>{offer.boxes.length} кор. · доставка {money.format(offer.deliveryFee)}</span><strong>{money.format(offer.total)}</strong></div>
      {offer.expiresAt&&<small>Оплатить до {new Date(offer.expiresAt).toLocaleString("ru-RU")}</small>}
      {offer.status==="draft"&&!readOnly&&<button type="button" className="admin-action" disabled={busy} onClick={()=>void send(offer.id)}>Уведомить клиента</button>}
    </article>)}
    <small>Повторная отправка уведомления не продлевает 48 часов. Истечение частичной отправки не отменяет остальные позиции заказа.</small>
  </section>;
}
