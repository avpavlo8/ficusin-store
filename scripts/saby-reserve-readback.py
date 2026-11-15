#!/usr/bin/env python3
"""Read-only inspection of one owner-controlled synthetic reserve invoice."""
import importlib.util
import json
import os
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
    if doc.get('@Документ') != DOC or doc.get('ИдентификаторДокумента') != UUID:
        raise r.guards.PreflightError('fixed_owned_invoice_identity_mismatch')
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
    return 0

if __name__=='__main__':
    try: raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc, {'phase':'owned_reserve_readback','documentUUID':UUID})))
        raise SystemExit(1)
