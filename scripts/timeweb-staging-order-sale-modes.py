"""Guarded three-key mock realization configuration for staging app 280757.

Dry-run is the default. Explicit apply never changes production, source branch,
non-target environment values, warehouses, documents, orders or inventory.
"""
import importlib.util
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('stage_audit', Path(__file__).with_name('timeweb-staging-order-sale-audit.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)
TARGET = {
    'ORDER_REALIZATION_MODE': 'mock',
    'ORDER_REALIZATION_LIVE_ENABLED': 'false',
    'ORDER_REALIZATION_SITE_WAREHOUSE_ID': '1',
}


def guarded(app):
    env = audit.guard(app)
    if audit.branch_name(app) != 'staging':
        raise audit.SafetyError('explicit_staging_branch_required')
    return env


def patch_app(previous, next_env):
    before = guarded(previous)
    if {key:value for key,value in next_env.items() if key not in TARGET} != {
            key:value for key,value in before.items() if key not in TARGET}:
        raise audit.SafetyError('unrelated_environment_change')
    if any(next_env.get(key) != value for key,value in TARGET.items()):
        raise audit.SafetyError('unapproved_target_values')
    request = urllib.request.Request(audit.APP_URL, method='PATCH', headers={
        'Authorization': 'Bearer ' + os.environ['TIMEWEB_CLOUD_TOKEN'],
        'Content-Type': 'application/json', 'Accept': 'application/json',
    }, data=json.dumps({'envs':next_env,'health_check':''}).encode())
    try:
        with urllib.request.build_opener(audit.NoRedirect()).open(request, timeout=40) as response:
            # Do not print a response containing environment credentials.
            response.read((2 << 20) + 1)
    except urllib.error.HTTPError as error:
        raise audit.SafetyError('patch_http_' + str(error.code) + '_inspect_before_retry') from None


def safe_modes(env):
    safe = audit.safe_flags(env)
    return {key:safe[key] for key in TARGET}


def run(apply=False, reader=None, inspector=None, writer=None):
    reader = reader or audit.read_app
    inspector = inspector or audit.inspect_app
    writer = writer or patch_app
    before = reader()
    env_before = guarded(before)
    if before.get('status') != 'active':
        raise audit.SafetyError('staging_app_must_be_active')
    inspection = inspector(before, emit_metadata=False)
    if (not inspection.get('databaseIsolated') or not inspection.get('databaseReadOnly') or
            inspection.get('rolePrivileged') or not any(row.get('id')==1 for row in inspection.get('activeWarehouses',[]))):
        raise audit.SafetyError('verified_active_staging_warehouse_1_required')
    next_env = dict(env_before)
    next_env.update(TARGET)
    changed = any(env_before.get(key)!=value for key,value in TARGET.items()) or bool(before.get('health_check'))
    report = {'appId':280757,'branch':'staging','applyRequested':bool(apply),
              'databaseVerifiedReadOnly':True,'activeSiteWarehouseId':1,
              'before':safe_modes(env_before),'target':dict(TARGET),
              'healthCheckEmptyBefore':not before.get('health_check'),'changed':changed,'applied':False}
    if not apply:
        report['after']=safe_modes(env_before)
        return report
    if changed:
        # Detect operator/deployment drift after the database preflight instead
        # of overwriting a newer complete environment dictionary.
        fresh = reader()
        env_fresh = guarded(fresh)
        if (env_fresh != env_before or fresh.get('commit_sha')!=before.get('commit_sha') or
                fresh.get('health_check')!=before.get('health_check') or fresh.get('status')!='active'):
            raise audit.SafetyError('staging_changed_since_preflight')
        writer(fresh, next_env)
    after = reader()
    env_after = guarded(after)
    if any(env_after.get(key)!=value for key,value in TARGET.items()) or after.get('health_check'):
        raise audit.SafetyError('approved_modes_not_confirmed')
    if {key:value for key,value in env_after.items() if key not in TARGET} != {
            key:value for key,value in env_before.items() if key not in TARGET}:
        raise audit.SafetyError('unrelated_environment_changed')
    report['after']=safe_modes(env_after)
    report['healthCheckEmptyAfter']=True
    report['applied']=changed
    return report


def main():
    try:
        result=run(apply=os.environ.get('APPLY_STAGING_ORDER_MODES')=='true')
        print(json.dumps(result))
    except audit.SafetyError as error:
        print(json.dumps({'error':str(error)}))
        raise SystemExit(1) from None
    except Exception as error:
        print(json.dumps({'error':type(error).__name__,'action':'inspect_read_only_before_retry'}))
        raise SystemExit(1) from None


if __name__=='__main__':main()
