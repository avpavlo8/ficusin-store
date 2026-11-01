"""One-time catalog-only copy. Production connection is transaction read-only."""
import base64, hashlib, hmac, json, os, tempfile, time, urllib.request, urllib.parse
import psycopg
from psycopg import sql

DATABASE = 'ficusin_staging_20261031'
LOGIN = 'ficusin_stage_20261031'
assert os.environ['APPROVED_STAGING_MONTHLY_RUB'] == '510'

def get(path):
    request = urllib.request.Request('https://api.timeweb.cloud'+path,
        headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN']})
    with urllib.request.urlopen(request,timeout=60) as response: return json.load(response)

source_env = get('/api/v1/apps/229223')['app']['envs']
app = get('/api/v1/apps/280757')['app']
assert app['id'] == 280757 and app['name'] == 'ficusin-staging'
env = app['envs']
assert env['APP_ENV'] == 'staging'
assert urllib.parse.urlsplit(env['DATABASE_URL']).path == '/'+DATABASE
assert urllib.parse.urlsplit(env['DATABASE_URL']).username == LOGIN
assert urllib.parse.urlsplit(source_env['DATABASE_URL']).path == '/default_db'
domain = next(row['fqdn'] for row in app['domains'] if row.get('is_technical'))
origin = 'https://'+domain
assert domain.endswith('.twc1.net')

for attempt in range(60):
    try:
        with urllib.request.urlopen(origin+'/api/v1/health/ready',timeout=15) as response:
            if response.status == 200 and response.headers.get('X-Ficusin-Environment') == 'staging': break
    except Exception: pass
    if attempt % 6 == 0: print('Waiting for isolated app migrations/readiness',flush=True)
    time.sleep(10)
else: raise RuntimeError('Staging not ready; no catalog data was copied')

temporary = tempfile.TemporaryDirectory()
def connect(settings, url=None, readonly=False):
    options = {'connect_timeout':20,'options':'-c statement_timeout=120000'}
    ca = settings.get('DATABASE_SSL_CA','').strip().strip('\"\'').replace('\\n','\n')
    verify = settings.get('DATABASE_SSL_VERIFY','true').lower() not in ('false','0')
    if ca:
        cert = temporary.name+'/root.crt'
        with open(cert,'w') as handle: handle.write(ca)
        options.update(sslrootcert=cert,sslmode='verify-full' if verify else 'require')
    connection = psycopg.connect(url or settings['DATABASE_URL'],**options)
    connection.read_only = readonly
    if readonly: connection.isolation_level = psycopg.IsolationLevel.REPEATABLE_READ
    return connection

with connect(env) as destination:
    assert destination.execute('SELECT current_database(),current_user').fetchone() == (DATABASE,LOGIN)
    role = destination.execute('SELECT rolsuper,rolcreatedb,rolcreaterole FROM pg_roles WHERE rolname=current_user').fetchone()
    assert role == (False,False,False), 'Staging role must have no cluster administration privileges'
    # Read-only privilege inspection, never a production INSERT/UPDATE attempt.
    parsed = urllib.parse.urlsplit(env['DATABASE_URL'])
    prod_as_stage = urllib.parse.urlunsplit(parsed._replace(path='/default_db'))
    try:
        with connect(env,prod_as_stage,readonly=True) as restricted:
            count = restricted.execute("SELECT count(*) FROM pg_tables WHERE schemaname='public' AND (has_table_privilege(current_user,schemaname||'.'||tablename,'SELECT') OR has_table_privilege(current_user,schemaname||'.'||tablename,'INSERT,UPDATE,DELETE,TRUNCATE'))").fetchone()[0]
            assert count == 0, 'Staging user can access production tables'
    except psycopg.OperationalError as error:
        if error.sqlstate not in ('42501','28000'): raise
    print('Verified: staging role cannot read or change production tables',flush=True)
    destination.execute('CREATE TABLE IF NOT EXISTS staging_catalog_seed (id INTEGER PRIMARY KEY CHECK(id=1),seeded_at TIMESTAMPTZ NOT NULL DEFAULT now())')
    already_seeded = destination.execute('SELECT count(*) FROM staging_catalog_seed').fetchone()[0] > 0
    tables = [
        'categories','attribute_definitions','attribute_options','category_attributes',
        'saby_nomenclature','products','product_variants','product_attribute_values',
        'variant_attribute_values','warehouses','inventory','product_media','media_mirror',
        'catalog_supplementary_media','collections','collection_products','product_url_aliases',
        'catalog_filters','product_external_ids']
    if not already_seeded:
        assert destination.execute('SELECT count(*) FROM customers').fetchone()[0] == 0
        assert destination.execute('SELECT count(*) FROM orders').fetchone()[0] == 0
        with connect(source_env,readonly=True) as source:
            assert source.execute('SHOW transaction_read_only').fetchone()[0] == 'on'
            assert source.execute('SELECT current_database()').fetchone()[0] == 'default_db'
            existing = {row[0] for row in source.execute("SELECT tablename FROM pg_tables WHERE schemaname='public'")}
            tables = [name for name in tables if name in existing]
            # This is the new, isolated DB only. No personal/financial table is copied.
            destination.execute(sql.SQL('TRUNCATE {} RESTART IDENTITY CASCADE').format(sql.SQL(',').join(map(sql.Identifier,tables))))
            for name in tables:
                columns = [row[0] for row in source.execute("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=%s AND is_generated='NEVER' ORDER BY ordinal_position",(name,))]
                identifiers = sql.SQL(',').join(map(sql.Identifier,columns))
                with source.cursor().copy(sql.SQL('COPY (SELECT {} FROM {}) TO STDOUT (FORMAT BINARY)').format(identifiers,sql.Identifier(name))) as output:
                    with destination.cursor().copy(sql.SQL('COPY {} ({}) FROM STDIN (FORMAT BINARY)').format(sql.Identifier(name),identifiers)) as target:
                        for block in output: target.write(block)
                count = destination.execute(sql.SQL('SELECT count(*) FROM {}').format(sql.Identifier(name))).fetchone()[0]
                print('Catalog table copied:',name,count,flush=True)
                if 'id' in columns:
                    sequence = destination.execute('SELECT pg_get_serial_sequence(%s,\'id\')',(name,)).fetchone()[0]
                    if sequence:
                        destination.execute(sql.SQL('SELECT setval(%s,GREATEST(COALESCE(max(id),0),1),COALESCE(max(id),0)>0) FROM {}').format(sql.Identifier(name)),(sequence,))
            destination.execute("UPDATE inventory SET reserved_qty=0")
            destination.execute("SELECT setval('ficusin_sku_seq',GREATEST(COALESCE(max(substring(sku FROM 5)::bigint),0),1),true) FROM product_variants WHERE sku ~ '^FIC-[0-9]{6}$'")
        destination.execute('INSERT INTO staging_catalog_seed(id) VALUES(1)')
    else: print('Catalog already initialized; preserving subsequent staging edits',flush=True)
    destination.execute("INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES ('staging-owner@example.invalid','+70000000901','','Владелец проверки',now()) ON CONFLICT(email) DO NOTHING")
    destination.execute("INSERT INTO admin_users(customer_id,role) SELECT id,'owner' FROM customers WHERE email='staging-owner@example.invalid' ON CONFLICT(customer_id) WHERE customer_id IS NOT NULL DO UPDATE SET role='owner'")
    access = os.environ['STAGING_ACCESS_TOKEN']
    session = hmac.new(access.encode(),b'ficusin-staging-owner-session',hashlib.sha256).hexdigest()
    token_hash = hashlib.sha256(session.encode()).hexdigest()
    destination.execute("INSERT INTO auth_sessions(token_hash,customer_id,expires_at) SELECT %s,id,now()+interval '4 hours' FROM customers WHERE email='staging-owner@example.invalid' ON CONFLICT(token_hash) DO UPDATE SET expires_at=excluded.expires_at",(token_hash,))
    assert destination.execute('SELECT count(*) FROM orders').fetchone()[0] == 0
    print('Synthetic owner prepared; production customer/order data not copied',flush=True)
print('Staging catalog setup committed',flush=True)
