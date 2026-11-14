-- Disabled until explicitly wired by application configuration. Existing
-- internal preparations are not promoted or replayed by this migration.
CREATE TABLE order_sale_dispatch_control (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 execution_mode TEXT NOT NULL DEFAULT 'disabled' CHECK(execution_mode IN ('disabled','mock','live')),
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 site_warehouse_id BIGINT NOT NULL DEFAULT 0,
 native_warehouse_id BIGINT NOT NULL DEFAULT 0,
 movement_warehouse_id BIGINT NOT NULL DEFAULT 0,
 organization_id BIGINT NOT NULL DEFAULT 0,
 regulation_id BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK(NOT enabled OR execution_mode<>'disabled'),
 CHECK(NOT enabled OR execution_mode<>'live' OR
  (site_warehouse_id>0 AND native_warehouse_id>0 AND movement_warehouse_id>0 AND organization_id>0 AND regulation_id>0))
);
INSERT INTO order_sale_dispatch_control(singleton) VALUES(TRUE);
CREATE TABLE order_sale_publications (
 id BIGSERIAL PRIMARY KEY,
 intent_id BIGINT NOT NULL UNIQUE REFERENCES order_sale_intents(id) ON DELETE RESTRICT,
 execution_mode TEXT NOT NULL CHECK(execution_mode IN ('mock','live')),
 request JSONB NOT NULL,
 request_hash TEXT NOT NULL,
 progress JSONB NOT NULL DEFAULT '{"stage":"inspect"}',
 status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','processing','posted','needs_attention','failed')),
 message TEXT NOT NULL DEFAULT '',
 document_id TEXT NOT NULL DEFAULT '',
 document_uuid TEXT NOT NULL DEFAULT '',
 stock_verified BOOLEAN NOT NULL DEFAULT FALSE,
 stock_proof JSONB NOT NULL DEFAULT '{}',
 attempts INTEGER NOT NULL DEFAULT 0,
 lease_token TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK(request->>'executionMode'=execution_mode),
 CHECK(status<>'posted' OR (document_id<>'' AND document_uuid<>'' AND stock_verified AND stock_proof<>'{}'::JSONB))
);
-- The same native document cannot satisfy two independent handovers. Normalize
-- numeric live IDs and UUID case/whitespace without changing immutable receipts.
CREATE UNIQUE INDEX order_sale_publications_document_id_unique ON order_sale_publications (
 execution_mode,(CASE WHEN execution_mode='live' AND btrim(document_id) ~ '^[0-9]+$'
  THEN ltrim(btrim(document_id),'0') ELSE btrim(document_id) END)
) WHERE document_id<>'';
CREATE UNIQUE INDEX order_sale_publications_document_uuid_unique ON order_sale_publications (
 execution_mode,lower(btrim(document_uuid))
) WHERE document_uuid<>'';
CREATE INDEX order_sale_publications_queue_idx ON order_sale_publications(execution_mode,next_attempt_at,id)
 WHERE status IN ('queued','processing');
-- A technical immutability guard, not a workflow: changing configuration or
-- retrying a job cannot retarget an already accepted stock operation.
CREATE FUNCTION order_sale_publication_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.intent_id IS DISTINCT FROM OLD.intent_id OR NEW.execution_mode IS DISTINCT FROM OLD.execution_mode
  OR NEW.request IS DISTINCT FROM OLD.request OR NEW.request_hash IS DISTINCT FROM OLD.request_hash THEN
  RAISE EXCEPTION 'order sale publication request is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_sale_publications_immutable BEFORE UPDATE ON order_sale_publications
 FOR EACH ROW EXECUTE FUNCTION order_sale_publication_immutable();
