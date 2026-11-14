-- shipped_at fed the retired finance packaging recognition workflow only.
-- saby_updated_at has no remaining reader or writer; inventory.synced_at is
-- the current stock freshness source. Preserve all historical values, including
-- NULLs, without changing orders, shipment status, prices or current timestamps.
LOCK TABLE public.orders, public.shipment_offers, public.products,
 public.product_variants IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.retired_workflow_timestamps AS
 SELECT 'orders'::TEXT AS source_table, id AS source_id,
  'shipped_at'::TEXT AS source_column, shipped_at AS value,
  CURRENT_TIMESTAMP AS archived_at FROM public.orders
 UNION ALL
 SELECT 'shipment_offers', id, 'shipped_at', shipped_at, CURRENT_TIMESTAMP
  FROM public.shipment_offers
 UNION ALL
 SELECT 'products', id, 'saby_updated_at', saby_updated_at, CURRENT_TIMESTAMP
  FROM public.products
 UNION ALL
 SELECT 'product_variants', id, 'saby_updated_at', saby_updated_at, CURRENT_TIMESTAMP
  FROM public.product_variants;
ALTER TABLE legacy_archive.retired_workflow_timestamps
 ADD PRIMARY KEY(source_table, source_id, source_column);
-- Detached archive: deleting an active row must not erase historical facts.
DROP TRIGGER orders_actual_shipment_time ON public.orders;
DROP TRIGGER shipment_offers_actual_shipment_time ON public.shipment_offers;
DROP FUNCTION public.stamp_actual_shipment_time();
-- No CASCADE: an unexpected dependency must abort the whole migration.
ALTER TABLE public.orders DROP COLUMN shipped_at;
ALTER TABLE public.shipment_offers DROP COLUMN shipped_at;
ALTER TABLE public.products DROP COLUMN saby_updated_at;
ALTER TABLE public.product_variants DROP COLUMN saby_updated_at;
