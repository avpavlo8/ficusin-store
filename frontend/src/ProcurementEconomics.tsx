import type { ProcurementChannelCosts, ProcurementChannelQuote, ProcurementEconomics } from "./adminTypes";

const names: Record<string,string> = {site:"Сайт / магазин",wb:"Wildberries · FBS",ozon:"Ozon · FBS"};
const rub = (value: number) => value.toLocaleString("ru-RU",{maximumFractionDigits:2});

export function ChannelCostsFields({value,onChange}:{value:ProcurementChannelCosts;onChange:(value:ProcurementChannelCosts)=>void}) {
  const field=(key:keyof ProcurementChannelCosts,label:string,percent=false)=><label key={key}>{label}{percent?", %":", ₽"}<input type="number" min="0" step={percent?"0.01":"0.01"} placeholder="Нет данных" value={value[key]==null?"":Math.round(value[key]!*(percent?100:1)*10000)/10000} onChange={event=>onChange({...value,[key]:event.target.value===""?null:Number(event.target.value)/(percent?100:1)})}/></label>;
  return <div className="procurement-settings-grid">{field("commissionRate","Комиссия",true)}{field("acquiringRate","Эквайринг",true)}{field("packagingRub","Упаковка")}{field("logisticsRub","Логистика, последняя миля, обработка")}{field("lossRub","Потери, возвраты и прочие затраты")}</div>;
}

export function PricingQuoteSummary({quotes}:{quotes?:Record<string,ProcurementChannelQuote>}) {
  return <div className="procurement-settings-grid">{["site","wb","ozon"].map(channel=>{
    const q=quotes?.[channel];
    return <div className="procurement-checklist" key={channel}><strong>{names[channel]}</strong>{q?.priceRub!=null && q.contributionRub!=null ? <><p>{q.manual?"Выбранная цена":"Предложение"}: {rub(q.priceRub)} ₽ · остаётся {rub(q.contributionRub)} ₽ с продажи на постоянные расходы и прибыль.</p><p>Нижняя граница: {rub(q.breakEvenRub || 0)} ₽ — покрывает только товар и расходы продажи.</p>{q.coverUnits!=null && <p>{q.coverUnits.toLocaleString("ru-RU")} таких продаж в месяц покроют постоянные расходы. Прибыль появляется сверх этого объёма.</p>}</>:<p className="procurement-warning">{q?.missing?.length?`${q.missing.join(", ")}.`:"Нужен расчёт расходов."}{q?.breakEvenRub!=null?` Нижняя граница: ${rub(q.breakEvenRub)} ₽ без покрытия постоянных расходов.`:""}</p>}</div>;
  })}</div>;
}

export function ProcurementEconomicsFields({value,onChange}:{value:ProcurementEconomics;onChange:(value:ProcurementEconomics)=>void}) {
  return <><h3>Постоянные расходы</h3><p>Аренда, зарплаты с начислениями, коммунальные и сервисы учитываются здесь один раз. Упаковку и доставку конкретной продажи заполняйте ниже. Если они уже входят в месячную сумму, исключите их из неё.</p><label>Расходы в месяц, ₽<input type="number" min="0" value={value.monthlyFixedCostsRub} onChange={event=>onChange({...value,monthlyFixedCostsRub:Number(event.target.value)})}/></label><h3>Расходы продажи по каналам</h3><p>Указывайте удержания от цены продавца до скидок за счёт площадки. Пустое поле означает неизвестную стоимость; если расхода нет, введите 0. Логистику берите по упакованному товару из кабинета, с последней милей и обработкой. Потери — средняя невозмещённая стоимость отмен, возвратной доставки, повреждений и списаний на завершённую продажу. Включайте сюда также сдельную сборку, рекламу и другие переменные затраты; не дублируйте их в месячном бюджете.</p>{["site","wb","ozon"].map(channel=><details className="procurement-explanation" key={channel} open><summary>{names[channel]}</summary><ChannelCostsFields value={value.channels[channel] || {}} onChange={costs=>onChange({...value,channels:{...value.channels,[channel]:costs}})}/></details>)}<p>Это базовые расходы для проверенного ассортимента. Для товара с другой комиссией, габаритами или упаковкой задайте расходы в строке закупки: «Ещё → Расходы продажи».</p></>;
}
