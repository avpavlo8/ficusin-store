import type {
  ProcurementActionBatch,
  ProcurementRecommendation,
  SalesSyncStatus,
} from "../adminTypes";

export function bulkSelectableRecommendations(
  items: ProcurementRecommendation[],
  supplierId?: number,
) {
  const available = items.filter((item) => item.status === "recommended" && item.supplierId > 0);
  const targetSupplierId = supplierId ?? available[0]?.supplierId;
  return available.filter((item) => item.supplierId === targetSupplierId);
}

export function recommendedPackageCount(
  item?: Pick<
    ProcurementRecommendation,
    "quantityKnown" | "suggestedQty" | "orderMultiple" | "minimumOrderQty"
  >,
) {
  if (item?.quantityKnown === false) return "";
  if (!item) return "1";
  return String(
    Math.max(
      1,
      Math.ceil(
        Math.max(item.suggestedQty || 0, item.minimumOrderQty || 1) /
          Math.max(1, item.orderMultiple || 1),
      ),
    ),
  );
}

export function salesDataProblems(channels: SalesSyncStatus[]) {
  return channels.filter(
    (channel) =>
      channel.status === "error" ||
      Boolean(channel.lastError) ||
      channel.freshness === "stale" ||
      channel.rowsUnlinked > 0 ||
      (channel.status !== "disabled" && !channel.lastSuccessAt),
  );
}

// An older failed attempt does not describe the result of a later successful
// action for the same line and channel. Cancelled batches are history only.
export function currentActionFailures(batches: ProcurementActionBatch[]) {
  const latest = new Map<string, ProcurementActionBatch["items"][number]>();
  for (const batch of [...batches].sort(
    (a, b) => a.createdAt.localeCompare(b.createdAt) || a.id - b.id,
  )) {
    if (batch.status === "cancelled") continue;
    for (const item of batch.items) {
      if (["draft", "skipped", "not_configured"].includes(item.status)) continue;
      const target =
        item.lineId > 0 ? item.lineId : item.productCode || item.externalArticle || item.id;
      latest.set(`${batch.kind}:${item.channel}:${target}`, item);
    }
  }
  return [...latest.values()].filter((item) => item.status === "failed");
}
