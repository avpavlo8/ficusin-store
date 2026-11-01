"""Restore the existing production owner's rights on its OWN objects only.
No catalog/customer/order data, ownership, or other roles are changed.
"""
import json,os,tempfile,urllib.request,urllib.parse
import psycopg
from psycopg import sql
assert os.environ['STAGING_OPERATION'] == 'repair-production-access'
request=urllib.request.Request('https://api.timeweb.cloud/api/v1/apps/229223',headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN']})
with urllib.request.urlopen(request,timeout=30) as response: app=json.load(response)['app']
assert app['id']==229223 and app['name']=='ficusin-store'
env=app['envs']; parsed=urllib.parse.urlsplit(env['DATABASE_URL'])
assert parsed.path=='/default_db' and parsed.username=='gen_user'
# Keep the provider's declarative grants aligned too. Creating the scoped
# preview role reconciled a stale CONNECT-only grant for the existing owner.
def api(method,path,data=None):
    assert method=='GET' or (method=='PATCH' and path=='/api/v1/databases/4192781/admins/429675')
    request=urllib.request.Request('https://api.timeweb.cloud'+path,method=method,
        headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN'],'Content-Type':'application/json'},
        data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(request,timeout=60) as response:return json.load(response)
stage=api('GET','/api/v1/apps/280757')['app']
assert stage['name']=='ficusin-staging' and stage['status']=='paused'
admin=next(row for row in api('GET','/api/v1/databases/4192781/admins')['admins'] if row['login']=='gen_user')
assert admin['id']==429675 and not admin['for_all']
available=api('GET','/api/v1/databases/4192781/privileges')['privileges']
grants=[row['privilege'] for row in available if row['group']!='GLOBAL']
api('PATCH','/api/v1/databases/4192781/admins/429675',{'instance_id':505949,'for_all':False,'privileges':grants})
print('Provider grant restored for existing gen_user on default_db only; no password or global privileges changed',flush=True)
with tempfile.TemporaryDirectory() as directory:
    options={'connect_timeout':20,'options':'-c statement_timeout=30000'}
    ca=env.get('DATABASE_SSL_CA','').strip().strip('\"\'').replace('\\n','\n')
    if ca:
        path=directory+'/root.crt'
        with open(path,'w') as handle:handle.write(ca)
        options.update(sslrootcert=path,sslmode='require' if env.get('DATABASE_SSL_VERIFY','true').lower() in ('false','0') else 'verify-full')
    with psycopg.connect(env['DATABASE_URL'],**options) as connection:
        assert connection.execute('SELECT current_database(),current_user').fetchone()==('default_db','gen_user')
        rows=connection.execute("SELECT n.nspname,c.relname,c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user) AND c.relkind IN ('r','p','v','m','f','S')").fetchall()
        assert rows and any(name=='categories' for _,name,_ in rows)
        print('Restoring owner privileges on existing owned objects:',len(rows),flush=True)
        for schema,name,kind in rows:
            object_type=sql.SQL('SEQUENCE' if kind=='S' else 'TABLE')
            connection.execute(sql.SQL('GRANT ALL PRIVILEGES ON {} {} TO CURRENT_USER').format(object_type,sql.Identifier(schema,name)))
        assert connection.execute("SELECT has_table_privilege(current_user,'public.categories','SELECT')").fetchone()[0]
        count=connection.execute('SELECT count(*) FROM categories').fetchone()[0]
        print('Owner access restored; categories readable:',count,flush=True)
print('Owner ACL recovery committed; no business data changed',flush=True)
