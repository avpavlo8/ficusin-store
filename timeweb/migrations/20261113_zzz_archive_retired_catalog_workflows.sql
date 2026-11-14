-- Retire the remaining one-off AI queue and pre-editor staging journal.
-- No worker or current creation endpoint writes either table. Original product
-- IDs and JSON are retained as history, rather than reassigned during merges.
CREATE SCHEMA IF NOT EXISTS legacy_archive;
ALTER TABLE public.catalog_ai_enrichment_jobs SET SCHEMA legacy_archive;
ALTER TABLE public.product_creation_drafts SET SCHEMA legacy_archive;

-- Future product/customer deletion must not erase or be blocked by this history.
-- Other archive tables already had their active-schema links detached by the
-- preceding migration; no live constraints or current editor receipts are changed.
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
    AND source.relname IN ('catalog_ai_enrichment_jobs','product_creation_drafts')
 LOOP
  EXECUTE format('ALTER TABLE legacy_archive.%I DROP CONSTRAINT %I',dependency.relname,dependency.conname);
 END LOOP;
END $$;
