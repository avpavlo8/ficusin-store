"""Create one fresh, tagged recovery point before the approved production release."""
import argparse,json,os,time,urllib.request,urllib.error,urllib.parse
import importlib.util
from pathlib import Path
_spec=importlib.util.spec_from_file_location('backup_guards',Path(__file__).with_name('production-release-preflight.py'));guards=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(guards)
APP=229223
DB=4192781
SHA='4a3f4dd4799b881d2e367d93a47c13e7a3f77273'
TAG='ficusin-release-20261114-41bd7ec'
BASE='https://api.timeweb.cloud/api/v1'
def request(method,path,body=None):
    allowed=path in (f'/apps/{APP}',f'/dbs/{DB}/backups') or path.startswith(f'/dbs/{DB}/backups/') and path.rsplit('/',1)[-1].isdigit()
    assert allowed and (method=='GET' or method=='POST' and path==f'/dbs/{DB}/backups')
    req=urllib.request.Request(BASE+path,method=method,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+os.environ['TIMEWEB_CLOUD_TOKEN'],'Content-Type':'application/json'})
    with urllib.request.build_opener(guards.NoRedirect()).open(req,timeout=30) as response:
        raw=response.read((4<<20)+1)
        assert len(raw)<=4<<20
        return json.loads(raw)
def safe(backup):
    return {key:backup.get(key) for key in ('id','status','created_at')}
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--apply',action='store_true');args=parser.parse_args()
    assert os.environ.get('GITHUB_ACTIONS')=='true'
    app=request('GET',f'/apps/{APP}')['app'];env=app['envs']
    guards.production_settings(app,SHA)
    assert (env.get('APP_ENV','').strip() or 'production')=='production'
    assert urllib.parse.urlsplit(env['DATABASE_URL']).path=='/default_db'
    backups=request('GET',f'/dbs/{DB}/backups')['backups']
    existing=[b for b in backups if b.get('comment')==TAG]
    assert len(existing)<=1
    if not args.apply:
        print(json.dumps({'mode':'dry-run','existing':list(map(safe,existing)),'creates':not bool(existing)}));return
    if existing: backup=existing[0]
    else:
        try: backup=request('POST',f'/dbs/{DB}/backups',{'comment':TAG})['backup']
        except (TimeoutError,urllib.error.URLError):
            found=[b for b in request('GET',f'/dbs/{DB}/backups')['backups'] if b.get('comment')==TAG]
            if len(found)!=1: raise RuntimeError('backup_create_outcome_unknown') from None
            backup=found[0]
    assert isinstance(backup.get('id'),int) and backup['id']>0
    print(json.dumps({'mode':'apply','backup':safe(backup)}),flush=True)
    deadline=time.monotonic()+900
    while backup.get('status')!='done':
        if backup.get('status') in ('error','failed') or time.monotonic()>=deadline: raise RuntimeError('backup_not_ready')
        time.sleep(10)
        backup=request('GET',f'/dbs/{DB}/backups/{backup["id"]}')['backup']
    print(json.dumps({'complete':True,'backup':safe(backup)}),flush=True)
if __name__=='__main__':
    try: main()
    except urllib.error.HTTPError as e:
        print(json.dumps({'complete':False,'httpStatus':e.code}));raise SystemExit(2) from None
    except Exception as e:
        print(json.dumps({'complete':False,'exceptionClass':type(e).__name__}));raise SystemExit(2) from None
