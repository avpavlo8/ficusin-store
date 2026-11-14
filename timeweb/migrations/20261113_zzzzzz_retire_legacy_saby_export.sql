-- Native editor publication supersedes the retired CommerceML HTTP exporter.
-- Refuse retirement if a prior deployment has queued, in-flight or unknown work.
-- The exclusive lock serializes this check with legacy Enqueue/Claim/Finish.
LOCK TABLE public.catalog_saby_exports IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.catalog_saby_exports
   WHERE state NOT IN ('idle','synced','error') OR lease_until>NOW()
      OR (state<>'synced' AND attempted_at IS NOT NULL)) THEN
  RAISE EXCEPTION 'Legacy Saby exports require reconciliation before retirement';
 END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
ALTER TABLE public.catalog_saby_exports SET SCHEMA legacy_archive;
-- Preserve original export identities even if a never-submitted local draft or
-- the initiating customer is later removed. Exported SKUs remain protected below.
ALTER TABLE legacy_archive.catalog_saby_exports
 DROP CONSTRAINT IF EXISTS catalog_saby_exports_variant_id_fkey,
 DROP CONSTRAINT IF EXISTS catalog_saby_exports_actor_id_fkey;
CREATE OR REPLACE FUNCTION protect_exported_catalogue_variant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM legacy_archive.catalog_saby_exports WHERE variant_id=OLD.id
  AND (attempted_at IS NOT NULL OR remote_id<>'')) THEN
  RAISE EXCEPTION 'Размер отправлен в СБИС: его можно только архивировать';
 END IF;
 RETURN OLD;
END $$;

-- Older application instances can retain prepared statements bound to this
-- relation's OID across SET SCHEMA. Block enqueue/claim writes at the relation
-- itself so a rolling deployment cannot restart the retired exporter.
CREATE FUNCTION reject_retired_saby_export_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'Legacy Saby exporter is retired; its archive is read-only';
END $$;
CREATE TRIGGER reject_retired_saby_export_write
 BEFORE INSERT OR UPDATE ON legacy_archive.catalog_saby_exports
 FOR EACH ROW EXECUTE FUNCTION reject_retired_saby_export_write();
