"""Copy only the operator's minimal owner profile. No provider role changes."""
import json,os,tempfile,urllib.request,urllib.parse
import psycopg
assert os.environ['APPROVED_STAGING_MONTHLY_RUB']=='510'
def get(app_id):
    request=urllib.request.Request(f'https://api.timeweb.cloud/api/v1/apps/{app_id}',headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN']})
    with urllib.request.urlopen(request,timeout=30) as response:return json.load(response)['app']
production=get(229223);stage=get(280757)
assert production['name']=='ficusin-store' and stage['name']=='ficusin-staging'
assert stage['envs']['APP_ENV']=='staging'
assert urllib.parse.urlsplit(stage['envs']['DATABASE_URL']).path=='/ficusin_staging_20261031'
temporary=tempfile.TemporaryDirectory()
def connect(env,readonly=False):
    kwargs={'connect_timeout':20,'options':'-c statement_timeout=30000'}
    ca=env.get('DATABASE_SSL_CA','').strip().strip('\"\'').replace('\\n','\n')
    if ca:
        path=temporary.name+'/root.crt'
        with open(path,'w') as file:file.write(ca)
        kwargs.update(sslrootcert=path,sslmode='require' if env.get('DATABASE_SSL_VERIFY','true').lower() in ('false','0') else 'verify-full')
    connection=psycopg.connect(env['DATABASE_URL'],**kwargs)
    connection.read_only=readonly
    return connection
with connect(production['envs'],True) as source:
    assert source.execute('SELECT current_database()').fetchone()[0]=='default_db'
    assert source.execute('SHOW transaction_read_only').fetchone()[0]=='on'
    owners=source.execute("SELECT c.phone,c.full_name,c.email FROM customers c JOIN admin_users a ON a.customer_id=c.id WHERE a.role='owner' AND c.phone IS NOT NULL").fetchall()
    selected=next((row for row in owners if ''.join(char for char in row[0] if char.isdigit())=='79156151100'),None)
    if selected is None and len(owners)==1:selected=owners[0]
    assert selected is not None,'Cannot identify one authorized owner profile'
    phone,name,email=selected
with connect(stage['envs']) as destination:
    assert destination.execute('SELECT current_database(),current_user').fetchone()==('ficusin_staging_20261031','ficusin_stage_20261031')
    row=destination.execute('SELECT id FROM customers WHERE phone=%s',(phone,)).fetchone()
    if row is None:
        row=destination.execute("INSERT INTO customers(phone,full_name,email,password_hash,consent_at) VALUES(%s,%s,%s,'',now()) RETURNING id",(phone,name,email or '')).fetchone()
    else:destination.execute('UPDATE customers SET full_name=%s,email=%s WHERE id=%s',(name,email or '',row[0]))
    destination.execute("INSERT INTO admin_users(customer_id,role) VALUES(%s,'owner') ON CONFLICT(customer_id) WHERE customer_id IS NOT NULL DO UPDATE SET role='owner'",(row[0],))
    assert destination.execute('SELECT count(*) FROM orders').fetchone()[0]==0
print('Operator owner profile prepared on staging; phone suffix:',phone[-4:])
print('No production writes, passwords, orders or customer list copied')
