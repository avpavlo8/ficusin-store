-- Not every order can be quoted automatically: a plant may have no box
-- dimensions filled in, CDEK may be down, or the customer may have asked
-- whether everything fits into one box. None of that should block the order,
-- so the price is simply marked as "a person will work it out".
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS delivery_fee_pending SMALLINT NOT NULL DEFAULT 0;

ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS delivery_repack_requested SMALLINT NOT NULL DEFAULT 0;
