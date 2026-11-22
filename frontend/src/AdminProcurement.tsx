import { useProcurementParam, useProcurementRoute } from "./useProcurementRoute";
import { procurementOrderHref, quantityLabel } from "./procurementUX";
import { bulkSelectableRecommendations } from "./lib/procurement-state";
import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import {
  ProcurementMatchDialog,
  ProcurementOrderDetailDialog,
  ProcurementPlanDialog,
  ProcurementRequestDialog,
  ProcurementUploadDialog,
  SupplierDialog,
} from "./AdminProcurementDialogs";
import {
  availabilityLabel,
  integrationChannelLabel,
  updateAvailability,
  updateRequestStatus,
} from "./AdminProcurementPanels";
import {
  ProcurementCostCalculator,
  type ProcurementCalculatorDraft,
} from "./ProcurementCostCalculator";
import { WorkspaceMore } from "./WorkspaceUI";
import { ProcurementDataHealth } from "./ProcurementDataHealth";
import { ProcurementRecommendationTable } from "./ProcurementRecommendationTable";
import { PageHeading, api } from "./adminShared";
import type {
  IntegrationHealth,
  ProcurementAlias,
  ProcurementAvailability,
  ProcurementData,
  ProcurementOrder,
  ProcurementOrderDetail,
  ProcurementRecommendation,
  ProcurementRequest,
} from "./adminTypes";
import "./styles/admin-procurement-plan.css";
import "./styles/admin-procurement-redesign.css";
import "./styles/admin-procurement-review.css";
import "./styles/admin-procurement-workspace.css";
import "./styles/admin-procurement-queues.css";
import "./styles/admin-procurement-composition.css";

const ProcurementProducts = lazy(() =>
  import("./AdminProcurementProducts").then((module) => ({ default: module.ProcurementProducts })),
);
const ProcurementUnlinkedSales = lazy(() =>
  import("./AdminProcurementSales").then((module) => ({
    default: module.ProcurementUnlinkedSales,
  })),
);

export function normalizeProcurementOrderDetail(
  item: ProcurementOrderDetail,
): ProcurementOrderDetail {
  return {
    ...item,
    validation: { ...item.validation, blockers: item.validation.blockers ?? [] },
    lines: (item.lines ?? []).map((line) => ({
      ...line,
      supplierCategory: line.supplierCategory || "",
      invoiceRawName: line.invoiceRawName || "",
      invoiceSupplierArticle: line.invoiceSupplierArticle || "",
      reconciliationStatus:
        line.reconciliationStatus || (line.invoicedQuantity == null ? "planned" : "matched"),
      invoiceExcluded: line.invoiceExcluded || false,
      invoiceExclusionReason: line.invoiceExclusionReason || "",
    })),
    batches: (item.batches ?? []).map((batch) => ({ ...batch, items: batch.items ?? [] })),
  };
}

export const procurementStatusLabels: Record<string, string> = {
  draft: "Черновик",
  ordered: "Заказано",
  invoice_received: "Инвойс получен",
  review: "Требует проверки",
  ready_to_receive: "Готово к поступлению",
  received: "Завершено",
  cancelled: "Отменено",
};

export const procurementSourceLabels: Record<string, string> = {
  manual: "Ручная закупка",
  recommendation: "По рекомендации",
  invoice: "Инвойс",
  payment_invoice: "Счёт на оплату",
};

export const procurementParserLabels: Record<string, string> = {
  holland_packing_list: "Голландский инвойс",
  domestic_payment_invoice: "Российский счёт",
  unknown: "Не определён",
};

function AvailabilityRow({
  item,
  reload,
  onError,
}: {
  item: ProcurementAvailability;
  reload: () => Promise<unknown>;
  onError: (value: string) => void;
}) {
  const [reason, setReason] = useState(item.reason || "");
  const [comment, setComment] = useState(item.comment || "");
  const [checkAfter, setCheckAfter] = useState(item.checkAfter || "");
  const save = (status: string) =>
    updateAvailability(item.supplierId, item.sabyId, status, reload, onError, {
      reason,
      comment,
      checkAfter: status === "available" ? "" : checkAfter,
    });
  return (
    <tr>
      <td>
        <strong>{item.name || item.sabyId}</strong>
        <small>{item.supplierArticle || "Артикул не заполнен"}</small>
      </td>
      <td>{item.supplierName}</td>
      <td>{item.balance}</td>
      <td>
        {item.unavailableSince
          ? new Date(`${item.unavailableSince}T00:00:00`).toLocaleDateString("ru-RU")
          : item.lastSeenAt
            ? new Date(item.lastSeenAt).toLocaleDateString("ru-RU")
            : "—"}
      </td>
      <td>
        {availabilityLabel(item.availabilityStatus)}
        <small>
          {item.lastActionAt
            ? `Последнее действие ${new Date(item.lastActionAt).toLocaleString("ru-RU")}`
            : "Действий ещё нет"}
        </small>
      </td>
      <td>
        <div className="procurement-availability-editor">
          <input
            aria-label={`Причина ${item.name}`}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="Причина"
          />
          <textarea
            aria-label={`Комментарий ${item.name}`}
            value={comment}
            onChange={(event) => setComment(event.target.value)}
            placeholder="Комментарий поставщика"
          />
          <label>
            Следующая проверка
            <input
              type="date"
              value={checkAfter}
              onChange={(event) => setCheckAfter(event.target.value)}
            />
          </label>
          <div className="procurement-inline-actions">
            <button onClick={() => void save("available")}>Появился — вернуть</button>
            <button onClick={() => void save("temporarily_unavailable")}>Всё ещё нет</button>
            <button onClick={() => void save("discontinued")}>Снят с продажи</button>
          </div>
        </div>
      </td>
    </tr>
  );
}

const recommendationTabs = {
  soldOut: { label: "Распродали", empty: "Распроданных растений нет" },
  lowStock: { label: "Заканчивается", empty: "Запаса хватает на выбранный период" },
  customer: { label: "Ждёт клиент", empty: "Нет открытых клиентских заявок" },
  hidden: { label: "Скрытые", empty: "Скрытых растений нет" },
} as const;

type RecommendationView = keyof typeof recommendationTabs;

function recommendationViewFor(item: ProcurementRecommendation): RecommendationView {
  if (item.customerWaiting > 0 || item.customerRequests > 0) return "customer";
  if (item.hiddenFromRestock || item.status === "excluded") return "hidden";
  return item.balance <= 0 && item.balanceKnown !== false ? "soldOut" : "lowStock";
}

type PlanDraftSummary = { id: number; title: string; updatedAt: string; createdAt: string };

export function Procurement({
  onError,
  canIntegrations,
  canDelete,
}: {
  onError: (value: string) => void;
  canIntegrations: boolean;
  canDelete: boolean;
}) {
  const [data, setData] = useState<ProcurementData | null>(null);
  const { view, setView, selectedOrder, setSelectedOrder } = useProcurementRoute();
  const [recommendationView, setRecommendationView] = useState<RecommendationView>("soldOut");
  const [selectedRecommendations, setSelectedRecommendations] = useState<string[]>([]);
  const [recommendationSupplierFilter, setRecommendationSupplierFilter] = useState(0);
  const [recommendationMinQty, setRecommendationMinQty] = useState(0);
  const [changingRecommendation, setChangingRecommendation] = useState(false);
  const [recommendationNotice, setRecommendationNotice] = useState("");
  const [initialRecommendations, setInitialRecommendations] = useState<ProcurementRecommendation[]>(
    [],
  );
  const [savingRecommendationSupplier, setSavingRecommendationSupplier] = useState("");
  const [availabilityDueOnly, setAvailabilityDueOnly] = useState(true);
  const [checkingIntegration, setCheckingIntegration] = useState<string>("");
  const [integrationNotice, setIntegrationNotice] = useState<{
    channel: string;
    ok: boolean;
    text: string;
  } | null>(null);
  const [syncingCatalog, setSyncingCatalog] = useState("");
  const [supplierDialog, setSupplierDialog] = useState(false);
  const [reviewExpanded, setReviewExpanded] = useState(false);
  const [uploadDialog, setUploadDialog] = useState(false);
  const [requestDialog, setRequestDialog] = useState(false);
  const [planDialog, setPlanDialog] = useState(false);
  const [drafts, setDrafts] = useState<PlanDraftSummary[]>([]);
  const [editingDraftId, setEditingDraftId] = useState<number | null>(null);
  const [matchDialog, setMatchDialog] = useState<ProcurementAlias | null>(null);
  const [orderFilter, setOrderFilter] = useProcurementParam("status", "all");
  const [orderQuery, setOrderQuery] = useProcurementParam("q", "");
  const [orderWindow, setOrderWindow] = useState({ scope: "", count: 5 });
  const [recommendationQuery, setRecommendationQuery] = useProcurementParam("recommendedQuery", "");
  const [recommendationSort, setRecommendationSort] = useProcurementParam(
    "recommendedSort",
    "priority",
  );
  const [requestFilter, setRequestFilter] = useProcurementParam("requests", "active");
  const [editingRequest, setEditingRequest] = useState<ProcurementRequest | undefined>();
  const [calculatorDraft, setCalculatorDraft] = useState<ProcurementCalculatorDraft | null>(null);
  const [calculatorRequest, setCalculatorRequest] = useState<{
    name: string;
    notes: string;
  } | null>(null);
  const load = useCallback(
    () =>
      api<ProcurementData>("/api/v1/admin/procurement")
        .then(setData)
        .catch((error) => onError((error as Error).message)),
    [onError],
  );
  useEffect(() => {
    void load();
  }, [load]);
  const refreshPlanDrafts = useCallback(
    () =>
      api<{ drafts: PlanDraftSummary[] }>("/api/v1/admin/procurement/plan-drafts")
        .then((result) => setDrafts(result.drafts || []))
        .catch((error) => onError(`Не удалось загрузить черновики: ${(error as Error).message}`)),
    [onError],
  );
  useEffect(() => {
    void refreshPlanDrafts();
  }, [refreshPlanDrafts]);
  const openNewPlan = () => {
    setEditingDraftId(null);
    setSelectedRecommendations([]);
    setInitialRecommendations([]);
    setRecommendationView("soldOut");
    setPlanDialog(true);
  };
  const deleteDraft = async (draft: PlanDraftSummary) => {
    if (!window.confirm(`Удалить черновик «${draft.title}» для всей команды?`)) return;
    try {
      await api(`/api/v1/admin/procurement/plan-drafts/${draft.id}`, { method: "DELETE" });
      await refreshPlanDrafts();
    } catch (error) {
      onError(`Не удалось удалить черновик: ${(error as Error).message}`);
    }
  };
  const changeRecommendationVisibility = async (
    item: ProcurementRecommendation,
    excluded: boolean,
  ) => {
    setChangingRecommendation(true);
    setRecommendationNotice("");
    try {
      await api("/api/v1/admin/procurement/exclusions", {
        method: "PUT",
        body: JSON.stringify({
          sabyId: item.sabyId,
          excluded,
          reason: excluded ? "Скрыто из рекомендаций" : "",
        }),
      });
      setSelectedRecommendations((current) =>
        current.filter((key) => key !== `${item.supplierId}:${item.sabyId}`),
      );
      setData(await api<ProcurementData>("/api/v1/admin/procurement"));
      setRecommendationNotice(excluded ? `Скрыто: ${item.name}` : `Возвращено: ${item.name}`);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setChangingRecommendation(false);
    }
  };
  const assignRecommendationSupplier = async (sabyId: string, supplierId: number) => {
    if (supplierId <= 0) return;
    setSavingRecommendationSupplier(sabyId);
    try {
      await api("/api/v1/admin/procurement/recommendations/supplier", {
        method: "PUT",
        body: JSON.stringify({ sabyId, supplierId }),
      });
      setSelectedRecommendations([]);
      await load();
    } catch (error) {
      onError(`Не удалось назначить поставщика: ${(error as Error).message}`);
    } finally {
      setSavingRecommendationSupplier("");
    }
  };
  const syncCatalog = async (channel: string) => {
    setSyncingCatalog(channel);
    setIntegrationNotice({
      channel,
      ok: true,
      text:
        channel === "saby"
          ? "Запрашиваем СБИС…"
          : `Запрашиваем обновление ${integrationChannelLabel(channel)}…`,
    });
    try {
      const result = await api<{
        link: {
          fetched: number;
          linked: number;
          unmatched: number;
          channelKeys: number;
          catalogKeys: number;
          channelSamples: string[];
          catalogSamples: string[];
          queued?: boolean;
          queueStatus?: string;
          nextAttemptAt?: string;
        };
      }>(`/api/v1/admin/procurement/integrations/${channel}/catalog`, { method: "POST" });
      const link = result.link;
      if (link.queued && channel === "saby") {
        setIntegrationNotice({ channel, ok: true, text: "СБИС: запрос принят…" });
        let targetGeneration = 0;
        const deadline = Date.now() + 120000;
        while (Date.now() < deadline) {
          await new Promise((resolve) => window.setTimeout(resolve, 2000));
          const snapshot = await api<ProcurementData>("/api/v1/admin/procurement");
          setData(snapshot);
          const state = snapshot.integrationSync.find(
            (item) => item.channel === "saby" && item.resource === "catalog",
          );
          if (!state) continue;
          if (!targetGeneration) targetGeneration = state.requestedGeneration;
          if (state.status === "error") {
            setIntegrationNotice({
              channel,
              ok: false,
              text: `СБИС: обновление не выполнено${state.lastError ? ` — ${state.lastError}` : "."}`,
            });
            return;
          }
          if (
            state.completedGeneration >= targetGeneration &&
            state.status !== "running" &&
            state.status !== "queued"
          ) {
            setIntegrationNotice({
              channel,
              ok: true,
              text: `СБИС обновлён: ${state.rowsSynced} позиций.`,
            });
            return;
          }
          setIntegrationNotice({
            channel,
            ok: true,
            text: state.status === "running" ? "СБИС: загрузка…" : "СБИС: в очереди…",
          });
        }
        setIntegrationNotice({
          channel,
          ok: false,
          text: "СБИС: синхронизация дольше 2 минут. Статус — в «Интеграциях».",
        });
        return;
      }
      if (link.queued) {
        setIntegrationNotice({
          channel,
          ok: true,
          text: `Текущее зеркало сопоставлено; обновление ${integrationChannelLabel(channel)} добавлено в общую очередь.`,
        });
        await load();
        return;
      }
      if (channel === "saby") {
        setIntegrationNotice({
          channel,
          ok: true,
          text: `Справочник СБИС обновлён: ${link.fetched} позиций. Новые карточки уже доступны для сопоставления.`,
        });
        await load();
        return;
      }
      const detail =
        link.linked === 0 && link.fetched > 0
          ? ` Сравнивали ${link.channelKeys} ключей канала с ${link.catalogKeys} ключами СБИС. У канала: ${(link.channelSamples || []).join(", ") || "нет"}. В СБИС: ${(link.catalogSamples || []).join(", ") || "нет"}.`
          : "";
      setIntegrationNotice({
        channel,
        ok: link.linked > 0,
        text: `Прочитано карточек: ${link.fetched}, связано: ${link.linked}, без совпадения: ${link.unmatched}.${detail}`,
      });
      await load();
    } catch (error) {
      setIntegrationNotice({ channel, ok: false, text: (error as Error).message });
    } finally {
      setSyncingCatalog("");
    }
  };
  const checkIntegration = async (channel: string) => {
    setCheckingIntegration(channel);
    setIntegrationNotice(null);
    try {
      const result = await api<{ integration: IntegrationHealth }>(
        `/api/v1/admin/procurement/integrations/${channel}/check`,
        { method: "POST" },
      );
      setData((current) =>
        current
          ? {
              ...current,
              integrationHealth: current.integrationHealth.map((item) =>
                item.channel === channel ? result.integration : item,
              ),
            }
          : current,
      );
      setIntegrationNotice({
        channel,
        ok: !result.integration.lastError,
        text:
          result.integration.lastError ||
          `${integrationChannelLabel(channel)}: подключение работает`,
      });
    } catch (error) {
      const message = (error as Error).message;
      setIntegrationNotice({ channel, ok: false, text: message });
      onError(message);
    } finally {
      setCheckingIntegration("");
    }
  };

  if (!data)
    return (
      <>
        <PageHeading title="Закупки" text="Загружаем данные закупок…" />
      </>
    );
  const activeSuppliers = data.suppliers.filter((item) => item.active !== false);
  const hiddenView = recommendationView === "hidden";
  const activeClientRequests = data.requests.filter(
    (item) => item.kind === "customer_order" && ["open", "included"].includes(item.status),
  );
  const clientRecommendations = new Set(
    data.recommendations
      .filter((item) => recommendationViewFor(item) === "customer")
      .map((item) => item.sabyId),
  );
  const unmatchedClientRequests = activeClientRequests.filter(
    (item) => !clientRecommendations.has(item.sabyId),
  );
  const matchesSearch = (value: string) =>
    value.toLocaleLowerCase("ru").includes(recommendationQuery.trim().toLocaleLowerCase("ru"));
  const inRecommendationView = (item: ProcurementRecommendation, view: RecommendationView) =>
    view === "hidden"
      ? item.hiddenFromRestock || item.status === "excluded"
      : recommendationViewFor(item) === view;
  const recommendationItems = data.recommendations
    .filter(
      (item) =>
        inRecommendationView(item, recommendationView) &&
        matchesSearch(`${item.name} ${item.supplierArticle}`) &&
        (hiddenView ||
          ((recommendationSupplierFilter === 0 ||
            (recommendationSupplierFilter === -1
              ? item.supplierId === 0
              : item.supplierId === recommendationSupplierFilter)) &&
            (recommendationMinQty === 0 || item.suggestedQty > recommendationMinQty))),
    )
    .sort((a, b) => {
      const byName = () => a.name.localeCompare(b.name, "ru");
      if (hiddenView || recommendationSort === "name") return byName();
      if (recommendationSort === "quantity") return b.suggestedQty - a.suggestedQty || byName();
      if (recommendationView === "customer")
        return (a.customerDueDate || "9999").localeCompare(b.customerDueDate || "9999") || byName();
      if (recommendationView === "soldOut")
        return (
          b.totalSales - a.totalSales ||
          (b.lastSaleAt || "").localeCompare(a.lastSaleAt || "") ||
          byName()
        );
      return (
        (a.daysOfCover ?? Infinity) - (b.daysOfCover ?? Infinity) ||
        b.suggestedQty - a.suggestedQty ||
        byName()
      );
    });
  const actionableRecommendations = data.recommendations.filter(
    (item) => item.status === "recommended",
  );
  const selectedRecommendationItems = actionableRecommendations.filter((item) =>
    selectedRecommendations.includes(`${item.supplierId}:${item.sabyId}`),
  );
  const selectedSupplierId = selectedRecommendationItems[0]?.supplierId;
  const visibleOrders = data.orders.filter(
    (item) =>
      (orderFilter === "all" ||
        (orderFilter === "active"
          ? !["received", "cancelled"].includes(item.status)
          : item.status === orderFilter)) &&
      (item.title + " " + item.orderNumber + " " + item.supplierName + " " + item.documentNumber)
        .toLocaleLowerCase("ru")
        .includes(orderQuery.toLocaleLowerCase("ru")),
  );
  const orderScope = JSON.stringify([orderFilter, orderQuery]);
  const orderLimit = orderWindow.scope === orderScope ? orderWindow.count : 5;
  const pagedOrders = visibleOrders.slice(0, orderLimit);
  const visibleRequests = data.requests.filter(
    (item) =>
      requestFilter === "all" ||
      (requestFilter === "active"
        ? ["open", "included"].includes(item.status)
        : ["fulfilled", "cancelled"].includes(item.status)),
  );
  const availabilityItems = data.availability.filter((item) => !availabilityDueOnly || item.due);
  const formatTotal = (item: ProcurementOrder) =>
    new Intl.NumberFormat("ru-RU", {
      style: "currency",
      currency: item.currency,
      maximumFractionDigits: 2,
    }).format(item.total);
  const renderTab = (target: typeof view, label: string, count = 0) => (
    <button
      className={view === target ? "active" : ""}
      aria-current={view === target ? "page" : undefined}
      onClick={() => setView(target)}
    >
      {label}
      {count > 0 && <span>{count}</span>}
    </button>
  );
  const renderRecommendations = (
    add: (items: ProcurementRecommendation[]) => boolean,
    navigate: (target: "unlinkedSales" | "integrations" | "requests") => void,
    addedSabyIds: string[],
    draftSupplierId?: number,
  ) => {
    const hasRecommendationFilters =
      Boolean(recommendationQuery.trim()) ||
      (!hiddenView && (recommendationSupplierFilter !== 0 || recommendationMinQty > 0));
    const offeredItems = recommendationItems.filter(
      (item) => hiddenView || !addedSabyIds.includes(item.sabyId),
    );
    const unmatched =
      recommendationView === "customer"
        ? unmatchedClientRequests.filter((item) => matchesSearch(item.requestedName))
        : [];
    const selectionSupplierId = draftSupplierId ?? selectedSupplierId;
    const selectableVisibleRecommendations = bulkSelectableRecommendations(
      offeredItems,
      selectionSupplierId,
    );
    const bulkSupplier = activeSuppliers.find(
      (supplier) => supplier.id === selectableVisibleRecommendations[0]?.supplierId,
    );
    const allVisibleSelected =
      selectableVisibleRecommendations.length > 0 &&
      selectableVisibleRecommendations.every((item) =>
        selectedRecommendations.includes(`${item.supplierId}:${item.sabyId}`),
      );
    const toggleVisibleRecommendations = () => {
      const keys = selectableVisibleRecommendations.map(
        (item) => `${item.supplierId}:${item.sabyId}`,
      );
      setSelectedRecommendations((current) =>
        allVisibleSelected
          ? current.filter((key) => !keys.includes(key))
          : [...new Set([...current, ...keys])],
      );
    };
    return (
      <section className="admin-block procurement-block procurement-recommendations-block">
        <nav className="procurement-recommendation-tabs" aria-label="Вкладки рекомендаций">
          {(Object.keys(recommendationTabs) as RecommendationView[]).map((tab) => (
            <button
              type="button"
              className={recommendationView === tab ? "active" : ""}
              aria-pressed={recommendationView === tab}
              key={tab}
              onClick={() => {
                setRecommendationView(tab);
                setSelectedRecommendations([]);
              }}
            >
              {recommendationTabs[tab].label}{" "}
              <span>
                {data.recommendations.filter((item) => inRecommendationView(item, tab)).length +
                  (tab === "customer" ? unmatchedClientRequests.length : 0)}
              </span>
            </button>
          ))}
        </nav>
        {!hiddenView && (
          <ProcurementDataHealth
            channels={data.salesSync || []}
            onUnlinked={() => navigate("unlinkedSales")}
            onIntegrations={canIntegrations ? () => navigate("integrations") : undefined}
          />
        )}
        <div className="procurement-recommendation-filters">
          <label>
            Поиск
            <input
              value={recommendationQuery}
              onChange={(event) => setRecommendationQuery(event.target.value)}
              placeholder="Название или артикул"
            />
          </label>
          {!hiddenView && (
            <details className="procurement-extra-filters">
              <summary>Фильтры и сортировка</summary>
              <div className="procurement-extra-filter-grid">
                <label>
                  Поставщик
                  <select
                    aria-label="Фильтр по поставщику"
                    value={recommendationSupplierFilter}
                    onChange={(event) => {
                      setRecommendationSupplierFilter(Number(event.target.value));
                      setSelectedRecommendations([]);
                    }}
                  >
                    <option value={0}>Все поставщики</option>
                    <option value={-1}>Без поставщика</option>
                    {activeSuppliers.map((supplier) => (
                      <option key={supplier.id} value={supplier.id}>
                        {supplier.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Рекомендовано
                  <select
                    aria-label="Фильтр по количеству"
                    value={recommendationMinQty}
                    onChange={(event) => {
                      setRecommendationMinQty(Number(event.target.value));
                      setSelectedRecommendations([]);
                    }}
                  >
                    <option value={0}>Любое количество</option>
                    <option value={10}>Больше 10 шт.</option>
                    <option value={20}>Больше 20 шт.</option>
                  </select>
                </label>
                <label>
                  Сортировка
                  <select
                    value={recommendationSort}
                    onChange={(event) => setRecommendationSort(event.target.value)}
                  >
                    <option value="priority">По приоритету</option>
                    <option value="quantity">По количеству</option>
                    <option value="name">По названию</option>
                  </select>
                </label>
              </div>
            </details>
          )}
        </div>
        {recommendationNotice && (
          <p className="procurement-inline-notice" role="status">
            {recommendationNotice}
          </p>
        )}
        {!hiddenView && (
          <div className="procurement-selection-bar">
            <span role="status" aria-live="polite">
              {selectedRecommendationItems.length
                ? `Выбрано: ${quantityLabel(selectedRecommendationItems.length)} · ${selectedRecommendationItems.reduce((sum, item) => sum + item.suggestedQty, 0)} шт.`
                : quantityLabel(offeredItems.length)}
            </span>
            {(selectableVisibleRecommendations.length > 0 ||
              selectedRecommendationItems.length > 0) && (
              <button
                type="button"
                onClick={toggleVisibleRecommendations}
                disabled={!selectableVisibleRecommendations.length}
              >
                {allVisibleSelected ? "Снять выделение" : "Выделить все видимые"}
              </button>
            )}
            {bulkSupplier && (
              <span className="procurement-selection-hint">Поставщик: {bulkSupplier.name}</span>
            )}
            <button
              aria-label="Добавить выбранное"
              className="admin-primary"
              disabled={
                changingRecommendation ||
                !selectedRecommendationItems.length ||
                !activeSuppliers.length
              }
              onClick={() => {
                if (add(selectedRecommendationItems)) setSelectedRecommendations([]);
              }}
            >
              Добавить в закупку · {selectedRecommendationItems.length}
            </button>
          </div>
        )}
        {unmatched.length > 0 && (
          <ul
            className="procurement-compact-list"
            aria-label="Клиентские заявки без связанного товара"
          >
            {unmatched.map((item) => (
              <li key={item.id}>
                <div>
                  <strong>{item.requestedName}</strong>
                  <small>
                    Осталось {item.remainingQuantity} шт.
                    {item.dueDate &&
                      ` · к ${new Date(`${item.dueDate}T00:00:00`).toLocaleDateString("ru-RU")}`}
                  </small>
                </div>
                <button type="button" onClick={() => navigate("requests")}>
                  Открыть заявки
                </button>
              </li>
            ))}
          </ul>
        )}
        {offeredItems.length ? (
          hiddenView ? (
            <ul className="procurement-compact-list" aria-label="Скрытые растения">
              {offeredItems.map((item) => (
                <li key={item.sabyId}>
                  <div>
                    <strong>{item.name}</strong>
                    {item.supplierArticle && <small>{item.supplierArticle}</small>}
                    {item.exclusionReason && item.exclusionReason !== "Скрыто из рекомендаций" && (
                      <small>{item.exclusionReason}</small>
                    )}
                  </div>
                  <button
                    type="button"
                    disabled={changingRecommendation}
                    aria-label={`Вернуть в рекомендации: ${item.name}`}
                    onClick={() => void changeRecommendationVisibility(item, false)}
                  >
                    Вернуть
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <ProcurementRecommendationTable
              items={offeredItems}
              suppliers={activeSuppliers}
              selected={selectedRecommendations}
              selectedSupplierId={selectionSupplierId}
              savingSupplier={savingRecommendationSupplier}
              periodDays={data.settings.recommendationDays}
              onSelect={(key, checked) =>
                setSelectedRecommendations((current) =>
                  checked ? [...current, key] : current.filter((value) => value !== key),
                )
              }
              onSupplier={(sabyId, supplierId) =>
                void assignRecommendationSupplier(sabyId, supplierId)
              }
              changingVisibility={changingRecommendation}
              onHide={(item) => void changeRecommendationVisibility(item, true)}
            />
          )
        ) : (
          unmatched.length === 0 && (
            <div className="procurement-zero">
              <strong>
                {hasRecommendationFilters
                  ? "Ничего не найдено"
                  : recommendationTabs[recommendationView].empty}
              </strong>
              {hasRecommendationFilters && (
                <button
                  onClick={() => {
                    setRecommendationQuery("");
                    setRecommendationSupplierFilter(0);
                    setRecommendationMinQty(0);
                    setSelectedRecommendations([]);
                  }}
                >
                  Сбросить фильтры
                </button>
              )}
            </div>
          )
        )}
      </section>
    );
  };
  if (planDialog)
    return (
      <ProcurementPlanDialog
        key={editingDraftId ?? "new"}
        draftId={editingDraftId}
        suppliers={activeSuppliers}
        recommendations={actionableRecommendations}
        initialRecommendations={initialRecommendations}
        renderRecommendations={renderRecommendations}
        onNavigate={(target) => setView(target)}
        settings={data.settings}
        onClose={() => {
          setPlanDialog(false);
          void refreshPlanDrafts();
        }}
        onSaved={(id) => {
          setPlanDialog(false);
          setView("orders");
          setSelectedOrder(id);
          void load();
          void refreshPlanDrafts();
        }}
        onError={onError}
      />
    );
  return (
    <>
      <div className="procurement-page-heading">
        <PageHeading title="Закупки" />
        <div className="procurement-inline-actions">
          <button
            className="admin-primary"
            onClick={openNewPlan}
            disabled={activeSuppliers.length === 0}
          >
            Создать закупку
          </button>
          <button
            type="button"
            disabled={activeSuppliers.length === 0}
            onClick={() => setUploadDialog(true)}
          >
            Загрузить инвойс / счёт
          </button>
        </div>
      </div>
      {data.summary.unresolvedAliases > 0 && (
        <div className="procurement-summary-links">
          <button
            onClick={() => {
              setView("orders");
              setReviewExpanded(true);
              window.requestAnimationFrame(() =>
                document.getElementById("procurement-review")?.scrollIntoView({ block: "start" }),
              );
            }}
          >
            Выбрать товары СБИС <strong>{data.summary.unresolvedAliases}</strong>
          </button>
        </div>
      )}
      {integrationNotice?.channel === "saby" && (
        <p
          className={`integration-check-result ${integrationNotice.ok ? "success" : "error"}`}
          role="status"
        >
          {integrationNotice.text}
        </p>
      )}

      <div className="procurement-workspace">
        <nav className="procurement-tabs" aria-label="Разделы закупок">
          {renderTab("orders", "Закупки")}
          {renderTab("recommendations", "Рекомендации")}
          {renderTab("drafts", "Черновики ", drafts.length)}
          {renderTab("requests", "Под заказ ", data.summary.openRequests)}
          <WorkspaceMore
            label="Справочники и сервисы"
            active={[
              "calculator",
              "availability",
              "products",
              "unlinkedSales",
              "integrations",
              "settings",
            ].includes(view)}
          >
            {renderTab("calculator", "Калькулятор цены")}
            <button type="button" onClick={() => setSupplierDialog(true)}>
              Поставщики
            </button>
            {renderTab("products", "Товары поставщиков")}
            {renderTab("availability", "Наличие у поставщиков ", data.summary.availabilityChecks)}
            {renderTab("unlinkedSales", "Продажи без товара")}
            {canIntegrations && renderTab("integrations", "Интеграции")}
            {canIntegrations && (
              <a href="/admin?section=settings&settingsTab=marketplaces">Настройки калькулятора</a>
            )}
          </WorkspaceMore>
        </nav>
        <div className="procurement-content">
          {view === "recommendations" &&
            renderRecommendations(
              (items) => {
                setInitialRecommendations(items);
                setEditingDraftId(null);
                setPlanDialog(true);
                return true;
              },
              setView,
              [],
            )}
          <div hidden={view !== "calculator"}>
            <ProcurementCostCalculator
              initialDraft={calculatorDraft}
              onDraftChange={setCalculatorDraft}
              settings={data.settings}
              onRequest={(name, notes) => setCalculatorRequest({ name, notes })}
            />
          </div>
          {view === "drafts" && (
            <section className="admin-block procurement-block">
              <div className="admin-block-heading">
                <div>
                  <h2>Черновики закупок</h2>
                </div>
                <button className="admin-primary" onClick={openNewPlan}>
                  Создать черновик
                </button>
              </div>
              {drafts.length ? (
                <div className="procurement-draft-list">
                  {drafts.map((draft) => (
                    <article key={draft.id}>
                      <div>
                        <strong>{draft.title}</strong>
                        <small>Изменён {new Date(draft.updatedAt).toLocaleString("ru-RU")}</small>
                      </div>
                      <div>
                        <button
                          onClick={() => {
                            setInitialRecommendations([]);
                            setEditingDraftId(draft.id);
                            setPlanDialog(true);
                          }}
                        >
                          Продолжить
                        </button>
                        <button className="danger" onClick={() => void deleteDraft(draft)}>
                          Удалить
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              ) : (
                <div className="procurement-zero">
                  <strong>Черновиков пока нет</strong>
                  <span>
                    Начните закупку — изменения сохранятся автоматически и будут доступны команде.
                  </span>
                </div>
              )}
            </section>
          )}
          {view === "orders" && (
            <>
              <section className="admin-block procurement-block">
                <div className="procurement-filter-bar">
                  <input
                    aria-label="Поиск закупки"
                    value={orderQuery}
                    onChange={(event) => setOrderQuery(event.target.value)}
                    placeholder="Название, номер, поставщик"
                  />
                  <select
                    aria-label="Статус закупки"
                    value={orderFilter}
                    onChange={(event) => setOrderFilter(event.target.value)}
                  >
                    <option value="all">Все закупки</option>
                    <option value="active">В работе</option>
                    <option value="received">Завершённые</option>
                    <option value="cancelled">Отменённые</option>
                  </select>
                  <span>Закупок: {visibleOrders.length}</span>
                </div>
                {visibleOrders.length ? (
                  <div className="admin-table-wrap">
                    <table className="admin-table procurement-orders" aria-label="Закупки">
                      <thead>
                        <tr>
                          <th>Закупка</th>
                          <th>Поставщик</th>
                          <th>Источник</th>
                          <th>Статус</th>
                          <th>Состав</th>
                          <th>Сумма</th>
                          <th>Проверка</th>
                        </tr>
                      </thead>
                      <tbody>
                        {pagedOrders.map((item) => (
                          <tr key={item.id}>
                            <td>
                              <a
                                className="procurement-order-link"
                                href={procurementOrderHref(item.id)}
                                onClick={(event) => {
                                  if (!event.metaKey && !event.ctrlKey) {
                                    event.preventDefault();
                                    setSelectedOrder(item.id);
                                  }
                                }}
                              >
                                {`Закупка № ${item.id}`}
                              </a>
                              <small>{new Date(item.createdAt).toLocaleDateString("ru-RU")}</small>
                            </td>
                            <td>
                              <strong>{item.supplierName}</strong>
                              <small>{item.currency}</small>
                            </td>
                            <td>{procurementSourceLabels[item.sourceKind] || item.sourceKind}</td>
                            <td>
                              <span className={`admin-pill procurement-${item.status}`}>
                                {procurementStatusLabels[item.status] || item.status}
                              </span>
                              {item.expectedArrivalDate &&
                                !["received", "cancelled"].includes(item.status) && (
                                  <small>
                                    Ожидается{" "}
                                    {new Date(
                                      `${item.expectedArrivalDate}T00:00:00`,
                                    ).toLocaleDateString("ru-RU")}
                                  </small>
                                )}
                            </td>
                            <td>
                              {item.units} шт.<small>{quantityLabel(item.lines)}</small>
                            </td>
                            <td>
                              {formatTotal(item)}
                              {item.pendingReview ? (
                                <small>Сумма принятого состава; есть несверенные строки</small>
                              ) : null}
                            </td>
                            <td>
                              {item.status === "cancelled" ? (
                                <span>Отменена</span>
                              ) : item.failedActions ? (
                                <span className="procurement-warning">
                                  Ошибки в журнале · {item.failedActions}
                                  <small>
                                    Включая прошлые попытки — проверьте результат по каналам
                                  </small>
                                </span>
                              ) : item.pendingReview ? (
                                <span className="procurement-warning">
                                  Сверить · {item.pendingReview}
                                </span>
                              ) : item.unmatched ? (
                                <span className="procurement-warning">
                                  Нет связи с СБИС: {item.unmatched}
                                  <small>Выберите карточки товаров в закупке</small>
                                </span>
                              ) : (
                                <span>
                                  {item.status === "received"
                                    ? "Закрыта"
                                    : item.status === "cancelled"
                                      ? "Отменена"
                                      : "—"}
                                </span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="orders-empty procurement-empty">
                    <h3>
                      {orderFilter === "active" && !orderQuery
                        ? "Заказов в работе нет"
                        : "Закупки не найдены"}
                    </h3>
                    <p>
                      {orderQuery
                        ? "Попробуйте другое название или сбросьте поиск."
                        : "Посмотрите потребность или откройте завершённые закупки."}
                    </p>
                    <div className="procurement-inline-actions">
                      <button className="admin-primary" onClick={openNewPlan}>
                        Создать закупку
                      </button>
                      <button
                        onClick={() => {
                          setOrderFilter("all");
                          setOrderQuery("");
                        }}
                      >
                        Показать все закупки
                      </button>
                    </div>
                  </div>
                )}
                {visibleOrders.length > pagedOrders.length && (
                  <div className="dialog-actions">
                    <span>
                      Показано {pagedOrders.length} из {visibleOrders.length}
                    </span>
                    <button
                      onClick={() => setOrderWindow({ scope: orderScope, count: orderLimit + 5 })}
                    >
                      Показать ещё закупки
                    </button>
                  </div>
                )}
              </section>

              {data.review.length > 0 && (
                <details
                  className="procurement-section-details procurement-review-details"
                  id="procurement-review"
                  open={reviewExpanded}
                  onToggle={(event) => setReviewExpanded(event.currentTarget.open)}
                >
                  <summary>
                    Связи с товарами СБИС <span>{data.review.length}</span>
                  </summary>
                  <section className="admin-block procurement-block">
                    {data.review.length ? (
                      <div className="admin-table-wrap">
                        <table className="admin-table procurement-review">
                          <thead>
                            <tr>
                              <th>Поставщик</th>
                              <th>Название в документе</th>
                              <th>Размер</th>
                              <th>Предложенный товар</th>
                              <th></th>
                            </tr>
                          </thead>
                          <tbody>
                            {data.review.map((item) => (
                              <tr key={item.id}>
                                <td>{item.supplierName}</td>
                                <td>
                                  <strong>{item.rawName}</strong>
                                  {item.supplierArticle && (
                                    <small>Артикул: {item.supplierArticle}</small>
                                  )}
                                </td>
                                <td>
                                  {[
                                    item.potDiameterCm && `D${item.potDiameterCm}`,
                                    item.heightCm && `${item.heightCm} см`,
                                  ]
                                    .filter(Boolean)
                                    .join(" · ") || "—"}
                                </td>
                                <td>{item.suggestedSabyName || "—"}</td>
                                <td>
                                  <button
                                    className="table-action"
                                    onClick={() => setMatchDialog(item)}
                                  >
                                    Выбрать товар СБИС
                                  </button>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ) : (
                      <div className="procurement-zero">
                        <strong>Нет позиций для сопоставления</strong>
                      </div>
                    )}
                  </section>
                </details>
              )}
            </>
          )}

          {view === "products" && (
            <Suspense
              fallback={
                <section className="admin-block procurement-block">
                  <p className="admin-hint">Загружаем каталог Saby…</p>
                </section>
              }
            >
              <ProcurementProducts
                suppliers={activeSuppliers}
                onError={onError}
                onSaved={() => void load()}
              />
            </Suspense>
          )}

          {view === "unlinkedSales" && (
            <Suspense fallback={<p className="admin-hint">Загружаем продажи…</p>}>
              <ProcurementUnlinkedSales onError={onError} />
            </Suspense>
          )}

          {view === "requests" && (
            <section className="admin-block procurement-block">
              <div className="admin-block-heading">
                <div>
                  <p className="eyebrow">Приоритет закупки</p>
                  <h2>Под заказ и идеи магазина</h2>
                </div>
                <button className="admin-primary" onClick={() => setRequestDialog(true)}>
                  Добавить
                </button>
              </div>
              <div className="procurement-filter-bar">
                <select
                  aria-label="Показать запросы"
                  value={requestFilter}
                  onChange={(event) => setRequestFilter(event.target.value)}
                >
                  <option value="active">Открытые и в закупке</option>
                  <option value="archive">История</option>
                  <option value="all">Все запросы</option>
                </select>
                <span>Запросов: {visibleRequests.length}</span>
              </div>
              {visibleRequests.length ? (
                <div className="admin-table-wrap">
                  <table className="admin-table procurement-requests">
                    <thead>
                      <tr>
                        <th>Тип</th>
                        <th>Товар</th>
                        <th>Количество</th>
                        <th>Статус</th>
                        <th>Комментарий</th>
                        <th>Действия</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visibleRequests.map((item) => (
                        <tr key={item.id}>
                          <td>
                            {item.kind === "customer_order" ? (
                              <span className="procurement-warning">Клиенту</span>
                            ) : (
                              "Рекомендация"
                            )}
                            <small>
                              {item.customerName ||
                                (item.kind === "customer_order"
                                  ? "Клиентский запрос"
                                  : "Запрос магазина")}
                            </small>
                            {item.customerOrderId && (
                              <a
                                href={`/admin?section=orders&customerOrderId=${item.customerOrderId}`}
                              >
                                Заказ CRM №{item.customerOrderId} ↗
                              </a>
                            )}
                          </td>
                          <td>
                            <strong>{item.requestedName}</strong>
                            <small>{item.sabyId || "Не связан со СБИС"}</small>
                          </td>
                          <td>
                            <strong>{item.quantity}</strong>
                            <small>
                              распределено {item.allocatedQuantity} · осталось{" "}
                              {item.remainingQuantity}
                            </small>
                          </td>
                          <td>
                            {{
                              included: "В закупке",
                              open: "Открыт",
                              fulfilled: "Выполнен",
                              cancelled: "Отменён",
                            }[item.status] || item.status}
                            <small>
                              {item.allocations
                                ?.map(
                                  (allocation) =>
                                    `${allocation.orderNumber || `№${allocation.procurementOrderId}`}: ${allocation.quantity} шт.`,
                                )
                                .join(" · ")}
                            </small>
                          </td>
                          <td>
                            {item.notes || "—"}
                            <small>
                              {item.dueDate
                                ? `Нужно к ${new Date(`${item.dueDate}T00:00:00`).toLocaleDateString("ru-RU")}`
                                : "Срок не указан"}
                            </small>
                            <small>{item.assignee || "Ответственный не назначен"}</small>
                            <small>Создан {new Date(item.createdAt).toLocaleString("ru-RU")}</small>
                            {Boolean(item.history?.length) && (
                              <details>
                                <summary>История запроса</summary>
                                {item.history?.map((event, index) => (
                                  <p key={index}>
                                    {new Date(event.at).toLocaleString("ru-RU")} ·{" "}
                                    {(
                                      {
                                        open: "Открыт",
                                        included: "В закупке",
                                        fulfilled: "Выполнен",
                                        cancelled: "Отменён",
                                      } as Record<string, string>
                                    )[event.status] || event.status}{" "}
                                    · {event.quantity} шт.
                                    <small>
                                      {event.name} {event.notes}
                                    </small>
                                    <small>
                                      {event.dueDate
                                        ? `Срок: ${new Date(`${event.dueDate}T00:00:00`).toLocaleDateString("ru-RU")}`
                                        : "Без срока"}{" "}
                                      · {event.assignee || "Ответственный не назначен"}
                                    </small>
                                  </p>
                                ))}
                              </details>
                            )}
                          </td>
                          <td>
                            <div className="procurement-inline-actions">
                              {["open", "included"].includes(item.status) ? (
                                <>
                                  <button
                                    onClick={() => {
                                      setEditingRequest(item);
                                      setRequestDialog(true);
                                    }}
                                  >
                                    Изменить
                                  </button>
                                  <button
                                    onClick={() =>
                                      void updateRequestStatus(item, "fulfilled", load, onError)
                                    }
                                  >
                                    Выполнен
                                  </button>
                                  <button
                                    onClick={() =>
                                      void updateRequestStatus(item, "cancelled", load, onError)
                                    }
                                  >
                                    Отменить
                                  </button>
                                </>
                              ) : item.status === "cancelled" ? (
                                <button
                                  onClick={() =>
                                    void updateRequestStatus(item, "open", load, onError)
                                  }
                                >
                                  Вернуть в работу
                                </button>
                              ) : (
                                <span>Выполнено</span>
                              )}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="procurement-zero">
                  <strong>Запросов нет</strong>
                  <span>Добавьте клиентский заказ или рекомендацию сотрудника.</span>
                </div>
              )}
            </section>
          )}

          {view === "availability" && (
            <section className="admin-block procurement-block">
              <div className="admin-block-heading">
                <div>
                  <p className="eyebrow">Наличие у поставщика</p>
                  <h2>Проверить наличие</h2>
                </div>
                <div className="procurement-inline-actions">
                  <button
                    className={availabilityDueOnly ? "active" : ""}
                    onClick={() => setAvailabilityDueOnly(true)}
                  >
                    Пора проверить · {data.availability.filter((item) => item.due).length}
                  </button>
                  <button
                    className={!availabilityDueOnly ? "active" : ""}
                    onClick={() => setAvailabilityDueOnly(false)}
                  >
                    Все · {data.availability.length}
                  </button>
                </div>
              </div>
              <p className="admin-hint procurement-note">
                Статус хранится на паре товар + поставщик, поэтому пометить можно любое растение из
                справочника, а не только встреченное в разобранном инвойсе.
              </p>
              {availabilityItems.length ? (
                <div className="admin-table-wrap">
                  <table className="admin-table procurement-availability">
                    <thead>
                      <tr>
                        <th>Товар</th>
                        <th>Поставщик</th>
                        <th>Остаток</th>
                        <th>С какого дня</th>
                        <th>Статус</th>
                        <th>Проверка вручную</th>
                      </tr>
                    </thead>
                    <tbody>
                      {availabilityItems.map((item) => (
                        <AvailabilityRow
                          key={`${item.supplierId}-${item.sabyId}`}
                          item={item}
                          reload={load}
                          onError={onError}
                        />
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="procurement-zero">
                  <strong>На сегодня проверок нет</strong>
                  <span>
                    {data.availability.length
                      ? "Будущие проверки доступны во вкладке «Все»."
                      : "Отмечайте товары для проверки в рекомендациях или справочнике."}
                  </span>
                </div>
              )}
            </section>
          )}

          {view === "integrations" && canIntegrations && (
            <section className="admin-block procurement-block">
              <div className="admin-block-heading">
                <div>
                  <p className="eyebrow">Безопасная диагностика</p>
                  <h2>Подключения API</h2>
                </div>
              </div>

              <div className="integration-health-grid">
                {(data.integrationHealth || []).map((item) => (
                  <article className={item.lastError ? "attention" : ""} key={item.channel}>
                    <div>
                      <strong>{integrationChannelLabel(item.channel)}</strong>
                      <span>
                        {!item.configured
                          ? "Переменные не найдены"
                          : item.lastError
                            ? "Ошибка подключения"
                            : item.lastSuccessAt
                              ? "Настроено · проверялось"
                              : "Не проверялось"}
                      </span>
                    </div>
                    <small>
                      {item.lastSuccessAt
                        ? `Успешная проверка подключения: ${new Date(item.lastSuccessAt).toLocaleString("ru-RU")}`
                        : "Успешных проверок ещё нет"}
                    </small>
                    <small>
                      Продажи:{" "}
                      {data.salesSync?.find((sync) => sync.channel === item.channel)?.lastSuccessAt
                        ? new Date(
                            data.salesSync.find(
                              (sync) => sync.channel === item.channel,
                            )!.lastSuccessAt!,
                          ).toLocaleString("ru-RU")
                        : "Успешная загрузка не зафиксирована"}
                    </small>
                    {item.lastError && <em>{item.lastError}</em>}
                    {integrationNotice?.channel === item.channel && (
                      <p
                        className={`integration-check-result ${integrationNotice.ok ? "success" : "error"}`}
                        role="status"
                      >
                        {integrationNotice.text}
                      </p>
                    )}
                    <button
                      className="secondary-button"
                      disabled={checkingIntegration !== ""}
                      onClick={() => void checkIntegration(item.channel)}
                    >
                      {checkingIntegration === item.channel
                        ? "Проверяем…"
                        : item.channel === "wb"
                          ? "Проверить зеркало"
                          : "Проверить подключение"}
                    </button>
                    <button
                      className="secondary-button"
                      disabled={syncingCatalog !== "" || !item.configured}
                      onClick={() => void syncCatalog(item.channel)}
                    >
                      {syncingCatalog === item.channel
                        ? "Сопоставляем…"
                        : item.channel === "saby"
                          ? "Обновить справочник"
                          : item.channel === "wb"
                            ? "Сопоставить из зеркала"
                            : "Подтянуть артикулы"}
                    </button>
                  </article>
                ))}
              </div>

              <details className="procurement-explanation">
                <summary>Как работают подключения</summary>
                <p className="admin-hint procurement-note">
                  Проверка только читает одну служебную запись. Цены, остатки, заказы и документы не
                  изменяются.
                </p>
                <p className="admin-hint procurement-note">
                  Подтягивание связывает карточки маркетплейса с номенклатурой СБИС по точному
                  совпадению кода, артикула или штрихкода и заполняет только пустые поля. Совпадение
                  по названию не используется: «Фикус 12» и «Фикус 14» — разные растения. Что не
                  совпало, разбирается руками на вкладке «Продажи без товара».
                </p>
                <p className="admin-hint procurement-note">
                  Wildberries раз в час фоново сохраняет карточки, артикулы, цены и продажи в
                  локальное зеркало. Разделы сайта и кнопка сопоставления читают только базу и не
                  создают дополнительных запросов к WB. Токену нужны категории «Цены и скидки»,
                  «Статистика» и «Контент».
                </p>
                <p className="admin-hint procurement-note">
                  СБИС здесь проверяет авторизацию, точку 278 и прайс-лист 6. Кнопка обновления
                  ставит один приоритетный проход в общую очередь; состояние и следующий запуск
                  видны в разделе «Маркетплейсы».
                </p>
              </details>
            </section>
          )}

          {view === "settings" && canIntegrations && (
            <a href="/admin?section=settings&settingsTab=marketplaces">
              Открыть настройки калькулятора в разделе «Настройки → Маркетплейсы»
            </a>
          )}
        </div>
      </div>
      {supplierDialog && (
        <SupplierDialog
          suppliers={data.suppliers}
          canDelete={canDelete}
          onClose={() => setSupplierDialog(false)}
          onSaved={() => void load()}
          onError={onError}
        />
      )}
      {uploadDialog && (
        <ProcurementUploadDialog
          suppliers={activeSuppliers}
          orders={data.orders}
          onClose={() => setUploadDialog(false)}
          onSaved={(id) => {
            setUploadDialog(false);
            setView("orders");
            setSelectedOrder(id);
            void load();
          }}
        />
      )}
      {requestDialog && (
        <ProcurementRequestDialog
          request={editingRequest}
          onClose={() => {
            setRequestDialog(false);
            setEditingRequest(undefined);
          }}
          onSaved={() => {
            setRequestDialog(false);
            setEditingRequest(undefined);
            void load();
          }}
          onError={onError}
        />
      )}
      {calculatorRequest && (
        <ProcurementRequestDialog
          initialName={calculatorRequest.name}
          initialNotes={calculatorRequest.notes}
          onClose={() => setCalculatorRequest(null)}
          onSaved={() => {
            setCalculatorRequest(null);
            setView("requests");
            void load();
          }}
          onError={onError}
        />
      )}
      {matchDialog && (
        <ProcurementMatchDialog
          alias={matchDialog}
          onClose={() => setMatchDialog(null)}
          onSaved={() => {
            setMatchDialog(null);
            void load();
          }}
          onError={onError}
        />
      )}
      {selectedOrder && (
        <ProcurementOrderDetailDialog
          settings={data.settings}
          orderId={selectedOrder}
          documents={data.documents.filter((item) => item.orderId === selectedOrder)}
          canDelete={canDelete}
          onClose={() => setSelectedOrder(null)}
          onSaved={() => {
            void load();
          }}
          onError={onError}
        />
      )}
    </>
  );
}
