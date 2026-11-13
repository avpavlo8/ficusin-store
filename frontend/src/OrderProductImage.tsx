import { useEffect, useState } from "react";
const placeholder = "/assets/product-photo-pending-graphite-v1.svg";
type Stage = "primary" | "retry" | "fallback" | "placeholder";
export function OrderProductImage({
  item,
  className,
}: {
  item: { image?: string; imageFallback?: string };
  className?: string;
}) {
  const source = item.image || placeholder;
  const [failure, setFailure] = useState<{ source: string; stage: Stage; waiting: boolean }>({
    source: "",
    stage: "primary",
    waiting: false,
  });
  const current =
    failure.source === source ? failure : { source, stage: "primary" as Stage, waiting: false };
  const fallback = item.imageFallback && item.imageFallback !== source ? item.imageFallback : "";
  const src =
    current.stage === "placeholder"
      ? placeholder
      : current.stage === "fallback"
        ? fallback
        : source;
  useEffect(() => {
    if (!current.waiting) return;
    const timer = setTimeout(() => setFailure({ source, stage: "retry", waiting: false }), 700);
    return () => clearTimeout(timer);
  }, [source, current.waiting]);
  return (
    <img
      key={`${source}:${current.stage}`}
      className={className}
      src={src}
      alt=""
      loading="lazy"
      onError={() => {
        if (src === placeholder || current.waiting) return;
        if (current.stage === "primary") setFailure({ source, stage: "primary", waiting: true });
        else if (current.stage === "retry")
          setFailure({ source, stage: fallback ? "fallback" : "placeholder", waiting: false });
        else setFailure({ source, stage: "placeholder", waiting: false });
      }}
    />
  );
}
