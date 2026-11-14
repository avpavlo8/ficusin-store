-- These former override/snapshot/preferences/manual-receipt fields have no
-- current readers or writers. Preserve exact typed values and identities in
-- detached history before removing them from active domain rows. Current
-- prices, customer discounts, fiscal statuses and existing JSON audit stay intact.
LOCK TABLE products,product_variants,organizations,marketplace_returns,payment_refunds
 IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;

CREATE TABLE legacy_archive.product_retired_sync_fields AS
 SELECT id AS product_id,saby_snapshot,created_at,updated_at,CURRENT_TIMESTAMP AS archived_at FROM products;
ALTER TABLE legacy_archive.product_retired_sync_fields ADD PRIMARY KEY(product_id);
CREATE TABLE legacy_archive.variant_retired_price_sync_fields AS
 SELECT id AS variant_id,product_id,price_override_minor,price_override_until,saby_snapshot,
  created_at,updated_at,CURRENT_TIMESTAMP AS archived_at FROM product_variants;
ALTER TABLE legacy_archive.variant_retired_price_sync_fields ADD PRIMARY KEY(variant_id);
CREATE TABLE legacy_archive.organization_retired_terms AS
 SELECT id AS organization_id,wholesale_discount_bps,payment_terms,created_at,approved_at,
  CURRENT_TIMESTAMP AS archived_at FROM organizations;
ALTER TABLE legacy_archive.organization_retired_terms ADD PRIMARY KEY(organization_id);
CREATE TABLE legacy_archive.marketplace_return_retired_amounts AS
 SELECT id AS marketplace_return_id,channel_withheld_rub,channel_compensation_rub,
  created_at,updated_at,CURRENT_TIMESTAMP AS archived_at FROM marketplace_returns;
ALTER TABLE legacy_archive.marketplace_return_retired_amounts ADD PRIMARY KEY(marketplace_return_id);
CREATE TABLE legacy_archive.refund_retired_receipt_fields AS
 SELECT id AS refund_id,receipt_required,receipt_confirmed_by,created_at,updated_at,
  CURRENT_TIMESTAMP AS archived_at FROM payment_refunds;
ALTER TABLE legacy_archive.refund_retired_receipt_fields ADD PRIMARY KEY(refund_id);

-- No CASCADE: an unexpected external dependency must reject retirement.
ALTER TABLE products DROP COLUMN saby_snapshot;
ALTER TABLE product_variants DROP COLUMN price_override_minor,
 DROP COLUMN price_override_until,DROP COLUMN saby_snapshot;
ALTER TABLE organizations DROP COLUMN wholesale_discount_bps,DROP COLUMN payment_terms;
ALTER TABLE marketplace_returns DROP COLUMN channel_withheld_rub,DROP COLUMN channel_compensation_rub;
ALTER TABLE payment_refunds DROP COLUMN receipt_required,DROP COLUMN receipt_confirmed_by;
