"""Operate only the separately named staging app. Never emit credentials."""
import json, os, re, urllib.request, urllib.error, urllib.parse

def api(method, path, data=None):
    if method != 'GET':
        assert method == 'PATCH' and path in (f'/api/v1/apps/{app["id"]}',f'/api/v1/apps/{app["id"]}/action/stop')
        assert app['id'] != 229223 and app['name'] == 'ficusin-staging'
        assert urllib.parse.urlsplit(app['envs']['DATABASE_URL']).path == '/ficusin_staging_20261031'
    request = urllib.request.Request('https://api.timeweb.cloud'+path, method=method,
        headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN'],'Content-Type':'application/json'},
        data=None if data is None else json.dumps(data).encode())
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            body = response.read()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as error:
        details = json.loads(error.read())
        message = str(details.get('message',''))
        for value in globals().get('app',{}).get('envs',{}).values():
            if value and len(value)>8: message=message.replace(value,'[redacted]')
        raise RuntimeError(f'{method} {path}: HTTP {error.code}: {details.get("error_code")} {message[:1000]}') from None

app = next(item for item in api('GET','/api/v1/apps')['apps'] if item['name'] == 'ficusin-staging')
app = api('GET',f'/api/v1/apps/{app["id"]}')['app']
assert app['id'] != 229223 and app['name'] == 'ficusin-staging'
operation = os.environ['STAGING_OPERATION']
if operation == 'pause':
    assert os.environ['APPROVED_STAGING_MONTHLY_RUB'] == '510'
    api('PATCH',f'/api/v1/apps/{app["id"]}/action/stop')
    app = api('GET',f'/api/v1/apps/{app["id"]}')['app']
if operation in ('activate','configure','repair-health'):
    assert os.environ['APPROVED_STAGING_MONTHLY_RUB'] == '510'
    domain = next(row['fqdn'] for row in app['domains'] if row.get('is_technical'))
    assert domain.endswith('.twc1.net') and '/' not in domain
    envs = app['envs']
    assert envs['APP_ENV'] == 'staging'
    envs['SITE_URL'] = 'https://'+domain
    sha = os.environ['STAGING_CANDIDATE_SHA']
    assert re.fullmatch('[a-f0-9]{40}',sha)
    payload = {'envs':envs}
    if operation == 'repair-health': payload['health_check'] = ''
    if operation == 'activate':
        payload.update(branch_name='staging',commit_sha=sha,is_auto_deploy=True)
    app = api('PATCH',f'/api/v1/apps/{app["id"]}',payload)['app']
safe = ('id','name','status','branch_name','commit_sha','is_auto_deploy','domains','preset_id','ip','branch','health_check')
print('Staging app:',json.dumps({key:app.get(key) for key in safe},ensure_ascii=False),flush=True)
deploys = api('GET',f'/api/v1/apps/{app["id"]}/deploys')['deploys']
endpoints = ['deploys','logs']
if deploys: endpoints.append(f'deploy/{deploys[0]["id"]}/logs')
for endpoint in endpoints:
    result = api('GET',f'/api/v1/apps/{app["id"]}/{endpoint}')
    serialized = json.dumps(result,ensure_ascii=False)
    for key,value in app.get('envs',{}).items():
        if any(part in key for part in ('TOKEN','KEY','SECRET','PASSWORD','DATABASE_URL','DATABASE_SSL_CA')) and value:
            serialized = serialized.replace(value,'[redacted]')
    serialized = re.sub(r'postgres(?:ql)?://[^\s\"}]+','[redacted database URL]',serialized)
    print(endpoint,serialized[-14000:],flush=True)
