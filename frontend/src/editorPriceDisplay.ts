// Match the existing Go executor: WB receives whole-ruble base price and a
// truncated integer discount, which need not reproduce the calculated target.
export function wbPricePresentation(priceMinor: number, compareAtMinor?: number | null) {
  const hasHigherCompare = compareAtMinor != null && compareAtMinor > priceMinor;
  const baseRub = Math.trunc((hasHigherCompare ? compareAtMinor : priceMinor) / 100);
  if (baseRub <= 0) return null;
  const discountPercent =
    (hasHigherCompare ? Math.trunc((1 - priceMinor / 100 / baseRub) * 100) : 0) || 0;
  return { baseRub, discountPercent, effectiveMinor: baseRub * (100 - discountPercent) };
}
