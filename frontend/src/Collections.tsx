import { useEffect, useMemo, useRef, useState } from "react";

export type Product = {
  image?: string;
  collections?: string[];
  lightLevel?: string;
  heightClass?: string;
  petSafety?: string;
  careLevel?: string;
  placement?: string;
  watering?: string;
};

export type Preset = {
  id: string;
  title: string;
  match: (product: Product) => boolean;
};

type CollectionDefinition = {
  slug: string;
  title: string;
  note: string;
  coverUrl?: string;
  count: number;
};

type CollectionSource = "loading" | "server" | "error";

const collectionImages: Record<string, string> = {
  dark: "/assets/redesign/collection-dark-4k.webp",
  shade: "/assets/redesign/collection-dark-4k.webp",
  easy: "/assets/redesign/collection-easy-4k.webp",
  pets: "/assets/redesign/collection-pets-4k.webp",
  "pet-safe": "/assets/redesign/collection-pets-4k.webp",
  bathroom: "/assets/redesign/filters/bathroom-wall-v2.webp",
  office: "/assets/redesign/filters/office-wall-v2.webp",
  tall: "/assets/redesign/filters/tall-wall-v2.webp",
  compact: "/assets/redesign/filters/compact-wall-v2.webp",
  rare: "/assets/redesign/filters/rare-water-wall-v2.webp",
  bedroom: "/assets/redesign/filters/bedroom-wall-v2.webp",
};

function serverPreset(collection: CollectionDefinition): Preset {
  return {
    id: collection.slug,
    title: collection.title,
    match: (product) => product.collections?.includes(collection.slug) === true,
  };
}

export function CollectionStrip<T extends Product>({
  products,
  activeSlug,
}: {
  products: T[];
  activeSlug?: string;
}) {
  const rail = useRef<HTMLDivElement>(null);
  const [serverCollections, setServerCollections] = useState<CollectionDefinition[]>([]);
  const [source, setSource] = useState<CollectionSource>("loading");
  const [scrollEdges, setScrollEdges] = useState({ previous: false, next: false });

  useEffect(() => {
    const element = rail.current;
    if (!element) return;
    const update = () => setScrollEdges({
      previous: element.scrollLeft > 2,
      next: element.scrollLeft + element.clientWidth < element.scrollWidth - 2,
    });
    update();
    element.addEventListener("scroll", update, { passive: true });
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => {
      element.removeEventListener("scroll", update);
      observer.disconnect();
    };
  }, [source, serverCollections.length]);

  const scroll = (direction: -1 | 1) => {
    const element = rail.current;
    const card = element?.querySelector<HTMLElement>(".preset");
    if (!element || !card) return;
    const gap = Number.parseFloat(getComputedStyle(element).columnGap) || 0;
    element.scrollBy({ left: direction * (card.offsetWidth + gap), behavior: "smooth" });
  };

  useEffect(() => {
    let alive = true;
    fetch("/api/v1/collections")
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("collections unavailable")))
      .then((data: { collections?: CollectionDefinition[] }) => {
        if (!alive) return;
        if (!Array.isArray(data.collections)) throw new Error("collections contract unavailable");
        setServerCollections(data.collections);
        setSource("server");
      })
      .catch(() => {
        if (!alive) return;
        setServerCollections([]);
        setSource("error");
      });
    return () => { alive = false; };
  }, []);

  const shown = useMemo(() => {
    if (source !== "server") return [];
    return serverCollections.map((collection) => {
      const preset = serverPreset(collection);
      const firstProductImage = products.find((product) => preset.match(product))?.image;
      return {
        preset,
        image: collection.coverUrl || collectionImages[collection.slug] || firstProductImage || "/assets/redesign/collection-easy-4k.webp",
        count: collection.count,
        note: collection.note,
      };
    }).filter((item) => item.count > 0);
  }, [products, serverCollections, source]);

  if (shown.length === 0) return null;
  const canScrollNext = scrollEdges.next || (!scrollEdges.previous && shown.length > 3);

  return (
    <section className="storefront-preset-carousel" aria-label="Подборки растений">
      <button className="preset-arrow previous" type="button" onClick={() => scroll(-1)} aria-label="Предыдущие подборки" disabled={!scrollEdges.previous}>←</button>
      <div className={`storefront-presets${scrollEdges.previous ? " can-scroll-previous" : ""}${canScrollNext ? " can-scroll-next" : ""}`} role="list" ref={rail}>
      {shown.map(({ preset, image, count, note }, index) => (
        <a
          key={preset.id}
          role="listitem"
          href={`/collections/${encodeURIComponent(preset.id)}`}
          className={activeSlug === preset.id ? "preset active" : "preset"}
          aria-current={activeSlug === preset.id ? "page" : undefined}
          title={note || `${preset.title} — ${count}`}
          style={{ backgroundImage: `url('${image}')` }}
        >
          <span className="preset-number">{String(index + 1).padStart(2, "0")}</span>
          <span className="preset-title">{preset.title}</span>
          <span className="preset-count">{count} растений</span>
          <span className="preset-go" aria-hidden="true">→</span>
        </a>
      ))}
      </div>
      <button className="preset-arrow next" type="button" onClick={() => scroll(1)} aria-label="Следующие подборки" disabled={!canScrollNext}>→</button>
    </section>
  );
}
