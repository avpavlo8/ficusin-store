export type CatalogProduct = { id: string; name: string; latin: string; price: number; image: string; size: string; stock: number };
export type ProductVariant = { id: number; sku: string; label: string; price: number; stock: number; heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number };
export type FAQItem = { question: string; answer: string };
export type PlantPassportData = {
  origin?: string; lighting?: string; watering?: string; humidity?: string; temperature?: string;
  soil?: string; fertilizer?: string; repotting?: string; careDifficulty?: string; growthRate?: string;
  matureSize?: string; toxicity?: string; problems?: string; pests?: string; faq?: FAQItem[];
};
export type ProductReview = { id: number; rating: number; text: string; author: string; date: string; verifiedPurchase: boolean; photos: string[] };
export type ProductDetail = {
  id: string; name: string; latin: string; shortDescription: string; description: string;
  careInstructions: string; images: string[]; variants: ProductVariant[]; recommendations: CatalogProduct[];
  catalogSection: string; plantKind?: string; lightLevel?: string; watering?: string;
  heightClass?: string; careLevel?: string; placement?: string; petSafety?: string; growthHabit?: string;
  passport: PlantPassportData; importantWarnings: string[]; rating: number; reviewsCount: number; reviews: ProductReview[];
};

export const money = (value: number) => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value);
export const stars = (rating: number) => `${"★".repeat(Math.round(rating))}${"☆".repeat(5 - Math.round(rating))}`;
