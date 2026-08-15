import { useState } from "react";
import type { ProductReview } from "./types";
import { stars } from "./types";

const filePayload = (file: File) => new Promise<{ contentType: string; data: string }>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve({ contentType: file.type, data: String(reader.result).split(",")[1] || "" }); reader.onerror = reject; reader.readAsDataURL(file); });

export function ProductReviews({ slug, rating, count, reviews }: { slug: string; rating: number; count: number; reviews: ProductReview[] }) {
  const [reviewRating, setReviewRating] = useState(5);
  const [reviewText, setReviewText] = useState("");
  const [reviewPhotos, setReviewPhotos] = useState<File[]>([]);
  const [notice, setNotice] = useState("");
  const [sending, setSending] = useState(false);
  const submit = async () => { setSending(true); setNotice(""); try { const photos = await Promise.all(reviewPhotos.map(filePayload)); const response = await fetch(`/api/v1/products/${encodeURIComponent(slug)}/reviews`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ rating: reviewRating, text: reviewText, photos }) }); const body = await response.json() as { error?: string }; if (!response.ok) throw new Error(body.error || "Не удалось отправить отзыв"); setNotice("Спасибо! Отзыв отправлен на модерацию."); setReviewText(""); setReviewPhotos([]); } catch (error) { setNotice(error instanceof Error ? error.message : "Не удалось отправить отзыв"); } finally { setSending(false); } };
  return <section className="pdp-reviews pdp-section" id="reviews"><header className="pdp-section-heading"><div><p className="eyebrow">Опыт покупателей</p><h2>Отзывы</h2></div>{count > 0 && <div className="review-summary"><strong>{rating.toFixed(1)}</strong><span>{stars(rating)}</span><small>{count} отзывов</small></div>}</header>
    <div className="reviews-layout"><div className="review-feed">{reviews.length ? reviews.map((review) => <article key={review.id}><header><div><strong>{review.author}</strong>{review.verifiedPurchase && <small>Подтверждённая покупка</small>}</div><div><span>{stars(review.rating)}</span><time dateTime={review.date}>{new Date(review.date).toLocaleDateString("ru-RU")}</time></div></header><p>{review.text}</p>{review.photos?.length > 0 && <div className="review-photos">{review.photos.map((photo) => <img key={photo} src={photo} alt="Фото растения от покупателя" loading="lazy" />)}</div>}</article>) : <div className="reviews-empty"><strong>Здесь пока тихо</strong><p>Станьте первым покупателем, который расскажет об этом растении после получения заказа.</p></div>}</div>
      <form className="review-form" onSubmit={(event) => { event.preventDefault(); void submit(); }}><p className="eyebrow">После покупки</p><h3>Оставить отзыв</h3><p>Форма доступна владельцу завершённого заказа. Все отзывы проходят модерацию.</p><label>Оценка<select value={reviewRating} onChange={(event) => setReviewRating(Number(event.target.value))}>{[5, 4, 3, 2, 1].map((value) => <option key={value} value={value}>{value} из 5</option>)}</select></label><label>Ваш опыт<textarea required minLength={10} maxLength={3000} rows={5} value={reviewText} onChange={(event) => setReviewText(event.target.value)} placeholder="Расскажите о состоянии растения, упаковке и доставке" /></label><label>Фотографии<input type="file" accept="image/jpeg,image/png,image/webp" multiple onChange={(event) => setReviewPhotos(Array.from(event.target.files || []).slice(0, 3))} /><small>До 3 файлов, JPEG, PNG или WebP</small></label><button type="submit" disabled={sending}>{sending ? "Отправляем…" : "Отправить на модерацию"}</button>{notice && <p className="review-form-notice" role="status">{notice}</p>}</form>
    </div>
  </section>;
}
