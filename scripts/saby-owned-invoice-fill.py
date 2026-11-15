#!/usr/bin/env python3
"""Fill the fixed synthetic invoice; no posting, payment or fiscal API exists here."""
import importlib.util
import json
import hashlib
import os
from pathlib import Path

spec=importlib.util.spec_from_file_location('reads',Path(__file__).with_name('saby-realization-readback.py'))
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
DOC=42866
UUID='01a1de0d-4726-7e92-98d8-5478ead64c28'
MARKER='FICUSIN-RESERVE-API-TEST-20261115-2. Только X380416519. Без оплаты и фискализации.'
METHODS={'СчетСкИсх.ПрочитатьДляУчастника','СчетСкИсх.DocNomList','СчетСкИсх.WriteForParticipant','СчетСкИсх.DocNomCreate','СчетСкИсх.CustomDocNomUpdate'}

def main():
    if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise r.guards.PreflightError('first_attempt_only')
    env=r.guards.production_settings(r.guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223')['app'],os.environ['EXPECTED_COMMIT'])
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise r.guards.PreflightError('auth_failed')
    print('::add-mask::'+token);r.READS.update(METHODS)
    proof={'documentID':DOC,'documentUUID':UUID,'paymentRequested':False,'fiscalRequested':False,'postingRequested':False,'attempts':[]}
    path=Path('saby-owned-invoice-fill.json')
    def save():path.write_text(json.dumps(proof,ensure_ascii=False))
    def rpc(method,params,write=False):
        if write:proof['attempts'].append({'method':method,'params':params,'attempted':True});save()
        answer=r.request(r.SERVICE,{'jsonrpc':'2.0','protocol':7,'method':method,'params':params,'id':1},token)
        if write:proof['attempts'][-1]['answer']=answer;save()
        if answer.get('error'):
            print(json.dumps({'apiError':answer['error'],'method':method},ensure_ascii=False));raise r.guards.PreflightError('owned_fill_api_error_no_retry')
        return answer.get('result')
    def read():
        raw=rpc('СчетСкИсх.ПрочитатьДляУчастника',{'ИдО':DOC,'ИмяМетода':'СчетСкИсх.ListLite','ДопПоля':{'ИмяОбъекта':'СчетСкИсх'}})
        doc=r.fields(raw)
        if doc.get('@Документ') not in (DOC,[DOC,'СчетСкИсх']) or any(doc.get(k)!=v for k,v in {'ИдентификаторДокумента':UUID,'ТипДокумента':231,'ТипДокумента.ТипДокумента':'СчетИсх','ДокументНашаОрганизация':259,'СкладскойДокумент.Склад':283,'Регламент':121326,'Удален':False}.items()):raise r.guards.PreflightError('fixed_owned_invoice_mismatch')
        state=next(c for c in raw['s'] if c['n']=='ДокументРасширение.Состояние')
        flags={name:doc['ДокументРасширение.Состояние'][int(i)] for i,name in state['t']['s'].items()}
        if any(flags.get(k) not in (None,False) for k in ['Проведен','ЕстьПроводки','ОшибкиПроведения','ВОчередиНаПроведение','ВОчередиНаРасчетПроводок']):raise r.guards.PreflightError('owned_invoice_has_accounting_activity')
        return doc
    def lines():
        return rpc('СчетСкИсх.DocNomList',{'Фильтр':r.record([('СкладскойДокумент','Число целое',DOC),('ЭтоРасходы','Логическое',False),('Раздел','Число целое',None)]),'Сортировка':None,'Навигация':r.record([('ЕстьЕще','Логическое',True),('Страница','Число целое',0),('РазмерСтраницы','Число целое',20)]),'ДопПоля':[]})
    read()
    if r.rows(lines()):raise r.guards.PreflightError('owned_invoice_not_empty_no_duplicate_fill')
    patch=r.record([('@Документ','Число целое',DOC),('ИдентификаторДокумента','UUID',UUID),('Номер','Строка','FCRT-20261115-2'),('ДокументРасширение.Название','Строка',MARKER)])
    doc=read()
    if doc.get('Номер')!='FCRT-20261115-2' or doc.get('ДокументРасширение.Название')!=MARKER:
        rpc('СчетСкИсх.WriteForParticipant',{'Запись':patch,'ДопПоля':{'ИмяОбъекта':'СчетСкИсх'}},True)
    doc=read()
    if doc.get('Номер')!='FCRT-20261115-2' or doc.get('ДокументРасширение.Название')!=MARKER:raise r.guards.PreflightError('owned_header_write_not_verified')
    filter_record=r.record([('ВызовИзБраузера','Логическое',True),('СкладскойДокумент','Число целое',DOC),('Номенклатура','Число целое',3899),('Количество','Число целое',1),('Цена','Строка','1')])
    captured=Path(__file__).with_name('saby-owned-invoice-prepared-row-43689193259.json').read_text().strip()
    if hashlib.sha256(captured.encode()).hexdigest()!='9feea5c7376fb80625b603fcdfffbe4f294375e4aa455a2f6ef40a841706a85c':raise r.guards.PreflightError('captured_prepared_row_changed')
    template=json.loads(captured)
    fields=r.fields(template)
    proof['preparedRow']=template;save()
    print(json.dumps({'preparedRow':{k:fields.get(k) for k in ['@НоменклатураДокумента','Номенклатура','СкладскойДокумент','Склад','Тип','Количество','Цена','СуммаЦен','Закрыто']}},ensure_ascii=False))
    if any(fields.get(k)!=v for k,v in {'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':1,'Цена':1,'СуммаЦен':1,'Закрыто':None,'@НоменклатураДокумента':None}.items()):raise r.guards.PreflightError('owned_prepared_row_not_exact_not_saved')
    answer=rpc('СчетСкИсх.CustomDocNomUpdate',{'Запись':template,'ДопПоля':{'СкладскойДокумент':DOC}},True)
    proof['saveReply']=answer;save()
    after=lines();rows=r.rows(after)
    proof['readback']=after;save()
    if len(rows)!=1 or any(rows[0].get(k)!=v for k,v in {'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':1,'Цена':1,'СуммаЦен':1,'Закрыто':False}.items()):raise r.guards.PreflightError('owned_saved_row_not_verified_no_retry')
    read()
    print(json.dumps({'verifiedInvoice':{'id':DOC,'uuid':UUID,'row':{k:rows[0].get(k) for k in ['@НоменклатураДокумента','Номенклатура','СкладскойДокумент','Склад','Тип','Количество','Цена','СуммаЦен','Закрыто']}}},ensure_ascii=False))
    return 0

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc,{'phase':'owned_invoice_fill','documentID':DOC})))
        raise SystemExit(1)
