-- Explicit offline reconciliation receipts. No daemon or migration releases
-- reservations, changes payments or writes to Saby. Historical identities are
-- detached from mutable catalogue/order rows and must survive their deletion.
CREATE TABLE legacy_reservation_reconciliations (
 operation_key TEXT PRIMARY KEY,
 variant_id BIGINT NOT NULL,
 fingerprint TEXT NOT NULL,
 before_snapshot JSONB NOT NULL,
 after_snapshot JSONB NOT NULL,
 reconciled_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK(length(fingerprint)=64)
);
CREATE FUNCTION legacy_reservation_receipt_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Legacy reservation reconciliation receipts are immutable'; END $$;
CREATE TRIGGER legacy_reservation_receipt_immutable
 BEFORE UPDATE OR DELETE ON legacy_reservation_reconciliations
 FOR EACH ROW EXECUTE FUNCTION legacy_reservation_receipt_immutable();
