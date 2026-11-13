#!/usr/bin/env python3
"""One explicit action on the fixed owned test realization; never a customer order."""
import argparse,importlib.util,json,os,time
from pathlib import Path
spec=importlib.util.spec_from_file_location('reads',Path(__file__).with_name('saby-realization-readback.py'))
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
DOC=42807;UUID='8d6e3870-40b3-4c82-8d2b-ccefd310637d';VI='1eacfa69-a5be-4790-8b9a-a198b3493220'
def flags(raw):
    data=r.fields(raw);column=next(c for c in raw['s'] if c['n']=='ДокументРасширение.Состояние');labels=column['t']['s'];values=data['ДокументРасширение.Состояние'];return {name:values[i] for i,name in enumerate(labels.values()) if i<len(values)}
def main():
    p=argparse.ArgumentParser();p.add_argument('--expected-commit',required=True);p.add_argument('--action',choices=['bind-header','post-once'],required=True);a=p.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise r.guards.PreflightError('first_attempt_only')
        app=r.guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=r.guards.production_settings(app,a.expected_commit)
        auth=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]});token=auth.get('token')
        if not token:raise r.guards.PreflightError('auth_failed')
        print('::add-mask::'+token)
        def read():
            raw=r.rpc('ДокОтгрИсх.Прочитать',{'ИдО':DOC,'ИмяМетода':'ДокОтгрИсх.Список'},token,6);d=r.fields(raw)
            required={'@Документ':DOC,'ИдентификаторДокумента':UUID,'ДокументРасширение.ИдентификаторВИ':VI,'ТипДокумента':226,'ТипДокумента.ТипДокумента':'ДокОтгрИсх','СкладскойДокумент.Склад':283,'ДокументНашаОрганизация':259,'Регламент':122498,'Лицо1':18655}
            if any(d.get(k)!=v for k,v in required.items()):raise r.guards.PreflightError('fixed_test_identity_mismatch')
            lines=r.rpc('РеалИсх.DocNomList',{'Фильтр':r.record([('СкладскойДокумент','Число целое',DOC),('Тип','Число целое',1),('ЭтоРасходы','Логическое',False),('Раздел','Число целое',None)]),'Сортировка':None,'Навигация':r.record([('ЕстьЕще','Логическое',True),('Страница','Число целое',0),('РазмерСтраницы','Число целое',100)]),'ДопПоля':[]},token,6)
            rows=r.rows(lines)
            required_line={'@НоменклатураДокумента':175645,'Номенклатура':3899,'СкладскойДокумент':DOC,'Склад':284,'Тип':1,'Количество':1,'Цена':1,'СуммаЦен':1}
            if len(rows)!=1 or any(rows[0].get(k)!=v for k,v in required_line.items()):raise r.guards.PreflightError('fixed_test_line_mismatch')
            f=flags(raw)
            if any(f.get(k) not in (False,None) for k in ['ОшибкиПроведения','НеактуальныеПроводки','ВОчередиНаПроведение','ВОчередиНаРасчетПроводок']):raise r.guards.PreflightError('test_state_requires_review')
            return raw,d,f,rows[0]
        raw,d,f,line=read()
        if f.get('Проведен') is True or f.get('ЕстьПроводки') is True or line.get('Закрыто') is True:
            print(json.dumps({'alreadyPostedOrClosed':True,'noWrite':True,'documentID':DOC,'flags':{k:f.get(k)for k in ['Проведен','ЕстьПроводки','ОшибкиПроведения']}}));return 0
        if a.action=='bind-header':
            method='РеалИсх.Записать';params={'Запись':r.record([('@Документ','Число целое',DOC),('Лицо1','Связь',18655)])}
        else:
            method='Документ.PostAsync';params=json.loads(Path(__file__).with_name('saby-realization-post-test-contract.json').read_text())
        attempt={'runId':os.environ.get('GITHUB_RUN_ID'),'action':a.action,'documentID':DOC,'documentUUID':UUID,'publicUUID':VI,'dedicatedTestNomenclatureID':3899,'testQuantity':1,'postingRequested':a.action=='post-once','paymentRequested':False,'fiscalRequested':False,'retryPolicy':'read-only'}
        Path('saby-realization-test-receipt.json').write_text(json.dumps(attempt));print(json.dumps({'attemptRecorded':attempt}),flush=True)
        r.READS.add(method)
        answer=r.request(r.SERVICE,{'jsonrpc':'2.0','protocol':7,'method':method,'params':params,'id':1},token)
        if answer.get('error'):
            err=answer['error'];print(json.dumps({'method':method,'apiError':{k:err.get(k)for k in ['code','message']},'retryPolicy':'read-only'},ensure_ascii=False));return 1
        result=answer.get('result');print(json.dumps({'method':method,'responseType':type(result).__name__,'response':result if isinstance(result,(str,bool,int,type(None))) else r.shape(result)},ensure_ascii=False))
        for i in range(6):
            raw,d,f,line=read();posted=f.get('Проведен')is True and f.get('ЕстьПроводки')is True and line.get('Закрыто')is True
            print(json.dumps({'readback':i,'documentID':DOC,'buyerVerified':d.get('Лицо1')==18655,'postedAndClosed':posted,'flags':{k:f.get(k) for k in ['Проведен','ЕстьПроводки','ОшибкиПроведения','НеактуальныеПроводки','ВОчередиНаПроведение','ВОчередиНаРасчетПроводок']},'testLine':{k:line.get(k)for k in ['@НоменклатураДокумента','Номенклатура','Склад','Тип','Количество','Цена','Закрыто']}},ensure_ascii=False))
            if a.action=='bind-header' or posted:return 0
            time.sleep(3)
        return 1
    except Exception as error:
        print(json.dumps(r.guards.safe_failure(error,{'phase':'owned_realization_test','documentID':DOC,'retryPolicy':'read-only'})));return 1
if __name__=='__main__':raise SystemExit(main())
