import { type FormEvent, useCallback, useEffect, useState } from "react";
import { useModalFocus } from "./lib/useModalFocus";

type Review = {
  id: number;
  product: string;
  slug: string;
  rating: number;
  text: string;
  status: "pending" | "published" | "rejected";
  createdAt: string;
};
const labels = { pending: "На проверке", published: "Опубликован", rejected: "Скрыт" };

function ReviewEditor({
  review,
  onClose,
  onSaved,
}: {
  review: Review;
  onClose: () => void;
  onSaved: (review: Review) => void;
}) {
  const [draft, setDraft] = useState(review);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const close = useCallback(() => {
    if (!saving) onClose();
  }, [saving, onClose]);
  const modal = useModalFocus(close);
  useEffect(() => {
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, []);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/v1/account/reviews/${draft.id}`, {
        method: "PATCH",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rating: draft.rating, text: draft.text.trim() }),
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => ({}))) as { error?: string };
        throw new Error(body.error || "Не удалось сохранить отзыв. Попробуйте ещё раз.");
      }
      onSaved({ ...draft, text: draft.text.trim() });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось сохранить отзыв");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div
      className="account-review-dialog"
      ref={modal as React.RefObject<HTMLDivElement>}
      role="dialog"
      aria-modal="true"
      aria-labelledby="review-editor-title"
      onClick={(event) => {
        if (event.target === event.currentTarget) close();
      }}
    >
      <form onSubmit={(event) => void save(event)} aria-busy={saving}>
        <header>
          <div>
            <h2 id="review-editor-title">Изменить отзыв</h2>
            <p>{review.product}</p>
          </div>
          <button
            type="button"
            disabled={saving}
            onClick={close}
            aria-label="Закрыть редактор отзыва"
          >
            ×
          </button>
        </header>
        <fieldset className="review-rating-field">
          <legend>Ваша оценка</legend>
          <div className="account-review-rating">
            {[1, 2, 3, 4, 5].map((value) => (
              <label key={value}>
                <input
                  type="radio"
                  name="rating"
                  value={value}
                  checked={draft.rating === value}
                  disabled={saving}
                  onChange={() => setDraft({ ...draft, rating: value })}
                />
                <span aria-hidden="true" className={value <= draft.rating ? "active" : ""}>
                  ★
                </span>
                <span className="sr-only">{value} из 5</span>
              </label>
            ))}
          </div>
        </fieldset>
        <label>
          Текст отзыва
          <textarea
            required
            minLength={10}
            maxLength={3000}
            rows={6}
            disabled={saving}
            value={draft.text}
            onChange={(event) => setDraft({ ...draft, text: event.target.value })}
          />
          <small>От 10 до 3000 символов</small>
        </label>
        {error && (
          <p className="auth-error" role="alert">
            {error}
          </p>
        )}
        <div className="form-actions">
          <button className="primary-button" disabled={saving}>
            {saving ? "Сохраняем…" : "Сохранить отзыв"}
          </button>
          <button type="button" className="secondary-button" disabled={saving} onClick={close}>
            Отмена
          </button>
        </div>
      </form>
    </div>
  );
}

export default function AccountReviews() {
  const [items, setItems] = useState<Review[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Review | null>(null);
  const [saved, setSaved] = useState(false);
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch("/api/v1/account/reviews", {
        credentials: "same-origin",
        signal,
      });
      const body = (await response.json()) as { reviews?: Review[]; error?: string };
      if (!response.ok) throw new Error(body.error || "Не удалось загрузить отзывы");
      if (!signal?.aborted) {
        setItems(body.reviews || []);
        setError("");
      }
    } catch (reason) {
      if (!signal?.aborted)
        setError(reason instanceof Error ? reason.message : "Не удалось загрузить отзывы");
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);
  const closeEditor = useCallback(() => setEditing(null), []);

  return (
    <div className="account-reviews">
      <div className="account-title">
        <div>
          <h1>Мои отзывы</h1>
        </div>
      </div>
      {loading && <p role="status">Загружаем отзывы…</p>}
      {error && (
        <div className="auth-error" role="alert">
          <p>{error}</p>
          <button
            type="button"
            className="secondary-button"
            onClick={() => {
              setLoading(true);
              setError("");
              void load();
            }}
          >
            Повторить загрузку
          </button>
        </div>
      )}
      {saved && (
        <p className="auth-saved" role="status">
          Отзыв сохранён
        </p>
      )}
      {!loading &&
        !error &&
        (items.length ? (
          items.map((item) => (
            <article key={item.id}>
              <header>
                <div>
                  <a href={`/product/${encodeURIComponent(item.slug)}#reviews`}>{item.product}</a>
                  <time dateTime={item.createdAt}>
                    {new Date(item.createdAt).toLocaleDateString("ru-RU", {
                      day: "numeric",
                      month: "long",
                      year: "numeric",
                    })}
                  </time>
                </div>
                <span className={`admin-pill ${item.status}`}>{labels[item.status]}</span>
              </header>
              <div
                className="account-review-stars"
                role="img"
                aria-label={`Оценка: ${item.rating} из 5`}
              >
                {"★".repeat(item.rating)}
                {"☆".repeat(5 - item.rating)}
              </div>
              <p>{item.text}</p>
              {item.status === "published" && (
                <button
                  type="button"
                  onClick={(event) => {
                    event.currentTarget.focus();
                    setSaved(false);
                    setEditing(item);
                  }}
                >
                  Редактировать
                </button>
              )}
            </article>
          ))
        ) : (
          <div className="orders-empty">
            <h2>Отзывов пока нет</h2>
            <p>Оставить отзыв можно на странице товара после выполненного заказа.</p>
            <a className="primary-button" href="/#catalog">
              Выбрать растение
            </a>
          </div>
        ))}
      {editing && (
        <ReviewEditor
          review={editing}
          onClose={closeEditor}
          onSaved={(review) => {
            setItems((current) => current.map((item) => (item.id === review.id ? review : item)));
            setEditing(null);
            setSaved(true);
          }}
        />
      )}
    </div>
  );
}
