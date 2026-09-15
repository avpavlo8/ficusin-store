-- Additive correction: do not rewrite the already applied stage 11 migration.
-- A payment alone is not evidence of site shipment. Unknown delivery stays uncharged.
CREATE OR REPLACE FUNCTION finance_packaging_eligible(event sales_events)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
  SELECT event.event_type='sale' AND event.event_status='confirmed'
    AND event.reconciliation_status='counted' AND event.units>0
    AND EXISTS (SELECT 1 FROM product_variants v JOIN products p ON p.id=v.product_id
                WHERE v.id=event.canonical_variant_id AND p.catalog_section='plants')
    AND (event.channel IN ('ozon','wb') OR (event.channel='site' AND (
      EXISTS (SELECT 1 FROM orders o WHERE o.order_number=event.source_document_id
              AND o.delivery_method='cdek' AND o.status IN ('shipped','completed')
              AND NOT EXISTS (SELECT 1 FROM shipment_offers so WHERE so.order_id=o.id))
      OR EXISTS (SELECT 1 FROM shipment_offers so JOIN orders o ON o.id=so.order_id
                 WHERE o.order_number=event.source_document_id
                   AND event.source_line_id='offer:'||so.id::TEXT||':'||event.external_product_id
                   AND so.delivery_method='cdek' AND so.status IN ('shipped','completed'))
    )));
$$;
CREATE OR REPLACE FUNCTION snapshot_finance_packaging() RETURNS TRIGGER AS $$
BEGIN
  IF finance_packaging_eligible(NEW) THEN
    INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub)
    VALUES(NEW.id,NEW.units,150,NEW.units*150)
    ON CONFLICT(sales_event_id) DO UPDATE SET units=EXCLUDED.units,
      amount_rub=EXCLUDED.units*finance_packaging_snapshots.rate_rub;
  ELSE
    DELETE FROM finance_packaging_snapshots WHERE sales_event_id=NEW.id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS sales_events_finance_packaging ON sales_events;
CREATE TRIGGER sales_events_finance_packaging AFTER INSERT OR UPDATE ON sales_events
FOR EACH ROW EXECUTE FUNCTION snapshot_finance_packaging();
DELETE FROM finance_packaging_snapshots p USING sales_events s
WHERE p.sales_event_id=s.id AND NOT finance_packaging_eligible(s);
INSERT INTO finance_packaging_snapshots(sales_event_id,units,rate_rub,amount_rub)
SELECT s.id,s.units,150,s.units*150 FROM sales_events s WHERE finance_packaging_eligible(s)
ON CONFLICT(sales_event_id) DO NOTHING;
