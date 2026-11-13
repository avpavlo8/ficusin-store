import { useEffect, useMemo, useState } from "react";
import type { ProductReview } from "./types";
import { stars } from "./types";
import { PurchaseReviewSummary } from "./PurchaseReviewSummary";

const ratingLabels = ["", "Плохо", "Есть проблемы", "Нормально", "Хорошо", "Отлично"];

export function ReviewComposer({
  slug,
  rating,
  count,
  onOpenReviews,
  open,
  onOpenChange,
}: {
  slug: string;
  rating: number;
  count: number;
  onOpenReviews: () => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [selected, setSelected] = useState(5);
  const setOpen = onOpenChange;
  const [text, setText] = useState("");
  const [media, setMedia] = useState<File[]>([]);
  const [notice, setNotice] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [sending, setSending] = useState(false);
  const previews = useMemo(
    () =>
      media.map((file) => {
        const objectURL = URL.createObjectURL(file);
        const parsed = new URL(objectURL);
        // Preview only locally generated blob URLs, never document text or external URLs.
        const url =
          parsed.protocol === "blob:" && parsed.origin === window.location.origin
            ? parsed.href
            : undefined;
        return { file, url, objectURL };
      }),
    [media],
  );
  useEffect(
    () => () => previews.forEach(({ objectURL }) => URL.revokeObjectURL(objectURL)),
    [previews],
  );
  useEffect(() => {
    if (!open) return;
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", key);
    document.body.classList.add("modal-open");
    return () => {
      document.removeEventListener("keydown", key);
      document.body.classList.remove("modal-open");
    };
  }, [open, setOpen]);
  const addMedia = (files: FileList | null) => {
    const accepted = Array.from(files || []).filter((file) =>
      ["image/jpeg", "image/png", "image/webp", "video/mp4", "video/webm"].includes(file.type),
    );
    const next = [...media, ...accepted].slice(0, 4);
    if (next.filter((file) => file.type.startsWith("video/")).length > 1) {
      setNotice("Можно прикрепить одно видео");
      return;
    }
    if (
      next.some((file) => file.type.startsWith("image/") && file.size > 5 * 1024 * 1024) ||
      next.some((file) => file.type.startsWith("video/") && file.size > 20 * 1024 * 1024)
    ) {
      setNotice("Фото — до 5 МБ, видео — до 20 МБ");
      return;
    }
    setMedia(next);
    setNotice("");
  };
  const submit = async () => {
    setSending(true);
    setNotice("");
    try {
      const photos = media.length
        ? await Promise.all(media.map((await import("./review-files")).filePayload))
        : [];
      const response = await fetch(`/api/v1/products/${encodeURIComponent(slug)}/reviews`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rating: selected, text, photos }),
      });
      const body = (await response.json()) as { error?: string };
      if (!response.ok) throw new Error(body.error || "Не удалось отправить отзыв");
      setText("");
      setMedia([]);
      setOpen(false);
      setSubmitted(true);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Не удалось отправить отзыв");
    } finally {
      setSending(false);
    }
  };
  return (
    <div className="purchase-review">
      <PurchaseReviewSummary rating={rating} count={count} onOpenReviews={onOpenReviews} />
      {submitted && (
        <p className="review-submit-success" role="status">
          Спасибо за отзыв.
        </p>
      )}
      {open && (
        <div
          className="review-modal"
          role="dialog"
          aria-modal="true"
          aria-labelledby="review-modal-title"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setOpen(false);
          }}
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            <header>
              <div>
                <span>{stars(selected)}</span>
                <h2 id="review-modal-title">{ratingLabels[selected]}</h2>
                <p>
                  Расскажите о товаре и доставке. Отзыв доступен после выполнения заказа — войдите в
                  аккаунт, с которого покупали.
                </p>
              </div>
              <button type="button" onClick={() => setOpen(false)} aria-label="Закрыть">
                ×
              </button>
            </header>
            <div className="review-modal-rating" role="radiogroup" aria-label="Ваша оценка">
              {[1, 2, 3, 4, 5].map((value) => (
                <button
                  type="button"
                  role="radio"
                  aria-checked={selected === value}
                  aria-label={`${value} из 5`}
                  onClick={() => setSelected(value)}
                  key={value}
                >
                  {value <= selected ? "★" : "☆"}
                </button>
              ))}
            </div>
            <label htmlFor="review-modal-text">Ваш отзыв</label>
            <textarea
              id="review-modal-text"
              autoFocus
              required
              minLength={10}
              maxLength={3000}
              rows={6}
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="В каком состоянии приехало растение? Как прошло получение?"
            />
            {previews.length > 0 && (
              <div className="review-media-preview">
                {previews.map(({ file, url }, index) => (
                  <figure key={`${file.name}-${file.lastModified}`}>
                    {file.type.startsWith("video/") ? (
                      <video src={url} controls preload="metadata" />
                    ) : (
                      <img src={url} alt="Предпросмотр" />
                    )}
                    <button
                      type="button"
                      onClick={() =>
                        setMedia((current) => current.filter((_, itemIndex) => itemIndex !== index))
                      }
                      aria-label={`Удалить ${file.name}`}
                    >
                      ×
                    </button>
                  </figure>
                ))}
              </div>
            )}
            <div className="review-modal-actions">
              <label className="review-media-button">
                <input
                  type="file"
                  accept="image/jpeg,image/png,image/webp,video/mp4,video/webm"
                  multiple
                  onChange={(event) => {
                    addMedia(event.target.files);
                    event.currentTarget.value = "";
                  }}
                />
                <span>＋</span>Добавить фото или видео
              </label>
              <button type="submit" disabled={sending}>
                {sending ? "Отправляем…" : "Отправить отзыв"}
              </button>
            </div>
            <small>До 4 файлов, одно видео.</small>
            {notice && (
              <p className="review-form-notice" role="alert">
                {notice}
              </p>
            )}
          </form>
        </div>
      )}
    </div>
  );
}

export function ProductReviews({
  reviews,
  onWriteReview,
}: {
  reviews: ProductReview[];
  onWriteReview: () => void;
}) {
  const [sort, setSort] = useState<"new" | "high" | "low">("new");
  const sorted = useMemo(
    () =>
      [...reviews].sort((left, right) =>
        sort === "high"
          ? right.rating - left.rating
          : sort === "low"
            ? left.rating - right.rating
            : new Date(right.date).getTime() - new Date(left.date).getTime(),
      ),
    [reviews, sort],
  );
  return (
    <section className="pdp-reviews pdp-section pdp-info-card" id="reviews">
      <header className="pdp-section-heading">
        <div>
          <p className="eyebrow">Опыт покупателей</p>
          <h2>Отзывы покупателей</h2>
        </div>
        <div className="review-feed-actions">
          <button className="review-write-button" type="button" onClick={onWriteReview}>
            Написать отзыв
          </button>
          {reviews.length > 1 && (
            <label className="review-sort">
              Сортировка
              <select value={sort} onChange={(event) => setSort(event.target.value as typeof sort)}>
                <option value="new">Сначала новые</option>
                <option value="high">С высокой оценкой</option>
                <option value="low">С низкой оценкой</option>
              </select>
            </label>
          )}
        </div>
      </header>
      <div className="review-feed">
        {sorted.length ? (
          sorted.map((review) => {
            const items = review.media?.length
              ? review.media
              : (review.photos || []).map((url) => ({ url, contentType: "image/jpeg" }));
            return (
              <article key={review.id}>
                <header>
                  <div>
                    <strong>{review.author}</strong>
                    {review.verifiedPurchase && <small>Подтверждённая покупка</small>}
                  </div>
                  <div>
                    <span>{stars(review.rating)}</span>
                    <time dateTime={review.date}>
                      {new Date(review.date).toLocaleDateString("ru-RU")}
                    </time>
                  </div>
                </header>
                <p>{review.text}</p>
                {items.length > 0 && (
                  <div className="review-photos">
                    {items.map((item) =>
                      item.contentType.startsWith("video/") ? (
                        <video key={item.url} src={item.url} controls preload="metadata" />
                      ) : (
                        <img
                          key={item.url}
                          src={item.url}
                          alt="Фото растения от покупателя"
                          loading="lazy"
                        />
                      ),
                    )}
                  </div>
                )}
              </article>
            );
          })
        ) : (
          <div className="reviews-empty">
            <strong>Здесь пока тихо</strong>
            <p>Уже получили заказ? Поделитесь впечатлениями о товаре и доставке.</p>
          </div>
        )}
      </div>
    </section>
  );
}
