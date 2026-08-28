export type Role = "owner" | "manager" | "";

export type Section = "dashboard" | "analytics" | "products" | "categories" | "orders" | "customers" | "settings" | "collections" | "procurement";

export type Category = { id: number; parentId: number | null; name: string; slug: string; sortOrder: number; icon: string; productsCount: number; childrenCount: number };

export type CategoryAttribute = {
  code: string; name: string; dataType: "text" | "number" | "boolean" | "enum" | "multi_enum";
  unit: string; options: string[]; optionLabels: Record<string, string>; audience: "customer" | "technical"; scope: "product" | "variant";
  required: boolean; filterable: boolean; showOnPdp: boolean; keyCharacteristic: boolean; badge: boolean; sortOrder: number;
};

export type AdminData = {
  user: { fullName: string };
  role: Role;
  permissions: string[];
  dashboard: {
    products: number; variants: number; orders: number; customers: number;
    wholesalePending: number;
    lastSync: null | { status: string; itemsUpdated: number };
    recentOrders: Array<{ orderNumber: string; customerName: string; total: number; status: string }>;
  };
};

export type Customer = {
  id: number; email: string; phone: string; fullName: string; lastName: string;
  patronymic: string; deliveryAddress: string; accountType: string;
  wholesaleStatus: string; retailDiscountBps: number; lifetimeSpend: number;
  active: boolean; adminRole: Role; ordersCount: number; createdAt: string;
};

export type Order = {
  id: number; orderNumber: string; customerId?: number; customerName: string;
  phone: string; email: string; address: string; comment: string;
  deliveryMethod: string; deliveryFeePending?: boolean; repackRequested?: boolean;
  paymentMethod?: string; paymentStatus: string; trackNumber?: string; hasPreorder?: boolean;
  status: string; total: number;
  createdAt: string; items: Array<{ productId: number; sku: string; variantLabel: string; productName: string; unitPrice: number; quantity: number }>;
};

export type Product = {
  id: number; sabyId: string; slug: string; name: string; latinName: string;
  shortDescription: string; description: string; careInstructions: string;
  status: string; featured: boolean; image: string; price: number; stock: number;
  sku: string; variantLabel: string; heightCm?: number; potDiameterCm?: number;
  packageLengthCm?: number; packageWidthCm?: number; packageHeightCm?: number;
  packageWeightGrams?: number; wholesaleMinQty: number; overrideFields: string[];
  sabyFields: string[]; sabyCode: string;
  catalogSection: string; plantKind?: string; lightLevel?: string; watering?: string;
  heightClass?: string; careLevel?: string; placement?: string; petSafety?: string;
  growthHabit?: string; sabyUpdatedAt?: string;
  categoryId?: number;
  passport: PlantPassport; importantWarnings: string[];
  externalIds: Array<{ provider: string; type: string; externalId: string }>;
  attributes: Record<string, string | number | boolean | string[]>;
  variantStage1Missing?: boolean;
  unknownEnumValues?: boolean;
};

export type PlantPassport = { origin: string; lighting: string; watering: string; humidity: string; temperature: string; soil: string; fertilizer: string; repotting: string; careDifficulty: string; growthRate: string; matureSize: string; toxicity: string; problems: string; pests: string; faq: Array<{ question: string; answer: string }> };
export type ReviewModerationItem = { id: number; product: string; author: string; rating: number; text: string; status: "published" | "rejected"; createdAt: string; media: Array<{ url: string; contentType: string }> };

export type ProcurementSupplier = {
  id: number; name: string; kind: "international" | "domestic"; countryCode: string; taxId: string; kpp: string;
  defaultCurrency: "EUR" | "USD" | "RUB"; active: boolean; createdAt: string;
};

export type ProcurementOrder = {
  id: number; supplierId: number; supplierName: string; orderNumber: string;
  documentNumber: string; documentDate?: string; sourceKind: string; currency: string;
  status: string; lines: number; units: number; total: number; unmatched: number; createdAt: string;
};

export type ProcurementSettings = {
  version: number; defaultExchangeRate: number; trolleyCostCurrency: number; trolleyCostRub: number; trolleyVolumeCm3: number;
  trolleyFillRatio: number; returnLossRate: number; marketplaceCostRate: number; taxRate: number;
  reserveRate: number; packageRub: number; priceChangeThreshold: number; domesticRetailMultiplier: number;
  internationalCostMultiplier: number; internationalRetailMultiplier: number; marketplaceStrikeMarkup: number;
  retailRoundStep: number; avoidRoundHundreds: boolean; recommendationDays: number; targetCoverDays: number;
  retailMarkupMultiplier: number; roundPrices: boolean; marketplaceLogisticsPerCm: number;
};

export type ProcurementAlias = {
  id: number; supplierId: number; supplierName: string; rawName: string; supplierArticle: string;
  potDiameterCm?: number; heightCm?: number; suggestedSabyId: string; suggestedSabyName: string;
  matchStatus: string; confidence: number; availabilityStatus: string; lastSeenAt?: string;
};

export type ProcurementDocument = {
  id: number; supplierId: number; supplierName: string; orderId: number; fileName: string;
  parserKind: string; parseStatus: string; arithmeticStatus: string; documentNumber: string;
  documentDate?: string; currency: string; lines: number; units: number; productSubtotal: number;
  packageTotal: number; documentTotal: number; calculatedTotal: number; parseError: string; createdAt: string;
};

export type NomenclatureCandidate = {
  sabyId: string; code: string; article: string; name: string; balance: number; price: number;
};

// Внешний код маркетплейса, продажи которого не дошли до товара СБИС.
// Агрегат по коду за всю загруженную историю: days — в скольких днях он
// встретился, lastSale — когда в последний раз. article и name — подпись
// карточки с площадки: у Wildberries код числовой, и без неё непонятно,
// какое растение разбираешь.
export type UnlinkedSale = {
  channel: string; externalId: string; article: string; name: string;
  days: number; units: number; grossRub: number; lastSale: string;
};

// Итог связывания. takenFrom не пустой, когда код пришлось отобрать у
// другого товара: карточка маркетплейса продаёт ровно одно растение.
export type SalesLinkResult = {
  channel: string; externalId: string; sabyId: string; sabyName: string;
  linkedRows: number; linkedUnits: number; takenFrom: string; remaining: number;
};

export type ProcurementRequest = { id: number; kind: string; sabyId: string; requestedName: string; quantity: number; status: string; notes: string; createdAt: string };

export type ProcurementRecommendation = { aliasId: number; supplierId: number; sabyId: string; name: string; supplierArticle: string; availability: string; balance: number; incoming: number; siteSales: number; sabySales: number; wbSales: number; ozonSales: number; totalSales: number; customerRequests: number; staffRequests: number; openRequests: number; minimumOrderQty: number; orderMultiple: number; suggestedQty: number; dailySales: number; daysOfCover?: number; lastOrderedAt?: string; status: RecommendationStatus; reason: string };

export type RecommendationStatus = "recommended" | "already_ordered" | "check_availability" | "supplier_unavailable" | "excluded";

export type ProcurementAvailability = { supplierId: number; supplierName: string; sabyId: string; name: string; supplierArticle: string; availabilityStatus: string; checkAfter: string; unavailableSince: string; balance: number; lastSeenAt?: string };

export type SalesSyncStatus = { channel: string; status: string; lastAttemptAt?: string; lastSuccessAt?: string; lastError: string; rowsSynced: number; rowsLinked: number; periodFrom: string; periodTo: string; latestSale: string };

export type IntegrationHealth = { channel: "saby" | "wb" | "ozon"; configured: boolean; lastCheckedAt?: string; lastSuccessAt?: string; lastError: string };

export type ProcurementProduct = { sabyId: string; sabyCode: string; sabyArticle: string; name: string; balance: number; currentPriceRub: number; supplierId: number; supplierName: string; supplierArticle: string; availabilityStatus: string; checkAfter: string; hollandArticle: string; wbNmId?: number; wbVendorCode: string; ozonOfferId: string; minimumOrderQty: number; orderMultiple: number; aliases: string[] };

export type ProcurementActionPreviewLine = { sabyId: string; code: string; name: string; quantity?: number; oldPrice?: number; newPrice?: number };
export type ProcurementActionItem = { id: number; lineId: number; productName: string; productCode: string; channel: string; externalArticle: string; oldValue?: number; newValue: number; compareAtValue?: number; quantity?: number; status: string; errorMessage: string; externalOperationId?: string; externalUrl?: string; previewLines?: ProcurementActionPreviewLine[] };

export type ProcurementActionBatch = { id: number; kind: string; status: string; createdAt: string; items: ProcurementActionItem[] };

export type ProcurementOrderLine = {
  id: number; sabyId: string; sabyName: string; rawName: string; supplierArticle: string; quantity: number;
  unitPrice: number; expectedUnitPrice?: number; orderedQuantity: number; invoicedQuantity?: number;
  loadUnit: string; potDiameterCm?: number; heightCm?: number; matchStatus: string;
  purchaseUnitRub?: number; trolleyDeliveryUnitRub?: number; ryazanDeliveryUnitRub?: number; unitCostRub?: number;
  currentRetailRub: number; proposedRetailRub?: number; proposedMarketplaceRub?: number;
  proposedMarketplaceStrikeRub?: number; priceChangeNeeded: boolean; customerRequest: boolean;
  comparisonMismatch: boolean; comparisonAccepted: boolean; comparisonNote: string;
};

export type ProcurementValidation = { canCalculate: boolean; canPrepareActions: boolean; blockers: string[] | null; arithmeticMismatch: number; comparisonMismatch: number; missingDimensions: number; missingLoadUnits: number; invalidLines: number; unmatched: number; trolleyCount: number; expectedTrolleyRub: number; allocatedTrolleyRub: number; expectedRyazanRub: number; allocatedRyazanRub: number };

export type ProcurementOrderDetail = { order: ProcurementOrder; costs: { exchangeRate: number; trolleyCostCurrency: number; trolleyCostRub: number; deliveryToMoscowRub: number; deliveryToRyazanRub: number }; validation: ProcurementValidation; lines: ProcurementOrderLine[]; batches: ProcurementActionBatch[] };

export type ProcurementData = {
  summary: { openOrders: number; unresolvedAliases: number; availabilityChecks: number; openRequests: number };
  integrations: { wb: boolean; ozon: boolean; saby: boolean };
  settings: ProcurementSettings; suppliers: ProcurementSupplier[]; orders: ProcurementOrder[];
  documents: ProcurementDocument[]; review: ProcurementAlias[]; requests: ProcurementRequest[];
  availability: ProcurementAvailability[]; recommendations: ProcurementRecommendation[]; salesSync: SalesSyncStatus[];
  integrationHealth: IntegrationHealth[];
};

export type AdminCollection = { id: number; slug: string; title: string; note: string; active: boolean; products: number[] };

export type SettingDefinition = { key: string; title: string; note: string; kind: string };

export type ImportEntry = { code: string; status: string; name: string; price: number; stock: number; productId?: number; slug: string };
