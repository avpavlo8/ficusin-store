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
METHODS={'СчетСкИсх.ПрочитатьДляУчастника','СчетСкИсх.DocNomList','СчетСкИсх.WriteForParticipant','СчетСкИсх.DocNomCreate','СчетСкИсх.CustomDocNomUpdate','СчетСкИсх.CustomDocNomDelete','Reserve.Apply','Reserve.Remove','Reserve.ListByDn'}

def main():
    if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise r.guards.PreflightError('first_attempt_only')
    env=r.guards.production_settings(r.guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223')['app'],os.environ['EXPECTED_COMMIT'])
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise r.guards.PreflightError('auth_failed')
    print('::add-mask::'+token);r.READS.update(METHODS)
    proof={'documentID':DOC,'documentUUID':UUID,'paymentRequested':False,'fiscalRequested':False,'postingRequested':False,'attempts':[]}
    path=Path('saby-owned-invoice-edit.json')
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
    doc=read()
    initial=lines();before=r.rows(initial)
    if len(before)!=1 or any(before[0].get(k)!=v for k,v in {'@НоменклатураДокумента':175787,'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':1,'Цена':1,'Закрыто':False}.items()):raise r.guards.PreflightError('fixed_owned_before_edit_mismatch')
    if r.rows(rpc('Reserve.ListByDn',{'DocNomIds':[175787]})):raise r.guards.PreflightError('unexpected_before_reserve')
    rpc('Reserve.Apply',{'DocId':DOC,'DocNomId':175787,'Count':1,'PlannedDate':None},True)
    reserved=rpc('Reserve.ListByDn',{'DocNomIds':[175787]});proof['beforeEditReserve']=reserved;save()
    if len(r.rows(reserved))!=1 or r.rows(reserved)[0].get('Количество')!=1 or r.rows(reserved)[0].get('Активен') is not True:raise r.guards.PreflightError('before_edit_reserve_not_confirmed')
    rpc('Reserve.Remove',{'DocId':DOC,'DocNomId':175787},True)
    if r.rows(rpc('Reserve.ListByDn',{'DocNomIds':[175787]})):raise r.guards.PreflightError('before_edit_remove_not_confirmed')
    rpc('СчетСкИсх.CustomDocNomDelete',{'ИдО':['175787'],'ДопПоля':{'СкладскойДокумент':DOC}},True)
    if r.rows(lines()):raise r.guards.PreflightError('owned_delete_not_confirmed')
    filter_record=r.record([('ВызовИзБраузера','Логическое',True),('СкладскойДокумент','Число целое',DOC),('Номенклатура','Число целое',3899),('Количество','Число целое',2),('Цена','Строка','1')])
    template=rpc('СчетСкИсх.DocNomCreate',{'Фильтр':filter_record,'ИмяМетода':None},True)
    fields=r.fields(template);proof['preparedRow']=template;save()
    if any(fields.get(k)!=v for k,v in {'@НоменклатураДокумента':None,'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':2,'Цена':1,'СуммаЦен':2,'Закрыто':None}.items()):raise r.guards.PreflightError('edited_template_not_exact')
    answer=rpc('СчетСкИсх.CustomDocNomUpdate',{'Запись':template,'ДопПоля':{'СкладскойДокумент':DOC}},True)
    saved_id=r.fields(answer).get('result')
    if not isinstance(saved_id,int) or saved_id<=0:raise r.guards.PreflightError('edited_save_id_missing_no_retry')
    proof['savedRowID']=saved_id;save()
    after=lines();rows=r.rows(after);proof['readback']=after;save()
    if len(rows)!=1 or any(rows[0].get(k)!=v for k,v in {'@НоменклатураДокумента':saved_id,'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':4,'Количество':2,'Цена':1,'СуммаЦен':2,'Закрыто':False}.items()):raise r.guards.PreflightError('edited_save_not_verified')
    try:
        rpc('Reserve.Apply',{'DocId':DOC,'DocNomId':saved_id,'Count':2,'PlannedDate':None},True)
        reserved=rpc('Reserve.ListByDn',{'DocNomIds':[saved_id]});proof['afterEditReserve']=reserved;save()
        if len(r.rows(reserved))!=1 or r.rows(reserved)[0].get('Количество')!=2 or r.rows(reserved)[0].get('Активен') is not True:raise r.guards.PreflightError('after_edit_reserve_not_confirmed')
    finally:
        rpc('Reserve.Remove',{'DocId':DOC,'DocNomId':saved_id},True)
        remaining=rpc('Reserve.ListByDn',{'DocNomIds':[saved_id]});proof['finalReserve']=remaining;save()
        if r.rows(remaining):raise r.guards.PreflightError('final_reserve_not_empty')
    read()
    print(json.dumps({'verifiedEdit':{'documentID':DOC,'documentUUID':UUID,'oldRowID':175787,'newRowID':saved_id,'quantityBefore':1,'quantityAfter':2,'afterEditReserve':r.rows(reserved),'finalReserveEmpty':not r.rows(remaining),'postingRequested':False,'paymentRequested':False,'fiscalRequested':False}},ensure_ascii=False))
    return 0

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc,{'phase':'owned_invoice_fill','documentID':DOC})))
        raise SystemExit(1)
