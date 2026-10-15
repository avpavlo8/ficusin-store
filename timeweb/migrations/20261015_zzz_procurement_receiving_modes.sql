-- Invoice lines remain the source of quantity and logistics. Receipt-only child
-- rows give each physically sorted cultivar its own existing catalog identity.
ALTER TABLE procurement_order_lines
 ADD COLUMN IF NOT EXISTS receiving_mode TEXT NOT NULL DEFAULT 'catalog'
 CHECK(receiving_mode IN ('catalog','assortment','customer_handoff','awaiting_catalog')),
 ADD COLUMN IF NOT EXISTS receiving_parent_line_id BIGINT REFERENCES procurement_order_lines(id) ON DELETE CASCADE,
 ADD COLUMN IF NOT EXISTS customer_handed_over BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE procurement_order_lines DROP CONSTRAINT IF EXISTS procurement_order_lines_match_status_check;
ALTER TABLE procurement_order_lines ADD CONSTRAINT procurement_order_lines_match_status_check
 CHECK(match_status IN ('unmatched','suggested','confirmed','new_product','ignored','deferred'));
CREATE INDEX IF NOT EXISTS procurement_receiving_children_idx ON procurement_order_lines(receiving_parent_line_id)
 WHERE receiving_parent_line_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS procurement_receiving_card_unique ON procurement_order_lines(receiving_parent_line_id,saby_id)
 WHERE receiving_parent_line_id IS NOT NULL;
