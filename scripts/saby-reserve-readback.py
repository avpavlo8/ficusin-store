#!/usr/bin/env python3
"""Read-only inspection of one owner-controlled synthetic reserve invoice."""
import importlib.util
import json
import os
import urllib.request
import urllib.parse
import time
from pathlib import Path

spec = importlib.util.spec_from_file_location('reads', Path(__file__).with_name('saby-realization-readback.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)
UUID = '01a1ddfd-3d8e-7cc1-a961-d87af3fbf7c2'
DOC = 42864
ROW = 175782

def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise r.guards.PreflightError('github_actions_only')
    app = r.guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN', '')).get('/apps/229223')['app']
    env = r.guards.production_settings(app, os.environ['EXPECTED_COMMIT'])
    auth = r.request(r.AUTH, {k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]})
    token = auth.get('token')
    if not token:
        raise r.guards.PreflightError('auth_failed')
    print('::add-mask::'+token)
    r.READS.update({'Документ.ПрочитатьДляУчастника','СчетСкИсх.DocNomList','Reserve.ListByDn'})
    reserve=r.rpc('Reserve.ListByDn',{'DocNomIds':[ROW]},token,7)
    print(json.dumps({'readOnly':True,'ownedRow':ROW,'reserve':reserve},ensure_ascii=False))
    answer = r.request(r.SERVICE, {'jsonrpc':'2.0','protocol':7,'method':'Документ.ПрочитатьДляУчастника','params':{'ИдО':DOC,'ИмяМетода':'Документ.СписокХраним','ДопПоля':{'ИмяОбъекта':'СчетСкИсх'}},'id':1},token)
    if answer.get('error'):
        error=answer['error']
        print(json.dumps({'readOnly':True,'documentUUID':UUID,'apiError':{k:error.get(k) for k in ['code','message','details']}},ensure_ascii=False))
        return 1
    raw=answer.get('result') or {}
    doc=r.fields(raw)
    print(json.dumps({'schemaNames':list(doc),'identity':{k:doc.get(k) for k in ['@Документ','ИдентификаторДокумента']},'nestedShape':r.shape(doc.get('РП.Документ'))},ensure_ascii=False))
    if doc.get('@Документ') not in (DOC,[DOC,'СчетСкИсх']) or doc.get('ИдентификаторДокумента') != UUID:
        raise r.guards.PreflightError('fixed_owned_invoice_identity_mismatch')
    rp=r.fields(doc.get('РП.Документ'))
    regulation=r.fields(rp.get('Регламент'))
    visual=regulation.get('ВизуальноеПредставление') or {}
    if isinstance(visual,dict) and 's' in visual: visual=r.fields(visual)
    print(json.dumps({'regulationKeys':list(regulation),'visualKeys':list(visual),'visual':{k:visual.get(k) for k in ['source','readMetaData','createMetaData','updateMetaData']} if isinstance(visual,dict) else type(visual).__name__},ensure_ascii=False))
    names=['@Документ','ИдентификаторДокумента','ДокументРасширение.ИдентификаторВИ','ТипДокумента','ТипДокумента.ТипДокумента','СкладскойДокумент.Склад','ДокументНашаОрганизация','Регламент','ReserveWhId','ReserveUnit','ReserveNumber','Лицо1','Дата','ДокументРасширение.Состояние']
    print(json.dumps({'readOnly':True,'documentUUID':UUID,'header':{k:doc.get(k) for k in names},'schema':[c for c in raw.get('s',[]) if c.get('n') in names]},ensure_ascii=False))
    lines=r.rpc('СчетСкИсх.DocNomList',{'Фильтр':r.record([('СкладскойДокумент','Число целое',DOC),('ЭтоРасходы','Логическое',False),('Раздел','Число целое',None)]),'Сортировка':None,'Навигация':r.record([('ЕстьЕще','Логическое',True),('Страница','Число целое',0),('РазмерСтраницы','Число целое',20)]),'ДопПоля':[]},token,6)
    rows=r.rows(lines)
    if len(rows)!=1 or rows[0].get('@НоменклатураДокумента')!=ROW or rows[0].get('Номенклатура')!=3899:
        raise r.guards.PreflightError('fixed_owned_invoice_line_mismatch')
    names=['@НоменклатураДокумента','Номенклатура','СкладскойДокумент','Склад','Тип','Количество','Цена','СуммаЦен','Закрыто','ЕстьРезерв','ДанныеРезерва','ДанныеРезерваПоСкладам']
    print(json.dumps({'lines':[{k:line.get(k) for k in names} for line in rows],'lineSchema':[c for c in lines.get('s',[]) if c.get('n') in names]},ensure_ascii=False))
    reserve=r.rpc('Reserve.ListByDn',{'DocNomIds':[ROW]},token,6)
    print(json.dumps({'reserve':reserve},ensure_ascii=False))
    if os.environ.get('NATIVE_RESERVE_ACTION')=='cycle':
        if os.environ.get('GITHUB_RUN_ATTEMPT')!='1' or r.rows(reserve):
            raise r.guards.PreflightError('owned_cycle_requires_first_attempt_empty_reserve')
        line=rows[0]
        if doc.get('ТипДокумента')!=231 or doc.get('ДокументНашаОрганизация')!=259 or doc.get('Лицо1')!=18655 or any(line.get(k)!=v for k,v in {'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':1,'Цена':0,'Закрыто':False}.items()):
            raise r.guards.PreflightError('owned_cycle_identity_or_line_changed')
        receipt={'runId':os.environ.get('GITHUB_RUN_ID'),'documentId':DOC,'documentUUID':UUID,'rowId':ROW,'nomenclatureId':3899,'paymentRequested':False,'fiscalRequested':False,'applyAttempted':False,'removeAttempted':False}
        path=Path('saby-owned-reserve-cycle.json')
        def save():path.write_text(json.dumps(receipt,ensure_ascii=False))
        def balance():
            query=urllib.parse.urlencode({'pointId':278,'priceListId':19,'withBalance':'true','noStopList':'true','pageSize':100,'page':0})
            request=urllib.request.Request('https://api.sbis.ru/retail/v2/nomenclature/list?'+query,headers={'X-SBISAccessToken':token,'Accept':'application/json'})
            with urllib.request.build_opener(r.guards.NoRedirect()).open(request,timeout=40) as response:
                data=json.loads(response.read(2<<20))
            matches=[item for item in data.get('nomenclatures',[]) if str(item.get('id'))=='3899']
            if len(matches)!=1:raise r.guards.PreflightError('dedicated_test_catalog_not_unique')
            return {'id':3899,'balance':matches[0].get('balance')}
        receipt['beforeBalance']=balance()
        receipt['applyParams']={'DocId':DOC,'DocNomId':ROW,'Count':1,'PlannedDate':None}
        receipt['applyAttempted']=True;save()
        r.READS.update({'Reserve.Apply','Reserve.Remove'})
        try:
            reply=r.request(r.SERVICE,{'jsonrpc':'2.0','protocol':7,'method':'Reserve.Apply','params':receipt['applyParams'],'id':1},token)
            receipt['applyReply']=reply;save()
            for _ in range(4):
                after=r.rpc('Reserve.ListByDn',{'DocNomIds':[ROW]},token,7)
                receipt['reservedReadback']=after;save()
                if r.rows(after):break
                time.sleep(2)
            receipt['duringBalance']=balance();save()
        finally:
            receipt['removeAttempted']=True;save()
            reply=r.request(r.SERVICE,{'jsonrpc':'2.0','protocol':7,'method':'Reserve.Remove','params':{'DocId':DOC,'DocNomId':ROW},'id':1},token)
            receipt['removeReply']=reply;save()
            for _ in range(4):
                after=r.rpc('Reserve.ListByDn',{'DocNomIds':[ROW]},token,7)
                receipt['removedReadback']=after;save()
                if after is not None and not r.rows(after):break
                time.sleep(2)
            receipt['afterBalance']=balance();save()
        print(json.dumps({'ownedCycle':receipt},ensure_ascii=False))
        if not r.rows(receipt.get('reservedReadback')) or receipt.get('removedReadback') is None or r.rows(receipt['removedReadback']):
            raise r.guards.PreflightError('native_reserve_cycle_not_verified')
    return 0

if __name__=='__main__':
    try: raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc, {'phase':'owned_reserve_readback','documentUUID':UUID})))
        raise SystemExit(1)
