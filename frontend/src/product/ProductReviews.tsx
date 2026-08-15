import { useEffect, useMemo, useRef, useState } from "react";
import type { ProductReview } from "./types";
import { stars } from "./types";

const filePayload = (file: File) => new Promise<{ contentType: string; data: string }>((resolve, reject) => {
  const reader = new FileReader();
  reader.onload = () => resolve({ contentType: file.type, data: String(reader.result).split(",")[1] || "" });
  reader.onerror = reject;
  reader.readAsDataURL(file);
});
const ratingLabels = ["", "Плохо", "Есть проблемы", "Нормально", "Хорошо", "Отлично"];

export function ProductReviews({ slug, rating, count, reviews }: { slug: string; rating: number; count: number; reviews: ProductReview[] }) {
  const [reviewRating, setReviewRating] = useState(0);
  const [hoveredRating, setHoveredRating] = useState(0);
  const [reviewText, setReviewText] = useState("");
  const [reviewMedia, setReviewMedia] = useState<File[]>([]);
  const [notice, setNotice] = useState("");
  const [sending, setSending] = useState(false);
  const editor = useRef<HTMLDivElement>(null);
  const previewURLs = useMemo(() => reviewMedia.map((file) => ({ file, url: URL.createObjectURL(file) })), [reviewMedia]);
  useEffect(() => () => previewURLs.forEach(({ url }) => URL.revokeObjectURL(url)), [previewURLs]);

  const selectRating = (value: number) => {
    setReviewRating(value); setNotice("");
    window.requestAnimationFrame(() => editor.current?.scrollIntoView({ behavior: "smooth", block: "nearest" }));
  };
  const selectMedia = (files: FileList | null) => {
    const accepted = Array.from(files || []).filter((file) => file.type.startsWith("image/") || file.type === "video/mp4" || file.type === "video/webm");
    const next = [...reviewMedia, ...accepted].slice(0, 4);
    if (next.filter((file) => file.type.startsWith("video/")).length > 1) { setNotice("К отзыву можно прикрепить одно видео"); return; }
    if (next.some((file) => file.type.startsWith("image/") && file.size > 5 * 1024 * 1024) || next.some((file) => file.type.startsWith("video/") && file.size > 20 * 1024 * 1024)) { setNotice("Фото должно быть до 5 МБ, видео — до 20 МБ"); return; }
    setReviewMedia(next); setNotice("");
  };
  const submit = async () => {
    setSending(true); setNotice("");
    try {
      const photos = await Promise.all(reviewMedia.map(filePayload));
      const response = await fetch(`/api/v1/products/${encodeURIComponent(slug)}/reviews`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ rating: reviewRating, text: reviewText, photos }) });
      const body = await response.json() as { error?: string };
      if (!response.ok) throw new Error(body.error || "Не удалось отправить отзыв");
      setNotice("Спасибо! Отзыв отправлен на модерацию."); setReviewText(""); setReviewMedia([]); setReviewRating(0);
    } catch (error) { setNotice(error instanceof Error ? error.message : "Не удалось отправить отзыв"); }
    finally { setSending(false); }
  };
  const closeEditor = () => { setReviewRating(0); setReviewText(""); setReviewMedia([]); setNotice(""); };

  return <section className="pdp-reviews pdp-section" id="reviews">
    <header className="pdp-section-heading"><div><p className="eyebrow">Опыт покупателей</p><h2>Отзывы</h2></div>{count > 0 && <div className="review-summary"><strong>{rating.toFixed(1)}</strong><span>{stars(rating)}</span><small>{count} отзывов</small></div>}</header>
    <div className="review-compose"><div><strong>Как вам растение?</strong><p>Оцените покупку — после выбора звёзд откроется форма.</p></div><div className="review-rating-picker" role="radiogroup" aria-label="Оценка товара" onMouseLeave={() => setHoveredRating(0)}>{[1, 2, 3, 4, 5].map((value) => <button type="button" role="radio" aria-checked={reviewRating === value} aria-label={`${value} из 5`} className={value <= (hoveredRating || reviewRating) ? "active" : ""} onMouseEnter={() => setHoveredRating(value)} onFocus={() => setHoveredRating(value)} onBlur={() => setHoveredRating(0)} onClick={() => selectRating(value)} key={value}>★</button>)}</div><span className="review-rating-label" aria-live="polite">{ratingLabels[hoveredRating || reviewRating] || "Выберите от 1 до 5"}</span></div>
    {reviewRating > 0 && <div className="review-editor" ref={editor}><form onSubmit={(event) => { event.preventDefault(); void submit(); }}><div className="review-editor-heading"><div><span>{stars(reviewRating)}</span><strong>{ratingLabels[reviewRating]}</strong></div><button type="button" onClick={closeEditor} aria-label="Закрыть форму">×</button></div><label htmlFor="review-text">Расскажите о покупке</label><textarea id="review-text" autoFocus required minLength={10} maxLength={3000} rows={5} value={reviewText} onChange={(event) => setReviewText(event.target.value)} placeholder="Например: в каком состоянии приехало растение, как было упаковано…" />
      {previewURLs.length > 0 && <div className="review-media-preview">{previewURLs.map(({ file, url }, index) => <figure key={`${file.name}-${file.lastModified}`}>{file.type.startsWith("video/") ? <video src={url} controls preload="metadata" /> : <img src={url} alt="Предпросмотр вложения" />}<button type="button" onClick={() => setReviewMedia((current) => current.filter((_, itemIndex) => itemIndex !== index))} aria-label={`Удалить ${file.name}`}>×</button></figure>)}</div>}
      <div className="review-editor-actions"><label className="review-media-button"><input type="file" accept="image/jpeg,image/png,image/webp,video/mp4,video/webm" multiple onChange={(event) => { selectMedia(event.target.files); event.currentTarget.value = ""; }} /><span aria-hidden="true">＋</span>Фото или видео</label><small>До 4 файлов · одно видео до 20 МБ</small><button type="submit" disabled={sending}>{sending ? "Отправляем…" : "Отправить отзыв"}</button></div><p className="review-policy">Отзыв доступен после завершённой покупки и появится после модерации.</p>{notice && <p className="review-form-notice" role="status">{notice}</p>}</form></div>}
    <div className="review-feed">{reviews.length ? reviews.map((review) => { const media = review.media?.length ? review.media : review.photos.map((url) => ({ url, contentType: "image/jpeg" })); return <article key={review.id}><header><div><strong>{review.author}</strong>{review.verifiedPurchase && <small>Подтверждённая покупка</small>}</div><div><span>{stars(review.rating)}</span><time dateTime={review.date}>{new Date(review.date).toLocaleDateString("ru-RU")}</time></div></header><p>{review.text}</p>{media.length > 0 && <div className="review-photos">{media.map((item) => item.contentType.startsWith("video/") ? <video key={item.url} src={item.url} controls preload="metadata" /> : <img key={item.url} src={item.url} alt="Фото растения от покупателя" loading="lazy" />)}</div>}</article>; }) : <div className="reviews-empty"><strong>Здесь пока тихо</strong><p>Станьте первым покупателем, который расскажет об этом растении после получения заказа.</p></div>}</div>
  </section>;
}
