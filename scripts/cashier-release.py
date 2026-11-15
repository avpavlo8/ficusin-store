"""Owner-approved fixed cashier release; secrets and raw provider replies never printed."""
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import time
import urllib.parse
import urllib.request

spec = importlib.util.spec_from_file_location('cashier_release_guards', Path(__file__).with_name('production-release-preflight.py'))
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)
SOURCE = 'c661449e2b6c300cda6766828154fc7c27d92cea'
OLD = '443e6f80e2ea90ce1987add53bab62dcf5ae18fc'
STAGE_OLD = os.environ.get('EXPECTED_STAGING_COMMIT','44bfa6c5807fe1411857416c2e07755ab55b332d')
TAG = 'ficusin-cashier-release-20261115-c661449'
MIGRATION = '20261115_cashier_stock_reservations.sql'
FLAGS = {'SABY_CASHIER_RESERVE_MODE':'live','SABY_CASHIER_RESERVE_LIVE_ENABLED':'true',
 'SABY_CASHIER_RESERVE_WAREHOUSE_ID':'283','SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID':'284',
 'SABY_CASHIER_RESERVE_ORGANIZATION_ID':'259','SABY_CASHIER_RESERVE_REGULATION_ID':'121326'}

class Provider:
    def __init__(self, token):
        if not token: raise g.PreflightError('timeweb_token_missing')
        self.token = token
        self.opener = urllib.request.build_opener(g.NoRedirect())
        self.writes = set()
    def request(self, method, path, body=None):
        allowed = method == 'GET' and (path in ('/apps/229223','/apps/280757','/dbs/4192781/backups') or bool(re.fullmatch(r'/dbs/4192781/backups/[0-9]+', path)))
        allowed |= method == 'POST' and path in ('/apps/229223/deploy','/apps/280757/deploy','/dbs/4192781/backups')
        allowed |= method == 'PATCH' and path == '/apps/229223' and set(body or {}) == {'envs'}
        if not allowed: raise g.PreflightError('release_path_not_allowed')
        if method != 'GET':
            if path in self.writes: raise g.PreflightError('write_already_attempted')
            self.writes.add(path)  # Persist the attempt boundary before HTTP; never retry.
        req = urllib.request.Request(g.API+path,method=method,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+self.token,'Content-Type':'application/json'})
        with self.opener.open(req, timeout=30) as resp:
            raw=resp.read(g.MAX_RESPONSE+1)
        if len(raw)>g.MAX_RESPONSE: raise g.PreflightError('oversized_response')
        return json.loads(raw) if raw else {}

def production(app, sha):
    env = g.production_settings(app, sha)
    if app.get('status') != 'active': raise g.PreflightError('production_not_active')
    if not all(env.get(k,'').strip() for k in ('SABY_APP_CLIENT_ID','SABY_APP_SECRET','SABY_SECRET_KEY')):
        raise g.PreflightError('saby_credentials_missing')
    return dict(env)

def stage(app):
    env=app.get('envs',{})
    branch=app.get('branch_name') or app.get('branch')
    if isinstance(branch,dict): branch=branch.get('name')
    db=urllib.parse.urlsplit(env.get('DATABASE_URL',''))
    if (app.get('id')!=280757 or app.get('status')!='active' or branch!='staging' or env.get('APP_ENV')!='staging'
        or db.path!='/ficusin_staging_20261031' or db.username!='ficusin_stage_20261031'
        or app.get('health_check') not in (None,'') or not env.get('STAGING_ACCESS_TOKEN')):
        raise g.PreflightError('staging_isolation_not_verified')
    if app.get('commit_sha') not in (STAGE_OLD,SOURCE):
        raise g.PreflightError('staging_changed_concurrently')
    return dict(env)

def database(env, migrated):
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            if conn.execute("SELECT current_database(),current_setting('transaction_read_only')").fetchone() != ('default_db','on'):
                raise g.PreflightError('wrong_readonly_database')
            risky=conn.execute("""SELECT count(*) FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id JOIN orders o ON o.id=i.order_id
              WHERE o.payment_method='on_delivery' AND p.execution_mode='live'
              AND (p.create_attempted OR p.post_attempted OR p.document_id<>'' OR p.document_uuid<>'' OR p.stock_verified)""").fetchone()[0]
            if risky: raise g.PreflightError('cashier_realizations_need_reconciliation')
            local=conn.execute("SELECT id FROM warehouses WHERE is_active<>0 AND saby_id='saby-ryazan-main'").fetchall()
            if len(local)!=1: raise g.PreflightError('site_warehouse_ambiguous')
            result={'riskyCashRealizations':risky,'siteWarehouseId':local[0][0]}
            result['activeCashOrders']=conn.execute("SELECT count(*) FROM orders WHERE payment_method='on_delivery' AND status NOT IN ('completed','cancelled')").fetchone()[0]
            applied=conn.execute('SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=%s)',(MIGRATION,)).fetchone()[0]
            result['migrationApplied']=applied
            if migrated and not applied: raise g.PreflightError('cashier_migration_missing')
            if migrated:
                rows=conn.execute("""SELECT o.order_number,n.status,n.provider_state#>>'{identity,id}',n.provider_state#>>'{identity,uuid}',
                  n.command_attempted_at IS NOT NULL,n.last_error,
                  (SELECT count(*) FROM cashier_native_attempts a WHERE a.reservation_id=r.id)
                  FROM cashier_stock_reservations r JOIN orders o ON o.id=r.order_id
                  LEFT JOIN cashier_native_reservations n ON n.reservation_id=r.id ORDER BY r.id""").fetchall()
                result['reservations']=[dict(zip(['orderNumber','status','documentId','documentUUID','attemptPending','message','attemptCount'],r)) for r in rows]
            return result

def backup(p):
    app=p.request('GET','/apps/229223')['app'];env=production(app,OLD)
    report=database(env,False)
    backups=p.request('GET','/dbs/4192781/backups')['backups']
    found=[x for x in backups if x.get('comment')==TAG]
    if len(found)>1: raise g.PreflightError('duplicate_backup_tag')
    if found: item=found[0]
    else:
        try: item=p.request('POST','/dbs/4192781/backups',{'comment':TAG})['backup']
        except Exception:
            found=[x for x in p.request('GET','/dbs/4192781/backups')['backups'] if x.get('comment')==TAG]
            if len(found)!=1: raise g.PreflightError('backup_result_unknown') from None
            item=found[0]
    if not isinstance(item.get('id'),int): raise g.PreflightError('backup_id_missing')
    deadline=time.monotonic()+900
    while item.get('status')!='done':
        if time.monotonic()>deadline or item.get('status') in ('failed','error'): raise g.PreflightError('backup_not_ready')
        time.sleep(10)
        item=p.request('GET',f'/dbs/4192781/backups/{item["id"]}')['backup']
    return {'backup':{k:item.get(k) for k in ('id','status','created_at')},'database':report}

def deploy_stage(p,restore=False):
    app=p.request('GET','/apps/280757')['app'];env=stage(app)
    target=STAGE_OLD if restore else SOURCE
    if app.get('commit_sha')==target and app.get('status')=='active': return {'alreadyActive':True,'commit':target}
    fresh=p.request('GET','/apps/280757')['app']
    if stage(fresh)!=env or fresh.get('commit_sha')!=app.get('commit_sha'): raise g.PreflightError('staging_changed_concurrently')
    p.request('POST','/apps/280757/deploy',{'commit_sha':target})
    return {'deployRequested':True,'appId':280757,'commit':target,'branchUntouched':True,'envUntouched':True}

def activate(p,sha):
    env=production(p.request('GET','/apps/229223')['app'],sha)
    report=database(env,True)
    fresh=production(p.request('GET','/apps/229223')['app'],sha)
    if fresh!=env: raise g.PreflightError('production_environment_changed')
    if any(env.get(k)!=v for k,v in FLAGS.items()):
        p.request('PATCH','/apps/229223',{'envs':{**env,**FLAGS}})
        readback=p.request('GET','/apps/229223')['app']['envs']
        if readback!={**env,**FLAGS}: raise g.PreflightError('environment_readback_differs')
        changed=True
    else: changed=False
    return {'patched':changed,'runtimeVerificationRequired':changed,'flags':FLAGS,'database':report}

def unposted(raw,doc):
    name='ДокументРасширение.Состояние';values=doc.get(name)
    schema=[s.get('t',{}) for s in raw.get('s',[]) if s.get('n')==name]
    if len(schema)!=1 or not isinstance(schema[0],dict) or schema[0].get('n')!='Флаги' or not isinstance(values,list):return False
    labels=schema[0].get('s',{})
    if not isinstance(labels,dict) or len(labels)!=len(values) or any(not k.isdigit() for k in labels):return False
    flags={labels[k]:v for k,v in zip(sorted(labels,key=int),values)}
    required=('Проведен','ЕстьПроводки','ОшибкиПроведения','НеактуальныеПроводки','ВОчередиНаПроведение','ВОчередиНаРасчетПроводок')
    return all(k in flags and (flags[k] is None or flags[k] is False) for k in required)

def native_readback(env):
    """Independent read only; never calls Execute, fiscal API, posting or Reserve.Apply."""
    spec=importlib.util.spec_from_file_location('cashier_native_reads',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            saved=conn.execute("""SELECT r.id,o.order_number,n.status,n.provider_state,n.request FROM cashier_stock_reservations r
              JOIN orders o ON o.id=r.order_id JOIN cashier_native_reservations n ON n.reservation_id=r.id
              WHERE r.completed_at IS NULL AND r.cancelled_at IS NULL ORDER BY r.id""").fetchall()
    if any(row[2]!='settled' for row in saved):return {'verified':False,'reason':'worker_not_settled_yet'}
    if not saved:return {'verified':True,'activeInvoices':[]}
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('native_read_auth_failed')
    print('::add-mask::'+token)
    r.READS={'СчетСкИсх.ПрочитатьДляУчастника','Reserve.ListByDn'}
    from decimal import Decimal
    result=[]
    for reservation,order,status,state,request in saved:
        identity=state.get('identity',{});doc_id=identity.get('id');uuid=identity.get('uuid');rows=state.get('rows',[])
        if not isinstance(doc_id,int) or doc_id<=0 or not rows:raise g.PreflightError('native_identity_not_saved')
        raw=r.rpc('СчетСкИсх.ПрочитатьДляУчастника',{'ИдО':doc_id,'ИмяМетода':'СчетСкИсх.ListLite','ДопПоля':{'ИмяОбъекта':'СчетСкИсх'}},token,7)
        doc=r.fields(raw)
        expected={'@Документ':doc_id,'ИдентификаторДокумента':uuid,'ТипДокумента':231,'ТипДокумента.ТипДокумента':'СчетИсх',
          'ДокументНашаОрганизация':259,'СкладскойДокумент.Склад':283,'Регламент':121326,'Удален':False,'Номер':f'FCR-{reservation}'}
        if any(doc.get(k)!=v for k,v in expected.items()):raise g.PreflightError('native_header_readback_mismatch')
        if not unposted(raw,doc):raise g.PreflightError('native_invoice_posted_or_unverified')
        for row in rows:
            raw=r.rpc('Reserve.ListByDn',{'DocNomIds':[row['id']]},token,7)
            reserves=r.rows(raw)
            if raw is None or len(reserves)!=1:raise g.PreflightError('native_reserve_missing_or_ambiguous')
            reserve=reserves[0]
            if (reserve.get('@НоменклатураДокумента')!=row['id'] or reserve.get('Склад')!=283
              or reserve.get('Активен') is not True or reserve.get('ByBaseWh') is not True
              or reserve.get('ЕстьСписаниеОтгрузкой') is not False
              or Decimal(str(reserve.get('Количество')))!=Decimal(str(row['line']['quantity']))
              or Decimal(str(reserve.get('КоличествоОтгружено')))!=0):raise g.PreflightError('native_reserve_quantity_mismatch')
        result.append({'orderNumber':order,'documentId':doc_id,'documentUUID':uuid,'verifiedRows':len(rows),'posted':False})
    return {'verified':True,'activeInvoices':result,'readOnly':True}

def main():
    if os.environ.get('GITHUB_ACTIONS')!='true': raise g.PreflightError('github_actions_only')
    p=Provider(os.environ.get('TIMEWEB_CLOUD_TOKEN',''))
    op=os.environ['CASHIER_RELEASE_OPERATION'].removeprefix('cashier-');sha=os.environ['EXPECTED_COMMIT']
    if not re.fullmatch('[0-9a-f]{40}',sha): raise g.PreflightError('invalid_sha')
    if op=='inspect':
        result={'apps':[]}
        for app_id in (229223,280757):
            app=p.request('GET',f'/apps/{app_id}')['app']
            result['apps'].append({k:app.get(k) for k in ('id','name','status','branch_name','commit_sha')})
        result['backups']=[{k:b.get(k) for k in ('id','status','created_at','comment')} for b in p.request('GET','/dbs/4192781/backups')['backups'] if b.get('comment')==TAG]
    elif op=='backup': result=backup(p)
    elif op in ('staging-deploy','staging-restore'): result=deploy_stage(p,op=='staging-restore')
    elif op=='production-activate': result=activate(p,sha)
    elif op=='production-restart':
        env=production(p.request('GET','/apps/229223')['app'],sha)
        if any(env.get(k)!=v for k,v in FLAGS.items()):raise g.PreflightError('native_flags_not_enabled')
        report=database(env,True)
        if production(p.request('GET','/apps/229223')['app'],sha)!=env:raise g.PreflightError('production_environment_changed')
        p.request('POST','/apps/229223/deploy',{'commit_sha':sha})
        result={'deployRequested':True,'commit':sha,'flags':FLAGS,'database':report}
    elif op=='production-verify':
        env=production(p.request('GET','/apps/229223')['app'],sha)
        result={'flagsEnabled':all(env.get(k)==v for k,v in FLAGS.items()),'database':database(env,True),'nativeReadback':native_readback(env)}
    else: raise g.PreflightError('invalid_release_operation')
    print(json.dumps(result,ensure_ascii=False),flush=True)

if __name__=='__main__':
    try: main()
    except Exception as exc:
        import traceback
        report=g.safe_failure(exc,{'phase':'cashier_release'})
        report['locations']=[{'file':Path(f.filename).name,'line':f.lineno} for f in traceback.extract_tb(exc.__traceback__)[-3:]]
        print(json.dumps(report))
        raise SystemExit(1)
