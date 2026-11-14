import { useEffect, useState } from "react";
import { api } from "./adminShared";
import type { ProcurementChannelQuote } from "./adminTypes";
import { marketplacePricingInput } from "./productMarketplacePricing";

type Result = {
  revision: number;
  key: string;
  wb: string;
  ozon: string;
  notes: string[];
  error?: string;
};
export function useProductMarketplacePricing(
  key: string,
  enabled: boolean,
  onCalculated: (key: string, prices: { wb: string; ozon: string }) => void,
) {
  const [result, setResult] = useState<Result | null>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const refresh = () => setRevision((value) => value + 1);
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", visible);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", visible);
    };
  }, []);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    const controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(20000)]);
    const timer = window.setTimeout(async () => {
      const next: Result = { revision, key, wb: "", ozon: "", notes: [] };
      try {
        const { price, attributes } = JSON.parse(key);
        const settings = await api<{ values: Record<string, string> }>(
          "/api/v1/admin/catalog-editor/settings",
          { signal },
        );
        const season = settings.values["cdek.packaging_season"];
        if (season !== "summer" && season !== "winter")
          throw new Error("Не удалось определить сезон упаковки в настройках сайта");
        const input = marketplacePricingInput(price, attributes, season);
        next.notes.push(`Автоматически · ${season === "winter" ? "зимняя" : "летняя"} упаковка`);
        if (input.error) next.error = input.error;
        else {
          const preview = await api<{ quotes: Record<string, ProcurementChannelQuote> }>(
            "/api/v1/admin/procurement/pricing/preview",
            {
              method: "POST",
              body: JSON.stringify(input.body),
              signal,
            },
          );
          for (const channel of ["wb", "ozon"] as const) {
            const quote = preview.quotes[channel];
            if (
              quote?.priceRub != null &&
              Number.isFinite(quote.priceRub) &&
              quote.priceRub > 0 &&
              !quote.missing?.length
            )
              next[channel] = String(quote.priceRub);
            else
              next.notes.push(
                `${channel === "wb" ? "WB" : "Ozon"}: ${quote?.missing?.join(", ") || "расчёт недоступен"}`,
              );
          }
        }
      } catch (error) {
        if (cancelled) return;
        next.error = `Не удалось рассчитать цены: ${(error as Error).message}`;
      }
      if (!cancelled) {
        setResult(next);
        onCalculated(key, { wb: next.wb, ozon: next.ozon });
      }
    }, 350);
    return () => {
      cancelled = true;
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [key, enabled, revision, onCalculated]);
  return {
    result: result?.key === key && result.revision === revision ? result : null,
    retry: () => setRevision((value) => value + 1),
  };
}
