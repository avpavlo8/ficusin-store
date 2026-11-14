-- The manual producer was retired by the preceding migration. Procurement
-- batches now have one source, and no current reader consumes that marker.
-- Missing columns continue to reject statements from cached old producers.
LOCK TABLE procurement_action_batches,catalog_editor_price_publications IN ACCESS EXCLUSIVE MODE;

-- old_value_minor was only copied during retirement. Its original NUMERIC
-- value remains in the independent archive, linked by legacy_proposal_id.
-- Refuse to discard an unexpected value without that exact provenance.
DO $$ BEGIN
 IF EXISTS (
  SELECT 1 FROM catalog_editor_price_publications p
  WHERE p.old_value_minor IS NOT NULL AND NOT EXISTS (
   SELECT 1 FROM legacy_archive.manual_price_items i
   WHERE i.id=p.legacy_proposal_id AND (i.old_value*100)::BIGINT=p.old_value_minor
  )
 ) THEN
  RAISE EXCEPTION 'Unarchived historical editor price requires review before column removal';
 END IF;
END $$;

-- No CASCADE: unexpected external schema dependencies must block deployment.
ALTER TABLE procurement_action_batches DROP COLUMN source;
ALTER TABLE catalog_editor_price_publications DROP COLUMN old_value_minor;
