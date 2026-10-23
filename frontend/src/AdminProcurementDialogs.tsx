import { receivingModeLabel } from "./procurementUX";
import { ProcurementLineEditor } from "./ProcurementLineEditor";
import { ProcurementReceivingDialog } from "./ProcurementReceivingDialog";
import { lazy, Suspense, useCallback, useEffect, useId, useRef, useState } from "react";
import {
  useProcurementDialog,
  useProcurementError,
  validateProcurementPDF,
  uploadProcurementPDF,
  quantityLabel,
} from "./procurementUX";
export { ProcurementRequestForm as ProcurementRequestDialog } from "./ProcurementRequestForm";
import { normalizeProcurementOrderDetail, procurementStatusLabels } from "./AdminProcurement";
import { ConfirmDialog, api, money } from "./adminShared";
import type {
  NomenclatureCandidate,
  ProcurementActionBatch,
  ProcurementActionItem,
  ProcurementAlias,
  ProcurementOrder,
  ProcurementOrderDetail,
  ProcurementOrderLine,
  ProcurementRecommendation,
  ProcurementSupplier,
  ProcurementSettings,
} from "./adminTypes";

const UnifiedPricingPreview = lazy(() => import("./UnifiedPricingPreview"));

const ProcurementInvoicePairDialog = lazy(() =>
  import("./ProcurementInvoicePairDialog").then((module) => ({
    default: module.ProcurementInvoicePairDialog,
  })),
);
const ProcurementLinePricing = lazy(() =>
  import("./ProcurementLinePricing").then((module) => ({ default: module.ProcurementLinePricing })),
);

const reconciliationLabel = (value: string) =>
  ({
    planned: "Ожидает инвойс",
    matched: "Совпадает",
    changed: "Изменено",
    missing: "Нет в инвойсе",
    added: "Добавлено поставщиком",
    excluded: "Исключено",
    superseded: "Прошлая версия",
  })[value] || value;
const decimalNumber = (value: string) => Number(value.trim().replace(",", "."));
const displayPrice = (value: string | number | null | undefined) => {
  const text = String(value ?? "")
    .trim()
    .replace(".", ",");
  return /^\d+(?:,\d*)?$/.test(text)
    ? text.replace(/,(\d*?)0*$/, (_, digits: string) => (digits ? `,${digits}` : ""))
    : text;
};

function sortPlanLines<T extends { category: string; expectedUnitPrice: string }>(lines: T[]): T[] {
  return [...lines].sort((a, b) => {
    const categoryA = a.category.trim();
    const categoryB = b.category.trim();
    if (!categoryA && categoryB) return 1;
    if (categoryA && !categoryB) return -1;
    const categoryOrder = categoryA.localeCompare(categoryB, "ru");
    if (categoryOrder) return categoryOrder;
    const priceA = decimalNumber(a.expectedUnitPrice);
    const priceB = decimalNumber(b.expectedUnitPrice);
    const priceOrder = (priceA > 0 ? priceA : Infinity) - (priceB > 0 ? priceB : Infinity);
    return priceOrder || 0;
  });
}

export function ProcurementPlanDialog({
  suppliers,
  recommendations,
  initialRecommendations,
  settings,
  draftId,
  onClose,
  onSaved,
  onError: externalError,
}: {
  suppliers: ProcurementSupplier[];
  recommendations: ProcurementRecommendation[];
  initialRecommendations: ProcurementRecommendation[];
  settings: ProcurementSettings;
  draftId?: number | null;
  onClose: () => void;
  onSaved: (id: number) => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(() => {
    void closePlan();
  });
  type NumericDraft = string;
  type DraftLine = {
    id: string;
    sabyId: string;
    sabyName: string;
    article: string;
    category: string;
    recommendedQty: number;
    packageCount: NumericDraft;
    unitsPerPackage: NumericDraft;
    expectedUnitPrice: NumericDraft;
    potDiameterCm: NumericDraft;
    heightCm: NumericDraft;
    loadUnit: string;
    rowTouched?: boolean;
  };
  type SavedPlan = {
    sourceKind?: string;
    supplierId: number;
    exchangeRate: NumericDraft;
    deliveryToMoscowRub: NumericDraft;
    deliveryToRyazanRub: NumericDraft;
    items: DraftLine[];
  };
  type RemoteDraft = { id: number; title: string; payload: SavedPlan; updatedAt: string };
  type ProductDefault = {
    potDiameterCm: number | null;
    heightCm: number | null;
    category: string;
    article: string;
    unitPrice: number | null;
    unitsPerPackage: number;
  };
  const preferred =
    suppliers.find((item) => item.id === initialRecommendations[0]?.supplierId) ||
    suppliers.find((item) => item.kind === "international") ||
    suppliers[0];
  const makeLine = (item?: ProcurementRecommendation): DraftLine => ({
    id: item?.sabyId || `new-${Date.now()}-${Math.random()}`,
    sabyId: item?.sabyId || "",
    sabyName: item?.name || "",
    article: item?.supplierArticle || "",
    category: item?.dutchName || "",
    recommendedQty: item?.suggestedQty || 0,
    packageCount: item?.quantityKnown === false ? "" : "1",
    unitsPerPackage: String(item?.orderMultiple || 1),
    expectedUnitPrice: displayPrice(item?.lastUnitPrice),
    potDiameterCm: item?.potDiameterCm?.toString() || "",
    heightCm: item?.heightCm?.toString() || "",
    loadUnit: "shelf",
  });
  const recommendationsFor = (id: number) =>
    recommendations.filter((item) => item.supplierId === id);
  const [showRecommendations, setShowRecommendations] = useState(false);
  const [confirmPlan, setConfirmPlan] = useState(false);
  const [sourceKind, setSourceKind] = useState(
    initialRecommendations.length ? "recommendation" : "manual",
  );
  const [supplierId, setSupplierId] = useState(preferred?.id || 0);
  const [saving, setSaving] = useState(false);
  const selectedSupplier = suppliers.find((item) => item.id === supplierId);
  const isDomestic = selectedSupplier?.kind === "domestic";
  const purchaseCurrency = selectedSupplier?.defaultCurrency || (isDomestic ? "RUB" : "EUR");
  const currencySymbol = purchaseCurrency === "RUB" ? "₽" : purchaseCurrency === "USD" ? "$" : "€";
  const [exchangeRate, setExchangeRate] = useState<NumericDraft>(
    String(settings.defaultExchangeRate),
  );
  const [deliveryToMoscowRub, setDeliveryToMoscowRub] = useState<NumericDraft>("");
  const [deliveryToRyazanRub, setDeliveryToRyazanRub] = useState<NumericDraft>("");
  const [sabyQuery, setSabyQuery] = useState("");
  const [sabyResults, setSabyResults] = useState<NomenclatureCandidate[]>([]);
  const [searchingSaby, setSearchingSaby] = useState(false);
  const [sabyBalances, setSabyBalances] = useState<
    Record<string, { balance: number; seenAt?: string; defaults: ProductDefault[] }>
  >({});
  const isEmptyLine = (item: DraftLine) =>
    !item.sabyId &&
    !item.sabyName.trim() &&
    !item.article.trim() &&
    !item.category.trim() &&
    !item.expectedUnitPrice &&
    !item.potDiameterCm &&
    !item.heightCm;
  const [items, setItems] = useState<DraftLine[]>(() =>
    initialRecommendations.length ? initialRecommendations.map(makeLine) : [makeLine()],
  );
  const [sortOrder, setSortOrder] = useState(() => sortPlanLines(items).map((item) => item.id));
  const sabyIDsKey = [...new Set(items.map((item) => item.sabyId).filter(Boolean))]
    .sort()
    .join("\u001f");
  useEffect(() => {
    if (!sabyIDsKey) {
      setSabyBalances({});
      return;
    }
    let cancelled = false;
    void api<{
      items: { sabyId: string; balance: number; seenAt?: string; defaults: ProductDefault[] }[];
    }>("/api/v1/admin/procurement/nomenclature/balances", {
      method: "POST",
      body: JSON.stringify({ supplierId, sabyIds: sabyIDsKey.split("\u001f") }),
    })
      .then((result) => {
        if (!cancelled)
          setSabyBalances(Object.fromEntries(result.items.map((item) => [item.sabyId, item])));
      })
      .catch(() => {
        if (!cancelled) setSabyBalances({});
      });
    return () => {
      cancelled = true;
    };
  }, [sabyIDsKey, supplierId]);
  useEffect(() => {
    setItems((current) =>
      current.map((line) => {
        const defaults = sabyBalances[line.sabyId]?.defaults || [];
        if (
          defaults.length !== 1 ||
          line.category ||
          line.expectedUnitPrice ||
          line.potDiameterCm ||
          line.heightCm
        )
          return line;
        const value = defaults[0];
        return {
          ...line,
          category: value.category,
          article: value.article || line.article,
          expectedUnitPrice: displayPrice(value.unitPrice),
          potDiameterCm: value.potDiameterCm?.toString() || "",
          heightCm: value.heightCm?.toString() || "",
          unitsPerPackage: String(value.unitsPerPackage),
        };
      }),
    );
  }, [sabyBalances]);
  const [remoteDraft, setRemoteDraft] = useState<RemoteDraft | null>(null);
  const [draftLoading, setDraftLoading] = useState(Boolean(draftId));
  const [title, setTitle] = useState(() =>
    initialRecommendations.length
      ? `Закупка по рекомендациям · ${new Date().toLocaleDateString("ru-RU")}`
      : `Новая закупка · ${new Date().toLocaleDateString("ru-RU")}`,
  );
  const [draftSaving, setDraftSaving] = useState(false);
  const [savedDraftSignature, setSavedDraftSignature] = useState("");
  const initialTitle = useRef(title);
  const remoteDraftRef = useRef<RemoteDraft | null>(null);
  const savedSignatureRef = useRef("");
  const failedSignatureRef = useRef("");
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  useEffect(() => {
    if (!draftId) return;
    void api<{ draft: RemoteDraft }>(`/api/v1/admin/procurement/plan-drafts/${draftId}`)
      .then(({ draft }) => {
        const saved = draft.payload;
        if (!saved?.items?.length) throw new Error("Черновик пуст или повреждён");
        const normalizedItems = saved.items.map((item) => ({
          ...item,
          expectedUnitPrice: displayPrice(item.expectedUnitPrice),
        }));
        const signature = JSON.stringify({
          title: draft.title,
          payload: {
            sourceKind: saved.sourceKind || "manual",
            supplierId: saved.supplierId,
            exchangeRate: saved.exchangeRate,
            deliveryToMoscowRub: saved.deliveryToMoscowRub,
            deliveryToRyazanRub: saved.deliveryToRyazanRub,
            items: normalizedItems,
          },
        });
        remoteDraftRef.current = draft;
        savedSignatureRef.current = signature;
        setRemoteDraft(draft);
        setTitle(draft.title);
        setSourceKind(saved.sourceKind || "manual");
        setSupplierId(saved.supplierId);
        setExchangeRate(saved.exchangeRate);
        setDeliveryToMoscowRub(saved.deliveryToMoscowRub);
        setDeliveryToRyazanRub(saved.deliveryToRyazanRub);
        setItems(normalizedItems);
        setSortOrder(sortPlanLines(normalizedItems).map((item) => item.id));
        setSavedDraftSignature(signature);
      })
      .catch((error) => onError(`Не удалось открыть черновик: ${(error as Error).message}`))
      .finally(() => setDraftLoading(false));
  }, [draftId, onError]);
  const currentDraft = (): SavedPlan => ({
    sourceKind,
    supplierId,
    exchangeRate,
    deliveryToMoscowRub,
    deliveryToRyazanRub,
    items: items.map((item) => ({
      ...item,
      expectedUnitPrice: displayPrice(item.expectedUnitPrice),
    })),
  });
  const draftSignature = JSON.stringify({ title: title.trim(), payload: currentDraft() });
  const persistDraft = useCallback(
    (signature: string): Promise<boolean> => {
      if (draftLoading || (draftId && !remoteDraftRef.current)) return Promise.resolve(false);
      const queued = saveQueueRef.current.then(async () => {
        if (signature === savedSignatureRef.current) return true;
        const snapshot = JSON.parse(signature) as { title: string; payload: SavedPlan };
        if (!snapshot.title) return false;
        setDraftSaving(true);
        try {
          const existing = remoteDraftRef.current;
          const result = await api<{ draft: RemoteDraft }>(
            existing
              ? `/api/v1/admin/procurement/plan-drafts/${existing.id}`
              : "/api/v1/admin/procurement/plan-drafts",
            { method: existing ? "PUT" : "POST", body: signature },
          );
          remoteDraftRef.current = result.draft;
          savedSignatureRef.current = signature;
          failedSignatureRef.current = "";
          setRemoteDraft(result.draft);
          setSavedDraftSignature(signature);
          return true;
        } catch (error) {
          failedSignatureRef.current = signature;
          onError(`Не удалось сохранить черновик: ${(error as Error).message}`);
          return false;
        } finally {
          setDraftSaving(false);
        }
      });
      saveQueueRef.current = queued;
      return queued;
    },
    [draftId, draftLoading, onError],
  );
  const hasDraftContent =
    initialRecommendations.length > 0 ||
    items.some((item) => !isEmptyLine(item)) ||
    title !== initialTitle.current;
  const commitPlan = useCallback(() => {
    // Sorting after focus changes keeps the row in place while a price or category is edited.
    const normalizedItems = items.map((item) => ({
      ...item,
      expectedUnitPrice: displayPrice(item.expectedUnitPrice),
    }));
    if (
      normalizedItems.some(
        (item, index) => item.expectedUnitPrice !== items[index].expectedUnitPrice,
      )
    )
      setItems(normalizedItems);
    window.setTimeout(() => setSortOrder(sortPlanLines(normalizedItems).map((item) => item.id)), 0);
    if (
      !draftLoading &&
      !saving &&
      hasDraftContent &&
      draftSignature !== savedSignatureRef.current &&
      draftSignature !== failedSignatureRef.current
    ) {
      void persistDraft(draftSignature);
    }
  }, [draftLoading, draftSignature, hasDraftContent, items, persistDraft, saving]);
  useEffect(() => {
    window.addEventListener("blur", commitPlan);
    return () => window.removeEventListener("blur", commitPlan);
  }, [commitPlan]);
  const closePlan = async () => {
    if (
      hasDraftContent &&
      draftSignature !== savedSignatureRef.current &&
      !(await persistDraft(draftSignature))
    )
      return;
    onClose();
  };
  const availableRecommendations = recommendationsFor(supplierId).filter(
    (item) => !items.some((line) => line.sabyId === item.sabyId),
  );
  const updateItem = (id: string, patch: Partial<DraftLine>) =>
    setItems((current) =>
      current.map((item) => {
        if (item.id !== id) return item;
        return { ...item, ...patch, rowTouched: true };
      }),
    );
  const markLineTouched = (id: string) =>
    setItems((current) =>
      current.map((item) =>
        item.id === id && !item.rowTouched ? { ...item, rowTouched: true } : item,
      ),
    );
  const draftNumber = (value: string): NumericDraft => value;
  const addRecommendation = (recommendation: ProcurementRecommendation) =>
    setItems((current) =>
      current.length === 1 && isEmptyLine(current[0])
        ? [makeLine(recommendation)]
        : [...current, makeLine(recommendation)],
    );
  const addSabyProduct = (candidate: NomenclatureCandidate) => {
    const recommendation = recommendationsFor(supplierId).find(
      (item) => item.sabyId === candidate.sabyId,
    );
    const line = recommendation
      ? makeLine(recommendation)
      : { ...makeLine(), id: candidate.sabyId, sabyId: candidate.sabyId, sabyName: candidate.name };
    setItems((current) =>
      current.length === 1 && isEmptyLine(current[0]) ? [line] : [...current, line],
    );
    setSabyQuery("");
    setSabyResults([]);
  };
  const sorted = [...items].sort(
    (a, b) => (sortOrder.indexOf(a.id) + 1 || Infinity) - (sortOrder.indexOf(b.id) + 1 || Infinity),
  );
  const selected = sorted.filter((item) => !isEmptyLine(item));
  const totalUnits = selected.reduce(
    (sum, item) => sum + Number(item.packageCount) * Number(item.unitsPerPackage),
    0,
  );
  const missingFields = (item: DraftLine) => {
    const missing: string[] = [];
    if (!item.sabyId && !item.sabyName.trim()) missing.push("название");
    if (!item.category.trim()) missing.push("название поставщика");
    if (!Number.isInteger(Number(item.packageCount)) || Number(item.packageCount) <= 0)
      missing.push("упаковки");
    if (!Number.isInteger(Number(item.unitsPerPackage)) || Number(item.unitsPerPackage) <= 0)
      missing.push("штук в упаковке");
    if (Number(item.packageCount) * Number(item.unitsPerPackage) > 1000000)
      missing.push("количество");
    if (!(decimalNumber(item.expectedUnitPrice) > 0)) missing.push("цена");
    if (!isDomestic && !(decimalNumber(item.potDiameterCm) > 0)) missing.push("горшок");
    if (!(decimalNumber(item.heightCm) > 0)) missing.push("высота");
    return missing;
  };
  const invalidItems = selected.filter((item) => missingFields(item).length > 0);
  const valid =
    selected.length > 0 &&
    (isDomestic || Number(exchangeRate) > 0) &&
    (isDomestic || (Number(deliveryToMoscowRub) >= 0 && Number(deliveryToRyazanRub) >= 0)) &&
    selected.every(
      (item) =>
        item.category.trim() &&
        (item.sabyId || item.sabyName.trim()) &&
        Number.isInteger(Number(item.packageCount)) &&
        Number(item.packageCount) > 0 &&
        Number.isInteger(Number(item.unitsPerPackage)) &&
        Number(item.unitsPerPackage) > 0 &&
        Number(item.packageCount) * Number(item.unitsPerPackage) <= 1000000 &&
        decimalNumber(item.expectedUnitPrice) > 0 &&
        decimalNumber(item.heightCm) > 0 &&
        (isDomestic || decimalNumber(item.potDiameterCm) > 0),
    );
  const totalPurchaseEUR = selected.reduce(
    (sum, item) =>
      sum +
      decimalNumber(item.expectedUnitPrice || "0") *
        Number(item.packageCount || 0) *
        Number(item.unitsPerPackage || 0),
    0,
  );
  const partialCost = (item: DraftLine) => {
    if (missingFields(item).length || (!isDomestic && !(Number(exchangeRate) > 0))) return null;
    const purchase =
      decimalNumber(item.expectedUnitPrice) * (isDomestic ? 1 : Number(exchangeRate));
    if (isDomestic) return purchase;
    const volume =
      Math.PI * (decimalNumber(item.potDiameterCm) / 2) ** 2 * decimalNumber(item.heightCm);
    return (
      purchase +
      (settings.trolleyCostRub * volume) / (settings.trolleyVolumeCm3 * settings.trolleyFillRatio)
    );
  };
  const requestBody = JSON.stringify({
    supplierId,
    orderNumber: "",
    title,
    sourceKind,
    costs: {
      exchangeRate: isDomestic ? 1 : Number(exchangeRate),
      deliveryToMoscowRub: isDomestic ? 0 : Number(deliveryToMoscowRub),
      deliveryToRyazanRub: isDomestic ? 0 : Number(deliveryToRyazanRub),
    },
    items: selected.map((item) => ({
      sabyId: item.sabyId,
      rawName: item.sabyName || item.category,
      category: item.category,
      supplierArticle: item.article,
      packageCount: Number(item.packageCount),
      unitsPerPackage: Number(item.unitsPerPackage),
      quantity: Number(item.packageCount) * Number(item.unitsPerPackage),
      expectedUnitPrice: decimalNumber(item.expectedUnitPrice),
      potDiameterCm: item.potDiameterCm === "" ? null : decimalNumber(item.potDiameterCm),
      heightCm: item.heightCm === "" ? null : decimalNumber(item.heightCm),
      loadUnit: item.loadUnit,
    })),
  });
  type PreviewLine = { purchase: number; cost: number; retail: number };
  const [preview, setPreview] = useState<{
    key: string;
    lines: PreviewLine[];
    error?: string;
  } | null>(null);
  useEffect(() => {
    if (!valid) return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void api<{ lines: PreviewLine[] }>("/api/v1/admin/procurement/plans/preview", {
        method: "POST",
        body: requestBody,
      })
        .then((result) => {
          if (!cancelled) setPreview({ key: requestBody, lines: result.lines });
        })
        .catch(() => {
          if (!cancelled)
            setPreview({
              key: requestBody,
              lines: [],
              error: "Не удалось рассчитать. Проверьте подключение и данные.",
            });
        });
    }, 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [requestBody, valid]);
  useEffect(() => {
    if (sabyQuery.trim().length < 2) {
      setSabyResults([]);
      setSearchingSaby(false);
      return;
    }
    let cancelled = false;
    const controller = new AbortController();
    let requestTimeout = 0;
    const timer = window.setTimeout(() => {
      setSearchingSaby(true);
      requestTimeout = window.setTimeout(() => controller.abort(), 8000);
      void api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(sabyQuery.trim())}`,
        { signal: controller.signal },
      )
        .then((result) => {
          if (!cancelled)
            setSabyResults(
              result.items.filter(
                (candidate) => !items.some((line) => line.sabyId === candidate.sabyId),
              ),
            );
        })
        .catch((error) => {
          if (!cancelled)
            onError(
              (error as Error).name === "AbortError"
                ? "Поиск занял слишком много времени. Попробуйте точный код СБИС."
                : (error as Error).message,
            );
        })
        .finally(() => {
          window.clearTimeout(requestTimeout);
          if (!cancelled) setSearchingSaby(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      window.clearTimeout(requestTimeout);
      controller.abort();
    };
  }, [items, onError, sabyQuery]);
  const save = async () => {
    if (!valid || saving) return;
    setSaving(true);
    try {
      await saveQueueRef.current;
      if (
        hasDraftContent &&
        draftSignature !== savedSignatureRef.current &&
        !(await persistDraft(draftSignature))
      )
        return;
      const result = await api<{ order: ProcurementOrder }>("/api/v1/admin/procurement/plans", {
        method: "POST",
        body: requestBody,
      });
      if (!result.order?.id)
        throw new Error(
          "Сервер не вернул номер заказа. Проверьте список закупок перед повторным сохранением.",
        );
      if (remoteDraftRef.current)
        await api(`/api/v1/admin/procurement/plan-drafts/${remoteDraftRef.current.id}`, {
          method: "DELETE",
        }).catch(() => undefined);
      onSaved(result.order.id);
    } catch (error) {
      setConfirmPlan(false);
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть"
        onClick={() => void closePlan()}
      />
      <div
        className="admin-dialog procurement-plan-dialog procurement-plan-fullscreen"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="plan-title"
        onBlurCapture={(event) => {
          const row = (event.target as HTMLElement).closest("tr");
          if (row && !row.contains(event.relatedTarget as Node | null)) commitPlan();
          else if (!row && hasDraftContent && draftSignature !== savedSignatureRef.current)
            void persistDraft(draftSignature);
        }}
      >
        <header>
          <div>
            <p className="eyebrow">{isDomestic ? "Российская закупка" : "Закупка в Голландии"}</p>
            <h2 id="plan-title">Новый заказ поставщику</h2>
          </div>
          <button
            className="procurement-plan-close"
            onClick={() => void closePlan()}
            aria-label="Закрыть"
          >
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        <div className="procurement-plan-settings">
          <label>
            Поставщик
            <select
              value={supplierId}
              onChange={(event) => {
                if (
                  selected.length &&
                  !window.confirm("Сменить поставщика и очистить текущий список?")
                )
                  return;
                setSupplierId(Number(event.target.value));
                setItems([makeLine()]);
              }}
            >
              {suppliers.map((item) => (
                <option value={item.id} key={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          {!isDomestic && (
            <>
              <label>
                Курс, ₽/{currencySymbol}
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={exchangeRate}
                  onChange={(event) => setExchangeRate(draftNumber(event.target.value))}
                />
              </label>
              <label>
                До Москвы, ₽
                <input
                  type="number"
                  min="0"
                  value={deliveryToMoscowRub}
                  placeholder="0"
                  onChange={(event) => setDeliveryToMoscowRub(draftNumber(event.target.value))}
                />
              </label>
              <label>
                Москва → Рязань, ₽
                <input
                  type="number"
                  min="0"
                  value={deliveryToRyazanRub}
                  placeholder="0"
                  onChange={(event) => setDeliveryToRyazanRub(draftNumber(event.target.value))}
                />
              </label>
            </>
          )}
        </div>
        <div className="procurement-plan-tools">
          <label className="procurement-draft-title">
            Название плана
            <input
              value={title}
              maxLength={120}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="Например, закупка для озеленения"
            />
          </label>
          <button onClick={() => setItems((current) => [...current, makeLine()])}>
            + Новая строка
          </button>
          <button
            aria-expanded={showRecommendations}
            onClick={() => setShowRecommendations(!showRecommendations)}
          >
            Каталог и рекомендации
          </button>
          <span className="procurement-sort-label">Название · цена ↑</span>
          {draftSignature === failedSignatureRef.current && (
            <button
              onClick={() => void persistDraft(draftSignature)}
              disabled={draftSaving || draftLoading}
            >
              Повторить сохранение
            </button>
          )}
          <small>
            {draftLoading
              ? "Загружаем черновик…"
              : draftSaving
                ? "Сохраняем для команды…"
                : remoteDraft && savedDraftSignature === draftSignature
                  ? `Сохранено для команды ${new Date(remoteDraft.updatedAt).toLocaleString("ru-RU")}`
                  : draftSignature === failedSignatureRef.current
                    ? "Не удалось сохранить изменения"
                    : hasDraftContent
                      ? "Сохраним при выходе"
                      : "План появится после заполнения"}
          </small>
          <span>
            <strong>{quantityLabel(selected.length)}</strong> · {totalUnits} шт.
          </span>
        </div>
        <div className="admin-table-wrap procurement-plan-table-wrap">
          <table className="admin-table procurement-plan-table">
            <thead>
              <tr>
                <th>Название у поставщика</th>
                <th>Товар / связь с СБИС</th>
                <th>Артикул</th>
                <th>Горшок · высота</th>
                <th>Упаковки · штук</th>
                <th>Цена, {currencySymbol}</th>
                <th>Себестоимость</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((item) => {
                const units = Number(item.packageCount) * Number(item.unitsPerPackage);
                const selectedIndex = selected.indexOf(item);
                const lineMissing = missingFields(item);
                const estimate =
                  selectedIndex >= 0 && valid && preview?.key === requestBody
                    ? preview.lines[selectedIndex]
                    : undefined;
                const preliminary = partialCost(item);
                return (
                  <tr
                    key={item.id}
                    className={item.rowTouched ? "procurement-row-touched" : undefined}
                    onClick={() => markLineTouched(item.id)}
                  >
                    <td>
                      <input
                        aria-invalid={Boolean(item.rowTouched && !item.category.trim())}
                        value={item.category}
                        onChange={(event) => updateItem(item.id, { category: event.target.value })}
                        aria-label="Название у поставщика"
                        placeholder="Например, Ficus lyrata"
                      />
                    </td>
                    <td>
                      {item.sabyId ? (
                        <strong>{item.sabyName}</strong>
                      ) : (
                        <input
                          aria-invalid={Boolean(item.rowTouched && !item.sabyName.trim())}
                          aria-label="Название новой позиции"
                          value={item.sabyName}
                          onChange={(event) =>
                            updateItem(item.id, { sabyName: event.target.value })
                          }
                          placeholder="Название растения"
                        />
                      )}
                      <div className="procurement-product-meta">
                        {item.recommendedQty > 0 && (
                          <small className="procurement-recommended">
                            Рекомендовано: {item.recommendedQty} шт.
                          </small>
                        )}
                        {item.sabyId && (
                          <small className="procurement-stock-note">
                            Остаток:{" "}
                            {sabyBalances[item.sabyId]
                              ? `${sabyBalances[item.sabyId].balance} шт.`
                              : "—"}
                          </small>
                        )}
                      </div>
                      {!item.potDiameterCm &&
                        !item.heightCm &&
                        (sabyBalances[item.sabyId]?.defaults?.length || 0) > 1 && (
                          <select
                            aria-label={`Размер для ${item.sabyName}`}
                            value=""
                            onChange={(event) => {
                              const value =
                                sabyBalances[item.sabyId].defaults[Number(event.target.value)];
                              if (value)
                                updateItem(item.id, {
                                  category: value.category,
                                  article: value.article || item.article,
                                  expectedUnitPrice: displayPrice(value.unitPrice),
                                  potDiameterCm: value.potDiameterCm?.toString() || "",
                                  heightCm: value.heightCm?.toString() || "",
                                  unitsPerPackage: String(value.unitsPerPackage),
                                });
                            }}
                          >
                            <option value="">Выберите размер</option>
                            {sabyBalances[item.sabyId].defaults.map((value, index) => (
                              <option key={index} value={index}>
                                {[
                                  value.potDiameterCm && `D${value.potDiameterCm}`,
                                  value.heightCm && `${value.heightCm} см`,
                                  value.unitPrice != null &&
                                    `${value.unitPrice} ${selectedSupplier?.defaultCurrency || ""}`,
                                ]
                                  .filter(Boolean)
                                  .join(" · ")}
                              </option>
                            ))}
                          </select>
                        )}
                    </td>
                    <td>
                      <input
                        value={item.article}
                        onChange={(event) => updateItem(item.id, { article: event.target.value })}
                        placeholder="Артикул"
                      />
                    </td>
                    <td>
                      <div className="procurement-size-inputs">
                        <input
                          aria-label="Горшок, см"
                          type="text"
                          inputMode="decimal"
                          value={item.potDiameterCm}
                          placeholder="Диаметр, см"
                          onChange={(event) =>
                            updateItem(item.id, { potDiameterCm: draftNumber(event.target.value) })
                          }
                        />
                        <input
                          aria-label="Высота, см"
                          type="text"
                          inputMode="decimal"
                          value={item.heightCm}
                          placeholder="Высота, см"
                          onChange={(event) =>
                            updateItem(item.id, { heightCm: draftNumber(event.target.value) })
                          }
                        />
                      </div>
                    </td>
                    <td>
                      <div className="procurement-quantity-inputs">
                        <input
                          aria-label="Количество упаковок"
                          type="text"
                          inputMode="numeric"
                          value={item.packageCount}
                          placeholder="Кол-во"
                          onChange={(event) =>
                            updateItem(item.id, { packageCount: draftNumber(event.target.value) })
                          }
                        />
                        <span>×</span>
                        <input
                          aria-label="Штук в упаковке"
                          type="text"
                          inputMode="numeric"
                          value={item.unitsPerPackage}
                          placeholder="Кратность"
                          onChange={(event) =>
                            updateItem(item.id, {
                              unitsPerPackage: draftNumber(event.target.value),
                            })
                          }
                        />
                      </div>
                      <small>
                        {units || 0} {units === 1 ? "штука" : "штук"}
                      </small>
                    </td>
                    <td>
                      <input
                        aria-invalid={Boolean(
                          item.rowTouched && !(decimalNumber(item.expectedUnitPrice) > 0),
                        )}
                        aria-label={isDomestic ? "Цена в рублях" : "Цена в евро"}
                        className="procurement-price-input"
                        type="text"
                        inputMode="decimal"
                        value={item.expectedUnitPrice}
                        placeholder="0,00"
                        onChange={(event) =>
                          updateItem(item.id, {
                            expectedUnitPrice: draftNumber(event.target.value),
                          })
                        }
                      />
                    </td>
                    <td className="procurement-cost-cell">
                      {estimate ? (
                        <>
                          <strong>{money.format(estimate.cost)} / шт.</strong>
                          <small>Розница ≈ {money.format(estimate.retail)}</small>
                        </>
                      ) : preliminary !== null ? (
                        <>
                          <strong>{money.format(preliminary)} / шт.</strong>
                          <small>
                            {valid ? "Считаем…" : isDomestic ? "Оценка" : "Без доставки"}
                          </small>
                        </>
                      ) : (
                        <small>
                          {lineMissing.length
                            ? item.rowTouched
                              ? `Заполните: ${lineMissing.join(", ")}`
                              : "Заполните данные растения"
                            : "Укажите курс"}
                        </small>
                      )}
                    </td>
                    <td>
                      <button
                        className="table-action danger"
                        aria-label={`Удалить ${item.sabyName || "позицию"}`}
                        onClick={() =>
                          setItems((current) => {
                            const remaining = current.filter((line) => line.id !== item.id);
                            return remaining.length ? remaining : [makeLine()];
                          })
                        }
                      >
                        ×
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p className="admin-hint">
          {isDomestic
            ? "Российский план в рублях. Строки вводятся вручную."
            : "Расчёт доставки по объёму и высоте. Плановая цена сохраняется в закупке."}
        </p>
        <div className="dialog-actions procurement-plan-footer">
          <div className="procurement-plan-total">
            <span>Сумма закупки</span>
            <strong>
              {totalPurchaseEUR.toLocaleString("ru-RU", {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              })}{" "}
              {currencySymbol}
            </strong>
            <small>
              {invalidItems.length
                ? `Нужно проверить строк: ${invalidItems.length}`
                : `${totalUnits} шт.`}
            </small>
          </div>
          <button onClick={() => void closePlan()}>Закрыть</button>
          <button
            className="primary"
            disabled={!supplierId || !valid || saving}
            onClick={() => setConfirmPlan(true)}
          >
            {saving ? "Сохраняем…" : "Заказ отправлен поставщику →"}
          </button>
        </div>
        <aside
          hidden={!showRecommendations}
          className="procurement-recommendation-drawer"
          aria-label="Рекомендации к закупке"
        >
          <header>
            <div>
              <p className="eyebrow">Что добавить</p>
              <h3>Рекомендации</h3>
            </div>
            <button onClick={() => setShowRecommendations(false)} aria-label="Закрыть рекомендации">
              ×
            </button>
          </header>
          <label className="procurement-saby-search">
            <span>Добавить товар из СБИС</span>
            <input
              value={sabyQuery}
              onChange={(event) => setSabyQuery(event.target.value)}
              placeholder="Название, код или артикул"
            />
            <small>Каталог СБИС</small>
          </label>
          <div className="procurement-recommendation-list">
            {sabyQuery.trim().length >= 2 ? (
              searchingSaby ? (
                <div className="procurement-plan-empty">
                  <span>Ищем в каталоге СБИС…</span>
                </div>
              ) : sabyResults.length ? (
                sabyResults.map((item) => (
                  <article key={item.sabyId} className="procurement-saby-result">
                    <div>
                      <div className="procurement-saby-title">
                        <strong>{item.name}</strong>
                        {item.supplierLinked && <span>Инвойсы Голландии</span>}
                      </div>
                      <b className="procurement-saby-code">
                        {item.code || item.article || item.sabyId}
                      </b>
                      <div className="procurement-recommendation-metrics">
                        <span>Остаток {item.balance}</span>
                      </div>
                      <small className="procurement-channel-sales">
                        Продажи и рекомендация подтянутся после добавления товара.
                      </small>
                    </div>
                    <button className="admin-primary" onClick={() => addSabyProduct(item)}>
                      + Добавить
                    </button>
                  </article>
                ))
              ) : (
                <div className="procurement-plan-empty">
                  <strong>Товар не найден</strong>
                  <span>Проверьте название или обновите каталог СБИС.</span>
                </div>
              )
            ) : availableRecommendations.length ? (
              availableRecommendations.map((item) => (
                <article key={item.sabyId}>
                  <div>
                    <strong>{item.name}</strong>
                    <div className="procurement-recommendation-metrics">
                      <span>Рекомендовано {item.suggestedQty}</span>
                      <span>Остаток {item.balance}</span>
                      <span>Продано {item.totalSales}</span>
                    </div>
                  </div>
                  <button className="admin-primary" onClick={() => addRecommendation(item)}>
                    + Добавить
                  </button>
                </article>
              ))
            ) : null}
          </div>
        </aside>
      </div>
      {confirmPlan && (
        <ConfirmDialog
          title="Зафиксировать заказ поставщику?"
          text={
            <>
              <p>
                {title} · {selectedSupplier?.name}
              </p>
              <p>
                {quantityLabel(selected.length)}, {totalUnits} шт.,{" "}
                {totalPurchaseEUR.toLocaleString("ru-RU")} {currencySymbol}.
              </p>
              <p>
                Закупка перейдёт в статус «Заказано». Состав и плановая цена сохранятся. После этого
                загрузите инвойс для сверки.
              </p>
            </>
          }
          confirmLabel="Зафиксировать заказ"
          busy={saving}
          onCancel={() => setConfirmPlan(false)}
          onConfirm={() => void save()}
        />
      )}
    </>
  );
}

export function ProcurementOrderDetailDialog({
  orderId,
  settings,
  canDelete,
  onClose,
  onSaved,
  onError: externalError,
}: {
  orderId: number;
  settings?: ProcurementSettings;
  canDelete: boolean;
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(onClose);
  const [lineQuery, setLineQuery] = useState("");
  const [lineWindow, setLineWindow] = useState({ scope: "", count: 5 });
  const [batchWindows, setBatchWindows] = useState<Record<number, number>>({});
  const [lineFilter, setLineFilter] = useState("all");
  const lineTabsID = useId();
  const lineTabsRef = useRef<HTMLDivElement>(null);
  const [detail, setDetail] = useState<ProcurementOrderDetail | null>(null);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [pairingLineID, setPairingLineID] = useState<number | null>(null);
  const [matchingLine, setMatchingLine] = useState<ProcurementOrderLine | null>(null);
  const [pricingLine, setPricingLine] = useState<ProcurementOrderLine | null>(null);
  const [receivingLine, setReceivingLine] = useState<ProcurementOrderLine | null>(null);
  const [lineEditor, setLineEditor] = useState<{
    line: ProcurementOrderLine;
    kind: "dimensions" | "comparison" | "exclusion";
  } | null>(null);
  const lineTableRef = useRef<HTMLDivElement>(null);
  const [costs, setCosts] = useState({
    exchangeRate: 0,
    deliveryToMoscowRub: 0,
    deliveryToRyazanRub: 0,
    additionalCostsRub: 0,
  });
  const load = useCallback(
    (preserveCosts = false) =>
      api<ProcurementOrderDetail>(`/api/v1/admin/procurement/orders/${orderId}`)
        .then((item) => {
          setDetail(normalizeProcurementOrderDetail(item));
          if (!preserveCosts)
            setCosts({
              exchangeRate: item.costs.exchangeRate || (item.order.currency === "RUB" ? 1 : 0),
              deliveryToMoscowRub:
                item.costs.deliveryToMoscowRub ||
                item.costs.trolleyCostRub * item.validation.trolleyCount,
              deliveryToRyazanRub: item.costs.deliveryToRyazanRub,
              additionalCostsRub: item.costs.additionalCostsRub || 0,
            });
        })
        .catch((error) => onError((error as Error).message)),
    [orderId, onError],
  );
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    if (!detail?.batches.some((batch) => batch.status === "processing")) return;
    const timer = window.setTimeout(() => void load(), 4000);
    return () => window.clearTimeout(timer);
  }, [detail, load]);
  const handOver = async (line: ProcurementOrderLine) => {
    if (
      !window.confirm(
        `Подтвердить, что «${line.rawName}», ${line.invoicedQuantity} шт., переданы клиенту?`,
      )
    )
      return;
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/order-lines/${line.id}`, {
        method: "PATCH",
        body: JSON.stringify({ customerHandedOver: true }),
      });
      await load();
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const calculate = async () => {
    setSaving(true);
    try {
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/orders/${orderId}/calculate`,
        { method: "POST", body: JSON.stringify(costs) },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const downloadSabyPrices = async () => {
    if (!detail) return;
    const bySaby = new Map<string, { sabyId: string; priceRub: number; name: string }>();
    for (const line of detail.lines) {
      if (
        line.canonicalVariantId ||
        line.matchStatus !== "confirmed" ||
        line.invoiceExcluded ||
        !line.sabyId ||
        !line.proposedRetailRub ||
        line.receivingMode !== "catalog"
      )
        continue;
      const previous = bySaby.get(line.sabyId);
      if (!previous || previous.priceRub < line.proposedRetailRub) {
        bySaby.set(line.sabyId, {
          sabyId: line.sabyId,
          priceRub: line.proposedRetailRub,
          name: line.sabyName || line.rawName,
        });
      }
    }
    const firstPrices = [...bySaby.values()];
    if (
      firstPrices.length &&
      !window.confirm(
        "Подтвердить первые цены новых товаров для файла СБИС?\n\n" +
          firstPrices.map((price) => `${price.name}: ${money.format(price.priceRub)}`).join("\n") +
          "\n\nБудет скачан файл. Импорт в СБИС выполняется отдельно.",
      )
    )
      return;
    const response = await fetch(`/api/v1/admin/procurement/orders/${orderId}/saby-prices.xlsx`, {
      credentials: "same-origin",
      cache: "no-store",
      ...(firstPrices.length
        ? {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              firstPrices: firstPrices.map(({ sabyId, priceRub }) => ({ sabyId, priceRub })),
            }),
          }
        : {}),
    });
    if (!response.ok) {
      const result = (await response.json().catch(() => ({}))) as { error?: string };
      throw new Error(result.error || "Не удалось сформировать XLSX для Saby");
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download =
      response.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] ||
      `saby-prices-${orderId}.xlsx`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };
  const downloadReceivingPDF = async () => {
    const response = await fetch(`/api/v1/admin/procurement/orders/${orderId}/receiving.pdf`, {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (!response.ok) {
      const result = (await response.json().catch(() => ({}))) as { error?: string };
      throw new Error(result.error || "Не удалось сформировать PDF приёмки");
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download =
      response.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] ||
      `receiving-${orderId}.pdf`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };
  const prepare = async (kind: "receipt" | "prices", channel?: string) => {
    setSaving(true);
    try {
      if (channel === "saby_price") await downloadSabyPrices();
      else
        await api(`/api/v1/admin/procurement/orders/${orderId}/batches`, {
          method: "POST",
          body: JSON.stringify({ kind, channels: channel ? [channel] : [] }),
        });
      await load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const approve = async (batch: ProcurementActionBatch) => {
    if (
      !window.confirm(
        batch.kind === "prices"
          ? "Подтвердить этот список и подготовить изменения только в выбранных каналах?"
          : "Подтвердить состав поступления?",
      )
    )
      return;
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/batches/${batch.id}/approve`, { method: "POST" });
      await load();
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const recheckReceipt = async (batch: ProcurementActionBatch) => {
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/batches/${batch.id}/recheck-receipt`, {
        method: "POST",
      });
      clearError();
      await load(true);
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const retry = async (batch: ProcurementActionBatch) => {
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/batches/${batch.id}/retry`, { method: "POST" });
      await load();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const editLine = (line: ProcurementOrderLine) => setLineEditor({ line, kind: "dimensions" });
  const acceptMismatch = (line: ProcurementOrderLine) =>
    setLineEditor({ line, kind: "comparison" });
  const setInvoiceExcluded = async (line: ProcurementOrderLine, excluded: boolean) => {
    if (excluded) {
      setLineEditor({ line, kind: "exclusion" });
      return;
    }
    setSaving(true);
    try {
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/order-lines/${line.id}`,
        { method: "PATCH", body: JSON.stringify({ invoiceExcluded: false, exclusionReason: "" }) },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const acceptAllChanged = async () => {
    if (!detail) return;
    const lines = detail.lines.filter(
      (line) =>
        line.reconciliationStatus === "changed" &&
        line.comparisonMismatch &&
        !line.comparisonAccepted &&
        !line.invoiceExcluded,
    );
    if (
      !lines.length ||
      !window.confirm(
        `Принять все изменения по цене/количеству: ${lines.length}? Несопоставленные и отсутствующие позиции сюда не входят.`,
      )
    )
      return;
    setSaving(true);
    try {
      await Promise.all(
        lines.map((line) =>
          api(`/api/v1/admin/procurement/order-lines/${line.id}`, {
            method: "PATCH",
            body: JSON.stringify({
              acceptComparison: true,
              comparisonNote: "Массово принято в сверке закупки",
            }),
          }),
        ),
      );
      await load();
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const matchInvoiceLines = async (
    plannedLine: ProcurementOrderLine,
    invoiceLines: ProcurementOrderLine[],
  ) => {
    setSaving(true);
    try {
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/order-lines/${plannedLine.id}`,
        {
          method: "PATCH",
          body: JSON.stringify({ invoiceLineIds: invoiceLines.map((line) => line.id) }),
        },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      setPairingLineID(null);
      onSaved();
    } finally {
      setSaving(false);
    }
  };
  const matchInvoiceLine = async (
    plannedLine: ProcurementOrderLine,
    invoiceLine: ProcurementOrderLine,
  ) => {
    setSaving(true);
    try {
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/order-lines/${plannedLine.id}`,
        { method: "PATCH", body: JSON.stringify({ invoiceLineId: invoiceLine.id }) },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      setPairingLineID(null);
      onSaved();
    } finally {
      setSaving(false);
    }
  };
  const acceptAddedLine = async (line: ProcurementOrderLine) => {
    if (
      !window.confirm(
        `Добавить «${line.invoiceRawName || line.rawName}» в закупку как новую позицию поставщика?`,
      )
    )
      return;
    setSaving(true);
    try {
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/order-lines/${line.id}`,
        {
          method: "PATCH",
          body: JSON.stringify({
            acceptComparison: true,
            comparisonNote: "Подтверждено как новая позиция из инвойса",
          }),
        },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const setStatus = async (status: "received" | "cancelled" | "review", force = false) => {
    const restoring = status === "review" && detail?.order.status === "cancelled";
    const confirmation = force
      ? "Принудительно завершить закупку? CRM не будет ждать подтверждения проведения из СБИС. Используйте это только если поступление уже действительно проведено."
      : status === "received"
        ? "Завершить приёмку и выполнить клиентские заявки? Поступление СБИС и передача под заказ должны быть подтверждены."
        : status === "cancelled"
          ? "Отменить закупку? Заявки вернутся в открытые."
          : restoring
            ? "Вернуть отменённую закупку в работу? Она откроется на этапе проверки, а старые действия останутся в истории."
            : "Вернуть закупку на проверку? Активные действия будут отменены и останутся в истории.";
    if (!window.confirm(confirmation)) return;
    setSaving(true);
    try {
      const note = force
        ? "Закупка завершена вручную оператором без автоматического подтверждения СБИС"
        : "";
      const item = await api<ProcurementOrderDetail>(
        `/api/v1/admin/procurement/orders/${orderId}/status`,
        { method: "PATCH", body: JSON.stringify({ status, force, note }) },
      );
      setDetail(normalizeProcurementOrderDetail(item));
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const remove = async () => {
    if (
      !window.confirm(
        "Удалить отменённую закупку вместе с загруженным PDF? После этого тот же файл можно будет загрузить заново.",
      )
    )
      return;
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/orders/${orderId}`, { method: "DELETE" });
      onSaved();
      onClose();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const uploadInvoice = async (file: File | null) => {
    if (!file || !detail) return;
    setUploading(true);
    try {
      await validateProcurementPDF(file);
      const body = new FormData();
      body.append("supplierId", String(detail.order.supplierId));
      body.append("orderId", String(orderId));
      body.append("file", file);
      const result = await uploadProcurementPDF<{ duplicate: boolean; order: ProcurementOrder }>(
        body,
      );
      if (result.duplicate)
        throw new Error(
          `Этот PDF уже загружен в закупку ${result.order.orderNumber || `№${result.order.id}`}.`,
        );
      await load();
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setUploading(false);
    }
  };
  const closed = detail ? ["received", "cancelled"].includes(detail.order.status) : false;
  const needsSaby = (line: ProcurementOrderLine) =>
    !line.invoiceExcluded && !["confirmed", "ignored", "deferred"].includes(line.matchStatus);
  const needsDimensionReview = (line: ProcurementOrderLine) =>
    detail?.order.currency !== "RUB" &&
    !line.invoiceExcluded &&
    ["confirmed", "deferred"].includes(line.matchStatus) &&
    Boolean(line.dimensionWarning);
  const dimensionReviewLines = detail?.lines.filter(needsDimensionReview) || [];
  const dimensionBlockers = new Set(
    dimensionReviewLines.map((line) => `${line.rawName}: ${line.dimensionWarning}`),
  );
  const otherBlockers = (detail?.validation.blockers || []).filter(
    (blocker) => !dimensionBlockers.has(blocker),
  );
  const needsAttention = (line: ProcurementOrderLine) =>
    !line.invoiceExcluded &&
    line.matchStatus !== "ignored" &&
    (needsDimensionReview(line) ||
      needsSaby(line) ||
      (line.comparisonMismatch && !line.comparisonAccepted) ||
      !line.heightCm ||
      (detail?.order.currency !== "RUB" && !line.potDiameterCm));
  const matchesSearch = (line: ProcurementOrderLine) =>
    `${line.rawName} ${line.sabyName} ${line.supplierArticle} ${line.invoiceRawName}`
      .toLocaleLowerCase("ru")
      .includes(lineQuery.toLocaleLowerCase("ru"));
  const pendingInvoiceLines =
    detail?.lines.filter(
      (line) =>
        line.reconciliationStatus === "added" && !line.comparisonAccepted && !line.invoiceExcluded,
    ) || [];
  const purchaseLines = detail?.lines.filter((line) => !pendingInvoiceLines.includes(line)) || [];
  const matchesFilter = (line: ProcurementOrderLine, filter: string) =>
    filter === "all" ||
    (filter === "attention"
      ? needsAttention(line)
      : filter === "dimensions"
        ? needsDimensionReview(line)
        : filter === "unmatched"
          ? needsSaby(line)
          : filter.startsWith("receiving_")
            ? !line.invoiceExcluded &&
              line.receivingMode === filter.slice(10) &&
              (line.receivingMode !== "customer_handoff" || !line.customerHandedOver) &&
              (line.receivingMode !== "assortment" ||
                (line.receivingAllocations || []).reduce((sum, part) => sum + part.quantity, 0) !==
                  line.invoicedQuantity)
            : !line.invoiceExcluded && line.reconciliationStatus === filter);
  const linesForFilter = (filter: string) =>
    filter === "invoice_only"
      ? pendingInvoiceLines
      : filter === "all"
        ? [...purchaseLines, ...pendingInvoiceLines]
        : purchaseLines.filter((line) => matchesFilter(line, filter));
  const lineTabs = [
    { value: "all", label: "Все" },
    { value: "receiving_assortment", label: "Разобрать миксы" },
    { value: "invoice_only", label: "Только в инвойсе" },
    { value: "missing", label: "Только в закупке" },
    { value: "receiving_customer_handoff", label: "Передать клиенту" },
    ...[
      { value: "dimensions", label: "Проверить размеры" },
      { value: "attention", label: "Требуют внимания" },
      { value: "unmatched", label: "Нет товара СБИС" },
      { value: "receiving_awaiting_catalog", label: "Выбрать карточки" },
      { value: "changed", label: "Изменения поставщика" },
    ].filter((tab) => linesForFilter(tab.value).length > 0 || lineFilter === tab.value),
  ].map((tab) => ({ ...tab, count: linesForFilter(tab.value).length }));
  const visibleLines =
    lineFilter === "invoice_only"
      ? []
      : purchaseLines.filter((line) => matchesFilter(line, lineFilter) && matchesSearch(line));
  const visibleInvoiceLines = ["all", "invoice_only"].includes(lineFilter)
    ? pendingInvoiceLines.filter(matchesSearch)
    : [];
  const shownLines = visibleLines.length + visibleInvoiceLines.length;
  const lineScope = JSON.stringify([orderId, lineFilter, lineQuery]);
  const lineLimit = lineWindow.scope === lineScope ? lineWindow.count : 5;
  const pagedLines = visibleLines.slice(0, lineLimit);
  const pagedInvoiceLines = visibleInvoiceLines.slice(
    0,
    Math.max(0, lineLimit - pagedLines.length),
  );
  const displayedLines = pagedLines.length + pagedInvoiceLines.length;
  const selectLineTab = (value: string) => {
    setLineQuery("");
    setLineFilter(value);
    setLineWindow({ scope: "", count: 5 });
  };
  const showDimensionReview = () => {
    selectLineTab("dimensions");
    window.requestAnimationFrame(() => {
      const tab = lineTabsRef.current?.querySelector<HTMLElement>(
        '[role="tab"][aria-selected="true"]',
      );
      tab?.focus();
      tab?.scrollIntoView({ block: "nearest" });
    });
  };
  const changedLines =
    detail?.lines.filter(
      (line) =>
        line.reconciliationStatus === "changed" &&
        line.comparisonMismatch &&
        !line.comparisonAccepted &&
        !line.invoiceExcluded,
    ) || [];
  const pairingLine =
    pairingLineID == null ? null : detail?.lines.find((line) => line.id === pairingLineID) || null;
  const invoicePairCandidates =
    detail?.lines.filter(
      (line) =>
        line.id !== pairingLineID &&
        (!line.receivingMode || line.receivingMode === "catalog") &&
        !line.invoiceExcluded &&
        line.reconciliationStatus !== "superseded" &&
        line.invoicedQuantity != null &&
        ((line.reconciliationStatus === "added" && !line.comparisonAccepted) ||
          (line.orderedQuantity > 0 && ["matched", "changed"].includes(line.reconciliationStatus))),
    ) || [];
  const renderBatch = (batch: ProcurementActionBatch) => (
    <section className="procurement-batch" key={batch.id}>
      <div className="admin-block-heading">
        <div>
          <p className="eyebrow">{batch.kind === "prices" ? "Цены" : "Поступление"}</p>
          <h3>
            {batch.kind === "prices"
              ? `Изменение цены · ${channelLabel(batch.items[0]?.channel || "site")}`
              : "Поступление СБИС"}
          </h3>
        </div>
        <span className="admin-pill">
          {batch.kind === "receipt" && batch.status === "draft"
            ? "Подготовлено"
            : batchStatusLabel(batch.status)}
        </span>
      </div>
      <div className="procurement-batch-list">
        {batchDisplayRows(batch)
          .slice(0, batchWindows[batch.id] || 5)
          .map((row) => (
            <article key={row.key}>
              <div className="procurement-batch-product">
                <strong>{row.productName}</strong>
                {row.productCode && <small>Главный код: {row.productCode}</small>}
              </div>
              <div>
                <small>Канал</small>
                <span>{channelLabel(row.item.channel)}</span>
                {(row.item.displayArticle || row.item.externalArticle) &&
                  !row.item.previewLines?.length && (
                    <small>{row.item.displayArticle || row.item.externalArticle}</small>
                  )}
              </div>
              <div>
                <small>{row.quantity == null ? "Было → станет" : "Остаток: было → станет"}</small>
                <span>
                  {row.quantity == null
                    ? `${row.oldValue == null ? "Не получено" : money.format(row.oldValue)} → ${money.format(row.newValue)}`
                    : `${row.oldBalance ?? 0} → ${row.newBalance ?? row.quantity}`}
                </span>
                {row.quantity != null && <small>Поступление: +{row.quantity}</small>}
                {row.item.compareAtValue && row.item.compareAtValue > row.newValue ? (
                  <small>до скидки {money.format(row.item.compareAtValue)}</small>
                ) : null}
              </div>
              {row.showStatus && (
                <div className="procurement-action-status">{actionStatus(row.item)}</div>
              )}
            </article>
          ))}
      </div>
      {batchDisplayRows(batch).length > (batchWindows[batch.id] || 5) && (
        <div className="dialog-actions">
          <span>
            Показано {Math.min(batchWindows[batch.id] || 5, batchDisplayRows(batch).length)} из{" "}
            {batchDisplayRows(batch).length}
          </span>
          <button
            aria-label={`Показать ещё строки пакета ${batch.id}`}
            onClick={() =>
              setBatchWindows((current) => ({
                ...current,
                [batch.id]: (current[batch.id] || 5) + 5,
              }))
            }
          >
            Показать ещё
          </button>
        </div>
      )}
      {batch.status === "draft" && (
        <div className="dialog-actions">
          <button
            className="primary"
            disabled={(closed && !batch.unified) || saving || !batch.items.length}
            onClick={() => void approve(batch)}
          >
            {batch.kind === "receipt"
              ? "Создать поступление в СБИС"
              : `Подтвердить ${batchDisplayRows(batch).length} строк`}
          </button>
        </div>
      )}
      {batch.kind === "receipt" &&
        !closed &&
        batch.items.some(
          (item) =>
            item.channel === "saby_receipt" &&
            /^\d+$/.test(item.externalOperationId || "") &&
            item.externalUrl &&
            item.status !== "completed",
        ) && (
          <div className="dialog-actions">
            <button
              disabled={
                saving ||
                detail?.batches.some(
                  (candidate) => candidate.kind === "receipt" && candidate.status === "processing",
                )
              }
              onClick={() => void recheckReceipt(batch)}
            >
              Проверить проведение в СБИС
            </button>
            <small>Проверяется существующий документ. Новое поступление не создаётся.</small>
          </div>
        )}
      {batch.items.some((item) => item.channel === "saby_receipt" && item.status === "failed") && (
        <p className="procurement-warning">
          Проверка поступления СБИС завершилась ошибкой. Сверьте документ и повторно проверьте его
          проведение. Повторная отправка товаров заблокирована.
        </p>
      )}
      {!batch.items.some((item) => item.channel === "saby_receipt" && item.status === "failed") &&
        batch.items.some((item) => item.status === "failed") && (
          <div className="dialog-actions">
            <button disabled={closed || saving} onClick={() => void retry(batch)}>
              Повторить ошибки
            </button>
          </div>
        )}
    </section>
  );
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        className="admin-dialog procurement-order-dialog"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="order-detail-title"
      >
        <header>
          <div>
            <p className="eyebrow">Закупка</p>
            <h2 id="order-detail-title">
              {detail?.order.title || detail?.order.orderNumber || `№${orderId}`}
            </h2>
          </div>
          <button onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        {!detail ? (
          <div className="procurement-zero">Загружаем строки…</div>
        ) : (
          <div className="procurement-order-body">
            <div className="procurement-order-summary">
              <strong>{detail.order.supplierName}</strong>
              <span>
                {
                  (
                    {
                      ordered: "Заказано",
                      received: "Завершено",
                      cancelled: "Отменено",
                      review: "Сверка",
                      ready_to_receive: "К поступлению",
                      invoice_received: "Инвойс загружен",
                      draft: "Черновик",
                    } as Record<string, string>
                  )[detail.order.status]
                }
              </span>
              <span>
                {quantityLabel(detail.order.lines)} · {detail.order.units} шт.
              </span>
              <strong>
                {detail.order.total.toLocaleString("ru-RU", {
                  style: "currency",
                  currency: detail.order.currency,
                })}
              </strong>
            </div>
            {detail.batches.some((batch) =>
              batch.items.some((item) => item.status === "failed"),
            ) && (
              <div className="procurement-checklist blocked" role="alert">
                <strong>Есть ошибка отправки</strong>
                <p>
                  Проверьте журнал действий ниже и фактическое поступление в СБИС. Статус закупки не
                  подтверждает проведение документа.
                </p>
              </div>
            )}
            <details
              className="procurement-cost-details"
              open={detail.order.status !== "ready_to_receive" && !closed}
            >
              <summary>
                Расчёт и доставка{" "}
                <span>
                  {detail.order.currency !== "RUB"
                    ? `Курс ${costs.exchangeRate} · доставка ${money.format(costs.deliveryToMoscowRub + costs.deliveryToRyazanRub)}`
                    : "Закупка в рублях"}
                </span>
              </summary>
              <fieldset
                className="procurement-readonly-fields"
                disabled={closed || saving || uploading}
              >
                <div
                  className={`procurement-costs ${detail.order.currency === "RUB" ? "procurement-costs-domestic" : ""}`}
                >
                  {detail.order.currency !== "RUB" && (
                    <>
                      <label>
                        Курс оплаты
                        <input
                          type="number"
                          step="0.01"
                          value={costs.exchangeRate}
                          onChange={(event) =>
                            setCosts({ ...costs, exchangeRate: Number(event.target.value) })
                          }
                        />
                      </label>
                      <label>
                        До Москвы, ₽
                        <input
                          type="number"
                          step="0.01"
                          value={costs.deliveryToMoscowRub}
                          onChange={(event) =>
                            setCosts({ ...costs, deliveryToMoscowRub: Number(event.target.value) })
                          }
                        />
                      </label>
                      <label>
                        Москва → Рязань, ₽
                        <input
                          type="number"
                          value={costs.deliveryToRyazanRub}
                          onChange={(event) =>
                            setCosts({ ...costs, deliveryToRyazanRub: Number(event.target.value) })
                          }
                        />
                      </label>
                    </>
                  )}
                  {detail.order.currency === "RUB" && (
                    <label>
                      Доставка до склада, ₽
                      <input
                        type="number"
                        min="0"
                        step="0.01"
                        value={costs.deliveryToMoscowRub}
                        onChange={(event) =>
                          setCosts({ ...costs, deliveryToMoscowRub: Number(event.target.value) })
                        }
                      />
                    </label>
                  )}
                  <label>
                    Прочие расходы закупки, ₽
                    <input
                      type="number"
                      min="0"
                      step="0.01"
                      value={costs.additionalCostsRub}
                      onChange={(event) =>
                        setCosts({ ...costs, additionalCostsRub: Number(event.target.value) })
                      }
                    />
                  </label>
                  <label className="secondary-button procurement-inline-upload">
                    {uploading
                      ? "Разбираем PDF…"
                      : detail.order.status === "ordered"
                        ? "Загрузить инвойс"
                        : "Заменить инвойс"}
                    <input
                      type="file"
                      accept="application/pdf,.pdf"
                      disabled={uploading}
                      onChange={(event) => void uploadInvoice(event.target.files?.[0] || null)}
                    />
                  </label>
                  <button
                    className="admin-primary"
                    aria-describedby={
                      !detail.validation.canCalculate
                        ? `${lineTabsID}-calculation-blockers`
                        : costs.exchangeRate <= 0
                          ? `${lineTabsID}-exchange-error`
                          : undefined
                    }
                    disabled={saving || !detail.validation.canCalculate || costs.exchangeRate <= 0}
                    onClick={calculate}
                  >
                    {saving ? "Считаем…" : "Рассчитать"}
                  </button>
                </div>
                {!closed && costs.exchangeRate <= 0 && (
                  <p id={`${lineTabsID}-exchange-error`} className="procurement-warning">
                    Для расчёта укажите курс оплаты больше нуля.
                  </p>
                )}
                <p>
                  Прочие расходы: банковская комиссия за закупку, пошлины, страхование и другие
                  затраты до склада. Невозвратная тара и аренда из инвойса добавляются
                  автоматически; повторно сюда их не включайте.
                </p>
                {Boolean(detail.costs.supplierChargesRub || detail.costs.returnableDepositRub) && (
                  <p>
                    Тара и аренда поставщика: {money.format(detail.costs.supplierChargesRub || 0)} —
                    в себестоимости. Возвратный залог:{" "}
                    {money.format(detail.costs.returnableDepositRub || 0)} — отдельно от расхода.
                  </p>
                )}
              </fieldset>
            </details>
            {detail.unifiedPreview && (
              <Suspense fallback={<p role="status">Загружаем расчёт цен…</p>}>
                <UnifiedPricingPreview
                  preview={detail.unifiedPreview}
                  saving={saving}
                  status={detail.order.status}
                  onPrepare={prepare}
                />
              </Suspense>
            )}
            {detail.order.status === "ready_to_receive" &&
              (Math.abs(
                detail.validation.allocatedTrolleyRub - detail.validation.expectedTrolleyRub,
              ) > 1 ||
                Math.abs(
                  detail.validation.allocatedRyazanRub - detail.validation.expectedRyazanRub,
                ) > 1) && (
                <section className="procurement-checklist blocked" role="alert">
                  <strong>Доставка распределена не полностью</strong>
                  <p>
                    До Москвы: {money.format(detail.validation.allocatedTrolleyRub)} из{" "}
                    {money.format(detail.validation.expectedTrolleyRub)}. До Рязани:{" "}
                    {money.format(detail.validation.allocatedRyazanRub)} из{" "}
                    {money.format(detail.validation.expectedRyazanRub)}. Пересчитайте закупку.
                  </p>
                </section>
              )}
            {!closed && Boolean(detail.validation.blockers?.length) && (
              <section
                id={`${lineTabsID}-calculation-blockers`}
                className="procurement-checklist blocked"
                role="status"
              >
                <strong>Перед расчётом нужно проверить</strong>
                {dimensionReviewLines.length > 0 && (
                  <>
                    <p>
                      Размеры в {dimensionReviewLines.length} поз. отличаются от инвойса или
                      выбранной карточки. От них зависит распределение доставки.
                    </p>
                    <button onClick={showDimensionReview}>
                      Проверить размеры ({dimensionReviewLines.length})
                    </button>
                  </>
                )}
                {otherBlockers.length > 0 && (
                  <ul>
                    {otherBlockers.map((blocker) => (
                      <li key={blocker}>
                        {blocker === "Не загружен инвойс или счёт" ? "Загрузите инвойс" : blocker}
                      </li>
                    ))}
                  </ul>
                )}
                {detail.validation.unmatched > 0 && (
                  <button
                    onClick={() => {
                      setLineQuery("");
                      setLineFilter("unmatched");
                      lineTableRef.current?.scrollIntoView({ block: "start" });
                    }}
                  >
                    Показать строки без товара СБИС ({detail.validation.unmatched})
                  </button>
                )}
              </section>
            )}
            {changedLines.length > 0 && (
              <div className="procurement-batch-buttons">
                <button disabled={closed || saving} onClick={() => void acceptAllChanged()}>
                  Принять все изменения ({changedLines.length})
                </button>
                <small>
                  Только изменения количества/цены. «Нет в инвойсе» и новые строки нужно разобрать
                  отдельно.
                </small>
              </div>
            )}
            <div
              ref={lineTabsRef}
              className="procurement-line-tabs"
              role="tablist"
              aria-label="Строки закупки"
            >
              {lineTabs.map((tab, index) => (
                <button
                  key={tab.value}
                  id={`${lineTabsID}-${tab.value}`}
                  role="tab"
                  aria-selected={lineFilter === tab.value}
                  aria-controls={`${lineTabsID}-panel`}
                  tabIndex={lineFilter === tab.value ? 0 : -1}
                  onClick={() => selectLineTab(tab.value)}
                  onKeyDown={(event) => {
                    const next =
                      event.key === "ArrowRight"
                        ? (index + 1) % lineTabs.length
                        : event.key === "ArrowLeft"
                          ? (index + lineTabs.length - 1) % lineTabs.length
                          : event.key === "Home"
                            ? 0
                            : event.key === "End"
                              ? lineTabs.length - 1
                              : null;
                    if (next == null) return;
                    event.preventDefault();
                    selectLineTab(lineTabs[next].value);
                    event.currentTarget.parentElement
                      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
                      [next]?.focus();
                  }}
                >
                  {tab.label}
                  <span>{tab.count}</span>
                </button>
              ))}
            </div>
            <div
              id={`${lineTabsID}-panel`}
              role="tabpanel"
              aria-labelledby={`${lineTabsID}-${lineFilter}`}
            >
              <div className="procurement-filter-bar">
                <input
                  aria-label="Поиск строки закупки"
                  value={lineQuery}
                  onChange={(event) => {
                    setLineQuery(event.target.value);
                    setLineWindow({ scope: "", count: 5 });
                  }}
                  placeholder="Название или артикул"
                />
                <span role="status">
                  Показано {displayedLines} из {shownLines} строк
                  {lineQuery && ` · всего в группе ${linesForFilter(lineFilter).length}`}
                </span>
              </div>
              {shownLines === 0 && (
                <p className="procurement-line-empty">
                  {lineQuery ? "По запросу ничего не найдено." : "В этой группе пока нет позиций."}
                </p>
              )}
              {pagedLines.length > 0 && (
                <div className="admin-table-wrap" ref={lineTableRef}>
                  <table className="admin-table procurement-lines procurement-order-lines">
                    <thead>
                      <tr>
                        <th>План</th>
                        <th>Инвойс</th>
                        <th>Сверка</th>
                        <th>Размер / телега</th>
                        <th>Упаковки</th>
                        <th>Количество</th>
                        <th>
                          Цена закупки<small>план / инвойс</small>
                        </th>
                        <th>Себестоимость</th>
                        <th>
                          Цена продажи<small>сайт сейчас / предложение</small>
                        </th>
                        <th></th>
                      </tr>
                    </thead>
                    <tbody>
                      {pagedLines.map((line) => (
                        <tr
                          key={line.id}
                          className={
                            needsSaby(line) || (line.comparisonMismatch && !line.comparisonAccepted)
                              ? "procurement-row-mismatch"
                              : ""
                          }
                        >
                          <td>
                            <strong>
                              {line.receivingMode && line.receivingMode !== "catalog"
                                ? line.rawName
                                : line.sabyName || line.rawName}
                            </strong>
                            <small>
                              {[line.supplierCategory, line.supplierArticle]
                                .filter(Boolean)
                                .join(" · ")}
                            </small>
                          </td>
                          <td>
                            <strong>
                              {line.invoiceRawName ||
                                (line.reconciliationStatus === "missing"
                                  ? "Позиция отсутствует"
                                  : "—")}
                            </strong>
                            {line.invoiceSupplierArticle && (
                              <small>{line.invoiceSupplierArticle}</small>
                            )}
                            {(line.invoiceComponents?.length || 0) > 1 && (
                              <details>
                                <summary>
                                  {line.invoiceComponents!.length} строки инвойса ·{" "}
                                  {line.invoicedQuantity} шт.
                                </summary>
                                {line.invoiceComponents!.map((part, index) => (
                                  <p key={index}>
                                    {part.rawName}: {part.quantity} шт. ×{" "}
                                    {part.unitPrice.toLocaleString("ru-RU")} {detail.order.currency}
                                    <small>
                                      Страница {part.sourcePage || "—"}, строка{" "}
                                      {part.sourceLine || "—"}
                                    </small>
                                  </p>
                                ))}
                              </details>
                            )}
                          </td>
                          <td>
                            <span
                              className={`procurement-reconciliation procurement-reconciliation-${line.reconciliationStatus}`}
                            >
                              {reconciliationLabel(line.reconciliationStatus)}
                            </span>
                            {(needsSaby(line) ||
                              (line.receivingMode && line.receivingMode !== "catalog")) && (
                              <small className={needsSaby(line) ? "procurement-warning" : ""}>
                                {line.receivingMode && line.receivingMode !== "catalog"
                                  ? receivingModeLabel(line.receivingMode)
                                  : "Выберите товар СБИС"}
                              </small>
                            )}
                            {line.receivingMode === "assortment" && (
                              <small>
                                Распределено{" "}
                                {(line.receivingAllocations || []).reduce(
                                  (sum, part) => sum + part.quantity,
                                  0,
                                )}{" "}
                                / {line.invoicedQuantity} шт.
                                {line.receivingAllocations?.map((part) => (
                                  <span key={part.sabyId}>
                                    {part.name}: {part.quantity} шт.
                                  </span>
                                ))}
                              </small>
                            )}
                            {line.receivingMode === "customer_handoff" && (
                              <small>
                                {line.customerHandedOver
                                  ? "Передано клиенту"
                                  : "Ожидает передачи клиенту"}
                              </small>
                            )}
                            {line.invoiceExclusionReason && (
                              <small>{line.invoiceExclusionReason}</small>
                            )}
                            {line.comparisonAccepted && (
                              <small className="procurement-ok">Расхождение принято</small>
                            )}
                          </td>
                          <td>
                            {line.loadUnit === "shelf" ? "Полка" : line.loadUnit || "—"}
                            <small>
                              {[
                                line.potDiameterCm && `D${line.potDiameterCm}`,
                                line.heightCm && `${line.heightCm} см`,
                              ]
                                .filter(Boolean)
                                .join(" · ") || "Размер не заполнен"}
                            </small>
                            {line.dimensionWarning && (
                              <small className="procurement-warning">
                                Размеры требуют подтверждения
                              </small>
                            )}
                          </td>
                          <td>
                            {line.packageCount && line.unitsPerPackage
                              ? `${line.packageCount} × ${line.unitsPerPackage}`
                              : "—"}
                          </td>
                          <td>
                            {line.orderedQuantity || "—"} / {line.invoicedQuantity ?? "—"}
                          </td>
                          <td>
                            {line.expectedUnitPrice
                              ? line.expectedUnitPrice.toLocaleString("ru-RU", {
                                  minimumFractionDigits: 2,
                                  maximumFractionDigits: 4,
                                })
                              : "—"}{" "}
                            /{" "}
                            {line.invoicedQuantity == null
                              ? "—"
                              : line.unitPrice.toLocaleString("ru-RU", {
                                  minimumFractionDigits: 2,
                                  maximumFractionDigits: 4,
                                })}{" "}
                            {detail.order.currency}
                          </td>
                          <td>
                            {line.unitCostRub == null ? "—" : money.format(line.unitCostRub)}
                            <small>
                              {line.additionalPurchaseUnitRub
                                ? `Тара и прочие затраты: ${money.format(line.additionalPurchaseUnitRub)} · `
                                : ""}
                              {detail.order.status === "ordered" ? "Плановая оценка · " : ""}
                              {line.currentUnitCostRub == null
                                ? ""
                                : `В учёте (отдельно от партии): ${money.format(line.currentUnitCostRub)}`}
                            </small>
                          </td>
                          <td className={line.priceChangeNeeded ? "procurement-retail-change" : ""}>
                            {line.receivingMode && line.receivingMode !== "catalog" ? (
                              <>
                                {line.proposedRetailRub == null
                                  ? "—"
                                  : money.format(line.proposedRetailRub)}
                                <small>
                                  {line.receivingMode === "assortment"
                                    ? "После разбора"
                                    : line.receivingMode === "awaiting_catalog"
                                      ? "Для новой карточки"
                                      : "Под заказ"}
                                </small>
                              </>
                            ) : (
                              <>
                                {line.currentRetailRub > 0
                                  ? money.format(line.currentRetailRub)
                                  : "—"}{" "}
                                /{" "}
                                <span
                                  className={line.priceChangeNeeded ? "procurement-price-new" : ""}
                                >
                                  {line.proposedRetailRub == null
                                    ? "—"
                                    : money.format(line.proposedRetailRub)}
                                </span>
                                {line.priceChangeNeeded && <small>Изменение выше порога</small>}
                              </>
                            )}
                            {line.channelQuotes && (
                              <details>
                                <summary>Доход и цены по каналам</summary>
                                {Object.entries(line.channelQuotes).map(([channel, q]) => (
                                  <p key={channel}>
                                    {channel === "site" ? "Сайт" : channel === "wb" ? "WB" : "Ozon"}
                                    :{" "}
                                    {q.priceRub == null
                                      ? `нет расчёта: ${q.missing.join(", ")}`
                                      : `${money.format(q.priceRub)} · остаток ${money.format(q.contributionRub || 0)}`}
                                  </p>
                                ))}
                              </details>
                            )}
                          </td>
                          <td>
                            <div className="procurement-inline-actions">
                              {needsDimensionReview(line) && (
                                <button disabled={closed || saving} onClick={() => editLine(line)}>
                                  Проверить размеры
                                </button>
                              )}
                              {!line.invoiceExcluded && (line.invoicedQuantity || 0) > 0 && (
                                <button
                                  disabled={closed || saving}
                                  onClick={() => setReceivingLine(line)}
                                >
                                  {line.receivingMode === "assortment"
                                    ? "Разобрать микс"
                                    : "Способ приёмки"}
                                </button>
                              )}
                              {line.receivingMode === "customer_handoff" &&
                                !line.customerHandedOver && (
                                  <button
                                    disabled={closed || saving}
                                    onClick={() => void handOver(line)}
                                  >
                                    Передано клиенту
                                  </button>
                                )}
                              {(needsSaby(line) ||
                                line.receivingMode === "awaiting_catalog" ||
                                (needsDimensionReview(line) &&
                                  (!line.receivingMode || line.receivingMode === "catalog"))) &&
                                line.matchAlias && (
                                  <button
                                    disabled={closed || saving}
                                    onClick={() => setMatchingLine(line)}
                                  >
                                    Выбрать товар СБИС
                                  </button>
                                )}
                              {line.reconciliationStatus === "changed" &&
                                line.comparisonMismatch &&
                                !line.comparisonAccepted &&
                                !line.invoiceExcluded && (
                                  <button
                                    disabled={closed || saving}
                                    onClick={() => void acceptMismatch(line)}
                                  >
                                    Принять расхождение
                                  </button>
                                )}
                              {line.reconciliationStatus === "missing" &&
                                !line.invoiceExcluded &&
                                pendingInvoiceLines.length > 0 && (
                                  <button
                                    disabled={closed || saving}
                                    onClick={() => setPairingLineID(line.id)}
                                  >
                                    Выбрать из инвойса
                                  </button>
                                )}
                              <details className="procurement-row-more">
                                <summary>
                                  Ещё<span className="sr-only">: {line.rawName}</span>
                                </summary>
                                <div>
                                  {detail.order.status !== "ordered" && (
                                    <button
                                      disabled={closed || saving}
                                      onClick={() => void editLine(line)}
                                    >
                                      Исправить размеры
                                    </button>
                                  )}
                                  <button
                                    disabled={closed || saving}
                                    onClick={() => setPricingLine(line)}
                                  >
                                    Расходы продажи
                                  </button>
                                  {["matched", "changed"].includes(line.reconciliationStatus) &&
                                    !line.invoiceExcluded &&
                                    pendingInvoiceLines.length > 0 && (
                                      <button
                                        disabled={closed || saving}
                                        onClick={() => setPairingLineID(line.id)}
                                      >
                                        Добавить строки инвойса
                                      </button>
                                    )}
                                  {["matched", "changed"].includes(line.reconciliationStatus) &&
                                    !line.invoiceExcluded &&
                                    line.invoicedQuantity != null &&
                                    invoicePairCandidates.length > 0 && (
                                      <button
                                        disabled={closed || saving}
                                        onClick={() => setPairingLineID(line.id)}
                                      >
                                        Сменить строку инвойса
                                      </button>
                                    )}
                                  {["missing", "added", "changed"].includes(
                                    line.reconciliationStatus,
                                  ) &&
                                    !line.invoiceExcluded && (
                                      <button
                                        disabled={closed || saving}
                                        onClick={() => void setInvoiceExcluded(line, true)}
                                      >
                                        Исключить
                                      </button>
                                    )}
                                  {line.invoiceExcluded && (
                                    <button
                                      disabled={closed || saving}
                                      onClick={() => void setInvoiceExcluded(line, false)}
                                    >
                                      Вернуть в сверку
                                    </button>
                                  )}
                                </div>
                              </details>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {pagedInvoiceLines.length > 0 && (
                <section className="procurement-checklist blocked">
                  <strong>Несопоставленные строки инвойса: {pendingInvoiceLines.length}</strong>
                  <p>Сопоставьте с позицией плана или добавьте как отдельный товар.</p>
                  <div className="admin-table-wrap">
                    <table className="admin-table procurement-lines">
                      <thead>
                        <tr>
                          <th>Инвойс</th>
                          <th>Количество</th>
                          <th>Цена</th>
                          <th>Связь с СБИС</th>
                          <th></th>
                        </tr>
                      </thead>
                      <tbody>
                        {pagedInvoiceLines.map((line) => (
                          <tr key={line.id}>
                            <td>
                              <strong>{line.invoiceRawName || line.rawName}</strong>
                              {line.invoiceSupplierArticle && (
                                <small>{line.invoiceSupplierArticle}</small>
                              )}
                            </td>
                            <td>{line.invoicedQuantity ?? "—"}</td>
                            <td>
                              {line.unitPrice.toLocaleString("ru-RU", {
                                minimumFractionDigits: 2,
                                maximumFractionDigits: 4,
                              })}{" "}
                              {detail.order.currency}
                            </td>
                            <td>
                              {line.sabyName ||
                                (line.matchStatus === "confirmed"
                                  ? line.sabyId
                                  : "Не сопоставлено")}
                            </td>
                            <td>
                              <button
                                disabled={closed || saving}
                                onClick={() => void acceptAddedLine(line)}
                              >
                                Это новая позиция
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              )}
              {shownLines > displayedLines && (
                <div className="dialog-actions">
                  <button
                    aria-label="Показать ещё строки закупки"
                    onClick={() => setLineWindow({ scope: lineScope, count: lineLimit + 5 })}
                  >
                    Показать ещё
                  </button>
                </div>
              )}
            </div>

            {detail.order.status === "ready_to_receive" && (
              <>
                <div className="procurement-receipt-actions">
                  <button
                    className="primary"
                    onClick={() => void prepare("receipt")}
                    disabled={
                      saving ||
                      !(detail.validation.canPrepareReceipt ?? detail.validation.canPrepareActions)
                    }
                  >
                    Подготовить поступление СБИС
                  </button>
                  <button
                    disabled={saving || !detail.validation.canPrepareActions}
                    onClick={() =>
                      void downloadReceivingPDF().catch((error) =>
                        onError((error as Error).message),
                      )
                    }
                  >
                    Скачать PDF приёмки
                  </button>
                  {!(
                    detail.validation.canPrepareReceipt ?? detail.validation.canPrepareActions
                  ) && <span>Сначала распределите миксы и выберите карточки.</span>}
                </div>
                <details className="procurement-section-details">
                  <summary>Цены и каналы продаж</summary>
                  <fieldset className="procurement-price-channels">
                    <legend>Подготовить изменения</legend>
                    {[
                      ["site", "Подготовить цену сайта"],
                      ["saby_price", "Скачать цены для СБИС"],
                      ["wb", "Подготовить Wildberries"],
                      ["ozon", "Подготовить Ozon"],
                      ["avito", "Подготовить Авито"],
                    ].map(([channel, label]) => (
                      <button
                        key={channel}
                        disabled={saving || !detail.validation.canPrepareActions}
                        onClick={() => void prepare("prices", channel)}
                      >
                        {label}
                      </button>
                    ))}
                  </fieldset>
                </details>
              </>
            )}
            {detail.order.status === "received" && (
              <button disabled={saving} onClick={() => void prepare("prices", "saby_price")}>
                Скачать цены для СБИС
              </button>
            )}
            {detail.batches
              .filter((batch) => !["completed", "cancelled"].includes(batch.status))
              .map(renderBatch)}
            {detail.batches.some((batch) => ["completed", "cancelled"].includes(batch.status)) && (
              <details className="procurement-batch procurement-batch-history">
                <summary>
                  <strong>История поступлений и изменений цен</strong> ·{" "}
                  {
                    detail.batches.filter((batch) =>
                      ["completed", "cancelled"].includes(batch.status),
                    ).length
                  }
                </summary>
                <div className="procurement-batch-history-list">
                  {detail.batches
                    .filter((batch) => ["completed", "cancelled"].includes(batch.status))
                    .map(renderBatch)}
                </div>
              </details>
            )}
            {!closed && (
              <div className="procurement-order-final">
                {detail.order.status === "ready_to_receive" &&
                  (detail.batches.some(
                    (batch) =>
                      batch.kind === "receipt" &&
                      batch.items.some(
                        (item) => item.channel === "saby_receipt" && item.status === "completed",
                      ),
                  ) ||
                    (detail.lines.every(
                      (line) =>
                        line.invoiceExcluded ||
                        line.matchStatus === "ignored" ||
                        line.receivingMode === "customer_handoff",
                    ) &&
                      !detail.validation.pendingHandoffs)) && (
                    <button
                      className="primary"
                      disabled={saving}
                      onClick={() => void setStatus("received")}
                    >
                      Завершить приёмку
                    </button>
                  )}
                <details className="procurement-section-details">
                  <summary>Другие действия</summary>
                  <div className="procurement-inline-actions">
                    {detail.order.status === "ready_to_receive" && (
                      <button disabled={saving} onClick={() => void setStatus("review")}>
                        Вернуть на проверку
                      </button>
                    )}
                    <button
                      className="danger"
                      disabled={saving}
                      onClick={() => void setStatus("cancelled")}
                    >
                      Отменить закупку
                    </button>
                    {detail.order.status === "ready_to_receive" && (
                      <button disabled={saving} onClick={() => void setStatus("received", true)}>
                        Завершить вручную
                      </button>
                    )}
                  </div>
                </details>
              </div>
            )}
            {detail.order.status === "received" && (
              <div className="procurement-order-final">
                <button disabled={saving} onClick={() => void setStatus("review")}>
                  Вернуть на проверку для редактирования
                </button>
              </div>
            )}
            {detail.order.status === "cancelled" && (
              <div className="procurement-order-final">
                <button
                  className="admin-primary"
                  disabled={saving}
                  onClick={() => void setStatus("review")}
                >
                  Вернуть закупку в работу
                </button>
                {canDelete && (
                  <button
                    className="text-button danger"
                    disabled={saving}
                    onClick={() => void remove()}
                  >
                    Удалить закупку и PDF
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>
      {pricingLine && (
        <Suspense fallback={<p role="status">Загружаем расходы продажи…</p>}>
          <ProcurementLinePricing
            key={pricingLine.id}
            line={pricingLine}
            settings={settings}
            onClose={() => setPricingLine(null)}
            onSaved={() => {
              setPricingLine(null);
              void load(true);
              onSaved();
            }}
            onError={externalError}
          />
        </Suspense>
      )}{" "}
      {lineEditor && (
        <ProcurementLineEditor
          key={`${lineEditor.line.id}-${lineEditor.kind}`}
          {...lineEditor}
          currency={detail?.order.currency || "EUR"}
          onClose={() => setLineEditor(null)}
          onSaved={() => {
            const reviewing = lineEditor.kind === "dimensions" && lineFilter === "dimensions";
            setLineEditor(null);
            void load(true).then(() => {
              if (reviewing) showDimensionReview();
            });
            onSaved();
          }}
          onError={externalError}
        />
      )}{" "}
      {pairingLine && detail && (
        <Suspense fallback={<p role="status">Загружаем строки инвойса…</p>}>
          <ProcurementInvoicePairDialog
            plannedLine={pairingLine}
            candidates={invoicePairCandidates}
            currency={detail.order.currency}
            saving={saving}
            onClose={() => setPairingLineID(null)}
            onPick={(invoiceLine) => matchInvoiceLine(pairingLine, invoiceLine)}
            onPickMany={(invoiceLines) => matchInvoiceLines(pairingLine, invoiceLines)}
            onError={externalError}
          />
        </Suspense>
      )}
      {matchingLine?.matchAlias && (
        <ProcurementMatchDialog
          alias={matchingLine.matchAlias}
          initialQuery={matchingLine.rawName}
          nested
          onClose={() => setMatchingLine(null)}
          onSaved={() => {
            setMatchingLine(null);
            void load(true);
            onSaved();
          }}
          onError={externalError}
        />
      )}
      {receivingLine && (
        <ProcurementReceivingDialog
          line={receivingLine}
          onClose={() => setReceivingLine(null)}
          onSaved={() => {
            setReceivingLine(null);
            void load(true);
            onSaved();
          }}
          onError={externalError}
        />
      )}
    </>
  );
}

export const channelLabel = (value: string) =>
  ({
    site: "Сайт",
    saby_price: "СБИС — цена",
    saby_receipt: "СБИС — поступление",
    wb: "Wildberries",
    ozon: "Ozon",
    avito: "Авито",
  })[value] || value;

const batchDisplayRows = (batch: ProcurementActionBatch) =>
  batch.items.flatMap((item) =>
    item.previewLines?.length
      ? item.previewLines.map((line, index) => ({
          key: `${item.id}-${line.sabyId}`,
          item,
          productName: line.name,
          productCode: [line.code, `ID ${line.sabyId}`].filter(Boolean).join(" · "),
          oldValue: line.oldPrice,
          newValue: line.newPrice || 0,
          quantity: line.quantity,
          oldBalance: line.oldBalance,
          newBalance: line.newBalance,
          showStatus: index === 0,
        }))
      : [
          {
            key: String(item.id),
            item,
            productName: item.productName,
            productCode: item.productCode,
            oldValue: item.oldValue,
            newValue: item.newValue,
            quantity: item.quantity,
            oldBalance: undefined,
            newBalance: undefined,
            showStatus: true,
          },
        ],
  );

export const batchStatusLabel = (value: string) =>
  ({
    draft: "Черновик",
    processing: "Выполняется",
    completed: "Выполнено",
    partially_completed: "Выполнено частично",
    failed: "Ошибка",
    cancelled: "Отменено",
  })[value] || value;

export const actionStatus = (item: ProcurementActionItem) => {
  const label =
    item.channel === "saby_receipt" && item.status === "draft"
      ? "Подготовлено"
      : item.channel === "saby_receipt" &&
          item.externalUrl &&
          ["queued", "processing"].includes(item.status)
        ? "Черновик создан · ждёт проведения"
        : item.channel === "saby_receipt" && item.status === "completed"
          ? "Проведение подтверждено"
          : item.channel === "wb" &&
              item.externalOperationId &&
              ["queued", "processing"].includes(item.status)
            ? "Wildberries обрабатывает загрузку"
            : (
                {
                  draft: "Черновик",
                  queued: "В очереди",
                  processing: "Отправляется",
                  completed: "Выполнено",
                  failed: "Ошибка",
                  skipped: "Пропущено",
                  not_configured: "API не подключён",
                } as Record<string, string>
              )[item.status] || item.status;
  return (
    <>
      <span
        className={
          item.status === "completed"
            ? "procurement-ok"
            : item.status === "failed" ||
                item.status === "not_configured" ||
                item.status === "skipped"
              ? "procurement-warning"
              : ""
        }
      >
        {label}
      </span>
      {item.externalUrl && (
        <a href={item.externalUrl} target="_blank" rel="noreferrer">
          <small>
            {item.channel === "saby_receipt"
              ? "Открыть поступление в СБИС"
              : "Открыть документ в СБИС"}
          </small>
        </a>
      )}
      {item.errorMessage && <small>{item.errorMessage}</small>}
    </>
  );
};

export function ProcurementMatchDialog({
  alias,
  initialQuery,
  nested = false,
  onClose,
  onSaved,
  onError: externalError,
}: {
  alias: ProcurementAlias;
  initialQuery?: string;
  nested?: boolean;
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(onClose);
  const [query, setQuery] = useState(initialQuery || alias.rawName);
  const [items, setItems] = useState<NomenclatureCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => {
    if (query.trim().length < 2) {
      setItems([]);
      setSearching(false);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      setSearching(true);
      api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`,
      )
        .then((result) => {
          if (!cancelled) setItems(result.items);
        })
        .catch((error) => {
          if (!cancelled) onError((error as Error).message);
        })
        .finally(() => {
          if (!cancelled) setSearching(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, onError]);
  const resolve = async (matchStatus: "confirmed" | "new_product" | "ignored", sabyId = "") => {
    setSaving(true);
    try {
      await api(`/api/v1/admin/procurement/aliases/${alias.id}`, {
        method: "PATCH",
        body: JSON.stringify({ matchStatus, sabyId }),
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const refreshSaby = async () => {
    setRefreshing(true);
    try {
      await api("/api/v1/admin/procurement/integrations/saby/catalog", { method: "POST" });
      const result = await api<{ items: NomenclatureCandidate[] }>(
        `/api/v1/admin/procurement/nomenclature?q=${encodeURIComponent(query.trim())}`,
      );
      setItems(result.items);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setRefreshing(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className={`admin-dialog-backdrop ${nested ? "procurement-match-backdrop" : ""}`}
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        className="admin-dialog procurement-match-dialog"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="match-title"
      >
        <header>
          <div>
            <p className="eyebrow">{alias.supplierName}</p>
            <h2 id="match-title">Выбрать товар СБИС</h2>
          </div>
          <button onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        <div className="procurement-match-source">
          <strong>{alias.rawName}</strong>
          <span>
            {[
              alias.supplierArticle && `Артикул ${alias.supplierArticle}`,
              alias.potDiameterCm && `D${alias.potDiameterCm}`,
              alias.heightCm && `${alias.heightCm} см`,
            ]
              .filter(Boolean)
              .join(" · ") || "Размер не указан"}
          </span>
        </div>
        <p className="admin-hint">
          Выбор сохранится для следующих инвойсов с тем же названием и размером.
        </p>
        <label className="procurement-match-search">
          Поиск товара СБИС
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            autoFocus
            placeholder="Название, артикул или код X…"
          />
        </label>
        <div className="procurement-candidates">
          {searching ? (
            <div className="procurement-zero">
              <span>Ищем в справочнике…</span>
            </div>
          ) : items.length ? (
            items.map((item) => (
              <article key={item.sabyId}>
                <div>
                  <strong>{item.name}</strong>
                  <span>{[item.code, item.article].filter(Boolean).join(" · ")}</span>
                  <small>
                    Остаток СБИС: {item.balance} · {money.format(item.price)}
                  </small>
                </div>
                <button disabled={saving} onClick={() => void resolve("confirmed", item.sabyId)}>
                  Выбрать
                </button>
              </article>
            ))
          ) : (
            <div className="procurement-zero">
              <strong>Кандидаты не найдены</strong>
              <span>Импортируйте новую карточку СБИС или измените запрос.</span>
            </div>
          )}
        </div>

        <div className="dialog-actions procurement-match-actions">
          <button onClick={() => void resolve("ignored")} disabled={saving || refreshing}>
            Игнорировать строку
          </button>
          <button onClick={() => void resolve("new_product")} disabled={saving || refreshing}>
            Это новый товар
          </button>
          <button onClick={() => void refreshSaby()} disabled={saving || refreshing}>
            {refreshing ? "Обновляем СБИС…" : "Обновить из СБИС"}
          </button>
          <button onClick={onClose}>Отмена</button>
        </div>
      </div>
    </>
  );
}

export function ProcurementUploadDialog({
  suppliers,
  orders,
  onClose,
  onSaved,
}: {
  suppliers: ProcurementSupplier[];
  orders: ProcurementOrder[];
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const dialogRef = useProcurementDialog(() => {
    if (!saving) onClose();
  });
  const suggestOrder = (supplier: number) => {
    const active = orders.filter(
      (item) => item.supplierId === supplier && !["received", "cancelled"].includes(item.status),
    );
    return active.length === 1 ? active[0].id : active.length ? -1 : 0;
  };
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0);
  const [orderId, setOrderId] = useState(() => suggestOrder(suppliers[0]?.id || 0));
  const [fileError, setFileError] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [saving, setSaving] = useState(false);
  const availableOrders = orders.filter(
    (item) => item.supplierId === supplierId && !["received", "cancelled"].includes(item.status),
  );
  const changeSupplier = (value: number) => {
    setSupplierId(value);
    setOrderId(suggestOrder(value));
  };
  const save = async () => {
    if (!file || !supplierId || orderId < 0) return;
    setFileError("");
    setSaving(true);
    try {
      await validateProcurementPDF(file);
      const body = new FormData();
      body.append("supplierId", String(supplierId));
      if (orderId) body.append("orderId", String(orderId));
      body.append("file", file);
      const result = await uploadProcurementPDF<{ duplicate: boolean; order: ProcurementOrder }>(
        body,
      );
      if (result.duplicate) {
        setFileError(
          `Этот PDF уже загружен в закупку ${result.order.orderNumber || `№${result.order.id}`} (${result.order.status === "cancelled" ? "отменена — откройте её и восстановите при необходимости" : "откройте существующую закупку"}).`,
        );
        return;
      }
      onSaved();
    } catch (error) {
      setFileError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть"
        onClick={() => {
          if (!saving) onClose();
        }}
        disabled={saving}
      />
      <div
        className="admin-dialog procurement-modal"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="upload-title"
      >
        <header>
          <h2 id="upload-title">Загрузить документ</h2>
          <button
            onClick={() => {
              if (!saving) onClose();
            }}
            disabled={saving}
            aria-label="Закрыть"
          >
            ×
          </button>
        </header>
        <div className="admin-form-grid">
          <label className="wide">
            Поставщик
            <select
              value={supplierId}
              onChange={(event) => changeSupplier(Number(event.target.value))}
            >
              {suppliers.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="wide">
            Связать с закупкой
            <select value={orderId} onChange={(event) => setOrderId(Number(event.target.value))}>
              {orderId < 0 && <option value={-1}>Выберите закупку или создание новой</option>}
              <option value={0}>Создать новую закупку из документа</option>
              {availableOrders.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.orderNumber || `Закупка №${item.id}`} ·{" "}
                  {item.title || new Date(item.createdAt).toLocaleDateString("ru-RU")} ·{" "}
                  {item.total.toLocaleString("ru-RU")} {item.currency} ·{" "}
                  {procurementStatusLabels[item.status] || item.status}
                </option>
              ))}
            </select>
          </label>
          {availableOrders.length > 0 && orderId === 0 && (
            <p className="wide procurement-warning">
              У поставщика есть активные закупки. Выберите существующую, если документ относится к
              уже отправленному заказу.
            </p>
          )}
          <label className="wide procurement-file">
            PDF-файл
            <input
              type="file"
              accept="application/pdf,.pdf"
              onChange={(event) => {
                const selected = event.target.files?.[0];
                setFile(null);
                setFileError("");
                if (selected)
                  void validateProcurementPDF(selected)
                    .then(() => setFile(selected))
                    .catch((error) => setFileError((error as Error).message));
              }}
            />
            <span>
              {file ? `${file.name} · ${(file.size / 1024 / 1024).toFixed(2)} МБ` : "До 20 МБ"}
            </span>
          </label>
          {fileError && (
            <p className="procurement-warning wide" role="alert">
              {fileError}
            </p>
          )}
        </div>
        <div className="dialog-actions">
          <button
            onClick={() => {
              if (!saving) onClose();
            }}
            disabled={saving}
          >
            Отмена
          </button>
          <button
            className="primary"
            disabled={!file || !supplierId || orderId < 0 || saving}
            onClick={save}
          >
            {saving ? "Разбираем PDF…" : "Загрузить и разобрать"}
          </button>
        </div>
      </div>
    </>
  );
}

export function SupplierDialog({
  suppliers,
  canDelete,
  onClose,
  onSaved,
  onError: externalError,
}: {
  suppliers: ProcurementSupplier[];
  canDelete: boolean;
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(() => close());
  const [editing, setEditing] = useState<ProcurementSupplier | null>(null);
  const edit = (item: ProcurementSupplier) => {
    setEditing(item);
    setName(item.name);
    setKind(item.kind as "international" | "domestic");
    setTaxId(item.taxId);
    setKpp(item.kpp);
    setCountryCode(item.countryCode);
    setCurrency(item.defaultCurrency as "EUR" | "USD" | "RUB");
  };
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"international" | "domestic">("international");
  const [taxId, setTaxId] = useState("");
  const [kpp, setKpp] = useState("");
  const [countryCode, setCountryCode] = useState("NL");
  const [currency, setCurrency] = useState<"EUR" | "USD" | "RUB">("EUR");
  const [saving, setSaving] = useState(false);
  const [deletingId, setDeletingId] = useState(0);
  const [deleteCandidate, setDeleteCandidate] = useState<ProcurementSupplier | null>(null);
  const close = () => {
    if (!saving && (!name.trim() || window.confirm("Закрыть форму поставщика без сохранения?")))
      onClose();
  };
  const save = async () => {
    setSaving(true);
    try {
      await api(
        editing
          ? `/api/v1/admin/procurement/suppliers/${editing.id}`
          : "/api/v1/admin/procurement/suppliers",
        {
          method: editing ? "PATCH" : "POST",
          body: JSON.stringify({
            name,
            kind,
            countryCode,
            taxId,
            kpp,
            defaultCurrency: currency,
            active: editing?.active ?? true,
          }),
        },
      );
      setEditing(null);
      setName("");
      setTaxId("");
      setKpp("");
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const remove = async (supplier: ProcurementSupplier) => {
    setDeletingId(supplier.id);
    try {
      await api(`/api/v1/admin/procurement/suppliers/${supplier.id}`, {
        method: "PATCH",
        body: JSON.stringify({ ...supplier, active: !supplier.active }),
      });
      setDeleteCandidate(null);
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setDeletingId(0);
    }
  };
  const changeKind = (value: "international" | "domestic") => {
    setKind(value);
    if (value === "domestic") {
      setCountryCode("RU");
      setCurrency("RUB");
    } else {
      setCountryCode("NL");
      setCurrency("EUR");
      setKpp("");
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть"
        onClick={close}
      />
      <div
        className="admin-dialog procurement-modal"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="supplier-title"
      >
        <header>
          <h2 id="supplier-title">Поставщики</h2>
          <button onClick={close} aria-label="Закрыть">
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        {suppliers.length > 0 && (
          <div className="admin-table-wrap">
            <table className="admin-table procurement-suppliers-table">
              <thead>
                <tr>
                  <th>Поставщик</th>
                  <th>Тип</th>
                  <th className="supplier-action-column">Действие</th>
                </tr>
              </thead>
              <tbody>
                {suppliers.map((supplier) => (
                  <tr key={supplier.id}>
                    <td>
                      <strong>{supplier.name}</strong>
                      <small>
                        {supplier.countryCode || "Страна не указана"} · {supplier.defaultCurrency}
                        {supplier.taxId ? ` · ИНН ${supplier.taxId}` : ""}
                        {supplier.kpp ? ` · КПП ${supplier.kpp}` : ""}
                      </small>
                    </td>
                    <td>
                      {supplier.kind === "international" ? "Иностранный" : "Российский"}
                      <small>{supplier.active ? "Действующий" : "Архив"}</small>
                    </td>
                    <td className="supplier-action-column">
                      <button className="table-action" onClick={() => edit(supplier)}>
                        Изменить
                      </button>
                      {canDelete && (
                        <button
                          className="table-action danger supplier-delete-button"
                          disabled={deletingId > 0}
                          onClick={() => setDeleteCandidate(supplier)}
                        >
                          {deletingId === supplier.id
                            ? "Сохраняем…"
                            : supplier.active
                              ? "В архив"
                              : "Восстановить"}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <h3>{editing ? "Редактировать поставщика" : "Добавить поставщика"}</h3>
        <div className="admin-form-grid">
          <label className="wide">
            Название
            <input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
          </label>
          <label>
            ИНН
            <input
              inputMode="numeric"
              value={taxId}
              onChange={(event) => setTaxId(event.target.value.replace(/\D/g, "").slice(0, 12))}
              placeholder="Для выбора в СБИС"
            />
          </label>
          {kind === "domestic" && (
            <label>
              КПП
              <input
                inputMode="numeric"
                value={kpp}
                onChange={(event) => setKpp(event.target.value.replace(/\D/g, "").slice(0, 9))}
                placeholder="9 цифр"
              />
            </label>
          )}
          <label>
            Тип
            <select
              value={kind}
              onChange={(event) => changeKind(event.target.value as "international" | "domestic")}
            >
              <option value="international">Иностранный</option>
              <option value="domestic">Российский</option>
            </select>
          </label>
          <label>
            Страна
            <input
              maxLength={2}
              value={countryCode}
              onChange={(event) => setCountryCode(event.target.value.toUpperCase())}
            />
          </label>
          <label>
            Валюта
            <select
              value={currency}
              onChange={(event) => setCurrency(event.target.value as "EUR" | "USD" | "RUB")}
            >
              <option>EUR</option>
              <option>USD</option>
              <option>RUB</option>
            </select>
          </label>
          <p className="admin-hint wide">
            Название поставщика не используется для автоматического сопоставления растений. У
            каждого поставщика будет собственный набор ключей.
          </p>
        </div>
        <div className="dialog-actions">
          <button onClick={close}>Отмена</button>
          <button
            className="primary"
            disabled={
              !name.trim() ||
              saving ||
              (kind === "domestic" && taxId.length === 10 && kpp.length !== 9)
            }
            onClick={save}
          >
            {saving ? "Сохраняем…" : editing ? "Сохранить" : "Добавить"}
          </button>
        </div>
      </div>
      {deleteCandidate && (
        <ConfirmDialog
          title={
            deleteCandidate.active ? "Перенести поставщика в архив?" : "Восстановить поставщика?"
          }
          text={
            <>
              Поставщик <strong>«{deleteCandidate.name}»</strong>{" "}
              {deleteCandidate.active
                ? "перестанет предлагаться для новых закупок. Документы и сопоставления сохранятся."
                : "снова станет доступен для новых закупок."}
            </>
          }
          confirmLabel={
            deletingId === deleteCandidate.id
              ? "Сохраняем…"
              : deleteCandidate.active
                ? "В архив"
                : "Восстановить"
          }
          busy={deletingId > 0}
          onCancel={() => setDeleteCandidate(null)}
          onConfirm={() => void remove(deleteCandidate)}
        />
      )}
    </>
  );
}

export function ProcurementOrderDialog({
  suppliers,
  onClose,
  onSaved,
  onError: externalError,
}: {
  suppliers: ProcurementSupplier[];
  onClose: () => void;
  onSaved: () => void;
  onError: (value: string) => void;
}) {
  const {
    error: dialogError,
    errorRef,
    notify: onError,
    clear: clearError,
  } = useProcurementError(externalError);
  const dialogRef = useProcurementDialog(onClose);
  const [supplierId, setSupplierId] = useState(suppliers[0]?.id || 0);
  const selected = suppliers.find((item) => item.id === supplierId);
  const [orderNumber, setOrderNumber] = useState("");
  const [sourceKind, setSourceKind] = useState("manual");
  const [notes, setNotes] = useState("");
  const [saving, setSaving] = useState(false);
  const save = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await api("/api/v1/admin/procurement/orders", {
        method: "POST",
        body: JSON.stringify({
          supplierId,
          orderNumber,
          sourceKind,
          currency: selected.defaultCurrency,
          notes,
        }),
      });
      onSaved();
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <button
        tabIndex={-1}
        className="admin-dialog-backdrop"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        className="admin-dialog procurement-modal"
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="procurement-title"
      >
        <header>
          <h2 id="procurement-title">Новая закупка</h2>
          <button onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </header>
        {dialogError && (
          <div ref={errorRef} tabIndex={-1} className="procurement-dialog-error" role="alert">
            <strong>Не удалось выполнить действие</strong>
            <span>{dialogError}</span>
            <button aria-label="Закрыть сообщение об ошибке" onClick={clearError}>
              ×
            </button>
          </div>
        )}
        <div className="admin-form-grid">
          <label className="wide">
            Поставщик
            <select
              value={supplierId}
              onChange={(event) => setSupplierId(Number(event.target.value))}
            >
              {suppliers.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} · {item.defaultCurrency}
                </option>
              ))}
            </select>
          </label>
          <label>
            Номер заказа
            <input
              value={orderNumber}
              onChange={(event) => setOrderNumber(event.target.value)}
              placeholder="Можно заполнить позже"
            />
          </label>
          <label>
            Основание
            <select value={sourceKind} onChange={(event) => setSourceKind(event.target.value)}>
              <option value="manual">Ручная закупка</option>
              <option value="recommendation">Рекомендации системы</option>
              <option value="invoice">Инвойс</option>
              <option value="payment_invoice">Счёт на оплату</option>
            </select>
          </label>
          <label className="wide">
            Комментарий
            <textarea rows={3} value={notes} onChange={(event) => setNotes(event.target.value)} />
          </label>
          <p className="admin-hint wide">
            Создаётся только черновик. Отправки в СБИС и изменения цен не будет.
          </p>
        </div>
        <div className="dialog-actions">
          <button onClick={onClose}>Отмена</button>
          <button className="primary" disabled={!supplierId || saving} onClick={save}>
            {saving ? "Создаём…" : "Создать черновик"}
          </button>
        </div>
      </div>
    </>
  );
}
