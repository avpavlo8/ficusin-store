-- Preserve unknown separately from a provider-confirmed live or test payment.
ALTER TABLE payments
 ADD COLUMN provider_test BOOLEAN,
 ADD COLUMN provider_test_source TEXT NOT NULL DEFAULT '',
 ADD COLUMN settlement_requested_at TIMESTAMPTZ,
 ADD COLUMN settlement_checked_at TIMESTAMPTZ;

-- Owner confirmed on 2026-11-13 that August payments were tests. Scope this
-- backfill to the cancelled legacy orders from the read-only fiscal audit;
-- never infer test mode from a 404 or from a missing receipt alone.
UPDATE payments p
SET provider_test=true, provider_test_source='owner_confirmed_august_2026'
FROM orders o
WHERE o.id=p.order_id AND o.status='cancelled' AND p.status='paid'
 AND p.created_at >= TIMESTAMPTZ '2026-08-01 00:00:00+03'
 AND p.created_at < TIMESTAMPTZ '2026-09-01 00:00:00+03'
 AND p.provider_payment_id<>'' AND p.receipt_status=''
 AND p.request_payload IS NULL
 AND NOT EXISTS(SELECT 1 FROM payment_settlement_receipts r WHERE r.payment_id=p.id);

ALTER TABLE payment_settlement_receipts
 ALTER COLUMN actor_id DROP NOT NULL,
 ADD COLUMN source TEXT NOT NULL DEFAULT 'owner',
 ADD CONSTRAINT payment_settlement_receipts_source_check CHECK (
  (source='owner' AND actor_id IS NOT NULL) OR
  (source='automatic' AND actor_id IS NULL)
 );

-- Both the worker and monitoring use the frozen original fiscal mode. A
-- missing or mixed snapshot needs review, not an invented prepayment receipt.
CREATE FUNCTION payment_fiscal_mode(payload JSONB) RETURNS TEXT
LANGUAGE SQL IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN jsonb_typeof(payload->'Items')='array' THEN CASE
  WHEN payload->>'ReceiptEnabled'='true' AND jsonb_array_length(payload->'Items')>0 THEN
   CASE
    WHEN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(payload->'Items') item
                    WHERE COALESCE(item->>'Mode','')<>'full_prepayment') THEN 'full_prepayment'
    WHEN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(payload->'Items') item
                    WHERE COALESCE(item->>'Mode','')<>'full_payment') THEN 'full_payment'
    ELSE 'unknown'
   END
  ELSE 'unknown'
 END ELSE 'unknown' END
$$;

-- Queue in the completion transaction, including CDEK updates and individual
-- shipment offers. Existing completed purchases are deliberately not replayed.
CREATE FUNCTION request_completed_payment_settlement() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='orders' THEN
  UPDATE payments SET settlement_requested_at=COALESCE(settlement_requested_at,CURRENT_TIMESTAMP)
   WHERE order_id=NEW.id AND shipment_offer_id IS NULL;
 ELSE
  UPDATE payments SET settlement_requested_at=COALESCE(settlement_requested_at,CURRENT_TIMESTAMP)
   WHERE shipment_offer_id=NEW.id;
 END IF;
 RETURN NEW;
END
$$;
CREATE TRIGGER orders_request_payment_settlement
 AFTER UPDATE OF status ON orders FOR EACH ROW
 WHEN (NEW.status='completed' AND OLD.status IS DISTINCT FROM NEW.status)
 EXECUTE FUNCTION request_completed_payment_settlement();
CREATE TRIGGER shipment_offers_request_payment_settlement
 AFTER UPDATE OF status ON shipment_offers FOR EACH ROW
 WHEN (NEW.status='completed' AND OLD.status IS DISTINCT FROM NEW.status)
 EXECUTE FUNCTION request_completed_payment_settlement();

-- Covers a payment inserted/adopted after the completion event. Updating the
-- provider classification alone cannot enqueue historical completed payments.
CREATE FUNCTION request_late_payment_settlement() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.status IS NOT DISTINCT FROM NEW.status THEN
   RETURN NEW;
  END IF;
 END IF;
 IF NEW.settlement_requested_at IS NULL AND EXISTS(
  SELECT 1 FROM orders o LEFT JOIN shipment_offers so ON so.id=NEW.shipment_offer_id
  WHERE o.id=NEW.order_id AND COALESCE(so.status,o.status)='completed'
 ) THEN
  NEW.settlement_requested_at=CURRENT_TIMESTAMP;
 END IF;
 RETURN NEW;
END
$$;
CREATE TRIGGER payments_request_late_settlement
 BEFORE INSERT OR UPDATE OF status ON payments FOR EACH ROW
 EXECUTE FUNCTION request_late_payment_settlement();

CREATE INDEX payments_automatic_settlement_idx ON payments(settlement_requested_at,id)
 WHERE settlement_requested_at IS NOT NULL AND status='paid' AND provider_test IS DISTINCT FROM true;
