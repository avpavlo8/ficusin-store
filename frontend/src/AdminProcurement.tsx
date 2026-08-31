import { useCallback, useEffect, useState } from "react";
import { ProcurementMatchDialog, ProcurementOrderDetailDialog, ProcurementOrderDialog, ProcurementPlanDialog, ProcurementRequestDialog, ProcurementUploadDialog, SupplierDialog } from "./AdminProcurementDialogs";
import { ProcurementProducts, ProcurementSettingsPanel, availabilityLabel, integrationChannelLabel, recommendationEmptyText, recommendationEmptyTitle, recommendationStatusLabel, salesChannelLabel, salesSyncLabel, setExclusion, updateAvailability, updateRequestStatus } from "./AdminProcurementPanels";
import { ProcurementUnlinkedSales } from "./AdminProcurementSales";
import { PageHeading, api } from "./adminShared";
import type { IntegrationHealth, ProcurementAlias, ProcurementData, ProcurementOrder, ProcurementOrderDetail, RecommendationStatus } from "./adminTypes";

export function normalizeProcurementOrderDetail(item: ProcurementOrderDetail): ProcurementOrderDetail {
  return {
    ...item,
    validation: { ...item.validation, blockers: item.validation.blockers ?? [] },
    lines: item.lines ?? [],
    batches: (item.batches ?? []).map((batch) => ({ ...batch, items: batch.items ?? [] })),
  };
}

export const procurementStatusLabels: Record<string, string> = {
  draft: "Черновик", ordered: "Заказано", invoice_received: "Инвойс получен",
  review: "Требует проверки", ready_to_receive: "Готово к поступлению",
  received: "Принято", cancelled: "Отменено",
};

export const procurementSourceLabels: Record<string, string> = {
  manual: "Ручная закупка", recommendation: "По рекомендации",
  invoice: "Инвойс", payment_invoice: "Счёт на оплату",
};

export const procurementParserLabels: Record<string, string> = {
  holland_packing_list: "Голландский инвойс", domestic_payment_invoice: "Российский счёт", unknown: "Не определён",
};

export function Procurement({ onError }: { onError: (value: string) => void }) {
  const [data, setData] = useState<ProcurementData | null>(null);
  const [view, setView] = useState<"orders" | "recommendations" | "products" | "unlinkedSales" | "requests" | "availability" | "integrations" | "settings">("orders");
  const [recommendationView, setRecommendationView] = useState<RecommendationStatus>("recommended");
  const [checkingIntegration, setCheckingIntegration] = useState<string>("");
  const [integrationNotice, setIntegrationNotice] = useState<{ channel: string; ok: boolean; text: string } | null>(null);
  const [syncingCatalog, setSyncingCatalog] = useState("");
  const [supplierDialog, setSupplierDialog] = useState(false);
  const [orderDialog, setOrderDialog] = useState(false);
  const [uploadDialog, setUploadDialog] = useState(false);
  const [requestDialog, setRequestDialog] = useState(false);
  const [planDialog, setPlanDialog] = useState(false);
  const [matchDialog, setMatchDialog] = useState<ProcurementAlias | null>(null);
  const [selectedOrder, setSelectedOrder] = useState<number | null>(null);
  const load = useCallback(() => api<ProcurementData>("/api/v1/admin/procurement")
    .then(setData).catch((error) => onError((error as Error).message)), [onError]);
  useEffect(() => { void load(); }, [load]);
  const syncCatalog = async (channel: string) => {
    setSyncingCatalog(channel);
    try {
      const result = await api<{ link: { fetched: number; linked: number; unmatched: number; channelKeys: number; catalogKeys: number; channelSamples: string[]; catalogSamples: string[] } }>(`/api/v1/admin/procurement/integrations/${channel}/catalog`, { method: "POST" });
      const link = result.link;
	  if (channel === "saby") {
		setIntegrationNotice({ channel, ok: true, text: `Справочник СБИС обновлён: ${link.fetched} позиций. Новые карточки уже доступны для сопоставления.` });
		await load();
		return;
	  }
      const detail = link.linked === 0 && link.fetched > 0
        ? ` Сравнивали ${link.channelKeys} ключей канала с ${link.catalogKeys} ключами СБИС. У канала: ${(link.channelSamples || []).join(", ") || "нет"}. В СБИС: ${(link.catalogSamples || []).join(", ") || "нет"}.`
        : "";
      setIntegrationNotice({ channel, ok: link.linked > 0, text: `Прочитано карточек: ${link.fetched}, связано: ${link.linked}, без совпадения: ${link.unmatched}.${detail}` });
      await load();
    } catch (error) { setIntegrationNotice({ channel, ok: false, text: (error as Error).message }); }
    finally { setSyncingCatalog(""); }
  };
  const checkIntegration = async (channel: string) => {
    setCheckingIntegration(channel);
    setIntegrationNotice(null);
    try {
      const result = await api<{ integration: IntegrationHealth }>(`/api/v1/admin/procurement/integrations/${channel}/check`, { method: "POST" });
      setData((current) => current ? {
        ...current,
        integrationHealth: current.integrationHealth.map((item) => item.channel === channel ? result.integration : item),
      } : current);
      setIntegrationNotice({
        channel,
        ok: !result.integration.lastError,
        text: result.integration.lastError || `${integrationChannelLabel(channel)}: подключение работает`,
      });
    } catch (error) {
      const message = (error as Error).message;
      setIntegrationNotice({ channel, ok: false, text: message });
      onError(message);
    } finally {
      setCheckingIntegration("");
    }
  };

  if (!data) return <><PageHeading eyebrow="Снабжение" title="Закупки" text="Загружаем данные закупок…" /></>;
  const recommendationItems = data.recommendations.filter((item) => item.status === recommendationView);
  const actionableRecommendations = data.recommendations.filter((item) => item.status === "recommended");
  const formatTotal = (item: ProcurementOrder) => new Intl.NumberFormat("ru-RU", {
    style: "currency", currency: item.currency, maximumFractionDigits: 2,
  }).format(item.total);
  const formatMoney = (value: number, currency: string) => currency ? new Intl.NumberFormat("ru-RU", {
    style: "currency", currency, maximumFractionDigits: 2,
  }).format(value) : "—";
  return <>
    <PageHeading eyebrow="Снабжение" title="Закупки" text="Заказ поставщику, разбор инвойса, сопоставление товаров и подготовка поступления." />
    <div className="procurement-safety">
      <div><strong>Изменения только после подтверждения</strong><p>Сайт применяет цену сразу. Для СБИС он создаёт документы поступления без проведения; остатки меняются после того, как вы сами нажмёте «Провести» в СБИС.</p><small>WB: {data.integrations.wb ? "подключён" : "нужен токен"} · Ozon: {data.integrations.ozon ? "подключён" : "нужны ключи"} · СБИС: {data.integrations.saby ? "поступления подключены" : "нужны ключи"}</small></div>
      <span>Проводка СБИС вручную</span>
    </div>
    <div className="admin-stats procurement-stats">
      <article><span>Активные закупки</span><strong>{data.summary.openOrders}</strong><small>кроме принятых и отменённых</small></article>
      <article className={data.summary.unresolvedAliases ? "attention" : ""}><span>Нужно сопоставить</span><strong>{data.summary.unresolvedAliases}</strong><small>названий поставщиков</small></article>
      <article className={data.summary.availabilityChecks ? "attention" : ""}><span>Проверить наличие</span><strong>{data.summary.availabilityChecks}</strong><small>временно пропавших позиций</small></article>
      <article><span>Запросы</span><strong>{data.summary.openRequests}</strong><small>под заказ и от сотрудников</small></article>
    </div>
    <div className="admin-toolbar procurement-toolbar">
      <button className="admin-primary" onClick={() => setUploadDialog(true)} disabled={!data.suppliers.length}>Загрузить PDF</button>
      <button className="admin-primary" onClick={() => setOrderDialog(true)} disabled={!data.suppliers.length}>Новая закупка</button>
      <button className="secondary-button" onClick={() => setSupplierDialog(true)}>Поставщики</button>
      <button className="secondary-button" onClick={() => setRequestDialog(true)}>Добавить запрос</button>
	  <button className="secondary-button" disabled={syncingCatalog !== "" || !data.integrations.saby} onClick={() => void syncCatalog("saby")}>{syncingCatalog === "saby" ? "Обновляем СБИС…" : "Обновить товары из СБИС"}</button>
      <span>{data.suppliers.length ? `Поставщиков: ${data.suppliers.length}` : "Сначала добавьте поставщика"}</span>
    </div>

    <div className="procurement-tabs" role="tablist">
      <button className={view === "orders" ? "active" : ""} onClick={() => setView("orders")}>Закупки</button>
      <button className={view === "recommendations" ? "active" : ""} onClick={() => setView("recommendations")}>Что заказать</button>
      <button className={view === "products" ? "active" : ""} onClick={() => setView("products")}>Справочник</button>
      <button className={view === "unlinkedSales" ? "active" : ""} onClick={() => setView("unlinkedSales")}>Продажи без товара</button>
      <button className={view === "requests" ? "active" : ""} onClick={() => setView("requests")}>Под заказ <span>{data.summary.openRequests}</span></button>
      <button className={view === "availability" ? "active" : ""} onClick={() => setView("availability")}>Проверить наличие <span>{data.summary.availabilityChecks}</span></button>
      <button className={view === "integrations" ? "active" : ""} onClick={() => setView("integrations")}>Интеграции</button>
      <button className={view === "settings" ? "active" : ""} onClick={() => setView("settings")}>Формула v{data.settings.version}</button>
    </div>

    {view === "orders" && <><section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Входящие документы</p><h2>Инвойсы и счета</h2></div><span className="admin-pill">{data.documents.length} загружено</span></div>
      {data.documents.length ? <div className="admin-table-wrap"><table className="admin-table procurement-documents"><thead><tr>
        <th>Документ</th><th>Поставщик</th><th>Формат</th><th>Строк / шт.</th><th>Растения</th><th>Упаковка</th><th>Проверка</th>
      </tr></thead><tbody>{data.documents.map((item) => <tr key={item.id}>
        <td><strong>{item.documentNumber || item.fileName}</strong><small>{item.documentDate ? new Date(item.documentDate).toLocaleDateString("ru-RU") : item.fileName}</small></td>
        <td>{item.supplierName}</td><td>{procurementParserLabels[item.parserKind] || item.parserKind}</td>
        <td>{item.lines} / {item.units}</td><td>{formatMoney(item.productSubtotal, item.currency)}</td><td>{formatMoney(item.packageTotal, item.currency)}</td>
        <td>{item.arithmeticStatus === "ok" ? <span className="procurement-ok">Суммы сходятся</span> : <span className="procurement-warning">Проверить суммы</span>}<small>{item.parseStatus === "review" ? "Есть несопоставленные строки" : "Разобрано"}</small></td>
      </tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Документов пока нет</strong><span>Загрузите PDF поставщика — строки и телеги будут разобраны автоматически.</span></div>}
    </section>

    <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Работа в процессе</p><h2>Текущие закупки</h2></div></div>
      {data.orders.length ? <div className="admin-table-wrap"><table className="admin-table procurement-orders"><thead><tr>
        <th>Закупка</th><th>Поставщик</th><th>Источник</th><th>Статус</th><th>Строк / шт.</th><th>Сумма</th><th>Проверка</th>
      </tr></thead><tbody>{data.orders.map((item) => <tr key={item.id} className="clickable" onClick={() => setSelectedOrder(item.id)}>
        <td><strong>{item.orderNumber || `Черновик №${item.id}`}</strong><small>{new Date(item.createdAt).toLocaleDateString("ru-RU")}</small></td>
        <td><strong>{item.supplierName}</strong><small>{item.currency}</small></td>
        <td>{procurementSourceLabels[item.sourceKind] || item.sourceKind}</td>
        <td><span className={`admin-pill procurement-${item.status}`}>{procurementStatusLabels[item.status] || item.status}</span></td>
        <td>{item.lines} / {item.units}</td><td>{formatTotal(item)}</td>
        <td>{item.unmatched ? <span className="procurement-warning">{item.unmatched} не сопоставлено</span> : <span className="procurement-ok">Готово</span>}</td>
      </tr>)}</tbody></table></div> : <div className="orders-empty procurement-empty"><span>⌁</span><h3>Закупок пока нет</h3><p>Создайте черновик вручную или загрузите PDF поставщика.</p></div>}
    </section>

    <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Контроль</p><h2>Очередь сопоставления</h2></div><span className="admin-pill">{data.review.length} показано</span></div>
      {data.review.length ? <div className="admin-table-wrap"><table className="admin-table procurement-review"><thead><tr>
        <th>Поставщик</th><th>Название в документе</th><th>Размер</th><th>Кандидат СБИС</th><th>Уверенность</th><th>Наличие</th><th></th>
      </tr></thead><tbody>{data.review.map((item) => <tr key={item.id}>
        <td>{item.supplierName}</td><td><strong>{item.rawName}</strong>{item.supplierArticle && <small>Артикул: {item.supplierArticle}</small>}</td>
        <td>{[item.potDiameterCm && `D${item.potDiameterCm}`, item.heightCm && `${item.heightCm} см`].filter(Boolean).join(" · ") || "—"}</td>
        <td><strong>{item.suggestedSabyName || "Кандидат не найден"}</strong><small>{item.suggestedSabyId}</small></td>
        <td>{Math.round(item.confidence * 100)}%</td><td>{item.availabilityStatus === "check" ? "Проверить" : "Неизвестно"}</td>
        <td><button className="table-action" onClick={() => setMatchDialog(item)}>Сопоставить</button></td>
      </tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Очередь пуста</strong><span>Новые названия появятся здесь после разбора первого документа.</span></div>}
    </section></>}

    {view === "recommendations" && <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Остаток СБИС + продажи всех каналов</p><h2>Рекомендации к закупке</h2></div><button className="admin-primary" disabled={!actionableRecommendations.length || !data.suppliers.length} onClick={() => setPlanDialog(true)}>Сформировать заказ</button></div>
      <p className="admin-hint procurement-note">К закупке = продажи СБИС, сайта, WB и Ozon за {data.settings.recommendationDays} дней, пересчитанные на запас {data.settings.targetCoverDays} дней, − текущий остаток СБИС − товар в пути. Заказы клиентов добавляются к потребности.</p>
      <div className="sales-sync-grid">{(data.salesSync || []).map((sync) => <article className={`sales-sync-${sync.status}`} key={sync.channel}><div><strong>{salesChannelLabel(sync.channel)}</strong><span>{salesSyncLabel(sync.status)}</span></div><small>{sync.lastSuccessAt ? `Обновлено ${new Date(sync.lastSuccessAt).toLocaleString("ru-RU")}` : "Ещё не загружалось"}</small><small>{sync.latestSale ? `Последняя продажа ${new Date(`${sync.latestSale}T00:00:00`).toLocaleDateString("ru-RU")}` : "Продаж за период нет"} · загружено {sync.rowsSynced}, связано {sync.rowsLinked}</small>{sync.rowsSynced > sync.rowsLinked && <em>Часть продаж не сопоставлена с товарами и не участвует в расчёте — разберите их на вкладке «Продажи без товара»</em>}{sync.lastError && <em>{sync.lastError}</em>}</article>)}</div>
      <div className="procurement-recommendation-tabs">
        {(["recommended", "already_ordered", "check_availability", "supplier_unavailable", "excluded"] as const).map((status) => <button className={recommendationView === status ? "active" : ""} key={status} onClick={() => setRecommendationView(status)}>{recommendationStatusLabel(status)} <span>{data.recommendations.filter((item) => item.status === status).length}</span></button>)}
      </div>
      {recommendationItems.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар</th><th>Поставщик / артикул</th><th>Остаток / в пути</th><th>Продажи</th><th>Хватит на</th><th>Запросы</th><th>Заказать</th><th></th></tr></thead><tbody>{recommendationItems.map((item) => <tr key={`${item.supplierId}-${item.sabyId}`}><td><strong>{item.name}</strong><small>{item.reason}</small>{item.lastOrderedAt && <small>Последний заказ: {new Date(item.lastOrderedAt).toLocaleDateString("ru-RU")}</small>}</td><td><strong>{data.suppliers.find((supplier) => supplier.id === item.supplierId)?.name || "—"}</strong><small>{item.supplierArticle || "Артикул не заполнен"}</small></td><td><strong>{item.balance} / {item.incoming}</strong></td><td><strong>{item.totalSales}</strong><small>магазин {item.sabySales} · сайт {item.siteSales} · WB {item.wbSales} · Ozon {item.ozonSales}</small></td><td>{item.daysOfCover == null ? "Нет продаж" : `${Math.round(item.daysOfCover)} дн.`}</td><td>{item.openRequests}<small>{item.customerRequests ? `клиенту ${item.customerRequests}` : ""}{item.staffRequests ? `${item.customerRequests ? " · " : ""}магазину ${item.staffRequests}` : ""}</small></td><td><strong>{item.suggestedQty ? `${item.suggestedQty} шт.` : "—"}</strong>{item.orderMultiple > 1 && <small>кратно {item.orderMultiple}</small>}</td><td><div className="procurement-inline-actions">{item.status !== "check_availability" && item.status !== "excluded" && <button onClick={() => void updateAvailability(item.supplierId, item.sabyId, "check", load, onError)}>Проверить наличие</button>}{item.status !== "supplier_unavailable" && item.status !== "excluded" && <button onClick={() => void updateAvailability(item.supplierId, item.sabyId, "temporarily_unavailable", load, onError)}>Нет у поставщика</button>}{item.status === "excluded" ? <button onClick={() => void setExclusion(item.sabyId, false, "", load, onError)}>Вернуть в закупку</button> : <button onClick={() => void setExclusion(item.sabyId, true, "", load, onError)}>Не закупать</button>}</div></td></tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>{recommendationEmptyTitle(recommendationView)}</strong><span>{recommendationEmptyText(recommendationView)}</span></div>}
    </section>}

    {view === "products" && <ProcurementProducts suppliers={data.suppliers} onError={onError} />}

    {view === "unlinkedSales" && <ProcurementUnlinkedSales onError={onError} />}

    {view === "requests" && <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Приоритет закупки</p><h2>Под заказ и идеи магазина</h2></div><button className="admin-primary" onClick={() => setRequestDialog(true)}>Добавить</button></div>
      {data.requests.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Тип</th><th>Товар</th><th>Количество</th><th>Статус</th><th>Комментарий</th><th>Действия</th></tr></thead><tbody>{data.requests.map((item) => <tr key={item.id}><td>{item.kind === "customer_order" ? <span className="procurement-warning">Клиенту</span> : "Рекомендация"}</td><td><strong>{item.requestedName}</strong><small>{item.sabyId || "Не связан со СБИС"}</small></td><td>{item.quantity}</td><td>{item.status === "included" ? "В закупке" : "Открыт"}</td><td>{item.notes || "—"}</td><td><div className="procurement-inline-actions"><button onClick={() => void updateRequestStatus(item, "fulfilled", load, onError)}>Выполнен</button><button onClick={() => void updateRequestStatus(item, "cancelled", load, onError)}>Отменить</button></div></td></tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Запросов нет</strong><span>Добавьте клиентский заказ или рекомендацию сотрудника.</span></div>}
    </section>}

    {view === "availability" && <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Наличие у поставщика</p><h2>Проверить наличие</h2></div><span className="admin-pill">{data.availability.length} позиций</span></div>
      <p className="admin-hint procurement-note">Статус хранится на паре товар + поставщик, поэтому пометить можно любое растение из справочника, а не только встреченное в разобранном инвойсе.</p>
      {data.availability.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Товар</th><th>Поставщик</th><th>Остаток</th><th>С какого дня</th><th>Статус</th><th>Действия</th></tr></thead><tbody>{data.availability.map((item) => <tr key={`${item.supplierId}-${item.sabyId}`}><td><strong>{item.name || item.sabyId}</strong><small>{item.supplierArticle || "Артикул не заполнен"}</small></td><td>{item.supplierName}</td><td>{item.balance}</td><td>{item.unavailableSince ? new Date(`${item.unavailableSince}T00:00:00`).toLocaleDateString("ru-RU") : item.lastSeenAt ? new Date(item.lastSeenAt).toLocaleDateString("ru-RU") : "—"}</td><td>{availabilityLabel(item.availabilityStatus)}</td><td><div className="procurement-inline-actions"><button onClick={() => void updateAvailability(item.supplierId, item.sabyId, "available", load, onError)}>Есть</button><button onClick={() => void updateAvailability(item.supplierId, item.sabyId, "temporarily_unavailable", load, onError)}>Нет временно</button><button onClick={() => void updateAvailability(item.supplierId, item.sabyId, "discontinued", load, onError)}>Снят с продажи</button></div></td></tr>)}</tbody></table></div> : <div className="procurement-zero"><strong>Список пуст</strong><span>Растения, помеченные как отсутствующие у поставщика, не мешают собирать заказ и ждут проверки здесь.</span></div>}
    </section>}

    {view === "integrations" && <section className="admin-block procurement-block">
      <div className="admin-block-heading"><div><p className="eyebrow">Безопасная диагностика</p><h2>Подключения API</h2></div></div>
      <p className="admin-hint procurement-note">Проверка только читает одну служебную запись. Цены, остатки, заказы и документы не изменяются.</p>
      <div className="integration-health-grid">{(data.integrationHealth || []).map((item) => <article className={item.lastError ? "attention" : item.lastSuccessAt ? "connected" : ""} key={item.channel}>
        <div><strong>{integrationChannelLabel(item.channel)}</strong><span>{!item.configured ? "Переменные не найдены" : item.lastError ? "Ошибка подключения" : item.lastSuccessAt ? "Подключено" : "Не проверялось"}</span></div>
        <small>{item.lastSuccessAt ? `Последний успех: ${new Date(item.lastSuccessAt).toLocaleString("ru-RU")}` : "Успешных проверок ещё нет"}</small>
        {item.lastError && <em>{item.lastError}</em>}
        {integrationNotice?.channel === item.channel && <p className={`integration-check-result ${integrationNotice.ok ? "success" : "error"}`} role="status">{integrationNotice.text}</p>}
		<button className="secondary-button" disabled={checkingIntegration !== ""} onClick={() => void checkIntegration(item.channel)}>{checkingIntegration === item.channel ? "Проверяем…" : item.channel === "wb" ? "Проверить зеркало" : "Проверить подключение"}</button>
		<button className="secondary-button" disabled={syncingCatalog !== "" || !item.configured} onClick={() => void syncCatalog(item.channel)}>{syncingCatalog === item.channel ? "Сопоставляем…" : item.channel === "saby" ? "Обновить справочник" : item.channel === "wb" ? "Сопоставить из зеркала" : "Подтянуть артикулы"}</button>
      </article>)}</div>
      <p className="admin-hint procurement-note">Подтягивание связывает карточки маркетплейса с номенклатурой СБИС по точному совпадению кода, артикула или штрихкода и заполняет только пустые поля. Совпадение по названию не используется: «Фикус 12» и «Фикус 14» — разные растения. Что не совпало, разбирается руками на вкладке «Продажи без товара».</p>
      <p className="admin-hint procurement-note">Wildberries раз в час фоново сохраняет карточки, артикулы, цены и продажи в локальное зеркало. Разделы сайта и кнопка сопоставления читают только базу и не создают дополнительных запросов к WB. Токену нужны категории «Цены и скидки», «Статистика» и «Контент».</p>
	  <p className="admin-hint procurement-note">СБИС здесь проверяет авторизацию, точку 278 и прайс-лист 6. Кнопка обновления сразу читает полный каталог и прайс-лист — ждать фоновой синхронизации больше не нужно.</p>
    </section>}

    {view === "settings" && <ProcurementSettingsPanel settings={data.settings} onSaved={() => void load()} onError={onError} />}
    {supplierDialog && <SupplierDialog suppliers={data.suppliers} onClose={() => setSupplierDialog(false)} onSaved={() => void load()} onError={onError} />}
    {orderDialog && <ProcurementOrderDialog suppliers={data.suppliers} onClose={() => setOrderDialog(false)} onSaved={() => { setOrderDialog(false); void load(); }} onError={onError} />}
    {uploadDialog && <ProcurementUploadDialog suppliers={data.suppliers} orders={data.orders} onClose={() => setUploadDialog(false)} onSaved={() => { setUploadDialog(false); void load(); }} onError={onError} />}
    {requestDialog && <ProcurementRequestDialog onClose={() => setRequestDialog(false)} onSaved={() => { setRequestDialog(false); void load(); }} onError={onError} />}
    {planDialog && <ProcurementPlanDialog suppliers={data.suppliers} recommendations={actionableRecommendations} onClose={() => setPlanDialog(false)} onSaved={() => { setPlanDialog(false); setView("orders"); void load(); }} onError={onError} />}
    {matchDialog && <ProcurementMatchDialog alias={matchDialog} onClose={() => setMatchDialog(null)} onSaved={() => { setMatchDialog(null); void load(); }} onError={onError} />}
    {selectedOrder && <ProcurementOrderDetailDialog orderId={selectedOrder} onClose={() => setSelectedOrder(null)} onSaved={() => { void load(); }} onError={onError} />}
  </>;
}
