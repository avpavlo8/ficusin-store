-- Current checkout stores customer_id; it neither creates nor reads the old
-- direct order -> organization link. Organization accounts and membership remain
-- active. Preserve every original order identity/value, including NULL links.
LOCK TABLE public.orders IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.order_retired_organization_links AS
 SELECT id AS order_id, organization_id, created_at AS original_created_at,
  CURRENT_TIMESTAMP AS archived_at
 FROM public.orders;
ALTER TABLE legacy_archive.order_retired_organization_links
 ADD PRIMARY KEY(order_id);
-- Detached history: deleting an active order or organization must not delete
-- its original link. orders has no updated_at column to archive.
-- No CASCADE: unexpected dependent objects must abort the whole migration.
ALTER TABLE public.orders DROP COLUMN organization_id;
