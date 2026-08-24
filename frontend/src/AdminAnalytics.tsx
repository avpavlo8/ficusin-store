import { useEffect, useMemo, useState } from "react";
import { api, money, PageHeading } from "./adminShared";

type Summary = {
  period: number; visitors: number; sessions: number; productViews: number;
  cartAdds: number; checkouts: number; orders: number; revenue: number;
  abandonedCarts: number; checkoutErrors: number;
  funnel: Array<{ name: string; sessions: number }>;
  sources: Array<{ source: string; sessions: number; orders: number; revenue: number }>;
  products: Array<{ productCode: string; views: number; cartAdds: number; orders: number; revenue: number }>;
  searches: Array<{ query: string; searches: number; zeroResults: number }>;
  daily: Array<{ date: string; sessions: number; orders: number; revenue: number }>;
};

const percent = (value: number, base: number) => base ? `${(value / base * 100).toFixed(1)}%` : "0%";

export function Analytics({ onError }: { onError: (message: string) => void }) {
  const [days, setDays] = useState(30);
  const [data, setData] = useState<Summary | null>(null);
  useEffect(() => {
    api<Summary>(`/api/v1/admin/analytics?days=${days}`).then(setData).catch((error) => onError(error.message));
  }, [days, onError]);
  const maxDaily = useMemo(() => Math.max(1, ...(data?.daily.map((item) => item.sessions) || [1])), [data]);
  if (!data) return <><PageHeading eyebrow="Аналитика" title="Воронка продаж" text="Загружаем данные…" /></>;
  const conversion = percent(data.orders, data.sessions);
  return <>
    <PageHeading eyebrow="E-commerce аналитика" title="Воронка продаж" text="Собственные данные сайта: от источника до созданного заказа" />
    <div className="analytics-period" role="group" aria-label="Период">{[7,30,90].map((value)=><button type="button" className={days===value?"active":""} onClick={()=>setDays(value)} key={value}>{value} дней</button>)}</div>
    <div className="admin-stats analytics-kpis">
      <article><span>Посетители</span><strong>{data.visitors}</strong><small>{data.sessions} сессий</small></article>
      <article><span>Заказы</span><strong>{data.orders}</strong><small>Конверсия {conversion}</small></article>
      <article><span>Выручка</span><strong>{money.format(data.revenue)}</strong><small>по созданным заказам</small></article>
      <article><span>Брошенные корзины</span><strong>{data.abandonedCarts}</strong><small>{data.checkoutErrors} ошибок оформления</small></article>
    </div>
    <section className="admin-block analytics-block"><div className="admin-block-heading"><div><p className="eyebrow">Динамика</p><h2>Сессии и заказы</h2></div></div><div className="analytics-bars">{data.daily.map((item)=><div key={item.date} title={`${item.date}: ${item.sessions} сессий, ${item.orders} заказов`}><i style={{height:`${Math.max(4,item.sessions/maxDaily*100)}%`}}/><b style={{height:`${Math.max(0,item.orders/maxDaily*100)}%`}}/><small>{new Date(`${item.date}T00:00:00`).toLocaleDateString("ru-RU",{day:"2-digit",month:"2-digit"})}</small></div>)}</div></section>
    <section className="admin-block analytics-block"><div className="admin-block-heading"><div><p className="eyebrow">Конверсия</p><h2>Воронка</h2></div></div><div className="analytics-funnel">{data.funnel.map((item,index)=><article key={item.name}><div><strong>{item.name}</strong><span>{item.sessions}</span></div><div><i style={{width:percent(item.sessions,data.funnel[0]?.sessions||0)}}/></div><small>{index?`${percent(item.sessions,data.funnel[index-1].sessions)} от предыдущего шага`:"100% сессий"}</small></article>)}</div></section>
    <div className="analytics-columns">
      <section className="admin-block analytics-block"><div className="admin-block-heading"><div><p className="eyebrow">Привлечение</p><h2>Источники</h2></div></div><div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Источник</th><th>Сессии</th><th>Заказы</th><th>Выручка</th></tr></thead><tbody>{data.sources.map((item)=><tr key={item.source}><td><strong>{item.source}</strong></td><td>{item.sessions}</td><td>{item.orders}</td><td>{money.format(item.revenue)}</td></tr>)}</tbody></table></div></section>
      <section className="admin-block analytics-block"><div className="admin-block-heading"><div><p className="eyebrow">Спрос</p><h2>Поиск без результата</h2></div></div><div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Запрос</th><th>Всего</th><th>Пустых</th></tr></thead><tbody>{data.searches.filter((item)=>item.zeroResults>0).map((item)=><tr key={item.query}><td><strong>{item.query}</strong></td><td>{item.searches}</td><td>{item.zeroResults}</td></tr>)}</tbody></table></div></section>
    </div>
    <section className="admin-block analytics-block"><div className="admin-block-heading"><div><p className="eyebrow">Каталог</p><h2>Эффективность товаров</h2></div></div><div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар</th><th>Просмотры</th><th>В корзину</th><th>Заказы</th><th>Конверсия в корзину</th></tr></thead><tbody>{data.products.map((item)=><tr key={item.productCode}><td><a href={`/product/${item.productCode}`} target="_blank" rel="noreferrer"><strong>{item.productCode}</strong></a></td><td>{item.views}</td><td>{item.cartAdds}</td><td>{item.orders}</td><td>{percent(item.cartAdds,item.views)}</td></tr>)}</tbody></table></div></section>
  </>;
}
