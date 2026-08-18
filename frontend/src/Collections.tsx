import { useRef } from "react";

// Подборки собираются из атрибутов товара, а не из отдельного списка:
// менеджер отмечает в панели «ванная», «затемнённое место», «высокий» — и
// подборка наполняется сама. Отдельный список пришлось бы вести руками
// вторым заходом, и он бы разошёлся с карточками через неделю.

export type Product = {
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
  icon: string;
  match: (product: Product) => boolean;
};

// Иконки нарочно тонкие и одного цвета: подборки стоят рядом с каталогом и
// не должны спорить с фотографиями растений.
const icons = {
  drop: "M12 3s5 5.5 5 9a5 5 0 0 1-10 0c0-3.5 5-9 5-9Z",
  moon: "M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z",
  case: "M3 8h18v11H3zM9 8V6a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2",
  tall: "M12 21V6m0 0-4 4m4-4 4 4",
  small: "M12 3v15m0 0-4-4m4 4 4-4",
  leaf: "M4 20c0-8 6-13 16-14 0 10-5 15-13 15H4Zm3-2c3-4 6-6 9-7",
  sun: "M12 6.5A5.5 5.5 0 1 0 12 17.5 5.5 5.5 0 0 0 12 6.5ZM12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4",
  bed: "M3 18v-6h18v6M3 12V8m0 4h6a3 3 0 0 1 3 3v-3h9",
  star: "m12 4 2.3 5 5.7.6-4.3 3.8 1.3 5.6L12 16l-5 3 1.3-5.6L4 9.6 9.7 9Z",
  paw: "M8.5 11a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm7 0a2 2 0 1 0 0-4 2 2 0 0 0 0 4ZM6 16.5C6 14.6 8.7 13 12 13s6 1.6 6 3.5S15.3 20 12 20s-6-1.6-6-3.5Z",
  water: "M4 12h9v5a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3v-5Zm9 1 5-3v7l-5-3M8 8V5",
};

export const presets: Preset[] = [
  { id: "bathroom", title: "Для ванной", icon: icons.drop, match: (p) => p.placement === "bathroom" },
  { id: "dark", title: "Для тёмной комнаты", icon: icons.moon, match: (p) => p.lightLevel === "low_light" },
  { id: "office", title: "Для офиса", icon: icons.case, match: (p) => p.placement === "office" },
  { id: "tall", title: "Вырастает высоким", icon: icons.tall, match: (p) => p.heightClass === "high" },
  { id: "compact", title: "Компактные", icon: icons.small, match: (p) => p.heightClass === "low" },
  { id: "easy", title: "Неприхотливые", icon: icons.leaf, match: (p) => p.careLevel === "easy" },
  { id: "rare", title: "Редкий полив", icon: icons.water, match: (p) => p.watering === "rare" },
  { id: "sunny", title: "На солнечное окно", icon: icons.sun, match: (p) => p.lightLevel === "sunny" },
  { id: "bedroom", title: "Для спальни", icon: icons.bed, match: (p) => p.placement === "bedroom" },
  { id: "nursery", title: "В детскую", icon: icons.star, match: (p) => p.placement === "nursery" },
  { id: "pets", title: "Безопасно для питомцев", icon: icons.paw, match: (p) => p.petSafety === "safe" },
];

const visualPresets = [
  // Первые три — утверждённые пользователем главные подборки. Они являются
  // обычными фильтрами, а не декоративными ссылками: клик применяет match.
  { id: "dark", image: "/assets/redesign/collection-dark-4k.webp" },
  { id: "easy", image: "/assets/redesign/collection-easy-4k.webp" },
  { id: "pets", image: "/assets/redesign/collection-pets-4k.webp" },
  { id: "bathroom", image: "/assets/redesign/filters/bathroom-wall-v2.webp" },
  { id: "office", image: "/assets/redesign/filters/office-wall-v2.webp" },
  { id: "tall", image: "/assets/redesign/filters/tall-wall-v2.webp" },
  { id: "compact", image: "/assets/redesign/filters/compact-wall-v2.webp" },
  { id: "rare", image: "/assets/redesign/filters/rare-water-wall-v2.webp" },
  { id: "bedroom", image: "/assets/redesign/filters/bedroom-wall-v2.webp" },
];

export function CollectionStrip<T extends Product>({
  products,
  active,
  onPick,
}: {
  products: T[];
  active: ReadonlySet<string>;
  onPick: (id: string) => void;
}) {
  // Пустые подборки не показываем совсем: плитка, ведущая в «ничего не
  // нашли», хуже, чем её отсутствие.
  const rail = useRef<HTMLDivElement>(null);
  const shown = visualPresets.map(({ id, image }) => {
    const preset = presets.find((item) => item.id === id)!;
    return { preset, image, count: products.filter(preset.match).length };
  });
  if (shown.length === 0) return null;

  return (
    <section className="storefront-preset-carousel" aria-label="Подборки по помещению">
      <button className="preset-arrow previous" type="button" onClick={() => rail.current?.scrollBy({ left: -420, behavior: "smooth" })} aria-label="Предыдущие подборки">←</button>
      <div className="storefront-presets" role="list" ref={rail}>
      {shown.map(({ preset, image, count }, index) => (
        <button
          key={preset.id}
          type="button"
          role="listitem"
          className={active.has(preset.id) ? "preset active" : "preset"}
          aria-pressed={active.has(preset.id)}
          onClick={() => onPick(preset.id)}
          title={`${preset.title} — ${count}`}
          style={{ backgroundImage: `url('${image}')` }}
        >
          <span className="preset-number">0{index + 1}</span>
          <span className="preset-title">{preset.title}</span>
          <span className="preset-count">{count} растений</span>
          <span className="preset-go" aria-hidden="true">→</span>
        </button>
      ))}
      </div>
      <button className="preset-arrow next" type="button" onClick={() => rail.current?.scrollBy({ left: 420, behavior: "smooth" })} aria-label="Следующие подборки">→</button>
    </section>
  );
}
