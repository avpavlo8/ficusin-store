#!/usr/bin/env python3
"""Create one identified EMPTY ordinary-realization test draft. No lines/post/payment."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import urllib.error
import urllib.request

spec=importlib.util.spec_from_file_location('preflight',Path(__file__).with_name('production-release-preflight.py'))
guards=importlib.util.module_from_spec(spec);spec.loader.exec_module(guards)
DOC_UUID='1eacfa69-a5be-4790-8b9a-a198b3493220'
NUMBER='FCT-20261113-1'
TITLE='ТЕСТ Фикусин API — Реализация 13.11.2026'
SERVICE='https://online.sbis.ru/service/?srv=1'

def request(url,body,token=''):
    allowed={'sabyWarehouse.List','СБИС.ПрочитатьДокумент','СБИС.ЗаписатьДокумент','ДокОтгрИсх.СписокХраним','ДокОтгрИсх.Прочитать'}
    if url==SERVICE and body.get('method') not in allowed:raise guards.PreflightError('method_forbidden')
    if url not in (SERVICE,'https://online.sbis.ru/oauth/service/'):raise guards.PreflightError('url_forbidden')
    headers={'Content-Type':'application/json;charset=utf-8','Accept':'application/json'}
    if token:headers['X-SBISAccessToken']=token
    req=urllib.request.Request(url,method='POST',data=json.dumps(body).encode(),headers=headers)
    try:response=urllib.request.build_opener(guards.NoRedirect()).open(req,timeout=45)
    except urllib.error.HTTPError as e:response=e
    with response:
        raw=response.read((8<<20)+1)
        if len(raw)>8<<20:raise guards.PreflightError('response_too_large')
        try:text=raw.decode(response.headers.get_content_charset() or 'utf-8')
        except UnicodeDecodeError:text=raw.decode('cp1251')
        return json.loads(text)

def safe_error(error,env):
    text=' '.join(str(error.get(k,'')) for k in ('message','details'))
    for key,value in env.items():
        if isinstance(value,str) and value and any(x in key for x in ('SECRET','TOKEN','KEY','PASSWORD','DATABASE_URL')):text=text.replace(value,'[redacted]')
    text=re.sub(r'https?://\S+','[URL]',text)
    text=re.sub(r'(?<![0-9a-zA-Z])[0-9]{10,12}(?![0-9a-zA-Z])','[identifier]',text)
    text=re.sub(r'\b[^\s@]+@[^\s@]+\.[^\s@]+','[email]',text)
    return {'code':error.get('code'),'class':(error.get('data') or {}).get('classid'),'errorCode':(error.get('data') or {}).get('error_code'),'message':text[:1200]}

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--expected-commit',required=True);args=parser.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise guards.PreflightError('first_github_attempt_only')
        app=guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=guards.production_settings(app,args.expected_commit)
        auth=request('https://online.sbis.ru/oauth/service/',{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]})
        token=auth.get('token')
        if not token:raise guards.PreflightError('auth_failed')
        print('::add-mask::'+token)
        def rpc(method,params,protocol=3):return request(SERVICE,{'jsonrpc':'2.0','protocol':protocol,'method':method,'params':params,'id':1},token)
        known=rpc('ДокОтгрИсх.Прочитать',{'ИдО':42737,'ИмяМетода':'ДокОтгрИсх.Список'},6)
        raw_known=known.get('result') or {}
        known_fields={c.get('n'):raw_known['d'][i] for i,c in enumerate(raw_known.get('s',[])) if i<len(raw_known.get('d',[]))}
        if known_fields.get('@Документ')!=42737 or known_fields.get('ИдентификаторДокумента')!='01a1cb92-38be-7fbd-865d-d89aa0b571eb' or known_fields.get('СкладскойДокумент.Склад')!=283:raise guards.PreflightError('known_tenant_document_not_verified')
        print(json.dumps({'tenantAnchorVerified':True,'headerWarehouseID':283,'movementWarehouseID':known_fields.get('Склад.Склад'),'ownOrganizationID':known_fields.get('ДокументНашаОрганизация'),'warehouseUUIDStable':False},ensure_ascii=False))
        warehouses=rpc('sabyWarehouse.List',{'filter':{'limit':100,'page':0}},6)
        found=[w for w in (warehouses.get('result') or {}).get('warehouses',[]) if w.get('warehouseId')==283]
        if len(found)!=1:
            print(json.dumps({'warehousePreflight':{'rootKeys':list(warehouses),'error':safe_error(warehouses['error'],env) if warehouses.get('error') else None,'resultType':type(warehouses.get('result')).__name__,'identifiers':[{k:w.get(k) for k in ('id','warehouseId')} for w in (warehouses.get('result') or {}).get('warehouses',[])]}},ensure_ascii=False))
            raise guards.PreflightError('known_warehouse_not_verified')
        org=found[0].get('organization') or {};inn=org.get('inn','')
        if not re.fullmatch(r'\d{12}',inn):raise guards.PreflightError('own_organization_not_verified')
        existing=rpc('СБИС.ПрочитатьДокумент',{'Документ':{'Идентификатор':DOC_UUID}})
        if not existing.get('error'):
            document=existing.get('result') or {}
            if document.get('Идентификатор')!=DOC_UUID or document.get('Номер')!=NUMBER:raise guards.PreflightError('existing_test_identity_mismatch')
            print(json.dumps({'alreadyExists':True,'documentUUID':DOC_UUID,'regulation':document.get('Регламент'),'noWrite':True},ensure_ascii=False));return 0
        error=safe_error(existing['error'],env)
        if not re.search('не найден|не существует|not found',error['message'],re.I):
            print(json.dumps({'preflightReadError':error,'noWrite':True},ensure_ascii=False));return 1
        # Fixed, reviewable test payload: no customer, XML, goods, posting,
        # payment, fiscalization or EDI sending action.
        document={'Идентификатор':DOC_UUID,'Тип':'ДокОтгрИсх','Регламент':{'Название':'Реализация'},'Дата':'13.11.2026','Номер':NUMBER,'Название':TITLE,'Примечание':'FICUSIN-REALIZATION-API-TEST-20261113. Пустой тестовый документ, без оплаты и фискализации.','НашаОрганизация':{'СвФЛ':{'ИНН':inn}}}
        print(json.dumps({'createAttemptRecorded':True,'runId':os.environ.get('GITHUB_RUN_ID'),'documentUUID':DOC_UUID,'type':'ДокОтгрИсх','regulationName':'Реализация','lineCount':0,'postingRequested':False,'paymentRequested':False},ensure_ascii=False),flush=True)
        result=rpc('СБИС.ЗаписатьДокумент',{'Документ':document})
        if result.get('error'):
            print(json.dumps({'createError':safe_error(result['error'],env),'documentUUID':DOC_UUID,'retryPolicy':'read-only'},ensure_ascii=False));return 1
        written=result.get('result') or {}
        summary={k:written.get(k) for k in ('Идентификатор','Тип','Подтип','Регламент','Номер')}
        print(json.dumps({'createdDraftResponse':summary,'stockChangedByThisProbe':False},ensure_ascii=False))
        if written.get('Идентификатор')!=DOC_UUID:raise guards.PreflightError('returned_uuid_mismatch')
        Path('saby-realization-test-receipt.json').write_text(json.dumps({'documentUUID':DOC_UUID,'number':NUMBER,'runId':os.environ.get('GITHUB_RUN_ID'),'result':summary},ensure_ascii=False))
        verify=rpc('СБИС.ПрочитатьДокумент',{'Документ':{'Идентификатор':DOC_UUID}})
        if verify.get('error'):raise guards.PreflightError('created_draft_readback_failed')
        saved=verify.get('result') or {}
        print(json.dumps({'verifiedEmptyTestDraft':saved.get('Идентификатор')==DOC_UUID,'regulation':saved.get('Регламент'),'documentUUID':DOC_UUID,'postingRequested':False,'noLinesAdded':True},ensure_ascii=False))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':'single_empty_realization_test','documentUUID':DOC_UUID,'retryPolicy':'read-only'})))
        return 1

if __name__=='__main__':raise SystemExit(main())
