import { useState } from "react";
import type { ProcurementUnifiedPreview } from "./adminTypes";
import "./styles/procurement-unified-prices.css";

const rub=(value:number)=>value.toLocaleString("ru-RU",{maximumFractionDigits:2});

export function UnifiedPricingPreview({preview}:{preview?:ProcurementUnifiedPreview}) {
 const [query,setQuery]=useState("");
 const [limit,setLimit]=useState(10);
 if(!preview)return null;
 const filtered=preview.lines.filter(line=>line.name.toLocaleLowerCase("ru").includes(query.toLocaleLowerCase("ru")));
 return <details className="procurement-explanation" open>
  <summary>Цены по общей формуле</summary>
  <p>Заработок с продажи — {rub(preview.markupRate*100)}% себестоимости после плановых расходов продажи, до постоянных расходов. Один принцип для всех товаров, без потолка. Это просмотр по текущей формуле; цены сайта и маркетплейсов не изменены.</p>
  <p>Рассчитано для {preview.pricedUnits} из {preview.units} растений. При продаже этого количества за месяц: вклад {rub(preview.contributionRub)} ₽ − расходы {rub(preview.fixedRub)} ₽ = <strong>{rub(preview.contributionRub-preview.fixedRub)} ₽</strong>. Каждый экземпляр учитывается один раз, независимо от площадки.</p>
  <label>Найти растение в расчёте<input type="search" value={query} onChange={event=>{setQuery(event.target.value);setLimit(10);}}/></label>
  <div className="admin-table-wrap"><table role="table" className="admin-table procurement-unified-prices"><caption>Предложения цен продавца до скидок за счёт площадки · версия {preview.version}</caption><thead><tr><th>Растение</th><th>Себестоимость</th><th>WB</th><th>Ozon</th><th>Плановый заработок</th></tr></thead><tbody>{filtered.slice(0,limit).map(line=><tr key={line.variantId}>
   <td data-label="Растение"><strong>{line.name}</strong><small>{line.quantity} шт.</small>{line.quotes.wb?.package&&<small>Коробка {line.quotes.wb.package.lengthCm} × {line.quotes.wb.package.widthCm} × {line.quotes.wb.package.heightCm} см</small>}</td>
   <td data-label="Себестоимость">{line.costRub>0?`${rub(line.costRub)} ₽`:"Нет себестоимости"}</td>
   {["wb","ozon"].map(ch=><td key={ch} data-label={ch==="wb"?"WB":"Ozon"}>{line.quotes[ch]?.priceRub!=null?<><strong>{rub(line.quotes[ch].priceRub!)} ₽</strong><small>Упаковка {rub(line.quotes[ch].costs?.packagingRub||0)} ₽ · доставка {rub(line.quotes[ch].costs?.logisticsRub||0)} ₽</small></>:<span>{line.quotes[ch]?.missing.join(", ") || "Нужен расчёт"}</span>}</td>)}
   <td data-label="Плановый заработок">{line.costRub>0?`${rub(line.costRub*preview.markupRate)} ₽`:"Нужна себестоимость"}<small>с одного растения</small></td>
  </tr>)}</tbody></table></div>
  {!filtered.length&&<p>Растения не найдены.</p>}
  {filtered.length>limit&&<button onClick={()=>setLimit(limit+20)}>Показать ещё {Math.min(20,filtered.length-limit)}</button>}
  <p>Логистика Ozon — средняя стоимость группы коробок. Дорогие и дешёвые маршруты компенсируют друг друга; прибыль отдельного заказа отличается от плана. Наличие расчётной цены не подтверждает связь или активность карточки МП. Неполные исходные данные показаны в строке.</p>
 </details>;
}
