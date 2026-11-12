import { useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import type { MarketplaceChannel } from "./catalogEditorChannels";
import type { RemoteMarketplaceCard } from "./marketplaceAutofill";

export function MarketplaceSellerArticle({
  channel,
  value,
  fallback,
  linked,
  productId,
  variantId,
  onChange,
  onResolved,
}: {
  channel: "wb" | "ozon";
  value: MarketplaceChannel;
  fallback: string;
  linked: boolean;
  productId?: number;
  variantId?: number;
  onChange: (value: MarketplaceChannel) => void;
  onResolved: (card: RemoteMarketplaceCard) => void;
}) {
  const title = channel === "wb" ? "Wildberries" : "Ozon";
  const article = value.article ?? (linked ? value.linkedArticle || "" : fallback);
  const [status, setStatus] = useState<{
    article: string;
    message: string;
    error: boolean;
    found?: boolean;
  }>({ article: "", message: "", error: false });
  const generation = useRef(0);
  const previousArticle = useRef(article);
  const resolved = useRef(onResolved);
  useEffect(() => {
    resolved.current = onResolved;
  });
  useEffect(() => {
    const edited = previousArticle.current.trim() !== "" && previousArticle.current !== article;
    previousArticle.current = article;
    if (!linked || (!value.replaceLink && !edited) || !article.trim() || !productId || !variantId)
      return;
    let active = true;
    const requestGeneration = ++generation.current;
    const timer = setTimeout(() => {
      setStatus({ article, message: "Проверяем артикул…", error: false });
      api<{ found: boolean; card: RemoteMarketplaceCard | null }>(
        `/api/v1/admin/products/${productId}/marketplace-cards?variantId=${variantId}&channel=${channel}&article=${encodeURIComponent(article.trim())}`,
      )
        .then((data) => {
          if (!active || requestGeneration !== generation.current) return;
          if (data.card) resolved.current(data.card);
          setStatus({
            article,
            found: data.found,
            error: false,
            message: data.found
              ? "Карточка найдена. Новая связь применится при сохранении."
              : "Артикул свободен. При сохранении создадим новую карточку.",
          });
        })
        .catch((e) => {
          if (active && requestGeneration === generation.current)
            setStatus({ article, message: (e as Error).message, error: true });
        });
    }, 600);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [article, value.replaceLink, productId, variantId, channel, linked]);
  const found =
    !value.replaceLink && linked ? true : status.article === article ? status.found : undefined;
  const fresh = (!linked && !value.replaceLink) || found === false;
  return (
    <div className="editor-existing-card">
      <label>
        Артикул
        <span className="editor-article-control">
          <input
            aria-label={`Артикул ${title}`}
            value={article}
            onChange={(e) => {
              generation.current++;
              onChange({
                ...value,
                article: e.target.value,
                replaceLink: linked && e.target.value.trim() !== (value.linkedArticle || ""),
              });
            }}
          />
          {article.trim() && found === true && (
            <span
              className="editor-article-linked"
              role="status"
              aria-label={`Карточка ${title} найдена`}
              title="Карточка найдена"
            >
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="m5 12 4 4L19 6" />
              </svg>
            </span>
          )}
          {article.trim() && fresh && (
            <span
              className="editor-article-new"
              role="status"
              aria-label={`Будет создана новая карточка ${title}`}
              title="Будет создана новая карточка"
            >
              NEW
            </span>
          )}
          {article.trim() && value.replaceLink && found === undefined && !status.error && (
            <span className="editor-article-pending" role="status" aria-label="Проверяем артикул">
              …
            </span>
          )}
        </span>
      </label>
      {value.replaceLink && status.article === article && status.error && (
        <p className="editor-secondary" role={status.error ? "alert" : "status"}>
          {status.message}
        </p>
      )}
    </div>
  );
}
