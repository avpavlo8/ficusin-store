import { useCallback, useEffect, useState } from "react";
import { salesChannelLabel } from "./AdminProcurementPanels";
import { api, money } from "./adminShared";
import type { NomenclatureCandidate, SalesLinkResult, UnlinkedSale } from "./adminTypes";

// Каналы, у которых внешний код продажи может разойтись с номенклатурой.
// Сайт и СБИС кладут в продажи сам код товара, разбирать там нечего.
const linkableChannels = ["ozon", "wb"];

const externalCodeHint = (channel: string) => channel === "wb"
  ? "Внешний код Wildberries — это nmID карточки."
  : "Внешний код Ozon — это offer_id карточки, он часто похож на название растения, поэтому подставляется в поиск как есть.";

export function ProcurementUnlinkedSales({ onError }: { onError: (value: string) => void }) {
  const [channel, setChannel] = useState("ozon");
  const [items, setItems] = useState<UnlinkedSale[]>([]);
  const [loading, setLoading] = useState(false);
  const [linking, setLinking] = useState<UnlinkedSale | null>(null);
  const [notice, setNotice] = useState("");
  const load = useCallback(() => {
    setLoading(true);
    return api<{ items: UnlinkedSale[] }>(`/api/v1/admin/procurement/sales/unlinked?channel=${channel}`)
      .then((result) => setItems(result.items || []))
      .catch((error) => onError((error as Error).message))
      .finally(() => setLoading(false));
  }, [channel, onError]);
  // Линтер запрещает setState прямо в теле эффекта, а переключение канала и
  // без того не должно бить в сервер на каждый щелчок.
  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 150);
    return () => window.clearTimeout(timer);
  }, [load]);
  const linked = (result: SalesLinkResult) => {
    setLinking(null);
    setNotice(`«${result.sabyName}» связан с кодом ${result.externalId}. В расчёт вернулось строк продаж: ${result.linkedRows} (${result.linkedUnits} шт.).${result.takenFrom ? ` Код снят с товара ${result.takenFrom}.` : ""} Осталось разобрать кодов: ${result.remaining}.`);
    void load();
  };
  return <section className="admin-block procurement-block">
    <div className="admin-block-heading"><div><p className="eyebrow">Продажи без товара</p><h2>Ручное сопоставление</h2></div><span className="admin-pill">{items.length} кодов</span></div>
    <p className="admin-hint procurement-note">Связывание по выгрузке идёт только на точное совпадение кода, артикула или штрихкода. По названию его делать нельзя: «Фикус Бенджамина 12» и «Фикус Бенджамина 14» — разные растения с разной ценой. Всё, что не совпало, ждёт здесь и в расчёт закупки не попадает. {externalCodeHint(channel)}</p>
    <div className="admin-toolbar">
      {linkableChannels.map((item) => <button key={item} className={item === channel ? "admin-primary" : "secondary-button"} onClick={() => { setChannel(item); setNotice(""); }}>{salesChannelLabel(item)}</button>)}
      <span>{loading ? "Загружаем…" : "Связь сохраняется в справочнике и чинит уже загруженные продажи"}</span>
    </div>
    {notice && <p className="integration-check-result success" role="status">{notice}</p>}
    {items.length ? <div className="admin-table-wrap"><table className="admin-table procurement-unlinked-sales"><thead><tr>
      <th>Канал</th><th>Внешний код</th><th>Штук</th><th>Сумма</th><th>Дней с продажами</th><th>Последняя продажа</th><th></th>
    </tr></thead><tbody>{items.map((item) => <tr key={item.externalId}>
      <td>{salesChannelLabel(item.channel)}</td>
      <td><strong>{item.externalId}</strong></td>
      <td>{item.units}</td>
      <td>{money.format(item.grossRub)}</td>
      <td>{item.days}</td>
      <td>{item.lastSale ? new Date(`${item.lastSale}T00:00:00`).toLocaleDateString("ru-RU") : "—"}</td>
      <td><button className="table-action" onClick={() => setLinking(item)}>Сопоставить</button></td>
    </tr>)}</tbody></table></div> : <div className="procurement-zero">
      <strong>{loading ? "Загружаем…" : "Разбирать нечего"}</strong>
      <span>Все продажи этого канала связаны с номенклатурой и участвуют в расчёте закупки.</span>
    </div>}
    {linking && <SalesLinkDialog sale={linking} onClose={() => setLinking(null)} onLinked={linked} onError={onError} />}
  </section>;
}

export function SalesLinkDialog({ sale, onClose, onLinked, onError }: {
  sale: UnlinkedSale; onClose: () => void; onLinked: (result: SalesLinkResult) => void; onError: (value: string) => void;
}) {
  // Поиск начинается с самого внешнего кода: у Ozon это обычно и есть
  // название растения, и чаще всего править запрос не приходится.
  const [query, setQuery] = useState(sale.externalId);
  const [candidates, setCandidates] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (query.trim().length < 2) { setCandidates([]); return; }
    const timer = window.setTimeout(() => {
      setSearching(true);
      api<{ items: NomenclatureCandidate[] }>(`/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`)
        .then((result) => setCandidates(result.items || []))
        .catch(() => setCandidates([]))
        .finally(() => setSearching(false));
    }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);
  const link = async (candidate: NomenclatureCandidate) => {
    setSaving(true);
    try {
      const result = await api<{ link: SalesLinkResult }>("/api/v1/admin/procurement/sales/link", {
        method: "POST",
        body: JSON.stringify({ channel: sale.channel, externalId: sale.externalId, sabyId: candidate.sabyId }),
      });
      onLinked(result.link);
    } catch (error) { onError((error as Error).message); }
    finally { setSaving(false); }
  };
  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} />
    <div className="admin-dialog" role="dialog" aria-modal="true" aria-labelledby="sales-link-title">
      <header><div><p className="eyebrow">{salesChannelLabel(sale.channel)} · {sale.units} шт.</p><h2 id="sales-link-title">Сопоставить продажи</h2></div><button onClick={onClose} aria-label="Закрыть">×</button></header>
      <p className="admin-hint procurement-note">Код <strong>{sale.externalId}</strong> закрепится за выбранным товаром: уже загруженные продажи под ним вернутся в расчёт, а следующие выгрузки свяжутся сами. Если код был закреплён за другим растением, он перейдёт сюда — карточка маркетплейса продаёт что-то одно.</p>
      <label>Поиск по номенклатуре СБИС<input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название, код или артикул" /></label>
      {candidates.length ? <div className="admin-table-wrap"><table className="admin-table"><tbody>{candidates.map((item) => <tr key={item.sabyId}>
        <td><strong>{item.name}</strong><small>{[item.code, item.article].filter(Boolean).join(" · ") || item.sabyId}</small></td>
        <td>{item.balance} шт.</td>
        <td><button className="table-action" disabled={saving} onClick={() => void link(item)}>Связать</button></td>
      </tr>)}</tbody></table></div> : <p className="admin-hint">{searching ? "Ищем…" : query.trim().length < 2 ? "Введите хотя бы два символа." : "Ничего не нашлось. Попробуйте часть названия без размера."}</p>}
      <div className="dialog-actions"><button onClick={onClose} disabled={saving}>Отмена</button></div>
    </div></>;
}
