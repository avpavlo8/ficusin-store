import type { MarketplaceReturn } from "./adminTypes";
import { receiptLabel, type ReturnCondition } from "./returnWorkspace";
export function ReturnStateIcon({ condition }: { condition: ReturnCondition }) {
  const paths = {
    inspection: "M12 7v5m0 4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0",
    ready: "m5 12 4 4 10-10",
    restoring: "M20 8a8 8 0 1 0 1 6M20 3v5h-5",
    dead: "m6 6 12 12M18 6 6 18",
  };
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={paths[condition]} />
    </svg>
  );
}
export function ReturnReceiptBadge({
  item,
}: {
  item: Pick<MarketplaceReturn, "receiptStatus" | "receiptExternalUrl">;
}) {
  return (
    <span className={`return-receipt-badge receipt-${item.receiptStatus}`}>
      {receiptLabel(item)}
    </span>
  );
}
