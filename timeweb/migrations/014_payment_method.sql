-- How the customer intends to pay. Three ways, and which are offered
-- depends on who is buying and how they collect:
--
--   online       card on the site, the default
--   on_delivery  pay at the counter, offered only for self-collection
--   invoice      an invoice from the manager, offered only to wholesale
--
-- Existing orders predate online payment and are all "online, unpaid",
-- which is exactly what payment_status already says about them.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS payment_method TEXT NOT NULL DEFAULT 'online';
