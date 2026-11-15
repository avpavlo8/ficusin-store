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
SOURCE = '642f2f583ee87e01ccda43278bfa0b723d50e5a4'
OLD = '443e6f80e2ea90ce1987add53bab62dcf5ae18fc'
STAGE_OLD = os.environ.get('EXPECTED_STAGING_COMMIT','44bfa6c5807fe1411857416c2e07755ab55b332d')
TAG = 'ficusin-cashier-release-20261115-c661449'
MIGRATION = '20261115_cashier_stock_reservations.sql'
FLAGS = {'SABY_CASHIER_RESERVE_MODE':'live','SABY_CASHIER_RESERVE_LIVE_ENABLED':'true',
 'SABY_CASHIER_RESERVE_WAREHOUSE_ID':'283','SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID':'284',
 'SABY_CASHIER_RESERVE_ORGANIZATION_ID':'259','SABY_CASHIER_RESERVE_REGULATION_ID':'121326'}
ONLINE_FLAGS={'ORDER_REALIZATION_MODE':'live','ORDER_REALIZATION_LIVE_ENABLED':'true',
 'ORDER_REALIZATION_SITE_WAREHOUSE_ID':'1','ORDER_REALIZATION_NATIVE_WAREHOUSE_ID':'283',
 'ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID':'284','ORDER_REALIZATION_ORGANIZATION_ID':'259',
 'ORDER_REALIZATION_REGULATION_ID':'122650','ORDER_REALIZATION_BUYER_ID':'18749',
 'SABY_CASHIER_RESERVE_BUYER_ID':'18749'}

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
            active=conn.execute("SELECT count(*) FROM orders WHERE payment_method='on_delivery' AND status NOT IN ('completed','cancelled')").fetchone()[0]
            saved=conn.execute("""SELECT r.id,o.order_number,n.status,n.provider_state,n.request FROM cashier_stock_reservations r
              JOIN orders o ON o.id=r.order_id LEFT JOIN cashier_native_reservations n ON n.reservation_id=r.id
              WHERE r.completed_at IS NULL AND r.cancelled_at IS NULL ORDER BY r.id""").fetchall()
    if len(saved)!=active:return {'verified':False,'reason':'native_reservation_headers_missing'}
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
        expected_buyer=int(env.get('SABY_CASHIER_RESERVE_BUYER_ID','0'))
        buyer=doc.get('Лицо1') or 0
        comment=doc.get('ДокументРасширение.Название')
        presentation=buyer==expected_buyer and expected_buyer>0 and comment=='Резерв заказа '+order
        result.append({'orderNumber':order,'documentId':doc_id,'documentUUID':uuid,'verifiedRows':len(rows),'rowIDs':[x['id'] for x in rows],
          'buyerId':buyer,'buyerName':doc.get('Лицо1.Название'),'comment':comment if presentation else '[legacy presentation]',
          'presentationVerified':presentation,'posted':False})
    return {'verified':True,'activeInvoices':result,'readOnly':True}

def order_23_audit(env):
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            if conn.execute("SELECT current_database(),current_setting('transaction_read_only')").fetchone()!=('default_db','on'):
                raise g.PreflightError('wrong_readonly_database')
            order=conn.execute("SELECT id,order_number,status,payment_method,payment_status,delivery_method,created_at::text FROM orders WHERE id=74 AND order_number='0000-23'").fetchone()
            if not order:raise g.PreflightError('fixed_order_missing')
            control=conn.execute("SELECT execution_mode,enabled,site_warehouse_id,native_warehouse_id,movement_warehouse_id,organization_id,regulation_id,buyer_id FROM order_sale_dispatch_control WHERE singleton").fetchone()
            lines=conn.execute("""SELECT oi.variant_id,oi.quantity,oi.unit_price::text,v.saby_id,
              (SELECT jsonb_agg(jsonb_build_object('type',e.id_type,'id',e.external_id)) FROM product_external_ids e WHERE e.variant_id=oi.variant_id AND e.provider='saby' AND e.status='active'),
              (SELECT jsonb_agg(jsonb_build_object('warehouse',i.warehouse_id,'available',i.available_qty,'observedAt',i.stock_observed_at)) FROM inventory i WHERE i.variant_id=oi.variant_id)
              FROM order_items oi JOIN product_variants v ON v.id=oi.variant_id WHERE oi.order_id=74 ORDER BY oi.id""").fetchall()
            intents=conn.execute("""SELECT i.id,i.event_trigger,i.state,p.execution_mode,p.status,p.document_id,p.document_uuid,p.create_attempted,p.post_attempted,p.stock_verified,p.cancellation_verified
              FROM order_sale_intents i LEFT JOIN order_sale_publications p ON p.intent_id=i.id WHERE i.order_id=74 ORDER BY i.id""").fetchall()
            return {'readOnly':True,'order':dict(zip(['id','number','status','paymentMethod','paymentStatus','deliveryMethod','createdAt'],order)),
              'control':dict(zip(['mode','enabled','siteWarehouse','nativeWarehouse','movementWarehouse','organization','regulation','buyer'],control)),
              'lines':[dict(zip(['variant','quantity','price','sabyId','mappings','inventory'],x)) for x in lines],
              'intents':[dict(zip(['id','trigger','state','mode','status','documentId','documentUUID','createAttempted','postAttempted','stockVerified','cancellationVerified'],x)) for x in intents],
              'configuredOrderRealization':{k:env.get(k,'') for k in ['ORDER_REALIZATION_MODE','ORDER_REALIZATION_LIVE_ENABLED','ORDER_REALIZATION_BUYER_ID','ORDER_REALIZATION_SITE_WAREHOUSE_ID','ORDER_REALIZATION_NATIVE_WAREHOUSE_ID','ORDER_REALIZATION_MOVEMENT_WAREHOUSE_ID','ORDER_REALIZATION_ORGANIZATION_ID','ORDER_REALIZATION_REGULATION_ID']}}

def order_23_rpc(env):
    """Read only: frozen publication and its public absence preflight."""
    import urllib.error
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            row=conn.execute("""SELECT p.request,p.progress,p.create_attempted,p.post_attempted,p.document_id,p.document_uuid,p.status
              FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id JOIN orders o ON o.id=i.order_id
              WHERE p.id=1 AND i.order_id=74 AND o.order_number='0000-23'""").fetchone()
    if not row:raise g.PreflightError('fixed_publication_missing')
    request,progress,created,posted,docid,uuid,status=row
    key=request.get('operationKey','')
    if not re.fullmatch(r'[0-9a-f-]{36}',key):raise g.PreflightError('operation_key_invalid')
    spec=importlib.util.spec_from_file_location('rpc_diag',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('auth_failed')
    print('::add-mask::'+token)
    body={'jsonrpc':'2.0','protocol':3,'method':'СБИС.ПрочитатьДокумент','params':{'Документ':{'Идентификатор':key}},'id':1}
    req=urllib.request.Request(r.SERVICE,method='POST',data=json.dumps(body).encode(),headers={'Content-Type':'application/json-rpc;charset=utf-8','Accept':'application/json','X-SBISAccessToken':token})
    try:response=urllib.request.build_opener(g.NoRedirect()).open(req,timeout=40)
    except urllib.error.HTTPError as error:response=error
    with response:
        raw=response.read((1<<20)+1);httpstatus=response.status; charset=response.headers.get_content_charset() or 'utf-8'
    if len(raw)>1<<20:raise g.PreflightError('rpc_too_large')
    try:raw.decode('utf-8');utf8=True
    except UnicodeDecodeError:utf8=False
    reply=json.loads(raw.decode(charset));error=reply.get('error') or {};data=error.get('data') or {}
    return {'readOnly':True,'order':'0000-23','operationKey':key,'status':status,'progress':progress,
      'createAttempted':created,'postAttempted':posted,'documentId':docid,'documentUUID':uuid,
      'httpStatus':httpstatus,'charset':charset,'validUTF8':utf8,'rpcError':{k:error.get(k) for k in ('code','message','details')},
      'errorData':{k:data.get(k) for k in ('classid','error_code')},'resultPresent':'result' in reply}

def buyer_read(env):
    spec=importlib.util.spec_from_file_location('buyer_native_reads',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('native_auth_failed')
    print('::add-mask::'+token)
    r.READS={'СчетСкИсх.ПрочитатьДляУчастника','Reserve.ListByDn'}
    raw=r.rpc('СчетСкИсх.ПрочитатьДляУчастника',{'ИдО':42869,'ИмяМетода':'СчетСкИсх.ListLite','ДопПоля':{'ИмяОбъекта':'СчетСкИсх'}},token,7)
    doc=r.fields(raw)
    if doc.get('@Документ')!=42869 or doc.get('ИдентификаторДокумента')!='01a1de39-6356-700b-8077-56876977c9b5' or not unposted(raw,doc):
        raise g.PreflightError('owned_invoice_mismatch')
    reserve=r.rpc('Reserve.ListByDn',{'DocNomIds':[175806]},token,7)
    if reserve is None or r.rows(reserve):raise g.PreflightError('owned_invoice_has_reserve')
    return {'readOnly':True,'ownedInvoice':42869,'buyerId':doc.get('Лицо1'),'buyerName':doc.get('Лицо1.Название'),'comment':doc.get('ДокументРасширение.Название'),'reserveEmpty':True,'unposted':True}

def activate_online(p,sha):
    env=production(p.request('GET','/apps/229223')['app'],sha)
    report=order_23_audit(env)
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            counts=conn.execute('SELECT (SELECT count(*) FROM order_sale_publications),(SELECT count(*) FROM order_sale_intents)').fetchone()
            warehouse=conn.execute("SELECT id FROM warehouses WHERE saby_id='saby-ryazan-main' AND is_active<>0").fetchall()
    if counts!=(0,0):raise g.PreflightError('existing_realization_queue_needs_review_before_activation')
    if warehouse!=[(1,)]:raise g.PreflightError('verified_site_warehouse_changed')
    if report['order']['paymentMethod']!='online' or report['order']['paymentStatus']!='paid' or report['order']['status']!='assembling' or report['intents']:
        raise g.PreflightError('fixed_order_changed_before_activation')
    buyer=buyer_read(env)
    if buyer.get('buyerId')!=18749 or buyer.get('buyerName')!='Сайт Фикусин':raise g.PreflightError('website_buyer_not_confirmed')
    fresh=production(p.request('GET','/apps/229223')['app'],sha)
    if fresh!=env:raise g.PreflightError('environment_changed_before_activation')
    changed=any(env.get(k)!=v for k,v in ONLINE_FLAGS.items())
    if changed:
        p.request('PATCH','/apps/229223',{'envs':{**env,**ONLINE_FLAGS}})
        if p.request('GET','/apps/229223')['app']['envs']!={**env,**ONLINE_FLAGS}:raise g.PreflightError('activation_readback_differs')
    return {'patched':changed,'flags':ONLINE_FLAGS,'existingIntents':0,'existingPublications':0,
      'runtimeVerificationRequired':True,'historicalOrdersEnqueued':False,'targetedRecoveryOrder':'0000-23','buyerVerified':True}

def created_price_audit(env):
    """Bounded read of latest site Saby publications and their native price data."""
    import urllib.error
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            if conn.execute("SELECT current_database(),current_setting('transaction_read_only')").fetchone()!=('default_db','on'):
                raise g.PreflightError('wrong_readonly_database')
            publications=conn.execute("""SELECT v.id,v.product_id,p.name,v.created_at::text,v.base_price_minor,v.saby_id,
              c.status,c.message,c.execution_mode,c.attempts,c.updated_at::text,c.payload,c.progress,
              c.pending_payload IS NOT NULL
              FROM catalog_editor_publications c JOIN product_variants v ON v.id=c.variant_id JOIN products p ON p.id=v.product_id
              WHERE c.channel='saby' ORDER BY v.created_at DESC,v.id DESC LIMIT 3""").fetchall()
            recent=conn.execute("""SELECT v.id,v.product_id,p.name,v.created_at::text,v.base_price_minor,v.saby_id
              FROM product_variants v JOIN products p ON p.id=v.product_id ORDER BY v.created_at DESC,v.id DESC LIMIT 5""").fetchall()
            variants=[x[0] for x in publications]
            prices=conn.execute("""SELECT variant_id,target_id,expected_site_price_minor,price_minor,status,message,execution_mode,attempts,updated_at::text,progress->>'stage'
              FROM catalog_editor_price_publications WHERE channel='saby' AND variant_id=ANY(%s) ORDER BY id DESC LIMIT 8""",(variants,)).fetchall() if variants else []
    spec=importlib.util.spec_from_file_location('created_price_reads',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('native_auth_failed')
    print('::add-mask::'+token)
    listid=int(env.get('SABY_PRICE_LIST_ID') or '6')
    def price_shape(value):
        return {'schema':value.get('s',[]),'rows':[{k:x.get(k) for k in ('Id','PriceList','Price','CanEdit','MeasureUnit','IsUpdated')} for x in r.rows(value)]} if isinstance(value,dict) else {'type':type(value).__name__}
    result=[]
    for row in publications:
        variant,product,name,created,siteprice,sabyid,status,message,mode,attempts,updated,payload,progress,pending=row
        state=progress.get('native') or {};ids=progress.get('ids') or {}
        if isinstance(state,str):state=json.loads(state)
        prepared=state.get('preparedData') or {};prepared_fields=r.fields(prepared)
        item={'variantId':variant,'productId':product,'name':name,'createdAt':created,'sitePriceMinor':siteprice,'sabyId':sabyid,
          'status':status,'message':message,'mode':mode,'attempts':attempts,'updatedAt':updated,
          'action':payload.get('action'),'requestPriceMinor':payload.get('priceMinor'),'stage':progress.get('stage'),
          'nativeCreated':state.get('created'),'ids':{k:ids.get(k) for k in ('productId','offerId','uuid')},'pending':pending,
          'preparedPriceData':price_shape(prepared_fields.get('PriceData'))}
        nativeid=ids.get('productId') or sabyid or ''
        if not str(nativeid).isdigit():
            result.append(item);continue
        body={'jsonrpc':'2.0','protocol':6,'method':'Nomenclature.Read','params':{'Id':int(nativeid),
          'AdditionalFields':['PriceData','Number','Identifier','IsDraft'],
          'Option':{'_type':'record','s':[{'n':'PriceList','t':{'n':'Массив','t':'Число целое'}}],'d':[[listid]]}},'id':1}
        request=urllib.request.Request(r.SERVICE,method='POST',headers={'Content-Type':'application/json-rpc;charset=utf-8','Accept':'application/json','X-SBISAccessToken':token},data=json.dumps(body).encode())
        try:response=urllib.request.build_opener(g.NoRedirect()).open(request,timeout=35)
        except urllib.error.HTTPError as error:response=error
        with response:
            raw=response.read((1<<20)+1);httpstatus=response.status;charset=response.headers.get_content_charset() or 'utf-8'
        if len(raw)>1<<20:raise g.PreflightError('native_read_oversized')
        reply=json.loads(raw.decode(charset));error=reply.get('error') or {};card=r.fields(reply.get('result'))
        item['nativeRead']={'httpStatus':httpstatus,'rpcErrorCode':error.get('code'),'rpcErrorMessage':error.get('message'),
          'id':card.get('Id'),'number':card.get('Number'),'uuid':card.get('Identifier'),'isDraft':card.get('IsDraft'),'priceData':price_shape(card.get('PriceData'))}
        result.append(item)
    return {'readOnly':True,'configuredPriceList':listid,'publications':result,
      'recentVariants':[dict(zip(['variantId','productId','name','createdAt','sitePriceMinor','sabyId'],x)) for x in recent],
      'pricePublications':[dict(zip(['variantId','targetId','sitePriceMinor','priceMinor','status','message','mode','attempts','updatedAt','stage'],x)) for x in prices]}


def created_price_default_audit(env):
    """Exactly one known site-created card, two read projections, no writes."""
    spec=importlib.util.spec_from_file_location('created_price_default_reads',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('native_auth_failed')
    print('::add-mask::'+token)
    listid=int(env.get('SABY_PRICE_LIST_ID') or '6');r.READS={'Nomenclature.Read'}
    result=[]
    for scope in ('default','configured'):
        option={'_type':'record','s':[],'d':[]} if scope=='default' else {'_type':'record','s':[{'n':'PriceList','t':{'n':'Массив','t':'Число целое'}}],'d':[[listid]]}
        raw=r.rpc('Nomenclature.Read',{'Id':3984,'AdditionalFields':['PriceData','Number','Identifier','IsDraft'],'Option':option},token,6)
        card=r.fields(raw)
        if card.get('Id')!=3984 or card.get('Number')!='X946056863' or card.get('Identifier')!='681a4876-f3a8-4dc3-a35d-2590b89ebbf5':raise g.PreflightError('known_created_card_changed')
        price=card.get('PriceData') or {}
        result.append({'scope':scope,'id':3984,'number':card.get('Number'),'isDraft':card.get('IsDraft'),
          'priceDataSchema':price.get('s'),
          'prices':[{k:p.get(k) for k in ('Id','PriceList','PriceListName','Price','CanEdit','MeasureUnit','IsUpdated','RestrictionData','IsAutoPrice')} for p in r.rows(price)]})
    return {'readOnly':True,'configuredPriceList':listid,'cardProjections':result}


def order_23_discovery(env):
    """Read-only exact native discovery; no raw headers or create payloads."""
    spec=importlib.util.spec_from_file_location('order23_native_reads',Path(__file__).with_name('saby-realization-readback.py'))
    r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
    with tempfile.TemporaryDirectory() as directory:
        with g.connect_production(env,directory) as conn:
            row=conn.execute("""SELECT p.request->>'operationKey',p.progress->>'stage',p.progress->'native',p.status,p.create_attempted,p.post_attempted
              FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id WHERE p.id=1 AND i.order_id=74""").fetchone()
    if not row or row[0]!='f6d636c3-d0e3-4851-95ec-90a08ee35bc4':raise g.PreflightError('fixed_publication_changed')
    key,stage,native,status,created,posted=row
    if isinstance(native,str):native=json.loads(native)
    native=native or {}
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise g.PreflightError('native_auth_failed')
    print('::add-mask::'+token)
    r.READS={'ДокОтгрИсх.СписокХраним','ДокОтгрИсх.Прочитать'}
    errors=[]
    def read(method,params):
        reply=r.request(r.SERVICE,{'jsonrpc':'2.0','protocol':6,'method':method,'params':params,'id':1},token)
        if reply.get('error'):
            error=reply['error'];errors.append({'method':method,'code':error.get('code'),'message':error.get('message')});return None
        return reply.get('result')
    listing=read('ДокОтгрИсх.СписокХраним',{'Фильтр':r.record([('rp_doc','Логическое',True),('usePages','Строка','full'),('НазваниеТипаДокумента','Строка','ДокОтгрИсх'),('ФильтрПоЛицуИзСписка','Строка','Нет'),('ФильтрПоМаске','Строка','FCS-1')]),'Сортировка':None,'Навигация':r.record([('ЕстьЕще','Логическое',True),('РазмерСтраницы','Число целое',20),('Страница','Число целое',0)]),'ДопПоля':[]})
    rows=r.rows(listing);headers=[]
    if len(rows)>20:raise g.PreflightError('too_many_candidates')
    for candidate in rows:
        docid=candidate.get('@Документ');docid=docid[0] if isinstance(docid,list) and len(docid)==2 else docid
        if not isinstance(docid,int) or isinstance(docid,bool) or docid<=0:raise g.PreflightError('invalid_native_id')
        raw=read('ДокОтгрИсх.Прочитать',{'ИдО':docid,'ИмяМетода':'ДокОтгрИсх.Список'});doc=r.fields(raw)
        names=('@Документ','ИдентификаторДокумента','ДокументРасширение.ИдентификаторВИ','Удален','ТипДокумента','ТипДокумента.ТипДокумента','Документ_','СкладскойДокумент.Склад','ДокументНашаОрганизация','Лицо1','Регламент','Дата','Номер')
        headers.append({'header':{k:doc.get(k) for k in names},'postingFlagSchema':[x for x in (raw or {}).get('s',[]) if x.get('n')=='ДокументРасширение.Состояние'],'postingFlags':doc.get('ДокументРасширение.Состояние')})
    return {'readOnly':True,'order':'0000-23','publicationId':1,'operationKey':key,'stage':stage,'phase':native.get('phase'),'polls':native.get('polls'),'status':status,'createAttempted':created,'postAttempted':posted,'nativeListType':type(listing).__name__,'nativeListSchema':(listing or {}).get('s'),'nativeCandidates':headers,'errors':errors}


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
    elif op in ('order-23-audit','buyer-read','order-23-rpc','created-price-audit','created-price-default','order-23-discovery'):
        env=production(p.request('GET','/apps/229223')['app'],sha)
        result=order_23_audit(env) if op=='order-23-audit' else order_23_rpc(env) if op=='order-23-rpc' else created_price_audit(env) if op=='created-price-audit' else created_price_default_audit(env) if op=='created-price-default' else order_23_discovery(env) if op=='order-23-discovery' else buyer_read(env)
    elif op=='online-activate':result=activate_online(p,sha)
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
