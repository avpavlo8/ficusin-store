-- These knobs belonged to an earlier calculator. Current fallback prices use
-- retail_markup_multiplier/round_prices, and unified prices use economics.
-- Preserve the exact old settings without changing the version: removing unused
-- fields must not invalidate existing quotes, approved batches or receipt guards.
LOCK TABLE procurement_pricing_settings IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.procurement_retired_pricing_settings AS
 SELECT id AS settings_id,version,domestic_retail_multiplier,
  international_cost_multiplier,international_retail_multiplier,
  retail_round_step,avoid_round_hundreds,updated_by,updated_at,
  CURRENT_TIMESTAMP AS archived_at
 FROM procurement_pricing_settings;
ALTER TABLE legacy_archive.procurement_retired_pricing_settings ADD PRIMARY KEY(settings_id);
-- The archive deliberately has no FK to the current settings or their author.
-- No CASCADE: unexpected dependent objects must block retirement.
ALTER TABLE procurement_pricing_settings
 DROP COLUMN domestic_retail_multiplier,
 DROP COLUMN international_cost_multiplier,
 DROP COLUMN international_retail_multiplier,
 DROP COLUMN retail_round_step,
 DROP COLUMN avoid_round_hundreds;
