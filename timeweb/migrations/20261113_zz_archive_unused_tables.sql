-- Finance UI/API were retired; the original records have not been audited in
-- production. Remove the unused workflow from the active schema without losing
-- rows, source files, object references, indexes or owned sequences.
CREATE SCHEMA IF NOT EXISTS legacy_archive;
COMMENT ON SCHEMA legacy_archive IS 'Retired Ficusin workflows; preserved historical records, excluded from application search_path';

-- Stop the retired accounting side effects before moving their target table.
-- Shipment timestamp triggers remain active: other order workflows need them.
DROP TRIGGER IF EXISTS sales_events_finance_packaging ON public.sales_events;
DROP TRIGGER IF EXISTS orders_finance_packaging_recognition ON public.orders;
DROP TRIGGER IF EXISTS shipment_offers_finance_packaging_recognition ON public.shipment_offers;
DROP FUNCTION IF EXISTS public.snapshot_finance_packaging();
DROP FUNCTION IF EXISTS public.recognize_site_order_packaging();
DROP FUNCTION IF EXISTS public.recognize_site_offer_packaging();
DROP FUNCTION IF EXISTS public.finance_packaging_eligible(public.sales_events);

ALTER TABLE public.finance_imports SET SCHEMA legacy_archive;
ALTER TABLE public.finance_transactions SET SCHEMA legacy_archive;
ALTER TABLE public.finance_classification_versions SET SCHEMA legacy_archive;
ALTER TABLE public.finance_cash_entries SET SCHEMA legacy_archive;
ALTER TABLE public.finance_cash_reconciliations SET SCHEMA legacy_archive;
ALTER TABLE public.finance_packaging_snapshots SET SCHEMA legacy_archive;
ALTER TABLE public.finance_marketplace_adjustments SET SCHEMA legacy_archive;
ALTER TABLE public.finance_tax_periods SET SCHEMA legacy_archive;
ALTER TABLE public.supplier_account_operations SET SCHEMA legacy_archive;
ALTER TABLE public.supplier_account_reconciliations SET SCHEMA legacy_archive;
ALTER TABLE public.finance_marketplace_periods SET SCHEMA legacy_archive;
ALTER TABLE public.finance_marketplace_report_rows SET SCHEMA legacy_archive;
ALTER TABLE public.finance_classification_rules SET SCHEMA legacy_archive;

-- The current editor stores draft photos in IndexedDB and uses the durable
-- catalog_editor publication workflow, rather than these STG-only records.
-- Keep product_creation_drafts: product merge/removal still consults it.
ALTER TABLE public.product_creation_photos SET SCHEMA legacy_archive;
ALTER TABLE public.product_creation_staging_cards SET SCHEMA legacy_archive;

-- Archived IDs are historical facts. Future deletion of a customer, sale or
-- active draft must neither erase archive rows nor be blocked by the archive.
-- Keep foreign keys within the archived group; detach only its outgoing links
-- to active tables. No active-table constraints are modified.
DO $$
DECLARE dependency RECORD;
BEGIN
 FOR dependency IN
  SELECT con.conname, source.relname
  FROM pg_constraint con
  JOIN pg_class source ON source.oid=con.conrelid
  JOIN pg_namespace source_schema ON source_schema.oid=source.relnamespace
  JOIN pg_class target ON target.oid=con.confrelid
  JOIN pg_namespace target_schema ON target_schema.oid=target.relnamespace
  WHERE con.contype='f' AND source_schema.nspname='legacy_archive'
    AND target_schema.nspname='public'
    AND source.relname IN (
      'finance_imports','finance_transactions','finance_classification_versions',
      'finance_cash_entries','finance_cash_reconciliations','finance_packaging_snapshots',
      'finance_marketplace_adjustments','finance_tax_periods','supplier_account_operations',
      'supplier_account_reconciliations','finance_marketplace_periods',
      'finance_marketplace_report_rows','finance_classification_rules',
      'product_creation_photos','product_creation_staging_cards')
 LOOP
  EXECUTE format('ALTER TABLE legacy_archive.%I DROP CONSTRAINT %I',dependency.relname,dependency.conname);
 END LOOP;
END $$;
