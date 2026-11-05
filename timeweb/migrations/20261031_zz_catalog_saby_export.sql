-- One durable command per site SKU. A lost response permits only reconciliation,
-- never a second create with a new identity. No existing products are exported.
CREATE TABLE catalog_saby_exports (
  variant_id BIGINT PRIMARY KEY REFERENCES product_variants(id) ON DELETE CASCADE,
  import_code TEXT NOT NULL UNIQUE DEFAULT gen_random_uuid()::TEXT,
  revision BIGINT NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'idle' CHECK(state IN ('idle','queued','checking','verifying','unknown','synced','error')),
  payload JSONB NOT NULL DEFAULT '{}'::JSONB,
  payload_hash TEXT NOT NULL DEFAULT '',
  applied_hash TEXT NOT NULL DEFAULT '',
  remote_id TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  actor_id BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  attempted_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_until TIMESTAMPTZ,
  claim_token TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX catalog_saby_exports_pending_idx ON catalog_saby_exports(next_check_at)
 WHERE state IN ('queued','checking','verifying','unknown');

-- A catalogue export can outlive an HTTP request. Keep its SKU identity even
-- if an old editor tries to delete a draft while Saby is processing the file.
CREATE FUNCTION protect_exported_catalogue_variant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM catalog_saby_exports WHERE variant_id=OLD.id
  AND (state IN ('queued','checking','verifying','unknown') OR attempted_at IS NOT NULL OR remote_id<>'')) THEN
  RAISE EXCEPTION 'Размер отправлен в СБИС: его можно только архивировать';
 END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER protect_exported_catalogue_variant BEFORE DELETE ON product_variants
 FOR EACH ROW EXECUTE FUNCTION protect_exported_catalogue_variant();
