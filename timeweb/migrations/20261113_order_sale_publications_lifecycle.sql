-- New orders only: historical handover receipts remain immutable and are never
-- backfilled into early stock deductions by enabling the dispatcher.
ALTER TABLE order_sale_intents
 ADD COLUMN event_trigger TEXT NOT NULL DEFAULT 'handover' CHECK(event_trigger IN ('handover','created')),
 ADD COLUMN cancellation_requested_at TIMESTAMPTZ,
 ADD COLUMN cancellation_blocked BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN cancellation_verified BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN cancellation_verified_at TIMESTAMPTZ;
CREATE UNIQUE INDEX order_sale_created_order_unique ON order_sale_intents(order_id) WHERE event_trigger='created';
ALTER TABLE order_sale_publications
 ADD COLUMN desired_state TEXT NOT NULL DEFAULT 'posted' CHECK(desired_state IN ('posted','cancelled')),
 ADD COLUMN create_attempted BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN post_attempted BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN cancellation_blocked BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN cancellation_verified BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN cancellation_proof JSONB NOT NULL DEFAULT '{}',
 ADD COLUMN stock_verified_at TIMESTAMPTZ,
 ADD COLUMN cancellation_verified_at TIMESTAMPTZ;
-- Existing receipts may already have sent a request before this release. Never
-- infer "no remote document" for that historical cohort.
UPDATE order_sale_publications SET create_attempted=TRUE,post_attempted=TRUE;
ALTER TABLE order_sale_publications DROP CONSTRAINT order_sale_publications_status_check;
ALTER TABLE order_sale_publications ADD CONSTRAINT order_sale_publications_status_check CHECK(status IN ('queued','processing','posted','cancelled','needs_attention','failed'));
ALTER TABLE order_sale_publications ADD CHECK(NOT cancellation_verified OR (status='cancelled' AND desired_state='cancelled' AND cancellation_verified_at IS NOT NULL AND cancellation_proof<>'{}'::JSONB));

ALTER TABLE order_sale_intent_lines ADD COLUMN variant_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX order_sale_intent_lines_variant_idx ON order_sale_intent_lines(variant_id,intent_id);
