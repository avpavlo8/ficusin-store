-- One price-only editor lane replaces the retired manual procurement source.
-- A migrated proposal remains an unapproved draft in disabled mode. This file
-- performs no remote writes and never promotes old intent into a live queue.
CREATE TABLE catalog_editor_price_publications (
 id BIGSERIAL PRIMARY KEY,
 product_id BIGINT NOT NULL, -- immutable historical snapshot; current owner is read through variant_id
 variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
 channel TEXT NOT NULL CHECK(channel IN ('wb','ozon','saby','avito')),
 execution_mode TEXT NOT NULL DEFAULT 'disabled' CHECK(execution_mode IN ('disabled','mock','live')),
 target_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 expected_site_price_minor BIGINT NOT NULL,
 price_minor BIGINT NOT NULL CHECK(price_minor>0),
 compare_at_minor BIGINT,
 old_value_minor BIGINT,
 price_origin TEXT NOT NULL DEFAULT 'formula' CHECK(price_origin IN ('formula','explicit')),
 payload JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','queued','processing','synced','superseded','failed','needs_attention')),
 message TEXT NOT NULL DEFAULT '',
 progress JSONB NOT NULL DEFAULT '{}'::JSONB,
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_until TIMESTAMPTZ,
 claim_token TEXT NOT NULL DEFAULT '',
 approved_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
 approved_at TIMESTAMPTZ,
 created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 legacy_proposal_id BIGINT UNIQUE
);
CREATE INDEX catalog_editor_price_publications_due ON catalog_editor_price_publications(next_attempt_at,id)
 WHERE status IN ('queued','processing');
CREATE INDEX catalog_editor_price_publications_variant ON catalog_editor_price_publications(variant_id,created_at DESC);
CREATE INDEX catalog_editor_price_publications_target ON catalog_editor_price_publications(channel,execution_mode,target_id);
CREATE UNIQUE INDEX catalog_editor_price_publications_target_busy ON catalog_editor_price_publications(execution_mode,channel,target_id)
 WHERE status IN ('queued','processing','needs_attention');

-- Serialize retirement with old request producers, approvals and claims. Any
-- attempted/submitted/active command must be reconciled before this release.
LOCK TABLE procurement_action_batches,procurement_action_items IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id
  WHERE b.source='manual' AND (i.status NOT IN ('draft','completed','skipped')
    OR i.attempts<>0 OR i.last_attempt_at IS NOT NULL
    OR NULLIF(i.external_operation_id,'') IS NOT NULL
    OR i.locked_until IS NOT NULL OR NULLIF(i.lock_owner,'') IS NOT NULL))
  OR EXISTS(SELECT 1 FROM procurement_action_batches WHERE source='manual' AND status NOT IN ('draft','completed','cancelled')) THEN
  RAISE EXCEPTION 'Manual price actions require reconciliation before retirement';
 END IF;
 IF EXISTS(SELECT 1 FROM return_intakes r JOIN procurement_action_items i ON i.id=r.receipt_action_id
  JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.source='manual')
  OR EXISTS(SELECT 1 FROM marketplace_returns r JOIN procurement_action_items i ON i.id=r.receipt_action_id
  JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.source='manual') THEN
  RAISE EXCEPTION 'Manual price actions unexpectedly referenced by return receipts';
 END IF;
 IF EXISTS(SELECT 1 FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id
  WHERE b.source='manual' AND i.status='draft' AND (i.channel NOT IN ('wb','ozon','saby_price','avito')
   OR i.new_value<=0 OR i.approved_by IS NOT NULL OR b.approved_by IS NOT NULL OR b.approved_at IS NOT NULL)) THEN
  RAISE EXCEPTION 'Invalid or already approved manual price draft requires review';
 END IF;
END $$;

CREATE SCHEMA IF NOT EXISTS legacy_archive;
-- Independent snapshots: no defaults, triggers or FKs to the working queue.
CREATE TABLE legacy_archive.manual_price_batches AS SELECT * FROM procurement_action_batches WHERE source='manual';
ALTER TABLE legacy_archive.manual_price_batches ADD PRIMARY KEY(id);
CREATE TABLE legacy_archive.manual_price_items AS SELECT i.* FROM procurement_action_items i
 JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.source='manual';
ALTER TABLE legacy_archive.manual_price_items ADD PRIMARY KEY(id);
ALTER TABLE legacy_archive.manual_price_items ADD FOREIGN KEY(batch_id) REFERENCES legacy_archive.manual_price_batches(id);

WITH drafts AS (
 SELECT i.id,i.channel,i.external_article,i.new_value,i.compare_at_value,i.old_value,i.created_at,i.updated_at,
  b.manual_variant_id variant_id,b.manual_site_price_minor site_price,b.created_by,v.product_id,
  jsonb_build_object('action','price','variantId',b.manual_variant_id,'productId',v.product_id,
   'priceMinor',b.manual_site_price_minor,'price',jsonb_strip_nulls(jsonb_build_object(
    'targetId',i.external_article,'priceMinor',(i.new_value*100)::BIGINT,'compareAtMinor',(i.compare_at_value*100)::BIGINT)))
  || CASE WHEN i.channel='saby_price' THEN jsonb_build_object('sabyId',COALESCE(v.saby_id,'')) ELSE '{}'::jsonb END payload
 FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id
 JOIN product_variants v ON v.id=b.manual_variant_id
 WHERE b.source='manual' AND i.status='draft'
)
INSERT INTO catalog_editor_price_publications(product_id,variant_id,channel,target_id,fingerprint,
 expected_site_price_minor,price_minor,compare_at_minor,old_value_minor,payload,status,message,created_by,created_at,updated_at,legacy_proposal_id)
SELECT product_id,variant_id,CASE channel WHEN 'saby_price' THEN 'saby' ELSE channel END,external_article,
 encode(sha256(convert_to(payload::TEXT,'UTF8')),'hex'),site_price,(new_value*100)::BIGINT,
 (compare_at_value*100)::BIGINT,(old_value*100)::BIGINT,payload,'draft',
 'Перенесено из прежнего расчёта. Проверьте цену и связь; отправка требует явного подтверждения.',created_by,created_at,updated_at,id
FROM drafts;

-- Delete only the retired source. Retain shared procurement/return work and
-- reject cached old producers rather than allowing new unreachable proposals.
DELETE FROM procurement_action_items i USING procurement_action_batches b WHERE b.id=i.batch_id AND b.source='manual';
DELETE FROM procurement_action_batches WHERE source='manual';
DROP INDEX IF EXISTS procurement_manual_price_batches_variant_idx;
ALTER TABLE procurement_action_batches DROP CONSTRAINT procurement_action_batches_source_check;
ALTER TABLE procurement_action_batches ALTER COLUMN procurement_order_id SET NOT NULL;
ALTER TABLE procurement_action_batches DROP COLUMN manual_variant_id,DROP COLUMN manual_site_price_minor;
ALTER TABLE procurement_action_batches ADD CONSTRAINT procurement_action_batches_source_check CHECK(source='procurement');
