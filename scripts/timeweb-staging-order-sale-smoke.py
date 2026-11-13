"""Prepare/run ONLY an isolated, synthetic staging realization worker smoke.

Default is read-only. This file has no environment/app mutation endpoint.
The worker runner imports the EXACT deployed source's core Enqueue helper.
All raw subprocess/driver output is captured: only bounded technical reports
are emitted. Never use this workflow ref to apply the environment setter.
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

_spec = importlib.util.spec_from_file_location('audit', Path(__file__).with_name('timeweb-staging-order-sale-audit.py'))
audit = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(audit)


def preflight(app, source_sha, inspector=None):
    env = audit.guard(app)
    if not re.fullmatch(r'[0-9a-f]{40}', source_sha or ''):
        raise audit.SafetyError('exact_source_sha_required')
    if (audit.branch_name(app) != 'staging' or app.get('status') != 'active' or
            app.get('commit_sha') != source_sha):
        raise audit.SafetyError('deployed_source_or_branch_mismatch')
    required = {'FILE_STORAGE_MODE': 'mock', 'CATALOG_PUBLICATION_MODE': 'mock',
                'CATALOG_PUBLICATION_LIVE_ENABLED': 'false',
                'ORDER_REALIZATION_MODE': 'mock', 'ORDER_REALIZATION_LIVE_ENABLED': 'false',
                'ORDER_REALIZATION_SITE_WAREHOUSE_ID': '1'}
    if any(str(env.get(key, '')).lower() != value for key, value in required.items()):
        raise audit.SafetyError('staging_mock_isolation_required')
    report = (inspector or audit.inspect_app)(app, emit_metadata=False)
    if (report['dispatchControl'] != {'executionMode': 'mock', 'enabled': True, 'siteWarehouseId': 1}
            or not any(row['id'] == 1 for row in report['activeWarehouses'])):
        raise audit.SafetyError('active_mock_worker_and_warehouse_required')
    return env


def candidate_head(candidate):
    result = subprocess.run(['git', '-C', str(candidate), 'rev-parse', 'HEAD'],
                            capture_output=True, text=True, timeout=20, check=False)
    if result.returncode:
        raise audit.SafetyError('candidate_git_unavailable')
    return result.stdout.strip()


def launch(candidate, env, source_sha, run=subprocess.run):
    backend = candidate / 'backend'
    if candidate_head(candidate) != source_sha:
        raise audit.SafetyError('candidate_source_mismatch')
    # No cloud token or integration credentials reach the child. Only pgx TLS
    # settings for the exact guarded staging database are passed in memory.
    child = {key: os.environ[key] for key in ('PATH', 'HOME', 'GOCACHE', 'GOMODCACHE') if key in os.environ}
    child.update({'GOENV': 'off', 'GOWORK': 'off', 'GOTOOLCHAIN': 'local',
                  'APP_ENV': 'staging', 'SMOKE_STAGE_GUARD': '280757',
                  'SMOKE_SOURCE_SHA': source_sha, 'DATABASE_URL': env['DATABASE_URL'],
                  'DATABASE_SSL_VERIFY': env.get('DATABASE_SSL_VERIFY', 'true'),
                  'DATABASE_SSL_CA': env.get('DATABASE_SSL_CA', '')})
    # Parent prepares dependencies separately, BEFORE injecting any credentials.
    # A temporary package inside backend may legally import internal/ordersale.
    with tempfile.TemporaryDirectory(prefix='stage_sale_smoke_', dir=backend) as package:
        shutil.copyfile(Path(__file__).with_name('staging-order-realization-smoke.go'), Path(package) / 'main.go')
        result = run(['go', 'run', './' + Path(package).name], cwd=backend, env=child,
                     capture_output=True, text=True, timeout=360, check=False)
    # Never echo stdout/stderr from a compiler, driver or SQL exception.
    if len(result.stdout) > 16384:
        raise audit.SafetyError('smoke_report_too_large')
    allowed_results = {'passed', 'runner_failed', 'runner_guard_failed', 'database_target_failed', 'database_config_failed', 'database_ca_failed', 'actual_database_guard_failed', 'worker_mock_guard_failed', 'database_connection_failed', 'random_failed', 'fixture_begin_failed', 'fixture_product_failed', 'fixture_variant_failed', 'fixture_order_failed', 'fixture_item_failed', 'core_enqueue_failed', 'publication_not_enqueued', 'fixture_commit_uncertain', 'invalid_receipt_json', 'mock_receipt_mismatch', 'request_line_mismatch', 'proof_mismatch', 'proof_line_mismatch', 'publication_observation_failed', 'publication_mode_changed', 'mock_worker_rejected_fixture', 'smoke_cancelled', 'mock_worker_timeout', 'cleanup_begin_failed', 'cleanup_ownership_query_failed', 'cleanup_ownership_failed', 'cleanup_lock_failed', 'cleanup_delete_failed', 'cleanup_commit_uncertain', 'cleanup_processing_retained'}
    reports = []
    for line in result.stdout.splitlines():
        try:
            value = json.loads(line)
        except (ValueError, TypeError):
            continue
        if not isinstance(value, dict) or value.get('probe') != 'staging_sale_smoke':
            continue
        safe = {key: value[key] for key in ('probe', 'result', 'cleanup', 'productId', 'variantId', 'orderId', 'intentId', 'publicationId') if key in value}
        if any(type(value) is not int or value <= 0 for key, value in safe.items() if key.endswith('Id')):
            raise audit.SafetyError('invalid_smoke_report')
        if safe.get('result') not in allowed_results or safe.get('cleanup') not in ('not_needed', 'removed', 'retained'):
            raise audit.SafetyError('invalid_smoke_report')
        reports.append(safe)
    if result.returncode or not reports or reports[-1].get('result') != 'passed' or reports[-1].get('cleanup') != 'removed':
        for report in reports:
            print(json.dumps(report))
        raise audit.SafetyError('smoke_failed_details_masked')
    return reports[-1]


def execute(source_sha, candidate, run_smoke=False, reader=None, inspector=None, launcher=None):
    app = (reader or audit.read_app)()
    env = preflight(app, source_sha, inspector)
    if not run_smoke:
        return {'probe': 'staging_sale_smoke_plan', 'appId': 280757, 'sourceSha': source_sha,
                'executionMode': 'mock', 'siteWarehouseId': 1, 'applied': False}
    # A second GET immediately before inserting rejects a concurrent deployment
    # or environment drift after the read-only database check.
    fresh = (reader or audit.read_app)()
    if fresh.get('envs') != app.get('envs') or fresh.get('commit_sha') != app.get('commit_sha'):
        raise audit.SafetyError('staging_changed_during_preflight')
    preflight(fresh, source_sha, inspector)
    result = (launcher or launch)(Path(candidate).resolve(), env, source_sha)
    after = (reader or audit.read_app)()
    if after.get('envs') != app.get('envs') or after.get('commit_sha') != app.get('commit_sha'):
        raise audit.SafetyError('staging_changed_during_smoke')
    preflight(after, source_sha, inspector)
    return result


def main():
    try:
        report = execute(os.environ.get('SMOKE_SOURCE_SHA', ''), os.environ.get('SMOKE_CANDIDATE', '../candidate'),
                         os.environ.get('RUN_STAGING_ORDER_SMOKE') == 'true')
        print(json.dumps(report))
    except audit.SafetyError as error:
        print(json.dumps({'probe': 'staging_sale_smoke', 'result': str(error)}))
        raise SystemExit(1) from None
    except Exception as error:
        print(json.dumps({'probe': 'staging_sale_smoke', 'result': type(error).__name__.lower()}))
        raise SystemExit(1) from None


if __name__ == '__main__':
    main()
