#!/usr/bin/env python3
"""Read-only inventory of cash-on-receipt orders and existing realizations.

No vendor writes, DB writes, payments or fiscal requests. Output excludes contacts.
"""
import importlib.util
import json
import os
from pathlib import Path
import tempfile

spec = importlib.util.spec_from_file_location('guards', Path(__file__).with_name('production-release-preflight.py'))
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)

def main():
    reader = g.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN', ''))
    payload = reader.get(f'/apps/{g.APP_ID}')
    settings = g.production_settings(payload['app'], os.environ['EXPECTED_COMMIT'])
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(settings, directory) as conn:
            if conn.execute("SELECT current_database(),current_setting('transaction_read_only')").fetchone() != (g.DATABASE, 'on'):
                raise g.PreflightError('wrong_database_or_not_readonly')
            control = conn.execute("SELECT execution_mode,enabled,site_warehouse_id,native_warehouse_id,movement_warehouse_id FROM order_sale_dispatch_control WHERE singleton").fetchone()
            cash = conn.execute("SELECT o.order_number,o.status,o.payment_status,o.delivery_method,o.created_at::text,i.id,i.event_trigger,i.state,p.execution_mode,p.status,p.document_id,p.document_uuid,p.create_attempted,p.post_attempted,p.stock_verified,p.cancellation_verified,p.progress->>'stage',p.progress->>'unknown' FROM orders o LEFT JOIN order_sale_intents i ON i.order_id=o.id LEFT JOIN order_sale_publications p ON p.intent_id=i.id WHERE o.payment_method='cash' ORDER BY o.id,i.id").fetchall()
            publications = conn.execute("SELECT o.order_number,o.payment_method,o.status,p.execution_mode,p.status,p.document_id,p.document_uuid,p.create_attempted,p.post_attempted,p.stock_verified,p.cancellation_verified FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id JOIN orders o ON o.id=i.order_id ORDER BY p.id").fetchall()
            methods = conn.execute("SELECT payment_method,status,count(*) FROM orders GROUP BY payment_method,status ORDER BY payment_method,status").fetchall()
            cols=['orderNumber','orderStatus','paymentStatus','deliveryMethod','createdAt','intentId','trigger','intentState','executionMode','publicationStatus','documentId','documentUUID','createAttempted','postAttempted','stockVerified','cancellationVerified','stage','unknown']
            pcols=['orderNumber','paymentMethod','orderStatus','executionMode','publicationStatus','documentId','documentUUID','createAttempted','postAttempted','stockVerified','cancellationVerified']
            print(json.dumps({'readOnly':True,'dispatcher':dict(zip(['mode','enabled','siteWarehouseId','nativeWarehouseId','movementWarehouseId'],control)), 'cashOrders':[dict(zip(cols,r)) for r in cash], 'allPublications':[dict(zip(pcols,r)) for r in publications], 'orderCohorts':[dict(zip(['paymentMethod','status','count'],r)) for r in methods]},ensure_ascii=False))

if __name__=='__main__':
    diagnostics={'phase':'cashier_inventory'}
    try: main()
    except Exception as exc:
        print(json.dumps(g.safe_failure(exc,diagnostics)))
        raise SystemExit(1)
