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

type CollectionSource = "loading" | "server" | "fallback";

// These definitions are only a graceful fallback for a server/database that
// has not exposed /api/v1/collections yet. As soon as the API returns the
// collections field (even an empty array), the backend becomes authoritative.
const legacyPresets: Preset[] = [
  { id: "bathroom", title: "Для ванной", match: (p) => p.placement === "bathroom" },
  { id: "dark", title: "Для тёмной комнаты", match: (p) => p.lightLevel === "low_light" },
  { id: "office", title: "Для офиса", match: (p) => p.placement === "office" },
  { id: "tall", title: "Вырастает высоким", match: (p) => p.heightClass === "high" },
  { id: "compact", title: "Компактные", match: (p) => p.heightClass === "low" },
  { id: "easy", title: "Неприхотливые", match: (p) => p.careLevel === "easy" },
  { id: "rare", title: "Редкий полив", match: (p) => p.watering === "rare" },
  { id: "sunny", title: "На солнечное окно", match: (p) => p.lightLevel === "sunny" },
  { id: "bedroom", title: "Для спальни", match: (p) => p.placement === "bedroom" },
  { id: "nursery", title: "В детскую", match: (p) => p.placement === "nursery" },
  { id: "pets", title: "Безопасно для питомцев", match: (p) => p.petSafety === "safe" },
];

// StorefrontPage uses this reference only as the legacy matching fallback for
// a collection URL. Collection navigation itself is deliberately link-based:
// a collection is page context, not a multi-select filter.
export const presets: Preset[] = [...legacyPresets];

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

const legacyVisualOrder = ["dark", "easy", "pets", "bathroom", "office", "tall", "compact", "rare", "bedroom"];

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
        const collections = data.collections;
        setServerCollections(collections);
        setSource("server");
        presets.splice(0, presets.length, ...collections.map(serverPreset));
      })
      .catch(() => {
        if (!alive) return;
        setServerCollections([]);
        setSource("fallback");
        presets.splice(0, presets.length, ...legacyPresets);
      });
    return () => { alive = false; };
  }, []);

  const shown = useMemo(() => {
    if (source === "loading") return [];
    if (source === "server") {
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
    }

    return legacyVisualOrder.map((id) => {
      const preset = legacyPresets.find((item) => item.id === id)!;
      return {
        preset,
        image: collectionImages[id] || "/assets/redesign/collection-easy-4k.webp",
        count: products.filter(preset.match).length,
        note: "",
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
