import { useEffect, useId, useState } from "react";
import { api, money } from "./adminShared";

type RevenueSummary = { revenue: number; orders: number; daily: Array<{ date: string; revenue: number; orders: number }> };

export function AdminRevenue({ onOpen }: { onOpen: () => void }) {
  const [days, setDays] = useState(7);
  const [retry, setRetry] = useState(0);
  const [result, setResult] = useState<{ days: number; retry: number; data?: RevenueSummary; error?: string } | null>(null);
  const gradient = useId();
  useEffect(() => {
    let current = true;
    api<RevenueSummary>(`/api/v1/admin/analytics?days=${days}`).then(data => {
      if (current) setResult({ days, retry, data });
    }).catch(() => { if (current) setResult({ days, retry, error: "Не удалось загрузить аналитику" }); });
    return () => { current = false; };
  }, [days, retry]);
  const loaded = result?.days === days && result.retry === retry;
  const data = loaded ? result?.data : undefined;
  const rows = data?.daily || [];
  const max = Math.max(1, ...rows.map(row => row.revenue));
  const points = rows.map((row, index) => [36 + index * 620 / Math.max(1, rows.length - 1), 148 - row.revenue / max * 116]);
  const line = points.map(([x, y]) => `${x},${y}`).join(" ");
  const dateLabel = (date: string) => new Date(`${date}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "short" });
  return <section className="workspace-panel workspace-revenue" aria-label="Продажи сайта">
    <header className="workspace-panel-heading"><div><p className="eyebrow">Продажи сайта</p><h2>Динамика выручки</h2></div><div className="workspace-filters" role="group" aria-label="Период продаж">{[7, 30].map(value => <button type="button" key={value} aria-pressed={days === value} onClick={() => setDays(value)}>{value === 7 ? "Неделя" : "Месяц"}</button>)}</div></header>
    <div className="workspace-revenue-body" aria-live="polite">
      {!loaded && <p className="workspace-empty">Загружаем продажи…</p>}
      {loaded && result?.error && <div className="workspace-empty"><strong>Аналитика временно недоступна</strong><p>Заказы и остальные разделы доступны. Попробуйте обновить данные.</p><button type="button" className="workspace-text-button" onClick={() => setRetry(value => value + 1)}>Попробовать снова ↻</button></div>}
      {data && <><div className="workspace-revenue-total"><strong>{money.format(data.revenue)}</strong><span>{data.orders} оплаченных или выполненных заказов<br />за {days} дней · только сайт</span></div>
        {rows.length > 0 ? <><svg className="workspace-chart" viewBox="0 0 680 176" role="img" aria-label={`Выручка сайта за ${days} дней: ${money.format(data.revenue)}`}>
          <defs><linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#648361" stopOpacity=".24" /><stop offset="100%" stopColor="#648361" stopOpacity=".02" /></linearGradient></defs>
          {[32, 90, 148].map(y => <line key={y} x1="36" x2="656" y1={y} y2={y} stroke="#e7e1d6" strokeDasharray="3 4" />)}
          {points.length > 1 && <polygon points={`36,148 ${line} ${points[points.length - 1][0]},148`} fill={`url(#${gradient})`} />}
          <polyline points={line} fill="none" stroke="#2b4c39" strokeWidth="2.5" strokeLinejoin="round" />
          {points.map(([x, y], index) => <circle key={rows[index].date} cx={x} cy={y} r={rows.length > 10 ? 2 : 3.5} fill="#f9f6ef" stroke="#2b4c39" strokeWidth="2"><title>{dateLabel(rows[index].date)}: {money.format(rows[index].revenue)}</title></circle>)}
        </svg><div className="workspace-chart-labels"><span>{dateLabel(rows[0].date)}</span><span>{dateLabel(rows[Math.floor((rows.length - 1) / 2)].date)}</span><span>{dateLabel(rows[rows.length - 1].date)}</span></div></> : <p className="workspace-empty">За этот период данных о продажах пока нет.</p>}
      </>}
    </div><button type="button" className="workspace-text-button" onClick={onOpen}>Открыть аналитику <span aria-hidden="true">↗</span></button>
  </section>;
}
