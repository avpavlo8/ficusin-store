-- This table is the immutable before-state of the one-off 20261008 local
-- reservation reset. No application worker reads or writes it. Retain every
-- original value for audit while removing it from the active schema.
CREATE SCHEMA IF NOT EXISTS legacy_archive;
ALTER TABLE public.reservation_reset_audit SET SCHEMA legacy_archive;
