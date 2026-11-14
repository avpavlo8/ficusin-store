-- Internal preparation only. No external dispatcher or stock movement is enabled.
-- Existing delivered orders are deliberately not backfilled.
CREATE TABLE order_sale_intents (
 id BIGSERIAL PRIMARY KEY,
 event_key TEXT NOT NULL UNIQUE,
 order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 shipment_offer_id BIGINT REFERENCES shipment_offers(id) ON DELETE RESTRICT,
 source TEXT NOT NULL CHECK(source IN ('automatic','manual')),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','prepared')),
 snapshot JSONB NOT NULL,
 warehouse_id BIGINT REFERENCES warehouses(id) ON DELETE RESTRICT,
 prepared_by BIGINT REFERENCES customers(id),
 prepared_at TIMESTAMPTZ,
 external_document_id TEXT NOT NULL DEFAULT '' CHECK(external_document_id=''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK((state='queued' AND prepared_at IS NULL) OR (state='prepared' AND prepared_at IS NOT NULL AND warehouse_id IS NOT NULL))
);
CREATE TABLE order_sale_intent_lines (
 intent_id BIGINT NOT NULL REFERENCES order_sale_intents(id) ON DELETE RESTRICT,
 order_item_id BIGINT NOT NULL REFERENCES order_items(id) ON DELETE RESTRICT,
 quantity INTEGER NOT NULL CHECK(quantity>0),
 PRIMARY KEY(intent_id,order_item_id)
);
CREATE INDEX order_sale_intent_lines_item_idx ON order_sale_intent_lines(order_item_id);
