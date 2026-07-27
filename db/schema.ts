import { sql } from "drizzle-orm";
import {
  index,
  integer,
  real,
  sqliteTable,
  text,
  uniqueIndex,
} from "drizzle-orm/sqlite-core";

// Access and customer identity
export const adminUsers = sqliteTable(
  "admin_users",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    email: text("email").notNull(),
    role: text("role").notNull().default("owner"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [uniqueIndex("admin_users_email_unique").on(table.email)],
);

export const customers = sqliteTable(
  "customers",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    authProvider: text("auth_provider").notNull().default("phone"),
    authSubject: text("auth_subject"),
    email: text("email"),
    phone: text("phone"),
    fullName: text("full_name").notNull().default(""),
    accountType: text("account_type").notNull().default("retail"),
    wholesaleStatus: text("wholesale_status").notNull().default("not_requested"),
    lifetimeSpendMinor: integer("lifetime_spend_minor").notNull().default(0),
    retailDiscountBps: integer("retail_discount_bps").notNull().default(0),
    isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("customers_auth_subject_unique").on(table.authProvider, table.authSubject),
    uniqueIndex("customers_email_unique").on(table.email),
    uniqueIndex("customers_phone_unique").on(table.phone),
    index("customers_account_type_idx").on(table.accountType, table.wholesaleStatus),
  ],
);

export const organizations = sqliteTable(
  "organizations",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    legalType: text("legal_type").notNull().default("legal_entity"),
    name: text("name").notNull(),
    inn: text("inn").notNull(),
    kpp: text("kpp"),
    legalAddress: text("legal_address").notNull().default(""),
    status: text("status").notNull().default("pending"),
    wholesaleDiscountBps: integer("wholesale_discount_bps").notNull().default(0),
    paymentTerms: text("payment_terms").notNull().default("invoice"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    approvedAt: text("approved_at"),
  },
  (table) => [
    uniqueIndex("organizations_inn_unique").on(table.inn),
    index("organizations_status_idx").on(table.status),
  ],
);

export const organizationMembers = sqliteTable(
  "organization_members",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    organizationId: integer("organization_id")
      .notNull()
      .references(() => organizations.id),
    customerId: integer("customer_id")
      .notNull()
      .references(() => customers.id),
    role: text("role").notNull().default("buyer"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("organization_members_unique").on(
      table.organizationId,
      table.customerId,
    ),
  ],
);

export const discountTiers = sqliteTable(
  "discount_tiers",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    name: text("name").notNull(),
    minSpendMinor: integer("min_spend_minor").notNull().default(0),
    discountBps: integer("discount_bps").notNull().default(0),
    isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
    sortOrder: integer("sort_order").notNull().default(0),
  },
  (table) => [
    uniqueIndex("discount_tiers_name_unique").on(table.name),
    index("discount_tiers_spend_idx").on(table.minSpendMinor),
  ],
);

// Catalog and Saby mirror
export const categories = sqliteTable(
  "categories",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    parentId: integer("parent_id"),
    name: text("name").notNull(),
    slug: text("slug").notNull(),
    sortOrder: integer("sort_order").notNull().default(0),
    isVisible: integer("is_visible", { mode: "boolean" }).notNull().default(true),
  },
  (table) => [
    uniqueIndex("categories_slug_unique").on(table.slug),
    index("categories_parent_idx").on(table.parentId, table.sortOrder),
  ],
);

export const products = sqliteTable(
  "products",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    sabyId: text("saby_id"),
    name: text("name").notNull(),
    slug: text("slug").notNull(),
    latinName: text("latin_name").notNull().default(""),
    shortDescription: text("short_description").notNull().default(""),
    description: text("description").notNull().default(""),
    careInstructions: text("care_instructions").notNull().default(""),
    searchText: text("search_text").notNull().default(""),
    status: text("status").notNull().default("draft"),
    isFeatured: integer("is_featured", { mode: "boolean" }).notNull().default(false),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    sabyUpdatedAt: text("saby_updated_at"),
  },
  (table) => [
    uniqueIndex("products_slug_unique").on(table.slug),
    uniqueIndex("products_saby_id_unique").on(table.sabyId),
    index("products_status_idx").on(table.status, table.isFeatured),
    index("products_name_idx").on(table.name),
  ],
);

export const productCategories = sqliteTable(
  "product_categories",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    productId: integer("product_id").notNull().references(() => products.id),
    categoryId: integer("category_id").notNull().references(() => categories.id),
  },
  (table) => [
    uniqueIndex("product_categories_unique").on(table.productId, table.categoryId),
  ],
);

export const productVariants = sqliteTable(
  "product_variants",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    productId: integer("product_id").notNull().references(() => products.id),
    sabyId: text("saby_id"),
    sku: text("sku").notNull(),
    label: text("label").notNull(),
    heightCm: integer("height_cm"),
    potDiameterCm: integer("pot_diameter_cm"),
    basePriceMinor: integer("base_price_minor").notNull().default(0),
    priceOverrideMinor: integer("price_override_minor"),
    priceOverrideUntil: text("price_override_until"),
    wholesaleMinQty: integer("wholesale_min_qty").notNull().default(1),
    isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    sabyUpdatedAt: text("saby_updated_at"),
  },
  (table) => [
    uniqueIndex("product_variants_sku_unique").on(table.sku),
    uniqueIndex("product_variants_saby_id_unique").on(table.sabyId),
    index("product_variants_product_idx").on(table.productId, table.isActive),
  ],
);

export const productMedia = sqliteTable(
  "product_media",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    productId: integer("product_id").notNull().references(() => products.id),
    variantId: integer("variant_id").references(() => productVariants.id),
    objectKey: text("object_key").notNull(),
    altText: text("alt_text").notNull().default(""),
    sortOrder: integer("sort_order").notNull().default(0),
    isPrimary: integer("is_primary", { mode: "boolean" }).notNull().default(false),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    index("product_media_product_idx").on(
      table.productId,
      table.variantId,
      table.sortOrder,
    ),
  ],
);

export const productRelations = sqliteTable(
  "product_relations",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    productId: integer("product_id").notNull().references(() => products.id),
    relatedProductId: integer("related_product_id")
      .notNull()
      .references(() => products.id),
    relationType: text("relation_type").notNull().default("similar"),
    sortOrder: integer("sort_order").notNull().default(0),
  },
  (table) => [
    uniqueIndex("product_relations_unique").on(
      table.productId,
      table.relatedProductId,
      table.relationType,
    ),
  ],
);

// Warehouses and stock
export const warehouses = sqliteTable(
  "warehouses",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    sabyId: text("saby_id"),
    name: text("name").notNull(),
    city: text("city").notNull(),
    address: text("address").notNull(),
    isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("warehouses_saby_id_unique").on(table.sabyId),
    index("warehouses_city_idx").on(table.city, table.isActive),
  ],
);

export const inventory = sqliteTable(
  "inventory",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    warehouseId: integer("warehouse_id").notNull().references(() => warehouses.id),
    variantId: integer("variant_id").notNull().references(() => productVariants.id),
    availableQty: integer("available_qty").notNull().default(0),
    reservedQty: integer("reserved_qty").notNull().default(0),
    syncedAt: text("synced_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("inventory_warehouse_variant_unique").on(
      table.warehouseId,
      table.variantId,
    ),
    index("inventory_variant_idx").on(table.variantId, table.availableQty),
  ],
);

// Editorial content
export const articles = sqliteTable(
  "articles",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    slug: text("slug").notNull(),
    title: text("title").notNull(),
    excerpt: text("excerpt").notNull().default(""),
    content: text("content").notNull().default(""),
    coverObjectKey: text("cover_object_key"),
    status: text("status").notNull().default("draft"),
    publishedAt: text("published_at"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex("articles_slug_unique").on(table.slug),
    index("articles_status_idx").on(table.status, table.publishedAt),
  ],
);

export const articleProducts = sqliteTable(
  "article_products",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    articleId: integer("article_id").notNull().references(() => articles.id),
    productId: integer("product_id").notNull().references(() => products.id),
  },
  (table) => [
    uniqueIndex("article_products_unique").on(table.articleId, table.productId),
  ],
);

// Orders keep immutable price snapshots. New account links are nullable for guests.
export const orders = sqliteTable(
  "orders",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    orderNumber: text("order_number").notNull().unique(),
    customerId: integer("customer_id").references(() => customers.id),
    organizationId: integer("organization_id").references(() => organizations.id),
    warehouseId: integer("warehouse_id").references(() => warehouses.id),
    customerType: text("customer_type").notNull().default("guest"),
    customerName: text("customer_name").notNull(),
    phone: text("phone").notNull(),
    email: text("email").notNull(),
    address: text("address").notNull().default(""),
    comment: text("comment").notNull().default(""),
    deliveryMethod: text("delivery_method").notNull(),
    deliveryFee: real("delivery_fee").notNull(),
    subtotal: real("subtotal").notNull(),
    discountBps: integer("discount_bps").notNull().default(0),
    discountAmount: real("discount_amount").notNull().default(0),
    total: real("total").notNull(),
    paymentMethod: text("payment_method").notNull().default("online"),
    paymentStatus: text("payment_status").notNull().default("payment_provider_pending"),
    status: text("status").notNull().default("new"),
    sabyOrderId: text("saby_order_id"),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    index("orders_customer_idx").on(table.customerId, table.createdAt),
    index("orders_organization_idx").on(table.organizationId, table.createdAt),
    index("orders_status_idx").on(table.status, table.createdAt),
  ],
);

export const orderItems = sqliteTable(
  "order_items",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    orderId: integer("order_id").notNull().references(() => orders.id),
    variantId: integer("variant_id").references(() => productVariants.id),
    productId: text("product_id").notNull(),
    productName: text("product_name").notNull(),
    sku: text("sku"),
    unitPrice: real("unit_price").notNull(),
    discountBps: integer("discount_bps").notNull().default(0),
    quantity: integer("quantity").notNull(),
  },
  (table) => [index("order_items_order_idx").on(table.orderId)],
);

// Operational audit and integrations
export const syncRuns = sqliteTable(
  "sync_runs",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    source: text("source").notNull().default("saby"),
    direction: text("direction").notNull().default("import"),
    status: text("status").notNull().default("running"),
    itemsRead: integer("items_read").notNull().default(0),
    itemsCreated: integer("items_created").notNull().default(0),
    itemsUpdated: integer("items_updated").notNull().default(0),
    errorsCount: integer("errors_count").notNull().default(0),
    errorSummary: text("error_summary").notNull().default(""),
    startedAt: text("started_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    finishedAt: text("finished_at"),
  },
  (table) => [index("sync_runs_source_idx").on(table.source, table.startedAt)],
);

export const priceImports = sqliteTable(
  "price_imports",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    uploadedByEmail: text("uploaded_by_email").notNull(),
    originalFileName: text("original_file_name").notNull(),
    status: text("status").notNull().default("preview"),
    rowsTotal: integer("rows_total").notNull().default(0),
    rowsValid: integer("rows_valid").notNull().default(0),
    rowsInvalid: integer("rows_invalid").notNull().default(0),
    createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
    appliedAt: text("applied_at"),
  },
  (table) => [index("price_imports_status_idx").on(table.status, table.createdAt)],
);

export const priceImportRows = sqliteTable(
  "price_import_rows",
  {
    id: integer("id").primaryKey({ autoIncrement: true }),
    importId: integer("import_id").notNull().references(() => priceImports.id),
    sku: text("sku").notNull(),
    currentPriceMinor: integer("current_price_minor"),
    proposedPriceMinor: integer("proposed_price_minor"),
    validationStatus: text("validation_status").notNull().default("pending"),
    validationMessage: text("validation_message").notNull().default(""),
  },
  (table) => [index("price_import_rows_import_idx").on(table.importId, table.sku)],
);
