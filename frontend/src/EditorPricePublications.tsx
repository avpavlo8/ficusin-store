import { useEffect, useState } from "react";
import { api, money } from "./adminShared";
import { wbPricePresentation } from "./editorPriceDisplay";

type PricePublication = {
  id: number;
  variantId: number;
  channel: string;
  targetId: string;
  priceMinor: number;
  compareAtMinor?: number | null;
  expectedSitePriceMinor: number;
  status: string;
  message: string;
  fingerprint: string;
  executionMode: string;
  canApprove: boolean;
  canRetry: boolean;
  blockReason: string;
};
const names: Record<string, string> = {
  wb: "Wildberries",
  ozon: "Ozon",
  avito: "Авито",
  saby: "СБИС",
};
const statusNames: Record<string, string> = {
  draft: "Ожидает подтверждения",
  queued: "В очереди",
  processing: "Проверяем результат",
  synced: "Цена подтверждена",
  failed: "Ошибка обмена",
  needs_attention: "Нужна проверка",
  superseded: "Заменено новой ценой",
  skipped: "Пропущено",
  prepared: "Подготовлено",
};

export function EditorPricePublications({
  productId,
  variantId,
  draftPriceMinor,
  saving,
  revision,
  canManage,
}: {
  productId: number;
  variantId?: number;
  draftPriceMinor: number;
  saving: boolean;
  revision: number;
  canManage: boolean;
}) {
  const [prices, setPrices] = useState<PricePublication[]>([]);
  const [mode, setMode] = useState("disabled");
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState<number | null>(null);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    if (saving) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const read = async () => {
      try {
        const data = await api<{ prices: PricePublication[]; publicationMode: string }>(
          `/api/v1/admin/products/${productId}/editor-prices`,
          { signal: controller.signal },
        );
        if (controller.signal.aborted) return;
        setPrices(data.prices || []);
        setMode(data.publicationMode || "disabled");
        setError("");
        if (
          data.prices?.some((price) => price.status === "queued" || price.status === "processing")
        )
          timer = setTimeout(() => void read(), 3000);
      } catch (cause) {
        if (!controller.signal.aborted) setError((cause as Error).message);
      }
    };
    void read();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [productId, saving, revision, reload]);
  const mutate = async (price: PricePublication, action: "approve" | "retry") => {
    setBusy(price.id);
    setActionError("");
    try {
      const updated = await api<PricePublication>(
        `/api/v1/admin/products/${productId}/editor-prices/${price.id}/${action}`,
        { method: "POST", body: JSON.stringify({ fingerprint: price.fingerprint }) },
      );
      setPrices((current) => current.map((row) => (row.id === updated.id ? updated : row)));
    } catch (cause) {
      setActionError((cause as Error).message);
    } finally {
      setBusy(null);
      setReload((current) => current + 1);
    }
  };
  const selected = prices.filter((price) => price.variantId === variantId);
  return (
    <section className="editor-section" aria-label="Подтверждение цен площадок">
      <h3>Цены площадок</h3>
      <p className="editor-secondary">
        Изменение цены сайта готовит отдельные предложения. Каждую цену площадки нужно подтвердить
        явно; сохранение карточки её не отправляет.
      </p>
      {mode === "disabled" && (
        <p role="status">
          Отправка цен отключена. После включения потребуется сохранить актуальную цену заново.
        </p>
      )}
      {mode === "mock" && (
        <p role="status">Локальная проверка цен. Реальные цены площадок не меняются.</p>
      )}
      {(error || actionError) && (
        <p role="alert">
          {actionError || error}{" "}
          <button
            type="button"
            className="admin-action"
            disabled={saving || busy !== null}
            onClick={() => {
              setActionError("");
              setReload((value) => value + 1);
            }}
          >
            Обновить цены
          </button>
        </p>
      )}
      {!selected.length && (
        <p className="editor-secondary">Для этого варианта нет предложений цен.</p>
      )}
      {selected.map((price) => {
        const wbPrice =
          price.channel === "wb"
            ? wbPricePresentation(price.priceMinor, price.compareAtMinor)
            : null;
        const unsavedPrice =
          !Number.isFinite(draftPriceMinor) || draftPriceMinor !== price.expectedSitePriceMinor;
        const disabled = saving || busy !== null || unsavedPrice || mode === "disabled";
        const retryDisabled =
          saving ||
          busy !== null ||
          mode === "disabled" ||
          (unsavedPrice && price.status !== "needs_attention");
        return (
          <article
            key={price.id}
            className="workspace-panel"
            aria-label={`${names[price.channel] || price.channel}: ${price.targetId}`}
          >
            <h4>
              {names[price.channel] || price.channel} · {price.targetId}
            </h4>
            <dl>
              <div>
                <dt>Цена сайта при подготовке</dt>
                <dd>{money.format(price.expectedSitePriceMinor / 100)}</dd>
              </div>
              <div>
                <dt>{price.channel === "wb" ? "Расчётная цена" : "Будет на площадке"}</dt>
                <dd>{money.format(price.priceMinor / 100)}</dd>
              </div>
              {price.compareAtMinor != null && !wbPrice && (
                <div>
                  <dt>Цена до скидки</dt>
                  <dd>{money.format(price.compareAtMinor / 100)}</dd>
                </div>
              )}
              {wbPrice && (
                <>
                  <div>
                    <dt>Базовая цена WB</dt>
                    <dd>{money.format(wbPrice.baseRub)}</dd>
                  </div>
                  <div>
                    <dt>Скидка WB</dt>
                    <dd>{wbPrice.discountPercent}%</dd>
                  </div>
                  <div>
                    <dt>Цена WB после округления скидки</dt>
                    <dd>{money.format(wbPrice.effectiveMinor / 100)}</dd>
                  </div>
                </>
              )}
            </dl>
            <p role="status">
              {statusNames[price.status] || price.status}
              {price.message && ` · ${price.message}`}
            </p>
            {price.blockReason && <p className="editor-secondary">{price.blockReason}</p>}
            {unsavedPrice &&
              (price.canApprove || (price.canRetry && price.status !== "needs_attention")) && (
                <p className="editor-secondary">
                  Сохраните текущую цену сайта и обновите предложение перед подтверждением.
                </p>
              )}
            {canManage && price.canApprove && (
              <button
                type="button"
                className="admin-action"
                disabled={disabled}
                onClick={() => void mutate(price, "approve")}
              >
                {busy === price.id ? "Подтверждаем…" : "Подтвердить цену"}
              </button>
            )}
            {canManage && price.canRetry && (
              <button
                type="button"
                className="admin-action"
                disabled={retryDisabled}
                onClick={() => void mutate(price, "retry")}
              >
                {busy === price.id
                  ? "Проверяем…"
                  : price.status === "needs_attention"
                    ? "Проверить результат"
                    : "Повторить отправку"}
              </button>
            )}
          </article>
        );
      })}
    </section>
  );
}
