import { useState } from "react";
import type { ProcurementSettings } from "./adminTypes";

const money = (value: number) => new Intl.NumberFormat("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
const number = (value: string) => Number(value.replace(",", "."));

export function ProcurementCostCalculator({ settings }: { settings: ProcurementSettings }) {
  const [name, setName] = useState("");
  const [price, setPrice] = useState("");
  const [rate, setRate] = useState(String(settings.defaultExchangeRate));
  const [pot, setPot] = useState("");
  const [height, setHeight] = useState("");
  const [markup, setMarkup] = useState("15");
  const [trolleyCost, setTrolleyCost] = useState(String(settings.trolleyCostRub));
  const [trolleyVolume, setTrolleyVolume] = useState(String(settings.trolleyVolumeCm3));
  const [fill, setFill] = useState(String(settings.trolleyFillRatio * 100));
  const inputs = [price, rate, pot, height, markup, trolleyCost, trolleyVolume, fill].map(number);
  const [eur, rubRate, diameter, plantHeight, percent, cartCost, cartVolume, occupancy] = inputs;
  const valid = inputs.every(Number.isFinite) && eur > 0 && rubRate > 0 && diameter > 0 && plantHeight > 0 && percent >= 0 && cartCost >= 0 && cartVolume > 0 && occupancy > 0 && occupancy <= 100;
  const purchase = eur * rubRate;
  const volume = Math.PI * (diameter / 2) ** 2 * plantHeight;
  const usableVolume = cartVolume * occupancy / 100;
  const delivery = cartCost * volume / usableVolume;
  const cost = purchase + delivery;
  const added = cost * percent / 100;
  return <section className="procurement-calculator" aria-labelledby="procurement-calculator-title">
    <div className="procurement-calculator-heading"><div><p className="eyebrow">Предварительная цена</p><h2 id="procurement-calculator-title">Калькулятор разовой закупки</h2><p>Для растения под клиента или крупномера. Название можно ввести свободно, без карточки в СБИС.</p></div><span>Расчёт до инвойса</span></div>
    <div className="procurement-calculator-layout"><div className="procurement-calculator-form">
      <div className="procurement-calculator-group"><h3>Растение</h3><div className="procurement-calculator-fields"><label className="wide">Название<input value={name} onChange={(event) => setName(event.target.value)} placeholder="Например, фикус Лирата для клиента" /></label><label>Цена поставщика, €<input inputMode="decimal" value={price} onChange={(event) => setPrice(event.target.value)} placeholder="0,00" /></label><label>Курс, ₽/€<input inputMode="decimal" value={rate} onChange={(event) => setRate(event.target.value)} /></label><label>Диаметр горшка, см<input inputMode="decimal" value={pot} onChange={(event) => setPot(event.target.value)} placeholder="D" /></label><label>Высота растения, см<input inputMode="decimal" value={height} onChange={(event) => setHeight(event.target.value)} placeholder="H" /></label><label>Прибавить к себестоимости, %<input inputMode="decimal" value={markup} onChange={(event) => setMarkup(event.target.value)} /></label></div></div>
      <details className="procurement-calculator-assumptions"><summary>Параметры телеги и доставки</summary><p>Начальные значения подставлены из действующей формулы закупок на сайте. Их можно менять для этого расчёта.</p><div className="procurement-calculator-fields"><label>Стоимость телеги, ₽<input inputMode="decimal" value={trolleyCost} onChange={(event) => setTrolleyCost(event.target.value)} /></label><label>Заполнение, %<input inputMode="decimal" value={fill} onChange={(event) => setFill(event.target.value)} /></label><label>Объём телеги, см³<input inputMode="decimal" value={trolleyVolume} onChange={(event) => setTrolleyVolume(event.target.value)} /></label></div></details>
    </div><aside className="procurement-calculator-result" aria-live="polite"><span className="eyebrow">Цена для клиента</span>{valid ? <><strong>{money(cost + added)} ₽</strong><p>{name.trim() || "Разовая позиция"}</p><dl><div><dt>Растение по курсу</dt><dd>{money(purchase)} ₽</dd></div><div><dt>Расчётная логистика</dt><dd>{money(delivery)} ₽</dd></div><div className="total"><dt>Себестоимость</dt><dd>{money(cost)} ₽</dd></div><div><dt>Прибавка {money(percent)}%</dt><dd>{money(added)} ₽</dd></div></dl><small>Предварительная логистика рассчитана по объёму растения и действующим параметрам телеги. После инвойса сайт распределит фактическую доставку по всей закупке. Цена не округляется до ценового шага.</small></> : <p>Введите цену, курс и размеры, чтобы увидеть расчёт.</p>}</aside></div>
  </section>;
}
