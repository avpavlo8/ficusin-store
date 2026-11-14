#!/usr/bin/env python3
"""Read-only fiscal warning reconciliation. Aggregate output only; no PII or money."""
import argparse
import base64
import collections
import datetime
from decimal import Decimal, InvalidOperation
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.error
import urllib.parse
import urllib.request

spec = importlib.util.spec_from_file_location('receipt_audit_guards', Path(__file__).with_name('production-release-preflight.py'))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)
BASE = 'https://api.yookassa.ru/v3'
MODES = {'full_payment', 'full_prepayment', 'partial_prepayment', 'advance', 'partial_payment', 'credit', 'credit_payment'}
STATUSES = {'pending', 'waiting_for_capture', 'succeeded', 'canceled', 'cancelled', 'paid', 'failed', 'manual_review', 'unknown', 'ready', 'new', 'confirmed', 'assembling', 'shipped', 'completed', 'offered', 'expired'}
QUERY = """
WITH facts AS (
 SELECT p.id,p.provider_payment_id,ROUND(p.amount*100)::bigint amount_minor,
 p.receipt_status,p.receipt_checked_at,p.created_at,COALESCE(so.status,o.status) effective_status,
 p.request_payload->'ReceiptEnabled' receipt_enabled,
 (p.request_payload IS NULL) snapshot_absent,
 COALESCE(jsonb_path_query_array(p.request_payload,'$.Items[*].Mode'),'[]'::jsonb) || COALESCE(jsonb_path_query_array(p.request_payload,'$.items[*].payment_mode'),'[]'::jsonb) request_modes,
 COALESCE(sr.status,'') settlement_status,(sr.provider_receipt_id IS NOT NULL AND sr.provider_receipt_id<>'') settlement_reference,
 (p.created_at<CURRENT_TIMESTAMP-INTERVAL '15 minutes' AND
   (p.receipt_status IN ('canceled','unknown') OR (p.receipt_status IN ('','pending') AND p.created_at<CURRENT_TIMESTAMP-INTERVAL '3 days'))) income_warning,
 (COALESCE(so.status,o.status)='completed' AND
   (sr.payment_id IS NULL OR sr.status IN ('failed','manual_review','canceled') OR (sr.status='pending' AND sr.created_at<CURRENT_TIMESTAMP-INTERVAL '3 days'))
   AND p.amount>COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status='succeeded'),0)) settlement_warning
 FROM payments p JOIN orders o ON o.id=p.order_id
 LEFT JOIN shipment_offers so ON so.id=p.shipment_offer_id
 LEFT JOIN payment_settlement_receipts sr ON sr.payment_id=p.id
 WHERE p.status='paid' AND p.provider_payment_id<>''
) SELECT * FROM facts WHERE income_warning OR settlement_warning ORDER BY id LIMIT 101
"""
PENDING_QUERY = """
SELECT p.id,p.order_id,p.provider_payment_id,ROUND(p.amount*100)::bigint amount_minor,
 p.created_at,o.status order_status,o.payment_status order_payment_status,
 EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP-p.created_at))/60 age_minutes
FROM payments p JOIN orders o ON o.id=p.order_id
WHERE p.status='pending' AND o.status<>'cancelled'
 AND p.created_at<CURRENT_TIMESTAMP-INTERVAL '30 minutes'
ORDER BY p.created_at LIMIT 26
"""
UNPAID_QUERY = """
SELECT o.id,o.order_number,o.status,o.payment_status,o.payment_method,o.delivery_method,
 count(p.id) FILTER(WHERE p.status='pending') pending_count,
 count(p.id) FILTER(WHERE p.status='pending' AND p.created_at<CURRENT_TIMESTAMP-INTERVAL '30 minutes') stale_pending_count
FROM orders o LEFT JOIN payments p ON p.order_id=o.id
WHERE o.status NOT IN ('cancelled','completed') AND o.payment_status<>'paid'
GROUP BY o.id ORDER BY o.created_at DESC LIMIT 26
"""


def pending_report(pending, orders, reader):
    if len(pending)>25 or len(orders)>25:
        raise guards.PreflightError('pending_audit_scope_exceeded')
    details=[]
    for row in pending:
        item={'localPaymentId':int(row['id']),'orderId':int(row['order_id']),
              'orderStatus':label(row['order_status']), 'orderPaymentStatus':label(row['order_payment_status']),
              'ageMinutes':int(row['age_minutes']), 'createdAt':row['created_at'].isoformat(),
              'providerReferencePresent':bool(row['provider_payment_id'])}
        reference=row['provider_payment_id']
        if reference:
            if not re.fullmatch(r'[A-Za-z0-9_-]{1,100}',reference):
                raise guards.PreflightError('invalid_provider_payment_reference')
            meta,payment=reader.get('/payments/'+reference)
            item['providerRead']=meta
            if payment is not None:
                if payment.get('id')!=reference:
                    raise guards.PreflightError('provider_payment_identity_mismatch')
                item.update(providerStatus=label(payment.get('status')),
                            providerTest=payment.get('test') if isinstance(payment.get('test'),bool) else 'not_returned',
                            providerPaid=payment.get('paid') if isinstance(payment.get('paid'),bool) else 'not_returned',
                            amountMatches=amount_matches(payment,row['amount_minor']))
        details.append(item)
    active=[]
    for row in orders:
        number=row['order_number']
        if not re.fullmatch(r'[A-Za-z0-9_-]{1,64}',number):
            raise guards.PreflightError('invalid_order_reference')
        active.append({'orderId':int(row['id']),'orderNumber':number,
                       'orderStatus':label(row['status']),'paymentStatus':label(row['payment_status']),
                       'paymentMethod':label(row['payment_method'],{'online','on_delivery','invoice','manager_confirmation'}),
                       'deliveryMethod':label(row['delivery_method'],{'pickup','cdek','courier','post'}),
                       'pendingPayments':int(row['pending_count']),'stalePendingPayments':int(row['stale_pending_count'])})
    return {'stalePendingCount':len(details),'stalePendingPayments':details,'activeUnpaidOrders':active}


def label(value, allowed=STATUSES):
    return value if value in allowed else 'absent' if value in ('', None) else 'other'


class YooReader:
    def __init__(self, shop, secret, opener=None):
        if not shop or not secret:
            raise guards.PreflightError('payment_credentials_missing')
        self.auth = base64.b64encode((shop + ':' + secret).encode()).decode()
        self.opener = opener or urllib.request.build_opener(guards.NoRedirect())
        self.calls = 0

    def get(self, path, params=None):
        if path != '/me' and path != '/receipts' and not re.fullmatch(r'/payments/[A-Za-z0-9_-]{1,100}', path):
            raise guards.PreflightError('payment_read_path_not_allowed')
        if params and (path != '/receipts' or set(params) - {'payment_id', 'limit', 'cursor'}):
            raise guards.PreflightError('payment_query_not_allowed')
        self.calls += 1
        if self.calls > 100:
            raise guards.PreflightError('payment_read_limit')
        url = BASE + path + ('?' + urllib.parse.urlencode(params) if params else '')
        request = urllib.request.Request(url, method='GET', headers={'Authorization': 'Basic ' + self.auth, 'Accept': 'application/json'})
        try:
            with self.opener.open(request, timeout=20) as response:
                raw = response.read((2 << 20) + 1)
                status = response.status
        except urllib.error.HTTPError as error:
            return {'ok': False, 'httpStatus': error.code}, None
        except (urllib.error.URLError, TimeoutError):
            return {'ok': False, 'networkError': True}, None
        if len(raw) > 2 << 20:
            raise guards.PreflightError('payment_response_too_large')
        try:
            value = json.loads(raw)
        except Exception:
            raise guards.PreflightError('payment_invalid_json') from None
        if not isinstance(value, dict):
            raise guards.PreflightError('payment_invalid_response')
        return {'ok': True, 'httpStatus': status}, value


def receipt_list(reader, payment_id):
    summaries = collections.Counter()
    cursor = None
    seen = set()
    for _ in range(3):
        params = {'payment_id': payment_id, 'limit': 100}
        if cursor:
            params['cursor'] = cursor
        meta, data = reader.get('/receipts', params)
        if not meta['ok']:
            return {'read': meta, 'complete': False}
        items = data.get('items')
        if not isinstance(items, list):
            raise guards.PreflightError('invalid_receipt_list')
        for item in items:
            if item.get('payment_id') != payment_id:
                raise guards.PreflightError('receipt_payment_mismatch')
            modes = sorted({label(row.get('payment_mode'), MODES) for row in item.get('items', [])})
            key = json.dumps({'type': label(item.get('type'), {'payment', 'refund'}), 'status': label(item.get('status')), 'modes': modes,
                              'hasPrepaymentSettlement': any(row.get('type') == 'prepayment' for row in item.get('settlements', []))}, sort_keys=True)
            summaries[key] += 1
        cursor = data.get('next_cursor')
        if not cursor:
            return {'read': meta, 'complete': True, 'receipts': [{**json.loads(key), 'count': count} for key, count in sorted(summaries.items())]}
        if not isinstance(cursor, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,100}', cursor) or cursor in seen:
            raise guards.PreflightError('receipt_cursor_invalid')
        seen.add(cursor)
    return {'read': meta, 'complete': False, 'paginationLimitReached': True}


def amount_matches(payment, expected):
    try:
        value = Decimal(payment['amount']['value'])
        return payment['amount']['currency'] == 'RUB' and value.is_finite() and value * 100 == expected
    except (KeyError, InvalidOperation, TypeError, ValueError):
        return False


def reconcile(rows, reader):
    groups = collections.Counter()
    warnings = collections.Counter()
    for row in rows:
        payment_id = row['provider_payment_id']
        if not re.fullmatch(r'[A-Za-z0-9_-]{1,100}', payment_id):
            raise guards.PreflightError('invalid_provider_payment_reference')
        meta, payment = reader.get('/payments/' + payment_id)
        info = {'localReceipt': label(row['receipt_status']), 'localSettlement': label(row['settlement_status']),
                'localSettlementReferencePresent': row['settlement_reference'], 'effectiveStatus': label(row['effective_status']),
                'requestReceiptEnabled': row['receipt_enabled'] if isinstance(row['receipt_enabled'], bool) else 'not_recorded',
                'requestModes': sorted({label(value, MODES) for value in (row['request_modes'] or [])}),
                'providerReferenceLooksUUID': bool(re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', payment_id)),
                'createdMonth': row['created_at'].strftime('%Y-%m'), 'checkedAtPresent': row['receipt_checked_at'] is not None,
                'providerRead': meta}
        if payment is not None:
            if payment.get('id') != payment_id:
                raise guards.PreflightError('provider_payment_identity_mismatch')
            info.update(providerStatus=label(payment.get('status')), providerTest=payment.get('test') if isinstance(payment.get('test'), bool) else 'not_returned',
                        providerReceipt=label(payment.get('receipt_registration')), amountMatches=amount_matches(payment, row['amount_minor']))
        if payment is not None or meta.get('httpStatus') == 404:
            info['providerReceipts'] = receipt_list(reader, payment_id)
        for group, key in (('income', 'income_warning'), ('settlement', 'settlement_warning')):
            if row[key]:
                warnings[group] += 1
                groups[json.dumps({'warning': group, **info}, sort_keys=True)] += 1
    return {'ok': True, 'readOnly': True, 'databaseWrites': False, 'providerWrites': False, 'paymentCount': len(rows),
            'ownerConfirmedLegacyBackfillMatches': sum(1 for row in rows if row.get('snapshot_absent') is True and row['effective_status'] == 'cancelled' and row['created_at'].strftime('%Y-%m') == '2026-08' and row['receipt_status'] == '' and row['settlement_status'] == ''),
            'warnings': dict(warnings), 'groups': [{**json.loads(key), 'count': count} for key, count in sorted(groups.items())]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--expected-commit', required=True)
    args = parser.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS') != 'true':
            raise guards.PreflightError('github_actions_only')
        app = guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN', '')).get('/apps/229223').get('app', {})
        env = guards.production_settings(app, args.expected_commit)
        reader = YooReader(env.get('YOOKASSA_SHOP_ID', '').strip(), env.get('YOOKASSA_SECRET_KEY', '').strip())
        import psycopg.rows
        with tempfile.TemporaryDirectory(prefix='ficusin-receipt-audit-') as directory:
            with guards.connect_production(env, directory) as connection:
                expected_user = urllib.parse.unquote(urllib.parse.urlsplit(env['DATABASE_URL']).username)
                identity = connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()
                if identity != ('default_db', expected_user, 'on'):
                    raise guards.PreflightError('wrong_database_identity_or_readonly')
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor:
                    cursor.execute(QUERY)
                    rows = cursor.fetchall()
                    cursor.execute(PENDING_QUERY)
                    pending = cursor.fetchall()
                    cursor.execute(UNPAID_QUERY)
                    unpaid = cursor.fetchall()
                    cursor.execute("SELECT value FROM settings WHERE key='orders.auto_cancel_hours'")
                    hours = cursor.fetchone()
                    hours = hours['value'] if hours else '24'
                    if not re.fullmatch(r'[0-9]+(?:\.[0-9]+)?',hours):
                        raise guards.PreflightError('invalid_expiry_hours')
                if len(rows) > 100:
                    raise guards.PreflightError('fiscal_audit_scope_exceeded')
        report = reconcile(rows, reader)
        report['pendingAudit'] = pending_report(pending,unpaid,reader)
        report['autoCancelHours'] = hours
        report['expectedProductionCommit'] = args.expected_commit
        report['providerGetCalls'] = reader.calls
        print(json.dumps(report, sort_keys=True))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error, {'phase': 'payment_receipt_readonly_audit'})))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
