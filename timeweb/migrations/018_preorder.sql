-- Orders for plants that are not on the shelf yet.
--
-- A shop that hides everything it has run out of loses the sale twice: the
-- customer does not see the plant, and nobody learns they wanted it. So the
-- card stays in the catalogue and the order is marked instead.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS has_preorder SMALLINT NOT NULL DEFAULT 0;

-- Marked per line, because an order can mix what is in stock with what is
-- not: the manager needs to know which plants hold the parcel up.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS is_preorder SMALLINT NOT NULL DEFAULT 0;
