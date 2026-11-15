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
    answer = r.request(r.SERVICE, {'jsonrpc':'2.0','protocol':3,'method':'СБИС.ПрочитатьДокумент','params':{'Документ':{'Идентификатор':UUID}},'id':1},token)
    if answer.get('error'):
        error=answer['error']
        print(json.dumps({'readOnly':True,'documentUUID':UUID,'apiError':{k:error.get(k) for k in ['code','message','details']}},ensure_ascii=False))
        return 1
    doc=answer.get('result') or {}
    print(json.dumps({'readOnly':True,'documentUUID':UUID,'resultKeys':list(doc),'document':{k:doc.get(k) for k in ['Идентификатор','Номер','Дата','Тип','Состояние','Расширение','Регламент']},'attachmentTypes':[{k:a.get(k) for k in ['Идентификатор','Тип']} for a in doc.get('Вложение',[])]},ensure_ascii=False))
    return 0

if __name__=='__main__':
    try: raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc, {'phase':'owned_reserve_readback','documentUUID':UUID})))
        raise SystemExit(1)
