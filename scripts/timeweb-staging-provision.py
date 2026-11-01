"""Scoped, resumable staging provisioning. No production writes or secret output."""
import json
import os
import secrets
import hashlib
import hmac
import base64
import time
import re
import urllib.error
import urllib.parse
import urllib.request

NAME = 'ficusin-staging'
DATABASE = 'ficusin_staging_20261031'
LOGIN = 'ficusin_stage_20261031'
CLUSTER = 4192781
PRESET = 2731
APPLY = os.environ.get('STAGING_OPERATION') == 'provision'
APPROVED = os.environ.get('APPROVED_STAGING_MONTHLY_RUB') == '510'
token = os.environ['TIMEWEB_CLOUD_TOKEN']

def api(method, path, data=None):
    if method != 'GET' and not (APPLY and APPROVED):
        raise RuntimeError('Provisioning requires explicit approval of 510 RUB/month')
    # There is deliberately no endpoint for editing/deleting production.
    if method != 'GET' and not (path in ('/api/v2/apps',f'/api/v1/databases/{CLUSTER}/instances',f'/api/v1/databases/{CLUSTER}/admins') or (method == 'PATCH' and path == f'/api/v1/apps/{stage_id}' and stage_id != 229223)):
        raise RuntimeError('Mutation outside staging allowlist')
    request = urllib.request.Request('https://api.timeweb.cloud'+path,method=method,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'},data=None if data is None else json.dumps(data).encode())
    try:
        with urllib.request.urlopen(request,timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        # Provider errors can echo input credentials. Never print response bodies.
        message = error.read().decode('utf-8','replace')
        for secret in (token,os.environ.get('STAGING_ACCESS_TOKEN',''),globals().get('password','')):
            if secret: message = message.replace(secret,'[redacted]')
        message = re.sub(r'postgres(?:ql)?://[^\s\"}]+','[redacted database URL]',message)
        raise RuntimeError(f'Timeweb {method} {path}: HTTP {error.code}: {message[:1500]}') from None

source = api('GET','/api/v1/apps/229223')['app']
assert source['name'] == 'ficusin-store' and source['repository']['full_name'] == 'avpavlo8/ficusin-store'
presets = api('GET','/api/v1/presets/apps')['backend_presets']
preset = next(p for p in presets if p['id'] == PRESET)
assert preset['price'] <= 510 and preset['location'] == 'ru-3'
apps = api('GET','/api/v1/apps')['apps']
existing = [app for app in apps if app['name'] == NAME]
print(json.dumps({'operation':'provision' if APPLY else 'plan','name':NAME,'monthly_rub':preset['price'],'region':preset['location'],'cpu':preset['cpu'],'ram_mb':preset['ram'],'database':DATABASE,'existing_cluster':CLUSTER,'new_cluster':False,'existing_app_ids':[app['id'] for app in existing]},ensure_ascii=False),flush=True)
if not APPLY:
    raise SystemExit(0)
assert APPROVED
if existing:
    print('Staging application already exists; no duplicate creation:',existing[0]['id'])
    raise SystemExit(0)
access = os.environ.get('STAGING_ACCESS_TOKEN','')
assert len(access) >= 32, 'Separate staging access secret required before creation'
instances = api('GET',f'/api/v1/databases/{CLUSTER}/instances')['instances']
instance = next((item for item in instances if item['name']==DATABASE),None)
if instance is None:
    instance = api('POST',f'/api/v1/databases/{CLUSTER}/instances',{'name':DATABASE,'description':'Isolated Ficusin staging - no customer or order copy'})['instance']
print('Isolated database instance:',instance['id'],flush=True)
for attempt in range(20):
    instance = api('GET',f'/api/v1/databases/{CLUSTER}/instances/{instance["id"]}')['instance']
    if instance.get('status') == 'created':
        break
    time.sleep(3)
else:
    raise RuntimeError('Staging database creation has not completed')
admins_result = api('GET',f'/api/v1/databases/{CLUSTER}/admins')
admins = admins_result.get('admins',[])
admin = next((item for item in admins if item.get('login')==LOGIN),None)
password = hmac.new(access.encode(),b'ficusin-stage-db-20261031',hashlib.sha256).hexdigest()[:26]+'Aa9!'
if admin:
    assert admin.get('description') == 'Ficusin staging database only', 'Existing role is not the scoped staging role'
else:
    privileges = api('GET',f'/api/v1/databases/{CLUSTER}/privileges')['privileges']
    granted = [item['privilege'] for item in privileges if item['group']!='GLOBAL']
    admin = api('POST',f'/api/v1/databases/{CLUSTER}/admins',{'login':LOGIN,'password':password,'instance_id':instance['id'],'for_all':False,'privileges':granted,'description':'Ficusin staging database only'})['admin']
print('Isolated staging database user created/reused:',LOGIN,flush=True)
db = urllib.parse.urlsplit(source['envs']['DATABASE_URL'])
host = db.hostname
assert host
if ':' in host:
    host = '['+host+']'
port = ':'+str(db.port) if db.port else ''
database_url = urllib.parse.urlunsplit((db.scheme,LOGIN+':'+urllib.parse.quote(password,safe='')+'@'+host+port,'/'+DATABASE,db.query,''))
envs = {key:source['envs'][key] for key in ('DATABASE_SSL_CA','DATABASE_SSL_VERIFY') if key in source['envs']}
envs.update({'DATABASE_URL':database_url,'APP_ENV':'staging','SITE_URL':'https://staging.ficusin.ru','AUTH_COOKIE_SECURE':'true','STAGING_ACCESS_TOKEN':access})
sha = os.environ['STAGING_CANDIDATE_SHA']
assert len(sha)==40 and all(c in '0123456789abcdef' for c in sha)
payload = {'type':'backend','name':NAME,'comment':'Isolated staging before production releases','provider_id':source['provider']['id'],'repository_id':source['repository']['id'],'branch_name':'feat/site-saby-catalog','commit_sha':sha,'language':'docker','framework':'docker','preset_id':PRESET,'build_cmd':'','run_cmd':'','env_version':None,'is_auto_deploy':False,'project_id':source['project_id'],'envs':envs,'health_check':''}
app = api('POST','/api/v2/apps',payload)['app']
stage_id = app['id']
assert stage_id != 229223 and app['name']==NAME
print('Created staging app:',stage_id,flush=True)
technical = next((domain['fqdn'] for domain in app.get('domains',[]) if domain.get('is_technical')),None)
if technical:
    envs['SITE_URL'] = 'https://'+technical
    api('PATCH',f'/api/v1/apps/{stage_id}',{'envs':envs})
    print('Staging URL:',envs['SITE_URL'],flush=True)
else:
    print('Technical domain not yet returned; inspect the new staging app before enabling auto-deploy',flush=True)
print('Only the new app/database/user were written. No production settings changed.',flush=True)
