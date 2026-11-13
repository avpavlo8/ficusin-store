#!/usr/bin/env python3
"""Bounded outgoing-document/schema reads. No document or inventory mutations."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('guards', Path(__file__).with_name('production-release-preflight.py'))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)
SERVICE = 'https://online.sbis.ru/service/?srv=1'
AUTH = 'https://online.sbis.ru/oauth/service/'
READS = {'ДокОтгрИсх.СписокХраним', 'ДокОтгрИсх.Прочитать', 'sabyWarehouse.List','РеалИсх.DocNomList','СБИС.ПрочитатьДокумент'}

def request(url, body, token=''):
    if url not in (SERVICE, AUTH) or (url == SERVICE and body.get('method') not in READS):
        raise guards.PreflightError('rpc_not_read_only')
    headers = {'Content-Type': 'application/json;charset=utf-8', 'Accept': 'application/json'}
    if token: headers['X-SBISAccessToken'] = token
    req = urllib.request.Request(url, method='POST', headers=headers, data=json.dumps(body).encode())
    opener = urllib.request.build_opener(guards.NoRedirect())
    try:
        response = opener.open(req, timeout=40)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read((8 << 20) + 1)
        if len(raw) > 8 << 20: raise guards.PreflightError('saby_read_too_large')
        charset = response.headers.get_content_charset() or 'utf-8'
        try: text = raw.decode(charset)
        except UnicodeDecodeError: text = raw.decode('cp1251')
        return json.loads(text)

def record(values):
    return {'_type':'record', 's':[{'n':name,'t':kind} for name,kind,value in values], 'd':[value for name,kind,value in values]}

def rows(value):
    if not isinstance(value, dict): return []
    schema = value.get('s', [])
    data = value.get('d', [])
    names = [column.get('n') for column in schema]
    return [{name: row[i] for i,name in enumerate(names) if i < len(row)} for row in data if isinstance(row,list)]

def fields(value):
    if not isinstance(value,dict): return {}
    return {c.get('n'):value['d'][i] for i,c in enumerate(value.get('s',[])) if i < len(value.get('d',[]))}

def shape(value, depth=0):
    if depth > 3: return type(value).__name__
    if isinstance(value,dict):
        if 's' in value and 'd' in value:
            return {'schema':value.get('s'), 'dataShape':shape(value.get('d'), depth+1)}
        return {k:shape(v,depth+1) for k,v in list(value.items())[:80]}
    if isinstance(value,list): return {'length':len(value),'first':shape(value[0],depth+1) if value else None}
    return type(value).__name__

def rpc(method, params, token, protocol=4):
    result = request(SERVICE, {'jsonrpc':'2.0','protocol':protocol,'method':method,'params':params,'id':1}, token)
    error = result.get('error')
    if error:
        print(json.dumps({'method':method,'errorCode':error.get('code'),'errorShape':shape(error)},ensure_ascii=False))
        return None
    return result.get('result')

def summary(entity):
    # Only native identifiers and accounting-state flags, never names, sums,
    # counterparties, contacts, notes, attachments or arbitrary scalar values.
    names = {'@Документ','ИдентификаторДокумента','ДокументРасширение.ИдентификаторВИ',
             'ТипДокумента','НазваниеТипаДокумента','Тип','Проведен','Проведён','Проведение',
             'Состояние','Статус','Склад','СкладОтправитель','СкладПолучатель','Регламент','ИдРегламента','ТипДокумента.ПодТипДокумента','ТипДокумента.ТипДокумента','СкладскойДокумент.Склад','СкладскойДокумент.Флаги','ДокументРасширение.Состояние','Документ_','СкладскойДокумент.СкладскойДокумент_','ТипДокумента.ИмяДиалога','ТипДокумента.ИмяБраузера','ТипДокумента.GUID','ТипДокумента.ТочкиПроведения'}
    return {k:v for k,v in entity.items() if k in names and (isinstance(v,(str,int,float,bool,type(None))) or k=='@Документ' and isinstance(v,list) and len(v)==2 and isinstance(v[0],int) and isinstance(v[1],str) or k in ('ТипДокумента.ТочкиПроведения','ДокументРасширение.Состояние') and isinstance(v,list) and all(x is None or isinstance(x,bool) for x in v))}

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--expected-commit',required=True)
    args=parser.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true': raise guards.PreflightError('github_actions_only')
        app=guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=guards.production_settings(app,args.expected_commit)
        auth=request(AUTH,{key:env.get(name,'') for key,name in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]})
        token=auth.get('token')
        if not token: raise guards.PreflightError('saby_auth_failed')
        print('::add-mask::'+token)
        filter_record=record([('rp_doc','Логическое',True),('usePages','Строка','full'),('НазваниеТипаДокумента','Строка','ДокОтгрИсх'),('ФильтрПоЛицуИзСписка','Строка','Нет'),('ФильтрПоМаске','Строка','FCT-20261113-1')])
        navigation=record([('ЕстьЕще','Логическое',True),('РазмерСтраницы','Число целое',5),('Страница','Число целое',0)])
        listing=rpc('ДокОтгрИсх.СписокХраним',{'Фильтр':filter_record,'Сортировка':None,'Навигация':navigation,'ДопПоля':[]},token)
        print(json.dumps({'readOnly':True,'providerWrites':False,'testDraftCandidates':[summary(row) for row in rows(listing)]},ensure_ascii=False))
        for row in rows(listing)[:3]:
            identity=row.get('@Документ')
            if isinstance(identity,list) and len(identity)==2 and isinstance(identity[0],int) and identity[0]>0: identity=identity[0]
            if isinstance(identity,bool) or not isinstance(identity,int) or identity<=0: continue
            doc=rpc('ДокОтгрИсх.Прочитать',{'ИдО':identity,'ИмяМетода':'ДокОтгрИсх.Список'},token)
            print(json.dumps({'identifiers':summary(fields(doc)),'stockConfig':{k:fields(doc).get(k) for k in ('Склад.@Лицо','Склад.Склад','Склад.НашаОрганизация','СкладскойДокумент.Склад.Склад','ДокументНашаОрганизация','Регламент.Название','Регламент.Идентификатор','Лицо1','Лицо3','Дата')}},ensure_ascii=False))
            if row == rows(listing)[0]:
                detail=rpc('РеалИсх.DocNomList',{'Фильтр':record([('СкладскойДокумент','Число целое',identity),('Тип','Число целое',1),('ЭтоРасходы','Логическое',False),('Раздел','Число целое',None)]),'Сортировка':None,'Навигация':record([('ЕстьЕще','Логическое',True),('Страница','Число целое',0),('РазмерСтраницы','Число целое',100)]),'ДопПоля':[]},token,6)
                print(json.dumps({'lineSchemaNames':[x['n'] for x in (detail or {}).get('s',[])],'lineProof':{'rowCount':len(rows(detail)),'testLines':[{k:r.get(k) for k in ('@НоменклатураДокумента','Номенклатура','СкладскойДокумент','Склад','Тип','Количество','Цена','СуммаЦен','Закрыто')} for r in rows(detail)],'documentIDs':list({r.get('СкладскойДокумент') for r in rows(detail)}),'warehouses':list({r.get('Склад') for r in rows(detail)}),'directions':list({r.get('Тип') for r in rows(detail)}),'closedAll':all(r.get('Закрыто') is True for r in rows(detail)) if rows(detail) else False}},ensure_ascii=False))
                public=rpc('СБИС.ПрочитатьДокумент',{'Документ':{'Идентификатор':fields(doc).get('ДокументРасширение.ИдентификаторВИ')}},token,3)
                print(json.dumps({'publicReadShape':shape(public),'regulation':(public or {}).get('Регламент')},ensure_ascii=False))
        public_test=rpc('СБИС.ПрочитатьДокумент',{'Документ':{'Идентификатор':'1eacfa69-a5be-4790-8b9a-a198b3493220','ДопПоля':'ЭтапВернутьВсеСертификаты'}},token,3)
        if public_test:
            def action_summary(v):
                if isinstance(v,list):return [action_summary(i) for i in v]
                if isinstance(v,dict):return {k:action_summary(val) for k,val in v.items() if k in ('Идентификатор','Название','Действие','ТребуетПодписания','ТребуетКомментария','ТребуемоеДействие','Состояние')}
                return v
            print(json.dumps({'testPublic':{'id':public_test.get('Идентификатор'),'regulation':public_test.get('Регламент'),'revision':action_summary(public_test.get('Редакция')),'stages':action_summary(public_test.get('Этап')),'extension':public_test.get('Расширение'),'keys':list(public_test)}},ensure_ascii=False))
        missing=request(SERVICE,{'jsonrpc':'2.0','protocol':3,'method':'СБИС.ПрочитатьДокумент','params':{'Документ':{'Идентификатор':'66fb5550-828a-4f6b-bf77-51f8e79ef776'}},'id':1},token)
        if missing.get('error'):
            import re
            err=missing['error'];msg=' '.join(str(err.get(k,'')) for k in ('message','details'))
            msg=re.sub(r'[0-9a-fA-F]{8}-[0-9a-fA-F-]{27,}', '[UUID]',msg)
            msg=re.sub(r'(?<![0-9a-zA-Z])[0-9]{10,12}(?![0-9a-zA-Z])','[ID]',msg)
            print(json.dumps({'missingDocumentError':{'code':err.get('code'),'data':{k:(err.get('data')or{}).get(k)for k in ('classid','error_code')},'message':msg[:1000]}},ensure_ascii=False))
        warehouses=rpc('sabyWarehouse.List',{'filter':{'limit':100,'page':0}},token,6)
        print(json.dumps({'warehouseShape':shape(warehouses),'warehouseIDs':[{k:w.get(k) for k in ('id','warehouseId')} for w in (warehouses or {}).get('warehouses',[])]},ensure_ascii=False))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':'saby_realization_readback'})))
        return 1

if __name__=='__main__': raise SystemExit(main())
