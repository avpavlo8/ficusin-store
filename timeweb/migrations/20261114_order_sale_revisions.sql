-- Snapshots are historical audit references, not live order-item ownership.
-- Keep every integer ID and intent parent FK while allowing removed live rows.
DO $$ DECLARE c RECORD; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='order_sale_intent_lines'::regclass AND confrelid='order_items'::regclass AND contype='f'
 LOOP EXECUTE format('ALTER TABLE order_sale_intent_lines DROP CONSTRAINT %I',c.conname); END LOOP;
END $$;
ALTER TABLE order_sale_intents ADD COLUMN origin_config JSONB,
 ADD COLUMN base_revision_id BIGINT, ADD COLUMN desired_revision_id BIGINT,
 ADD COLUMN active_revision_id BIGINT, ADD COLUMN applied_revision_id BIGINT;
CREATE TABLE order_sale_revisions (
 id BIGSERIAL PRIMARY KEY, intent_id BIGINT NOT NULL REFERENCES order_sale_intents(id) ON DELETE RESTRICT,
 revision_number BIGINT NOT NULL, request JSONB NOT NULL, request_hash TEXT NOT NULL,
 content_hash TEXT NOT NULL, order_items_snapshot JSONB NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('origin','queued','active','applied','superseded','needs_attention')),
 unpost_attempted BOOLEAN NOT NULL DEFAULT FALSE, unpost_verified BOOLEAN NOT NULL DEFAULT FALSE,
 unpost_proof JSONB NOT NULL DEFAULT '{}', unpost_verified_at TIMESTAMPTZ,
 post_attempted BOOLEAN NOT NULL DEFAULT FALSE, post_verified BOOLEAN NOT NULL DEFAULT FALSE,
 stock_proof JSONB NOT NULL DEFAULT '{}', stock_verified_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, applied_at TIMESTAMPTZ,
 UNIQUE(intent_id,revision_number)
);
ALTER TABLE order_sale_intents ADD FOREIGN KEY(base_revision_id) REFERENCES order_sale_revisions(id),
 ADD FOREIGN KEY(desired_revision_id) REFERENCES order_sale_revisions(id),
 ADD FOREIGN KEY(active_revision_id) REFERENCES order_sale_revisions(id),
 ADD FOREIGN KEY(applied_revision_id) REFERENCES order_sale_revisions(id);
CREATE TABLE order_sale_revision_lines (
 id BIGSERIAL PRIMARY KEY,revision_id BIGINT NOT NULL REFERENCES order_sale_revisions(id) ON DELETE RESTRICT,
 order_item_id BIGINT NOT NULL,variant_id BIGINT NOT NULL,name TEXT NOT NULL,sku TEXT NOT NULL,
 saby_id TEXT NOT NULL,quantity INTEGER NOT NULL CHECK(quantity>0),unit_price NUMERIC(14,2) NOT NULL CHECK(unit_price>=0),
 line_snapshot JSONB NOT NULL
);
CREATE INDEX order_sale_revision_lines_revision_variant_idx ON order_sale_revision_lines(revision_id,variant_id);
CREATE FUNCTION order_sale_revision_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.intent_id IS DISTINCT FROM OLD.intent_id OR NEW.revision_number IS DISTINCT FROM OLD.revision_number OR NEW.request IS DISTINCT FROM OLD.request OR NEW.request_hash IS DISTINCT FROM OLD.request_hash OR NEW.content_hash IS DISTINCT FROM OLD.content_hash OR NEW.order_items_snapshot IS DISTINCT FROM OLD.order_items_snapshot THEN RAISE EXCEPTION 'order sale revision content is immutable'; END IF;
 RETURN NEW; END $$;
CREATE TRIGGER order_sale_revision_immutable BEFORE UPDATE ON order_sale_revisions FOR EACH ROW EXECUTE FUNCTION order_sale_revision_immutable();
CREATE FUNCTION order_sale_revision_line_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'order sale revision lines are immutable'; END $$;
CREATE TRIGGER order_sale_revision_line_immutable BEFORE UPDATE OR DELETE ON order_sale_revision_lines FOR EACH ROW EXECUTE FUNCTION order_sale_revision_line_immutable();
CREATE TABLE order_sale_revision_effects (
 id BIGSERIAL PRIMARY KEY,intent_id BIGINT NOT NULL REFERENCES order_sale_intents(id),revision_id BIGINT NOT NULL REFERENCES order_sale_revisions(id),
 operation TEXT NOT NULL CHECK(operation IN ('origin_post','revision_unpost','revision_post','cancel_unpost')),
 posted BOOLEAN NOT NULL,proof JSONB NOT NULL,verified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(revision_id,operation)
);
CREATE TABLE order_sale_revision_effect_lines (
 effect_id BIGINT NOT NULL REFERENCES order_sale_revision_effects(id),variant_id BIGINT NOT NULL,quantity INTEGER NOT NULL CHECK(quantity>0),PRIMARY KEY(effect_id,variant_id)
);
CREATE TABLE order_sale_hold_layers (
 id BIGSERIAL PRIMARY KEY,intent_id BIGINT NOT NULL REFERENCES order_sale_intents(id),revision_id BIGINT NOT NULL REFERENCES order_sale_revisions(id),
 variant_id BIGINT NOT NULL,quantity INTEGER NOT NULL CHECK(quantity>0),admitted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(revision_id,variant_id)
);
CREATE TABLE order_sale_hold_releases (
 id BIGSERIAL PRIMARY KEY,layer_id BIGINT NOT NULL REFERENCES order_sale_hold_layers(id),effect_id BIGINT NOT NULL REFERENCES order_sale_revision_effects(id),
 quantity INTEGER NOT NULL CHECK(quantity>0),released_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(layer_id,effect_id)
);
-- Existing handover rows are untouched. Existing creation cohorts are seeded by
-- BeforeOrderEdit/worker using their exact immutable origin, never today's data.
ALTER TABLE order_sale_revisions ADD COLUMN before_revision_id BIGINT REFERENCES order_sale_revisions(id);
CREATE FUNCTION order_sale_revision_pending(order_id_arg BIGINT) RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM order_sale_intents WHERE order_id=order_id_arg AND event_trigger='created' AND desired_revision_id IS NOT NULL AND desired_revision_id IS DISTINCT FROM base_revision_id AND (desired_revision_id IS DISTINCT FROM applied_revision_id OR active_revision_id IS NOT NULL))
$$;
-- Pre-ledger orders without a writer do not carry trustworthy historical mode
-- admission. They require explicit operator review, never today's live config.
UPDATE order_sale_intents i SET origin_config=jsonb_build_object('Mode','disabled','Enabled',FALSE,'SiteWarehouseID',COALESCE(i.warehouse_id,0))
WHERE i.event_trigger='created' AND i.origin_config IS NULL AND NOT EXISTS(SELECT 1 FROM order_sale_publications p WHERE p.intent_id=i.id);
