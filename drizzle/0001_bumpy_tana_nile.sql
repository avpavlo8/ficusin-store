CREATE TABLE `admin_users` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`email` text NOT NULL,
	`role` text DEFAULT 'owner' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `admin_users_email_unique` ON `admin_users` (`email`);--> statement-breakpoint
CREATE TABLE `article_products` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`article_id` integer NOT NULL,
	`product_id` integer NOT NULL,
	FOREIGN KEY (`article_id`) REFERENCES `articles`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `article_products_unique` ON `article_products` (`article_id`,`product_id`);--> statement-breakpoint
CREATE TABLE `articles` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`slug` text NOT NULL,
	`title` text NOT NULL,
	`excerpt` text DEFAULT '' NOT NULL,
	`content` text DEFAULT '' NOT NULL,
	`cover_object_key` text,
	`status` text DEFAULT 'draft' NOT NULL,
	`published_at` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `articles_slug_unique` ON `articles` (`slug`);--> statement-breakpoint
CREATE INDEX `articles_status_idx` ON `articles` (`status`,`published_at`);--> statement-breakpoint
CREATE TABLE `categories` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`parent_id` integer,
	`name` text NOT NULL,
	`slug` text NOT NULL,
	`sort_order` integer DEFAULT 0 NOT NULL,
	`is_visible` integer DEFAULT true NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `categories_slug_unique` ON `categories` (`slug`);--> statement-breakpoint
CREATE INDEX `categories_parent_idx` ON `categories` (`parent_id`,`sort_order`);--> statement-breakpoint
CREATE TABLE `customers` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`auth_provider` text DEFAULT 'phone' NOT NULL,
	`auth_subject` text,
	`email` text,
	`phone` text,
	`full_name` text DEFAULT '' NOT NULL,
	`account_type` text DEFAULT 'retail' NOT NULL,
	`wholesale_status` text DEFAULT 'not_requested' NOT NULL,
	`lifetime_spend_minor` integer DEFAULT 0 NOT NULL,
	`retail_discount_bps` integer DEFAULT 0 NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `customers_auth_subject_unique` ON `customers` (`auth_provider`,`auth_subject`);--> statement-breakpoint
CREATE UNIQUE INDEX `customers_email_unique` ON `customers` (`email`);--> statement-breakpoint
CREATE UNIQUE INDEX `customers_phone_unique` ON `customers` (`phone`);--> statement-breakpoint
CREATE INDEX `customers_account_type_idx` ON `customers` (`account_type`,`wholesale_status`);--> statement-breakpoint
CREATE TABLE `discount_tiers` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`name` text NOT NULL,
	`min_spend_minor` integer DEFAULT 0 NOT NULL,
	`discount_bps` integer DEFAULT 0 NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`sort_order` integer DEFAULT 0 NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `discount_tiers_name_unique` ON `discount_tiers` (`name`);--> statement-breakpoint
CREATE INDEX `discount_tiers_spend_idx` ON `discount_tiers` (`min_spend_minor`);--> statement-breakpoint
CREATE TABLE `inventory` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`warehouse_id` integer NOT NULL,
	`variant_id` integer NOT NULL,
	`available_qty` integer DEFAULT 0 NOT NULL,
	`reserved_qty` integer DEFAULT 0 NOT NULL,
	`synced_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`warehouse_id`) REFERENCES `warehouses`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`variant_id`) REFERENCES `product_variants`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `inventory_warehouse_variant_unique` ON `inventory` (`warehouse_id`,`variant_id`);--> statement-breakpoint
CREATE INDEX `inventory_variant_idx` ON `inventory` (`variant_id`,`available_qty`);--> statement-breakpoint
CREATE TABLE `organization_members` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`organization_id` integer NOT NULL,
	`customer_id` integer NOT NULL,
	`role` text DEFAULT 'buyer' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`organization_id`) REFERENCES `organizations`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`customer_id`) REFERENCES `customers`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `organization_members_unique` ON `organization_members` (`organization_id`,`customer_id`);--> statement-breakpoint
CREATE TABLE `organizations` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`legal_type` text DEFAULT 'legal_entity' NOT NULL,
	`name` text NOT NULL,
	`inn` text NOT NULL,
	`kpp` text,
	`legal_address` text DEFAULT '' NOT NULL,
	`status` text DEFAULT 'pending' NOT NULL,
	`wholesale_discount_bps` integer DEFAULT 0 NOT NULL,
	`payment_terms` text DEFAULT 'invoice' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`approved_at` text
);
--> statement-breakpoint
CREATE UNIQUE INDEX `organizations_inn_unique` ON `organizations` (`inn`);--> statement-breakpoint
CREATE INDEX `organizations_status_idx` ON `organizations` (`status`);--> statement-breakpoint
CREATE TABLE `price_import_rows` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`import_id` integer NOT NULL,
	`sku` text NOT NULL,
	`current_price_minor` integer,
	`proposed_price_minor` integer,
	`validation_status` text DEFAULT 'pending' NOT NULL,
	`validation_message` text DEFAULT '' NOT NULL,
	FOREIGN KEY (`import_id`) REFERENCES `price_imports`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE INDEX `price_import_rows_import_idx` ON `price_import_rows` (`import_id`,`sku`);--> statement-breakpoint
CREATE TABLE `price_imports` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`uploaded_by_email` text NOT NULL,
	`original_file_name` text NOT NULL,
	`status` text DEFAULT 'preview' NOT NULL,
	`rows_total` integer DEFAULT 0 NOT NULL,
	`rows_valid` integer DEFAULT 0 NOT NULL,
	`rows_invalid` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`applied_at` text
);
--> statement-breakpoint
CREATE INDEX `price_imports_status_idx` ON `price_imports` (`status`,`created_at`);--> statement-breakpoint
CREATE TABLE `product_categories` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`product_id` integer NOT NULL,
	`category_id` integer NOT NULL,
	FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`category_id`) REFERENCES `categories`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `product_categories_unique` ON `product_categories` (`product_id`,`category_id`);--> statement-breakpoint
CREATE TABLE `product_media` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`product_id` integer NOT NULL,
	`variant_id` integer,
	`object_key` text NOT NULL,
	`alt_text` text DEFAULT '' NOT NULL,
	`sort_order` integer DEFAULT 0 NOT NULL,
	`is_primary` integer DEFAULT false NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`variant_id`) REFERENCES `product_variants`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE INDEX `product_media_product_idx` ON `product_media` (`product_id`,`variant_id`,`sort_order`);--> statement-breakpoint
CREATE TABLE `product_relations` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`product_id` integer NOT NULL,
	`related_product_id` integer NOT NULL,
	`relation_type` text DEFAULT 'similar' NOT NULL,
	`sort_order` integer DEFAULT 0 NOT NULL,
	FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action,
	FOREIGN KEY (`related_product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `product_relations_unique` ON `product_relations` (`product_id`,`related_product_id`,`relation_type`);--> statement-breakpoint
CREATE TABLE `product_variants` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`product_id` integer NOT NULL,
	`saby_id` text,
	`sku` text NOT NULL,
	`label` text NOT NULL,
	`height_cm` integer,
	`pot_diameter_cm` integer,
	`base_price_minor` integer DEFAULT 0 NOT NULL,
	`price_override_minor` integer,
	`price_override_until` text,
	`wholesale_min_qty` integer DEFAULT 1 NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`saby_updated_at` text,
	FOREIGN KEY (`product_id`) REFERENCES `products`(`id`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `product_variants_sku_unique` ON `product_variants` (`sku`);--> statement-breakpoint
CREATE UNIQUE INDEX `product_variants_saby_id_unique` ON `product_variants` (`saby_id`);--> statement-breakpoint
CREATE INDEX `product_variants_product_idx` ON `product_variants` (`product_id`,`is_active`);--> statement-breakpoint
CREATE TABLE `products` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`saby_id` text,
	`name` text NOT NULL,
	`slug` text NOT NULL,
	`latin_name` text DEFAULT '' NOT NULL,
	`short_description` text DEFAULT '' NOT NULL,
	`description` text DEFAULT '' NOT NULL,
	`care_instructions` text DEFAULT '' NOT NULL,
	`search_text` text DEFAULT '' NOT NULL,
	`status` text DEFAULT 'draft' NOT NULL,
	`is_featured` integer DEFAULT false NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`saby_updated_at` text
);
--> statement-breakpoint
CREATE UNIQUE INDEX `products_slug_unique` ON `products` (`slug`);--> statement-breakpoint
CREATE UNIQUE INDEX `products_saby_id_unique` ON `products` (`saby_id`);--> statement-breakpoint
CREATE INDEX `products_status_idx` ON `products` (`status`,`is_featured`);--> statement-breakpoint
CREATE INDEX `products_name_idx` ON `products` (`name`);--> statement-breakpoint
CREATE TABLE `sync_runs` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`source` text DEFAULT 'saby' NOT NULL,
	`direction` text DEFAULT 'import' NOT NULL,
	`status` text DEFAULT 'running' NOT NULL,
	`items_read` integer DEFAULT 0 NOT NULL,
	`items_created` integer DEFAULT 0 NOT NULL,
	`items_updated` integer DEFAULT 0 NOT NULL,
	`errors_count` integer DEFAULT 0 NOT NULL,
	`error_summary` text DEFAULT '' NOT NULL,
	`started_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`finished_at` text
);
--> statement-breakpoint
CREATE INDEX `sync_runs_source_idx` ON `sync_runs` (`source`,`started_at`);--> statement-breakpoint
CREATE TABLE `warehouses` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`saby_id` text,
	`name` text NOT NULL,
	`city` text NOT NULL,
	`address` text NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `warehouses_saby_id_unique` ON `warehouses` (`saby_id`);--> statement-breakpoint
CREATE INDEX `warehouses_city_idx` ON `warehouses` (`city`,`is_active`);--> statement-breakpoint
ALTER TABLE `order_items` ADD `variant_id` integer REFERENCES product_variants(id);--> statement-breakpoint
ALTER TABLE `order_items` ADD `sku` text;--> statement-breakpoint
ALTER TABLE `order_items` ADD `discount_bps` integer DEFAULT 0 NOT NULL;--> statement-breakpoint
CREATE INDEX `order_items_order_idx` ON `order_items` (`order_id`);--> statement-breakpoint
ALTER TABLE `orders` ADD `customer_id` integer REFERENCES customers(id);--> statement-breakpoint
ALTER TABLE `orders` ADD `organization_id` integer REFERENCES organizations(id);--> statement-breakpoint
ALTER TABLE `orders` ADD `warehouse_id` integer REFERENCES warehouses(id);--> statement-breakpoint
ALTER TABLE `orders` ADD `customer_type` text DEFAULT 'guest' NOT NULL;--> statement-breakpoint
ALTER TABLE `orders` ADD `discount_bps` integer DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE `orders` ADD `discount_amount` real DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE `orders` ADD `payment_method` text DEFAULT 'online' NOT NULL;--> statement-breakpoint
ALTER TABLE `orders` ADD `saby_order_id` text;--> statement-breakpoint
CREATE INDEX `orders_customer_idx` ON `orders` (`customer_id`,`created_at`);--> statement-breakpoint
CREATE INDEX `orders_organization_idx` ON `orders` (`organization_id`,`created_at`);--> statement-breakpoint
CREATE INDEX `orders_status_idx` ON `orders` (`status`,`created_at`);
--> statement-breakpoint
INSERT INTO `admin_users` (`email`, `role`)
VALUES ('avpavlomail@gmail.com', 'owner');
--> statement-breakpoint
INSERT INTO `warehouses` (`name`, `city`, `address`, `is_active`)
VALUES ('Основной склад', 'Рязань', 'Рязань, Новосёлов, 40А', true);
