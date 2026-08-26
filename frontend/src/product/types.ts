export type CatalogProduct = { id: string; sku: string; name: string; latin: string; price: number; image: string; size: string; stock: number };
export type ProductAttribute = {
  code: string; name: string; unit?: string; value: string | number | boolean | string[];
  displayValue?: string | number | string[]; options?: string[]; optionLabels?: Record<string, string>;
  badge: boolean; filterable?: boolean; summaryPosition?: number; showInCharacteristics?: boolean;
};
export type ProductVariant = {
  id: number; sku: string; label: string; price: number; stock: number;
  heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number; images: string[];
  attributes: ProductAttribute[];
};
export type FAQItem = { question: string; answer: string };
export type PlantPassportData = {
  origin?: string; lighting?: string; watering?: string; humidity?: string; temperature?: string;
  soil?: string; fertilizer?: string; repotting?: string; careDifficulty?: string; growthRate?: string;
  matureSize?: string; toxicity?: string; problems?: string; pests?: string; faq?: FAQItem[];
};
export type ProductReview = { id: number; rating: number; text: string; author: string; date: string; verifiedPurchase: boolean; photos: string[] | null; media?: Array<{ url: string; contentType: string }> | null };
export type ProductDetail = {
  id: string; name: string; latin: string; shortDescription: string; description: string;
  careInstructions: string; images: string[]; variants: ProductVariant[]; recommendations: CatalogProduct[];
  catalogSection: string; plantKind?: string; lightLevel?: string; watering?: string;
  heightClass?: string; careLevel?: string; placement?: string; petSafety?: string; growthHabit?: string;
  passport: PlantPassportData; importantWarnings: string[]; rating: number; reviewsCount: number; reviews: ProductReview[]; attributes: ProductAttribute[];
};

// Attribute codes are stable machine values. The API now carries labels for
// owner-defined options; this fallback only covers the seeded catalogue.
export const attributeLabels: Record<string, string> = {
  sunny: "Яркий свет", diffused: "Рассеянный свет", low_light: "Полутень",
  frequent: "Частый", moderate: "Умеренный", rare: "Редкий", low: "Низкая",
  medium: "Средняя", high: "Высокая", easy: "Лёгкий", demanding: "Требовательный",
  non_toxic: "Нетоксично", toxic: "Токсично", unknown: "Не проверено", safe: "Безопасно",
  caution: "С осторожностью", bathroom: "Ванная", bedroom: "Спальня", office: "Офис",
  nursery: "Детская", living_room: "Гостиная", kitchen: "Кухня", upright: "Вертикальная",
  bushy: "Кустовая", trailing: "Ампельная", climbing: "Вьющаяся", rosette: "Розетка",
  palm: "Пальма", cactus: "Кактус", bonsai: "Бонсай", succulent: "Суккулент",
  fern: "Папоротник", orchid: "Орхидея", flowering: "Цветущее", decorative_leaf: "Декоративно-лиственное",
};

export const attributeLabel = (value: string | number | boolean) => {
  if (typeof value === "boolean") return value ? "Да" : "Нет";
  const key = String(value);
  return attributeLabels[key] || key.replaceAll("_", " ");
};

export const attributeValue = (value: string | number | boolean | string[], unit?: string) => {
  const values = Array.isArray(value) ? value : [value];
  return `${values.map(attributeLabel).join(", ")}${unit ? ` ${unit}` : ""}`;
};

export const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);
export const stars = (rating: number) => `${"★".repeat(Math.round(rating))}${"☆".repeat(5 - Math.round(rating))}`;
