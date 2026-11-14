#!/usr/bin/env python3
"""Aggregate-only inventory of remaining legacy data. No object or database writes."""
import argparse
import datetime
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.parse

spec = importlib.util.spec_from_file_location('preflight', Path(__file__).with_name('production-release-preflight.py'))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)

FILES = {
    'avatars': ('customers', 'avatar_object_key', 'avatar_image'),
    'returns': ('marketplace_return_photos', 'object_key', 'image'),
    'procurement': ('procurement_documents', 'object_key', 'content'),
    'chat': ('support_messages', 'object_key', 'image_data'),
    'reviews': ('product_review_photos', 'object_key', 'image'),
}

def select(cursor, sql):
    if not re.match(r'^\s*(SELECT|WITH)\s', sql, re.I) or re.search(r'\b(INSERT|UPDATE|DELETE|ALTER|DROP|CREATE|TRUNCATE|COPY|CALL|GRANT|VACUUM)\b', sql, re.I):
        raise guards.PreflightError('non_readonly_inventory_sql')
    cursor.execute(sql)
    return cursor.fetchall()

def inspect(cursor):
    columns = select(cursor, "SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public'")
    known = {}
    for item in columns:
        known.setdefault(item['table_name'], set()).add(item['column_name'])
    files = {}
    for kind, (table, key, data) in FILES.items():
        if not {key, data}.issubset(known.get(table, set())):
            files[kind] = {'available': False}
            continue
        sql = f"""SELECT COUNT(*)::bigint total,
          COUNT(*) FILTER(WHERE NULLIF({key},'') IS NULL AND {data} IS NOT NULL)::bigint legacy_unkeyed,
          COUNT(*) FILTER(WHERE NULLIF({key},'') IS NOT NULL)::bigint referenced,
          COUNT(*) FILTER(WHERE NULLIF({key},'') IS NOT NULL AND {data} IS NOT NULL)::bigint retained_bytes_with_reference,
          COUNT(*) FILTER(WHERE {data} IS NOT NULL AND octet_length({data})=0)::bigint empty_sources,
          COUNT(*) FILTER(WHERE NULLIF({key},'') IS NULL AND octet_length({data})>67108864)::bigint oversized_sources,
          COALESCE(SUM(octet_length({data})) FILTER(WHERE NULLIF({key},'') IS NULL),0)::bigint legacy_bytes
          FROM public.{table}"""
        files[kind] = {'available': True, **select(cursor, sql)[0]}
    if 'storage_scope' in known.get('product_review_photos', set()):
        files['reviewScopes'] = select(cursor, """SELECT
          CASE WHEN storage_scope IN ('private','legacy-public') THEN storage_scope ELSE 'other' END scope,
          COUNT(*)::bigint total,COUNT(*) FILTER(WHERE NULLIF(object_key,'') IS NOT NULL)::bigint referenced
          FROM public.product_review_photos GROUP BY 1 ORDER BY 1""")
    report = {'files': files}
    if {'reserved_qty','available_qty'}.issubset(known.get('inventory', set())):
        report['inventory'] = select(cursor, """SELECT COUNT(*) FILTER(WHERE reserved_qty<>0)::bigint reserved_rows,
          COALESCE(SUM(reserved_qty),0)::bigint reserved_units,
          COUNT(*) FILTER(WHERE reserved_qty<0)::bigint negative_reservations FROM public.inventory""")[0]
    if {'reserved_qty','order_id'}.issubset(known.get('order_items', set())):
        report['orderReservations'] = select(cursor, """SELECT
          CASE WHEN o.status IN ('cancelled','completed') THEN o.status ELSE 'active' END order_group,
          (o.stock_released_at IS NOT NULL) already_released,
          COUNT(DISTINCT o.id)::bigint orders,COUNT(*)::bigint lines,COALESCE(SUM(i.reserved_qty),0)::bigint units
          FROM public.order_items i JOIN public.orders o ON o.id=i.order_id
          WHERE i.reserved_qty<>0 GROUP BY 1,2 ORDER BY 1,2""")
    for table in ['shipment_offer_inventory_reservations', 'order_sale_intents']:
        if table in known:
            report[table] = select(cursor, f'SELECT COUNT(*)::bigint total FROM public.{table}')[0]
    if 'payments' in known:
        report['paymentStates'] = select(cursor, """SELECT
          CASE WHEN status IN ('paid','pending','cancelled','superseded','succeeded','canceled') THEN status ELSE 'other' END state,
          (NULLIF(provider_payment_id,'') IS NOT NULL) provider_reference_present,
          COUNT(*)::bigint total FROM public.payments GROUP BY 1,2 ORDER BY 1,2""")
    if 'catalog_editor_publications' in known:
        report['publications'] = select(cursor, """SELECT
          CASE WHEN channel IN ('saby','wb','ozon') THEN channel ELSE 'other' END channel,
          CASE WHEN status IN ('prepared','queued','processing','failed','needs_attention','created','synced','linked','simulated','cancelled') THEN status ELSE 'other' END state,
          CASE WHEN execution_mode IN ('live','mock','disabled') THEN execution_mode ELSE 'other' END execution_mode,COUNT(*)::bigint total FROM public.catalog_editor_publications GROUP BY 1,2,3 ORDER BY 1,2,3""")
    if 'catalog_saby_exports' in known:
        report['legacySabyExports'] = select(cursor, "SELECT COUNT(*)::bigint total,COUNT(*) FILTER(WHERE state IN ('queued','checking','verifying','unknown'))::bigint unresolved FROM public.catalog_saby_exports")[0]
    if 'catalog_editor_plans' in known:
        report['editorPlans'] = select(cursor, """WITH variants AS (
          SELECT p.product_id,v.value variant FROM public.catalog_editor_plans p,
          LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(p.config->'variants')='array' THEN p.config->'variants' ELSE '[]'::jsonb END) v
        ) SELECT (SELECT COUNT(*) FROM public.catalog_editor_plans)::bigint plans,COUNT(*)::bigint variants,
          COUNT(*) FILTER(WHERE NOT COALESCE(variant->'marketplace'->'wb','{}'::jsonb)?'article' OR variant->'marketplace'->'wb'->'article'='null'::jsonb)::bigint wb_article_fallback,
          COUNT(*) FILTER(WHERE NOT COALESCE(variant->'marketplace'->'ozon','{}'::jsonb)?'article' OR variant->'marketplace'->'ozon'->'article'='null'::jsonb)::bigint ozon_article_fallback
          FROM variants""")[0]
    return report

def storage_flags(env):
    def text(key):
        return str(env.get(key, '')).strip()
    public = bool(text('S3_BUCKET') and text('S3_ACCESS_KEY') and text('S3_SECRET_KEY'))
    private = bool(text('PRIVATE_S3_BUCKET') and (text('PRIVATE_S3_ACCESS_KEY') or text('S3_ACCESS_KEY')) and (text('PRIVATE_S3_SECRET_KEY') or text('S3_SECRET_KEY')))
    return {'privateConfigured': private, 'publicConfigured': public,
            'distinctBuckets': bool(text('PRIVATE_S3_BUCKET')) and text('PRIVATE_S3_BUCKET') != text('S3_BUCKET'),
            'objectsReadOrWritten': 0}

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--expected-commit',required=True)
    args=parser.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true': raise guards.PreflightError('github_actions_only')
        app=guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=guards.production_settings(app,args.expected_commit)
        import psycopg.rows
        with tempfile.TemporaryDirectory(prefix='ficusin-legacy-inventory-') as directory:
            with guards.connect_production(env,directory) as connection:
                expected=urllib.parse.unquote(urllib.parse.urlsplit(env['DATABASE_URL']).username)
                if connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()!=('default_db',expected,'on'):
                    raise guards.PreflightError('wrong_database_identity_or_readonly')
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor: report=inspect(cursor)
        report.update(expectedProductionCommit=args.expected_commit,storage=storage_flags(env),readOnly=True,
                      databaseWrites=0,providerCalls=0,objectsWritten=0,checkedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
        print(json.dumps(report,sort_keys=True));return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':'legacy_data_readonly_inventory'})));return 1

if __name__=='__main__':raise SystemExit(main())
