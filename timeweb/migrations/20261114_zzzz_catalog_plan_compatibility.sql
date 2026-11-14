-- Canonicalize editable plans only. Frozen publication payloads, fingerprints,
-- pending revisions and provider receipts are deliberately never rewritten.
CREATE OR REPLACE FUNCTION catalog_editor_canonical_variant(value JSONB)
RETURNS JSONB LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE output JSONB:=value; market JSONB; entry JSONB; channel TEXT; common TEXT;
 trim_chars TEXT:=E' \t\n\r\f'||chr(11)||U&'\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000';
BEGIN
 IF jsonb_typeof(value) IS DISTINCT FROM 'object' THEN RETURN value; END IF;
 IF (NOT value?'priceMode' OR value->'priceMode'='null'::JSONB OR value->>'priceMode'='')
  AND ((jsonb_typeof(value->'wbPrice')='string' AND BTRIM(value->>'wbPrice',trim_chars)<>'')
    OR (jsonb_typeof(value->'ozonPrice')='string' AND BTRIM(value->>'ozonPrice',trim_chars)<>'')) THEN
  output:=jsonb_set(output,'{priceMode}','"manual"');
 END IF;
 market:=value->'marketplace';
 IF jsonb_typeof(market) IS DISTINCT FROM 'object' THEN RETURN output; END IF;
 IF market?'article' AND jsonb_typeof(market->'article') NOT IN ('string','null') THEN RETURN output; END IF;
 common:=COALESCE(market->>'article','');
 FOREACH channel IN ARRAY ARRAY['wb','ozon'] LOOP
  entry:=market->channel;
  IF entry IS NULL OR entry='null'::JSONB THEN entry:='{}'::JSONB; END IF;
  IF jsonb_typeof(entry)='object' THEN
   -- Go's old *string reader treats both absent and JSON null as inherited.
   -- An explicit empty string is a cleared identity and must remain empty.
   IF NOT entry?'article' OR entry->'article'='null'::JSONB THEN
    entry:=jsonb_set(entry,'{article}',to_jsonb(common));
   END IF;
   market:=jsonb_set(market,ARRAY[channel],entry);
  END IF;
 END LOOP;
 IF jsonb_typeof(market#>'{wb,article}')='string' AND jsonb_typeof(market#>'{ozon,article}')='string' THEN
  market:=market-'article';
 END IF;
 RETURN jsonb_set(output,'{marketplace}',market);
END $$;

CREATE OR REPLACE FUNCTION catalog_editor_canonical_plan(value JSONB)
RETURNS JSONB LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE output JSONB:='[]'::JSONB; entry JSONB;
BEGIN
 IF jsonb_typeof(value) IS DISTINCT FROM 'object' OR jsonb_typeof(value->'variants') IS DISTINCT FROM 'array' THEN RETURN value; END IF;
 FOR entry IN SELECT jsonb_array_elements(value->'variants') LOOP
  output:=output||jsonb_build_array(catalog_editor_canonical_variant(entry));
 END LOOP;
 RETURN jsonb_set(value,'{variants}',output);
END $$;

CREATE TABLE IF NOT EXISTS catalog_plan_format_history (
 product_id BIGINT NOT NULL,source_hash TEXT NOT NULL,
 before_config JSONB NOT NULL,canonical_hash TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(product_id,source_hash)
);
LOCK TABLE catalog_editor_plans IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO catalog_plan_format_history(product_id,source_hash,before_config,canonical_hash)
 SELECT product_id,encode(sha256(convert_to(config::TEXT,'UTF8')),'hex'),config,
  encode(sha256(convert_to(catalog_editor_canonical_plan(config)::TEXT,'UTF8')),'hex')
 FROM catalog_editor_plans WHERE config IS DISTINCT FROM catalog_editor_canonical_plan(config)
 ON CONFLICT DO NOTHING;
UPDATE catalog_editor_plans SET config=catalog_editor_canonical_plan(config)
 WHERE config IS DISTINCT FROM catalog_editor_canonical_plan(config);

-- This is historical operation evidence, not a new active Saby mapping. Keep
-- orphan IDs and every original receipt even after a local draft is removed.
CREATE TABLE IF NOT EXISTS catalog_external_operation_history (
 id BIGSERIAL PRIMARY KEY,provider TEXT NOT NULL,source_kind TEXT NOT NULL,
 source_key TEXT NOT NULL,variant_id BIGINT NOT NULL,product_id BIGINT,
 external_id TEXT NOT NULL,operation_state TEXT NOT NULL,
 protects_identity BOOLEAN NOT NULL,needs_reconciliation BOOLEAN NOT NULL,
 evidence JSONB NOT NULL,evidence_hash TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(provider,source_kind,source_key)
);
CREATE INDEX IF NOT EXISTS catalog_external_operation_history_variant_idx
 ON catalog_external_operation_history(variant_id,protects_identity);
LOCK TABLE legacy_archive.catalog_saby_exports IN SHARE MODE;
INSERT INTO catalog_external_operation_history(provider,source_kind,source_key,variant_id,product_id,
 external_id,operation_state,protects_identity,needs_reconciliation,evidence,evidence_hash)
 SELECT 'saby','commerce-export',e.import_code,e.variant_id,v.product_id,e.remote_id,e.state,
  e.attempted_at IS NOT NULL OR e.remote_id<>'' OR e.state IN ('queued','checking','verifying','unknown'),
  e.state IN ('queued','checking','verifying','unknown') OR (e.attempted_at IS NOT NULL AND (e.state<>'synced' OR e.remote_id='')),
  to_jsonb(e),encode(sha256(convert_to(to_jsonb(e)::TEXT,'UTF8')),'hex')
 FROM legacy_archive.catalog_saby_exports e LEFT JOIN product_variants v ON v.id=e.variant_id
 ON CONFLICT DO NOTHING;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM legacy_archive.catalog_saby_exports e LEFT JOIN catalog_external_operation_history h
  ON h.provider='saby' AND h.source_kind='commerce-export' AND h.source_key=e.import_code
  WHERE h.id IS NULL OR h.evidence IS DISTINCT FROM to_jsonb(e)
    OR h.evidence_hash<>encode(sha256(convert_to(to_jsonb(e)::TEXT,'UTF8')),'hex')) THEN
  RAISE EXCEPTION 'Saby historical evidence copy was not verified';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION catalog_history_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'Catalogue historical evidence is immutable';
END $$;
DROP TRIGGER IF EXISTS catalog_external_operation_history_immutable ON catalog_external_operation_history;
CREATE TRIGGER catalog_external_operation_history_immutable BEFORE UPDATE OR DELETE ON catalog_external_operation_history
 FOR EACH ROW EXECUTE FUNCTION catalog_history_immutable();
DROP TRIGGER IF EXISTS catalog_plan_format_history_immutable ON catalog_plan_format_history;
CREATE TRIGGER catalog_plan_format_history_immutable BEFORE UPDATE OR DELETE ON catalog_plan_format_history
 FOR EACH ROW EXECUTE FUNCTION catalog_history_immutable();

CREATE OR REPLACE FUNCTION protect_catalog_operation_variant() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM catalog_external_operation_history WHERE variant_id=OLD.id AND protects_identity) THEN
  RAISE EXCEPTION 'Размер связан с внешней операцией: его можно только архивировать';
 END IF;
 RETURN OLD;
END $$;
DROP TRIGGER IF EXISTS protect_exported_catalogue_variant ON product_variants;
DROP FUNCTION IF EXISTS protect_exported_catalogue_variant();
DROP TRIGGER IF EXISTS protect_catalog_operation_variant ON product_variants;
CREATE TRIGGER protect_catalog_operation_variant BEFORE DELETE ON product_variants
 FOR EACH ROW EXECUTE FUNCTION protect_catalog_operation_variant();

-- Counts only: safe inventory for operators. A nonzero immutable-data count is
-- a retirement blocker, not permission to reset/rewrite the original command.
CREATE OR REPLACE VIEW catalog_compatibility_inventory AS
 WITH frozen AS (
  SELECT p.*,CASE WHEN p.channel IN ('wb','ozon') THEN
   COALESCE(p.payload#>>'{marketplace,article}','')<>'' AND
   (p.payload#>'{channel,article}' IS NULL OR p.payload#>'{channel,article}'='null'::JSONB) AND
   (p.payload#>ARRAY['marketplace',p.channel,'article'] IS NULL OR p.payload#>ARRAY['marketplace',p.channel,'article']='null'::JSONB)
   ELSE FALSE END AS article_fallback
  FROM catalog_editor_publications p
 )
 SELECT 'editable_noncanonical_plan'::TEXT AS kind,COUNT(*)::BIGINT AS count
 FROM catalog_editor_plans WHERE config IS DISTINCT FROM catalog_editor_canonical_plan(config)
 UNION ALL SELECT 'editable_invalid_shape',COUNT(*) FROM catalog_editor_plans p
 WHERE jsonb_typeof(config) IS DISTINCT FROM 'object' OR jsonb_typeof(config->'variants') IS DISTINCT FROM 'array'
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(config->'variants')='array' THEN config->'variants' ELSE '[]'::JSONB END) v
  WHERE jsonb_typeof(v) IS DISTINCT FROM 'object'
   OR (v?'priceMode' AND v->'priceMode'<>'null'::JSONB AND COALESCE(v->>'priceMode','') NOT IN ('','auto','manual'))
   OR (v?'marketplace' AND v->'marketplace'<>'null'::JSONB AND jsonb_typeof(v->'marketplace')<>'object')
   OR (v->'marketplace'?'article' AND jsonb_typeof(v#>'{marketplace,article}') NOT IN ('string','null'))
   OR EXISTS(SELECT 1 FROM unnest(ARRAY['wb','ozon']) ch WHERE
    ((v->'marketplace')?ch AND v#>ARRAY['marketplace',ch]<>'null'::JSONB AND jsonb_typeof(v#>ARRAY['marketplace',ch])<>'object')
    OR ((v#>ARRAY['marketplace',ch])?'article' AND jsonb_typeof(v#>ARRAY['marketplace',ch,'article']) NOT IN ('string','null'))))
 UNION ALL SELECT 'frozen_shared_article',COUNT(*) FROM frozen WHERE article_fallback
 UNION ALL SELECT 'pending_or_unknown_shared_article',COUNT(*) FROM frozen WHERE article_fallback
  AND (status NOT IN ('created','synced','linked','cancelled','simulated') OR progress->>'ambiguous'='true')
 UNION ALL SELECT 'pending_revision_shared_article',COUNT(*) FROM frozen p
 WHERE p.channel IN ('wb','ozon') AND COALESCE(p.pending_payload#>>'{marketplace,article}','')<>''
  AND (p.pending_payload#>'{channel,article}' IS NULL OR p.pending_payload#>'{channel,article}'='null'::JSONB)
  AND (p.pending_payload#>ARRAY['marketplace',p.channel,'article'] IS NULL OR p.pending_payload#>ARRAY['marketplace',p.channel,'article']='null'::JSONB)
 UNION ALL SELECT 'saby_history_needs_reconciliation',COUNT(*) FROM catalog_external_operation_history
 WHERE provider='saby' AND needs_reconciliation;
