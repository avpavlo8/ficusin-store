-- Payment reconciliation uses payments.paid_at. The duplicate order timestamp
-- belonged to the retired Start/Sync path and has no current reader or writer.
-- Keep every historical value, including NULL, detached from active orders.
LOCK TABLE public.orders IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.order_retired_payment_timestamps AS
 SELECT id AS order_id,paid_at,created_at AS original_created_at,
  CURRENT_TIMESTAMP AS archived_at
 FROM public.orders;
ALTER TABLE legacy_archive.order_retired_payment_timestamps
 ADD PRIMARY KEY(order_id);
-- No CASCADE: unexpected readers must block the complete transaction.
-- Actual payment rows and their authoritative paid_at remain unchanged.
ALTER TABLE public.orders DROP COLUMN paid_at;
