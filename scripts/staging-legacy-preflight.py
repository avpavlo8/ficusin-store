#!/usr/bin/env python3
"""Read-only cutover inventory of the fixed isolated staging database."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.parse
import urllib.request

def module(name,file):
    spec=importlib.util.spec_from_file_location(name,Path(__file__).with_name(file));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
guards=module('guards','production-release-preflight.py')
inventory=module('inventory','production-legacy-data-inventory.py')
APP=280757;DATABASE='ficusin_staging_20261031';LOGIN='ficusin_stage_20261031'

def require(condition,code):
    if not condition:raise guards.PreflightError(code)

def staging_settings(app,expected):
    require(bool(re.fullmatch('[0-9a-f]{40}',expected)),'invalid_expected_commit')
    require(app.get('id')==APP and app.get('name')=='ficusin-staging','wrong_staging_app')
    branch=app.get('branch_name') or app.get('branch');branch=branch.get('name') if isinstance(branch,dict) else branch
    require(branch=='staging' and app.get('commit_sha')==expected,'wrong_staging_revision')
    env=app.get('envs',{});require(isinstance(env,dict) and env.get('APP_ENV')=='staging','wrong_environment')
    require(env.get('CATALOG_PUBLICATION_MODE')=='mock' and env.get('CATALOG_PUBLICATION_LIVE_ENABLED','').lower()=='false','unsafe_publication_mode')
    require(env.get('FILE_STORAGE_MODE')=='mock','unsafe_file_mode')
    require(env.get('ORDER_REALIZATION_MODE','disabled') in ('mock','disabled') and env.get('ORDER_REALIZATION_LIVE_ENABLED','false').lower()=='false','unsafe_order_mode')
    parsed=urllib.parse.urlsplit(env.get('DATABASE_URL',''))
    require(parsed.scheme in ('postgres','postgresql') and parsed.path=='/'+DATABASE and urllib.parse.unquote(parsed.username or '')==LOGIN and bool(parsed.hostname) and not parsed.fragment,'wrong_stage_database')
    require(not {'dbname','database','host','hostaddr','user','port','service','options'}.intersection(urllib.parse.parse_qs(parsed.query)),'ambiguous_database')
    return env

def read_app():
    token=os.environ.get('TIMEWEB_CLOUD_TOKEN','');require(bool(token),'token_missing')
    req=urllib.request.Request('https://api.timeweb.cloud/api/v1/apps/280757',headers={'Authorization':'Bearer '+token},method='GET')
    with urllib.request.build_opener(guards.NoRedirect()).open(req,timeout=30) as response:raw=response.read((4<<20)+1)
    require(len(raw)<=4<<20,'response_too_large');return json.loads(raw).get('app',{})

def inspect(cursor):
    report=inventory.inspect(cursor)
    read=lambda sql:inventory.select(cursor,sql)
    report['migrations']=read('SELECT COUNT(*) total,MAX(name) latest FROM schema_migrations')[0]
    report['offerReserved']=read('SELECT COUNT(*) FILTER(WHERE checkout_reserved_qty<>0)::BIGINT rows FROM shipment_offer_items')[0]
    known={r['table_name'] for r in read("SELECT table_name FROM information_schema.tables WHERE table_schema='public'")}
    if 'catalog_saby_exports' in known:
        report['legacyExporterGate']=read("SELECT COUNT(*) FILTER(WHERE state NOT IN ('idle','synced','error') OR lease_until>NOW() OR (state<>'synced' AND attempted_at IS NOT NULL))::BIGINT blockers FROM catalog_saby_exports")[0]
    columns={r['column_name'] for r in read("SELECT column_name FROM information_schema.columns WHERE table_name='procurement_action_batches' AND table_schema='public'")}
    if 'source' in columns:
        report['manualPriceGate']=read("""SELECT COUNT(*) FILTER(WHERE i.status NOT IN('draft','completed','skipped') OR i.attempts<>0 OR i.last_attempt_at IS NOT NULL OR NULLIF(i.external_operation_id,'') IS NOT NULL OR i.locked_until IS NOT NULL OR NULLIF(i.lock_owner,'') IS NOT NULL)::BIGINT attempted,
        COUNT(*) FILTER(WHERE i.status='draft' AND (i.channel NOT IN('wb','ozon','saby_price','avito') OR i.new_value<=0 OR i.approved_by IS NOT NULL OR b.approved_by IS NOT NULL OR b.approved_at IS NOT NULL))::BIGINT invalid_drafts FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.source='manual'""")[0]
    if 'catalog_external_operation_history' in known:
        report['historyProof']=read("""SELECT COUNT(*)::BIGINT total,
        COUNT(*) FILTER(WHERE h.evidence IS DISTINCT FROM to_jsonb(e) OR h.evidence_hash IS DISTINCT FROM encode(sha256(convert_to(to_jsonb(e)::TEXT,'UTF8')),'hex'))::BIGINT mismatched
        FROM legacy_archive.catalog_saby_exports e LEFT JOIN catalog_external_operation_history h ON h.provider='saby' AND h.source_kind='commerce-export' AND h.source_key=e.import_code""")[0]
        report['catalogCompatibility']=read('SELECT kind,count FROM catalog_compatibility_inventory ORDER BY kind')
        report['planBackups']=read("SELECT COUNT(*)::BIGINT total,COUNT(*) FILTER(WHERE source_hash<>encode(sha256(convert_to(before_config::TEXT,'UTF8')),'hex'))::BIGINT bad_hash FROM catalog_plan_format_history")[0]
        report['fileArchive']=read('SELECT kind,COUNT(*)::BIGINT total FROM legacy_archive.file_binary_payloads GROUP BY kind ORDER BY kind')
        report['zeroBarriers']=read("SELECT COUNT(*)::BIGINT total,COUNT(*) FILTER(WHERE NOT convalidated)::BIGINT not_validated FROM pg_constraint WHERE connamespace='public'::regnamespace AND conname IN('inventory_legacy_reservation_zero','order_items_legacy_reservation_zero','shipment_offer_items_legacy_reservation_zero','shipment_offer_inventory_reservations_retired','customer_avatar_s3_only','return_photo_s3_only','procurement_document_s3_only','support_attachment_s3_only','review_media_s3_only','review_private_only')")[0]
        report['priceQueueModes']=read("SELECT CASE WHEN execution_mode IN('mock','disabled','live') THEN execution_mode ELSE 'other' END mode,COUNT(*)::BIGINT total FROM catalog_editor_price_publications GROUP BY 1 ORDER BY 1")
        report['reservedArchive']=read("SELECT (SELECT COUNT(*) FROM legacy_archive.reservation_cutover_orders)::BIGINT orders,(SELECT COUNT(*) FROM legacy_archive.reservation_cutover_inventory)::BIGINT inventory_rows,(SELECT COUNT(*) FROM legacy_archive.reservation_cutover_order_items)::BIGINT order_items")[0]
    report['mockObjects']={}
    for kind,(table,key,data) in inventory.FILES.items():
        scope=" AND f.storage_scope='private'" if kind=='reviews' else ''
        report['mockObjects'][kind]=read(f"""SELECT COUNT(*)::BIGINT referenced,
        COUNT(*) FILTER(WHERE m.object_key IS NULL)::BIGINT missing,
        COUNT(*) FILTER(WHERE m.object_key IS NOT NULL AND encode(sha256(m.body),'hex')<>m.sha256)::BIGINT corrupt,
        COUNT(*) FILTER(WHERE f.{data} IS NOT NULL AND m.body=f.{data})::BIGINT legacy_checksum_matched,
        COUNT(*) FILTER(WHERE f.{data} IS NOT NULL AND m.body IS DISTINCT FROM f.{data})::BIGINT legacy_mismatch
        FROM {table} f LEFT JOIN private_file_mock_objects m ON m.object_key=f.{key}
        WHERE NULLIF(f.{key},'') IS NOT NULL{scope}""")[0]
    return report

def main():
    p=argparse.ArgumentParser();p.add_argument('--expected-commit',required=True);args=p.parse_args()
    try:
        require(os.environ.get('GITHUB_ACTIONS')=='true','github_actions_only');env=staging_settings(read_app(),args.expected_commit)
        import psycopg.rows
        with tempfile.TemporaryDirectory(prefix='staging-cutover-') as directory:
            with guards.connect_production(env,directory) as connection:
                require(connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()==(DATABASE,LOGIN,'on'),'wrong_readonly_database')
                require(connection.execute("SELECT rolsuper,rolcreatedb,rolcreaterole FROM pg_roles WHERE rolname=current_user").fetchone()==(False,False,False),'unsafe_stage_role')
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor:report=inspect(cursor)
        report.update(readOnly=True,databaseWrites=0,businessWrites=0,stageApp=APP,expectedStageCommit=args.expected_commit,isolatedMock=True)
        print(json.dumps(report,sort_keys=True));return 0
    except Exception as e:
        print(json.dumps(guards.safe_failure(e,{'phase':'staging_legacy_preflight'})));return 1
if __name__=='__main__':raise SystemExit(main())
