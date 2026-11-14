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
    if 'source' in known.get('procurement_action_batches', set()):
        report['manualPriceGate'] = select(cursor,"""SELECT
          COUNT(*) FILTER(WHERE i.status NOT IN('draft','completed','skipped') OR i.attempts<>0 OR i.last_attempt_at IS NOT NULL OR NULLIF(i.external_operation_id,'') IS NOT NULL OR i.locked_until IS NOT NULL OR NULLIF(i.lock_owner,'') IS NOT NULL)::BIGINT attempted,
          COUNT(*) FILTER(WHERE i.status='draft' AND (i.channel NOT IN('wb','ozon','saby_price','avito') OR i.new_value<=0 OR i.approved_by IS NOT NULL OR b.approved_by IS NOT NULL OR b.approved_at IS NOT NULL))::BIGINT invalid_drafts,
          (SELECT COUNT(*) FROM procurement_action_batches WHERE source='manual' AND status NOT IN('draft','completed','cancelled'))::BIGINT active_batches
          FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.source='manual'""")[0]
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

def cutover_proof(cursor):
    if not select(cursor,"SELECT to_regclass('legacy_archive.file_binary_payloads') IS NOT NULL ready")[0]['ready']:
        return {'applied':False}
    report={'applied':True}
    report['migrations']=select(cursor,'SELECT COUNT(*)::BIGINT total,MAX(name) latest FROM schema_migrations')[0]
    manifest=json.loads(Path(__file__).with_name('production-release-migrations.json').read_text())
    if manifest.get('commit')!='b6ecc26cbbadaf725c7ebe7274008bb74510e31d' or len(manifest.get('names',[]))!=235:
        raise guards.PreflightError('invalid_release_migration_manifest')
    expected=set(manifest['names'])
    actual={row['name'] for row in select(cursor,'SELECT name FROM schema_migrations ORDER BY name')}
    if any(not re.fullmatch(r'[A-Za-z0-9_-]+\.sql',name) for name in actual):
        raise guards.PreflightError('invalid_migration_name')
    report['migrationSet']={'expected':len(expected),'missing':sorted(expected-actual),'retainedHistorical':sorted(actual-expected)}
    report['fileArchive']=select(cursor,"SELECT kind,COUNT(*)::BIGINT total,COUNT(*) FILTER(WHERE sha256<>encode(sha256(payload),'hex'))::BIGINT bad_hash FROM legacy_archive.file_binary_payloads GROUP BY kind ORDER BY kind")
    report['planBackups']=select(cursor,"SELECT COUNT(*)::BIGINT total,COUNT(*) FILTER(WHERE source_hash<>encode(sha256(convert_to(before_config::TEXT,'UTF8')),'hex'))::BIGINT bad_hash FROM catalog_plan_format_history")[0]
    report['historyProof']=select(cursor,"""SELECT COUNT(*)::BIGINT total,
      COUNT(*) FILTER(WHERE h.evidence IS DISTINCT FROM to_jsonb(e) OR h.evidence_hash IS DISTINCT FROM encode(sha256(convert_to(to_jsonb(e)::TEXT,'UTF8')),'hex'))::BIGINT mismatched
      FROM legacy_archive.catalog_saby_exports e LEFT JOIN catalog_external_operation_history h ON h.provider='saby' AND h.source_kind='commerce-export' AND h.source_key=e.import_code""")[0]
    report['zeroBarriers']=select(cursor,"SELECT COUNT(*)::BIGINT total,COUNT(*) FILTER(WHERE NOT convalidated)::BIGINT not_validated FROM pg_constraint WHERE connamespace='public'::regnamespace AND conname IN('inventory_legacy_reservation_zero','order_items_legacy_reservation_zero','shipment_offer_items_legacy_reservation_zero','shipment_offer_inventory_reservations_retired','customer_avatar_s3_only','return_photo_s3_only','procurement_document_s3_only','support_attachment_s3_only','review_media_s3_only','review_private_only')")[0]
    report['reservedArchive']=select(cursor,"SELECT (SELECT COUNT(*) FROM legacy_archive.reservation_cutover_orders)::BIGINT orders,(SELECT COUNT(*) FROM legacy_archive.reservation_cutover_inventory)::BIGINT inventory_rows,(SELECT COUNT(*) FROM legacy_archive.reservation_cutover_order_items)::BIGINT order_items")[0]
    report['compatibility']=select(cursor,'SELECT kind,count FROM catalog_compatibility_inventory ORDER BY kind')
    return report

def verify_private_objects(cursor, env):
    spec=importlib.util.spec_from_file_location('object_reader',Path(__file__).with_name('readonly-private-object-verification.py'))
    reader=importlib.util.module_from_spec(spec);spec.loader.exec_module(reader)
    import hashlib
    result={}
    archived=select(cursor,"SELECT to_regclass('legacy_archive.file_binary_payloads') IS NOT NULL ready")[0]['ready']
    for kind,(table,key,data) in FILES.items():
        scope=" AND f.storage_scope='private'" if kind=='reviews' else ''
        join=f" LEFT JOIN legacy_archive.file_binary_payloads a ON a.kind='{kind}' AND a.source_id=f.id AND a.destination_key=f.{key}" if archived else ''
        payload=f'COALESCE(f.{data},a.payload)' if archived else f'f.{data}'
        rows=select(cursor,f"SELECT f.{key} object_key,{payload} legacy_data FROM public.{table} f{join} WHERE NULLIF(f.{key},'') IS NOT NULL{scope} LIMIT 101")
        if len(rows)>100:raise guards.PreflightError('object_inventory_limit_exceeded')
        summary={'readable':0,'checksumMatched':0,'noRetainedSource':0,'mismatch':0,'unavailable':0}
        for row in rows:
            try:raw=reader.signed_get(env,row['object_key'])
            except Exception:
                summary['unavailable']+=1
                continue
            summary['readable']+=1
            if row['legacy_data'] is None:summary['noRetainedSource']+=1
            elif hashlib.sha256(raw).digest()==hashlib.sha256(bytes(row['legacy_data'])).digest():summary['checksumMatched']+=1
            else:summary['mismatch']+=1
        result[kind]=summary
    return result

def publication_diagnostics(cursor):
    rows=select(cursor,"""SELECT channel,status,message,progress->>'stage' stage,
      progress->>'ambiguous' ambiguous,progress->>'createAttempted' create_attempted,
      COALESCE(progress#>>'{ids,productId}','')<>'' has_remote_id,
      COALESCE(progress#>>'{ids,uuid}','')<>'' has_uuid,
      COALESCE(progress#>>'{native,preparedHash}','') LIKE 'json-v1:%' prepared_hash_v1,
      COALESCE(progress#>>'{native,preparedRequestHash}','') LIKE 'json-v1:%' request_hash_v1,
      pending_payload IS NOT NULL has_pending,attempts,lease_until>NOW() leased
      FROM public.catalog_editor_publications WHERE channel IN('saby','wb') ORDER BY variant_id LIMIT 10""")
    output=[]
    for row in rows:
        message=str(row.pop('message','')).lower()
        row['reasonTags']=[tag for tag,words in {'tnved':['тнвэд','тн вэд','tnved'], 'hash':['hash','хеш','хэш'], 'timeout':['timeout','timed out'], 'not_found':['not found','не найден'], 'preflight':['preflight'], 'identity':['identity','идентич'], 'mandatory':['обязател','required'], 'api_error':['ошиб','error']}.items() if any(word in message for word in words)]
        row['httpStatuses']=sorted(set(re.findall(r'(?i)HTTP[ :]+([45][0-9]{2})',message)))
        for field in ['stage','ambiguous','create_attempted']:
            if row[field] is not None and not re.fullmatch(r'[a-z_-]{1,60}',str(row[field])):row[field]='redacted'
        output.append(row)
    return output

def storage_flags(env):
    def text(key):
        return str(env.get(key, '')).strip()
    public = bool(text('S3_BUCKET') and text('S3_ACCESS_KEY') and text('S3_SECRET_KEY'))
    private = bool(text('PRIVATE_S3_BUCKET') and (text('PRIVATE_S3_ACCESS_KEY') or text('S3_ACCESS_KEY')) and (text('PRIVATE_S3_SECRET_KEY') or text('S3_SECRET_KEY')))
    return {'privateConfigured': private, 'publicConfigured': public,
            'distinctBuckets': bool(text('PRIVATE_S3_BUCKET')) and text('PRIVATE_S3_BUCKET') != text('S3_BUCKET'),
            'objectsWritten': 0}

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
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor:
                    report=inspect(cursor)
                    report['cutover']=cutover_proof(cursor)
                    report['frozenPublicationProof']=select(cursor,"SELECT COUNT(*)::BIGINT total,encode(sha256(convert_to(COALESCE(jsonb_agg(jsonb_build_object('variant',variant_id,'channel',channel,'payload',payload,'fingerprint',fingerprint,'pending',pending_payload,'pending_fingerprint',pending_fingerprint) ORDER BY variant_id,channel)::TEXT,'[]'),'UTF8')),'hex') sha256 FROM catalog_editor_publications")[0]
                    cursor.execute("SELECT has_database_privilege(current_user,current_database(),'CREATE') database_create,has_schema_privilege(current_user,'public','CREATE') schema_create,(SELECT COUNT(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p','v','m') AND NOT pg_has_role(current_user,c.relowner,'USAGE'))::BIGINT unowned_public_relations")
                    report['migrationPrivileges']=cursor.fetchone()
                    report['staticAssetsConfigured']=bool(env.get('STATIC_ASSETS_BASE_URL','').strip())
                    report['objectVerification']=verify_private_objects(cursor,env)
                    report['objectReadAttempts']=sum(item['readable']+item['unavailable'] for item in report['objectVerification'].values())
                    report['publicationDiagnostics']=publication_diagnostics(cursor)
                    if any(item['mismatch'] or item['unavailable'] for item in report['objectVerification'].values()):
                        raise guards.PreflightError('private_object_verification_failed')
                    proof=report['cutover']
                    if proof['applied']:
                        if args.expected_commit!='b6ecc26cbbadaf725c7ebe7274008bb74510e31d':
                            raise guards.PreflightError('wrong_cutover_manifest_revision')
                        report['operations']=select(cursor,Path(__file__).with_name('production-release-operations.sql').read_text())
                    if proof['applied'] and (proof['migrationSet']['missing'] or proof['zeroBarriers']!={'total':10,'not_validated':0} or proof['planBackups']['bad_hash'] or proof['historyProof']['mismatched'] or any(row['bad_hash'] for row in proof['fileArchive'])):
                        raise guards.PreflightError('cutover_proof_failed')
        report.update(expectedProductionCommit=args.expected_commit,storage=storage_flags(env),readOnly=True,
                      databaseWrites=0,providerCalls=0,objectsWritten=0,checkedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
        print(json.dumps(report,sort_keys=True));return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':'legacy_data_readonly_inventory'})));return 1

if __name__=='__main__':raise SystemExit(main())
