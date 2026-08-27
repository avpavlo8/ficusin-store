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
  cachepot: "Кашпо", planting_pot: "Горшок", planter: "Вазон", hanging: "Подвесное", self_watering: "С автополивом",
  ceramic: "Керамика", plastic: "Пластик", terracotta: "Терракота", concrete: "Бетон", metal: "Металл", glass: "Стекло", wood: "Дерево", fiberstone: "Файберстоун", textile: "Текстиль",
  round: "Круглая", square: "Квадратная", rectangular: "Прямоугольная", oval: "Овальная", other: "Другая",
  indoor: "Для дома", outdoor: "Для улицы", both: "Для дома и улицы",
  liquid: "Жидкость", granules: "Гранулы", powder: "Порошок", sticks: "Палочки", tablets: "Таблетки", spray: "Спрей",
  mineral: "Минеральное", organic: "Органическое", organomineral: "Органоминеральное", microbial: "Микробиологическое",
  root: "Корневая подкормка", foliar: "По листу", soil_mixing: "Внесение в грунт",
  support: "Опоры и подвязки", tool: "Инструменты", watering: "Полив", care: "Уход", protection: "Защита", decor: "Декор", propagation: "Размножение",
};

export const attributeLabel = (value: string | number | boolean) => {
  if (typeof value === "boolean") return value ? "Есть" : "Нет";
  const key = String(value);
  return attributeLabels[key] || key.replaceAll("_", " ");
};

export const attributeValue = (value: string | number | boolean | string[], unit?: string) => {
  const values = Array.isArray(value) ? value : [value];
  return `${values.map(attributeLabel).join(", ")}${unit ? ` ${unit}` : ""}`;
};

export const productAttributeValue = (attribute: ProductAttribute) => {
  const displayed = attribute.displayValue;
  if (displayed !== undefined && displayed !== null) {
    const values = Array.isArray(displayed) ? displayed : [displayed];
    return `${values.join(", ")}${attribute.unit ? ` ${attribute.unit}` : ""}`;
  }
  const values = Array.isArray(attribute.value) ? attribute.value : [attribute.value];
  const labels = values.map((value) => attribute.optionLabels?.[String(value)] || attributeLabel(value));
  return `${labels.join(", ")}${attribute.unit ? ` ${attribute.unit}` : ""}`;
};

export const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);
export const stars = (rating: number) => `${"★".repeat(Math.round(rating))}${"☆".repeat(5 - Math.round(rating))}`;

export type KeyCharacteristic = { code: string; label: string; value: string };

const firstAttribute = (attributes: ProductAttribute[], ...tokens: string[]) => attributes.find((item) =>
  tokens.some((token) => `${item.code} ${item.name}`.toLocaleLowerCase("ru").includes(token)),
);
const plantOnlyAttributeCodes = new Set(["plant_type", "height_cm", "pot_diameter_cm", "light_level", "watering", "humidity", "care_level", "toxicity", "pet_safety", "placement", "growth_habit", "height_class", "flowering"]);

/** One source for the first-screen facts. Codes returned here are excluded from the full table. */
export function keyCharacteristics(product: ProductDetail, variant?: ProductVariant): KeyCharacteristic[] {
  const attributes = [...product.attributes, ...(variant?.attributes || [])].filter((item) => item.showInCharacteristics !== false);
  const result: KeyCharacteristic[] = [];
  const add = (code: string, label: string, value?: string | number) => {
    if (value === undefined || value === null || String(value).trim() === "" || result.some((item) => item.code === code)) return;
    result.push({ code, label, value: String(value) });
  };
  if (product.catalogSection === "plants") {
    add("height_cm", "Высота", variant?.heightCm ? `${variant.heightCm} см` : undefined);
    add("pot_diameter_cm", "Диаметр горшка", variant?.potDiameterCm ? `${variant.potDiameterCm} см` : undefined);
    const kind = firstAttribute(attributes, "plant_type", "тип растения");
    const pets = firstAttribute(attributes, "pet_safety", "toxicity", "питом", "токсич");
    add(kind?.code || "plant_type", "Тип растения", kind ? productAttributeValue(kind) : product.plantKind ? attributeLabel(product.plantKind) : undefined);
    add("origin", "Происхождение", product.passport.origin);
    add(pets?.code || "pet_safety", "Для питомцев", pets ? productAttributeValue(pets) : product.petSafety ? attributeLabel(product.petSafety) : undefined);
  } else {
    attributes.filter((item) => !plantOnlyAttributeCodes.has(item.code)).slice(0, 5).forEach((item) => add(item.code, item.name, productAttributeValue(item)));
  }
  return result.slice(0, 5);
}
