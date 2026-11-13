import { stars } from "./types";

export function PurchaseReviewSummary({
  rating,
  count,
  onOpenReviews,
}: {
  rating: number;
  count: number;
  onOpenReviews: () => void;
}) {
  return (
    <>
      {count > 0 && (
        <span
          className="purchase-review-stars"
          role="img"
          aria-label={`Оценка ${rating.toFixed(1)} из 5`}
        >
          {stars(rating)}
        </span>
      )}
      <button className="purchase-review-meta" type="button" onClick={onOpenReviews}>
        {count ? `${rating.toFixed(1)} · ${count} отзывов` : "Пока нет отзывов"}
      </button>
    </>
  );
}
