CREATE TABLE `integration_credentials` (
	`provider` text PRIMARY KEY NOT NULL,
	`encrypted_payload` text NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
ALTER TABLE `orders` ADD `cdek_city_code` integer;--> statement-breakpoint
ALTER TABLE `orders` ADD `cdek_city_name` text;--> statement-breakpoint
ALTER TABLE `orders` ADD `cdek_office_code` text;--> statement-breakpoint
ALTER TABLE `orders` ADD `cdek_tariff_code` integer;