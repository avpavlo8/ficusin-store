"""Read only one isolated staging app and its active local warehouse IDs.

No environment mutation, schema change, authentication write or production
request is implemented. Secrets and database connection details stay in memory.
"""
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.error
import urllib.parse
import urllib.request

APP_URL = 'https://api.timeweb.cloud/api/v1/apps/280757'
SITE = 'https://avpavlo8-ficusin-store-1b1b.twc1.net'
DATABASE = 'ficusin_staging_20261031'
LOGIN = 'ficusin_stage_20261031'
FLAGS = ('APP_ENV', 'FILE_STORAGE_MODE', 'CATALOG_PUBLICATION_MODE',
         'CATALOG_PUBLICATION_LIVE_ENABLED', 'ORDER_REALIZATION_MODE',
         'ORDER_REALIZATION_LIVE_ENABLED', 'ORDER_REALIZATION_SITE_WAREHOUSE_ID')


class SafetyError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


def read_app():
    request = urllib.request.Request(APP_URL, method='GET', headers={
        'Authorization': 'Bearer ' + os.environ['TIMEWEB_CLOUD_TOKEN'],
        'Accept': 'application/json',
    })
    opener = urllib.request.build_opener(NoRedirect())
    try:
        with opener.open(request, timeout=40) as response:
            raw = response.read((2 << 20) + 1)
    except urllib.error.HTTPError as error:
        raise SafetyError('app_get_http_' + str(error.code)) from None
    if len(raw) > 2 << 20:
        raise SafetyError('app_response_too_large')
    return json.loads(raw)['app']


def branch_name(app):
    value=app.get('branch_name') or app.get('branch')
    if isinstance(value,dict):value=value.get('name')
    return value if isinstance(value,str) and re.fullmatch(r'[A-Za-z0-9_./-]{1,200}',value) else None


def guard(app):
    if app.get('id') != 280757 or app.get('name') != 'ficusin-staging':
        raise SafetyError('wrong_application')
    env = app.get('envs')
    if not isinstance(env, dict) or env.get('APP_ENV') != 'staging':
        raise SafetyError('wrong_environment')
    if env.get('SITE_URL', '').rstrip('/') != SITE:
        raise SafetyError('wrong_staging_origin')
    # Some App API revisions omit branch_name or return branch instead. Its
    # absence is reported, never fabricated, and does not relax database guards.
    if branch_name(app) not in (None,'staging'):
        raise SafetyError('wrong_staging_branch')
    parsed = urllib.parse.urlsplit(env.get('DATABASE_URL', ''))
    if (parsed.scheme not in ('postgres', 'postgresql') or
            urllib.parse.unquote(parsed.path) != '/' + DATABASE or
            urllib.parse.unquote(parsed.username or '') != LOGIN or not parsed.hostname):
        raise SafetyError('wrong_database_configuration')
    return env


def safe_flags(env):
    result = {}
    for name in FLAGS:
        value = str(env.get(name, '')).strip()
        if not value:
            result[name] = '<unset>'
        elif name.endswith('_ID'):
            result[name] = value if re.fullmatch(r'[1-9][0-9]{0,18}', value) else '<invalid>'
        else:
            result[name] = value if value in ('staging', 'mock', 'live', 'disabled', 's3', 'true', 'false') else '<invalid>'
    return result


def safe_expiry(row):
    # Match settings.Service.Number: missing/invalid values use default24;
    # zero or negative values disable expiry. Never print the raw DB value.
    if row is None:
        return {'orderAutoCancelHours': 24, 'orderAutoCancelSource': 'default', 'orderAutoCancelEnabled': True}
    raw = str(row[0]).strip()
    value = None
    if len(raw) <= 128 and re.fullmatch(r'[+-]?[0-9]+', raw):
        parsed = int(raw)
        if -(1 << 63) <= parsed < (1 << 63):
            value = parsed
    if value is None:
        return {'orderAutoCancelHours': 24, 'orderAutoCancelSource': 'invalid_fallback', 'orderAutoCancelEnabled': True}
    return {'orderAutoCancelHours': value, 'orderAutoCancelSource': 'stored', 'orderAutoCancelEnabled': value > 0}


def database_options(env, directory):
    options = {'connect_timeout': 20,
               'options': '-c default_transaction_read_only=on -c statement_timeout=10000 -c lock_timeout=3000',
               'sslmode': 'verify-full' if str(env.get('DATABASE_SSL_VERIFY', 'true')).lower() not in ('false', '0') else 'require'}
    ca = env.get('DATABASE_SSL_CA', '').strip().strip('"\'').replace('\\n', '\n')
    if ca:
        path = Path(directory) / 'staging-root.crt'
        path.touch(mode=0o600)
        path.write_text(ca)
        options['sslrootcert'] = str(path)
    return options


def inspect(connect=None):
    return inspect_app(read_app(), connect)


def inspect_app(app, connect=None, emit_metadata=True):
    if emit_metadata and app.get('id')==280757 and app.get('name')=='ficusin-staging':
        envs=app.get('envs') or {}
        print(json.dumps({'probe':'stage_metadata','siteHost':urllib.parse.urlsplit(envs.get('SITE_URL','')).hostname,'branch':branch_name(app),'branchFieldsPresent':[key for key in ('branch','branch_name') if key in app]}))
    env = guard(app)
    if connect is None:
        import psycopg
        connect = psycopg.connect
    with tempfile.TemporaryDirectory() as directory:
        with connect(env['DATABASE_URL'], **database_options(env, directory)) as connection:
            name, user, readonly = connection.execute(
                "SELECT current_database(),current_user,current_setting('transaction_read_only')"
            ).fetchone()
            if (name, user, readonly) != (DATABASE, LOGIN, 'on'):
                raise SafetyError('wrong_actual_database_or_readonly_state')
            role = connection.execute(
                'SELECT rolsuper,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname=current_user'
            ).fetchone()
            if role != (False, False, False):
                raise SafetyError('privileged_staging_role')
            warehouses = connection.execute(
                'SELECT id,name FROM public.warehouses WHERE is_active<>0 ORDER BY id LIMIT 101'
            ).fetchall()
            if len(warehouses) > 100 or any(row[0] <= 0 for row in warehouses):
                raise SafetyError('unexpected_warehouse_directory')
            expiry = safe_expiry(connection.execute("SELECT value FROM public.settings WHERE key='orders.auto_cancel_hours'").fetchone())
            control = None
            exists = connection.execute("SELECT to_regclass('public.order_sale_dispatch_control') IS NOT NULL").fetchone()[0]
            if exists:
                mode, enabled, site_id = connection.execute(
                    'SELECT execution_mode,enabled,site_warehouse_id FROM public.order_sale_dispatch_control WHERE singleton'
                ).fetchone()
                if mode not in ('disabled', 'mock', 'live'):
                    raise SafetyError('unexpected_control_mode')
                control = {'executionMode': mode, 'enabled': bool(enabled), 'siteWarehouseId': site_id}
    return {'appId': 280757, 'application': 'ficusin-staging', 'branch': branch_name(app) or '<unreported>',
            'commit': app.get('commit_sha'), 'status': app.get('status'),
            'healthCheckEmpty': not app.get('health_check'),
            'databaseIsolated': True, 'databaseReadOnly': True, 'rolePrivileged': False,
            'flags': safe_flags(env),
            'activeWarehouses': [{'id': row[0], 'name': row[1]} for row in warehouses],
            'dispatchControl': control, **expiry}


def main():
    try:
        print(json.dumps(inspect(), ensure_ascii=False))
    except SafetyError as error:
        print(json.dumps({'error': str(error)}))
        raise SystemExit(1) from None
    except Exception as error:
        # Driver and HTTP errors can contain credentials/connection details.
        print(json.dumps({'error': type(error).__name__}))
        raise SystemExit(1) from None


if __name__ == '__main__':
    main()
