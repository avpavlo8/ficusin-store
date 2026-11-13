#!/usr/bin/env python3
"""Read-only inspection of one approved test product and three fixed seller reads."""
import argparse
import importlib.util
import json
import os
import re
from pathlib import Path
import tempfile
import urllib.parse
import urllib.request
import urllib.error

spec = importlib.util.spec_from_file_location('catalog_audit_guards', Path(__file__).with_name('production-release-preflight.py'))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)

QUERY = """
SELECT p.channel,p.status,p.execution_mode,p.message,p.attempts,
 p.progress->>'stage' stage,p.progress->>'taskId' task_id,
 p.progress->'ids' ids,p.progress->>'createAttempted' create_attempted,
 p.payload->'packaging' packaging,
 p.payload->'marketplace'->>'tnved' sent_tnved,
 COALESCE(p.payload->'channel'->>'article',p.payload->'marketplace'->>'article') sent_article,
 p.payload->'channel'->>'mode' channel_mode,
 p.payload->>'action' action,
 p.pending_payload IS NOT NULL pending,
 p.pending_payload->'marketplace'->>'tnved' pending_tnved,
 p.pending_payload->'packaging' pending_packaging,
 p.progress->'native'->>'preparedHash' prepared_hash,
 p.progress->'native'->>'preparedRequestHash' prepared_request_hash,
 p.payload->'schema' schema,
 p.updated_at
FROM catalog_editor_publications p JOIN product_variants v ON v.id=p.variant_id
WHERE p.variant_id=11412 AND v.product_id=11412
ORDER BY p.channel
"""

def safe_vendor_text(value, env, limit=400):
    if not isinstance(value, str):
        return None
    for name, secret in env.items():
        if any(part in name for part in ('TOKEN', 'KEY', 'SECRET', 'PASSWORD', 'DATABASE_URL')) and isinstance(secret, str) and secret:
            value = value.replace(secret, '[redacted]')
    value = re.sub(r'https?://[^\s<>]+', '[redacted URL]', value)
    value = re.sub(r'\b[^\s@]+@[^\s@]+\.[^\s@]+', '[redacted email]', value)
    return ''.join(char if char.isprintable() else ' ' for char in value)[:limit]


def json_kind(value):
    if value is None: return 'null'
    if isinstance(value, dict): return 'object'
    if isinstance(value, list): return 'array'
    if isinstance(value, bool): return 'boolean'
    if isinstance(value, str): return 'string'
    return 'number'


def vendor_shape(payload, env):
    root = payload if isinstance(payload, dict) else {}
    data = root.get('data')
    data_object = data if isinstance(data, dict) else {}
    keys = lambda value: [safe_vendor_text(str(key), env, 80) for key in sorted(value)[:40]] if isinstance(value, dict) else []
    count = lambda value: len(value) if isinstance(value, list) else None
    samples = lambda value: [keys(item) for item in value[:3] if isinstance(item, dict)] if isinstance(value, list) else []
    return {
        'rootType': json_kind(payload), 'rootKeys': keys(root),
        'dataType': json_kind(data), 'dataKeys': keys(data),
        'itemCounts': {'dataItems': count(data_object.get('items')), 'dataArray': count(data),
                       'rootItems': count(root.get('items')), 'rootCards': count(root.get('cards')),
                       'dataCards': count(data_object.get('cards'))},
        'itemKeySamples': {'dataItems': samples(data_object.get('items')), 'dataArray': samples(data), 'rootCards': samples(root.get('cards'))},
        'errorFlag': root.get('error') if isinstance(root.get('error'), bool) else None,
        'errorText': safe_vendor_text(root.get('errorText'), env),
    }


def wb_error_report(payload, env):
    root = payload if isinstance(payload, dict) else {}
    data = root.get('data')
    batches = data.get('items') if isinstance(data, dict) else data
    recognized = isinstance(batches, list)
    result = {'shape': vendor_shape(payload, env), 'parserFormat': 'data.items' if isinstance(data, dict) and recognized else 'data[]' if recognized else 'unsupported',
              'firstPageOnly': True, 'batchCountScanned': len(batches) if recognized else None, 'testErrors': []}
    result['batchIdentitySummary']=[]
    cursor = data.get('cursor') if isinstance(data, dict) else root.get('cursor')
    result['morePages'] = cursor.get('next') if isinstance(cursor, dict) and isinstance(cursor.get('next'), bool) else None
    for batch in batches if recognized else []:
        if not isinstance(batch, dict): continue
        errors = batch.get('errors')
        codes=batch.get('vendorCodes',[])
        own=isinstance(codes,list) and 'сам' in codes
        result['batchIdentitySummary'].append({'testArticlePresent':own,'vendorCodeCount':len(codes) if isinstance(codes,list) else None,'errorType':json_kind(errors),'errorKeysCount':len(errors) if isinstance(errors,dict) else None})
        if own and len(codes)==1:
            result['singleTestBatchErrorShape']={'keys':[safe_vendor_text(str(k),env,80) for k in errors] if isinstance(errors,dict) else [],'value':safe_vendor_text(json.dumps(errors,ensure_ascii=False),env,1800),**{f:safe_vendor_text(batch[f],env,100) for f in ('updatedAt','batchUUID') if isinstance(batch.get(f),str)}}
        if isinstance(errors, dict) and 'сам' in errors:
            messages = errors['сам']
        elif batch.get('vendorCode') == 'сам':
            messages = errors
        else:
            continue
        if isinstance(messages, str): messages = [messages]
        matching = {'keys': [safe_vendor_text(key, env, 80) for key in sorted(batch)[:40]], 'errors': [safe_vendor_text(message, env) for message in messages[:20] if isinstance(message, str)] if isinstance(messages, list) else [],
                    'errorsType': json_kind(messages), 'errorCount': len(messages) if isinstance(messages,list) else None,
                    'errorValueTypes': sorted({json_kind(message) for message in messages}) if isinstance(messages,list) else []}
        for field in ('createdAt', 'updatedAt', 'created_at', 'updated_at', 'batchUUID'):
            if isinstance(batch.get(field), str):
                matching[field] = safe_vendor_text(batch[field], env, 100)
        result['testErrors'].append(matching)
    return result


def vendor_reads(env, opener=None):
    """Fixed read-only seller queries; an HTTP POST here never changes a card."""
    wb=env.get('CATALOG_WB_API_TOKEN','').strip() or env.get('WB_API_TOKEN','').strip()
    client=env.get('CATALOG_OZON_CLIENT_ID','').strip() or env.get('OZON_CLIENT_ID','').strip()
    key=env.get('CATALOG_OZON_API_KEY','').strip() or env.get('OZON_API_KEY','').strip()
    opener=opener or urllib.request.build_opener(guards.NoRedirect())
    reads={}
    requests=[
      ('wbErrors','https://content-api.wildberries.ru/content/v2/cards/error/list',{'cursor':{'limit':100},'order':{'ascending':False}},{'Authorization':wb}),
      ('wbCards','https://content-api.wildberries.ru/content/v2/get/cards/list',{'settings':{'cursor':{'limit':100},'filter':{'textSearch':'сам','withPhoto':-1}}},{'Authorization':wb}),
      ('ozonCard','https://api-seller.ozon.ru/v3/product/info/list',{'product_id':[6975452207]},{'Client-Id':client,'Api-Key':key})]
    for kind,url,body,auth in requests:
        if not all(auth.values()):
            reads[kind]={'configured':False}
            continue
        request=urllib.request.Request(url,method='POST',data=json.dumps(body).encode(),headers={'Content-Type':'application/json','Accept':'application/json',**auth})
        try:
            with opener.open(request,timeout=25) as response:
                raw=response.read((8<<20)+1)
                if len(raw)>8<<20:
                    raise guards.PreflightError('vendor_read_too_large')
                payload=json.loads(raw)
                result={'httpStatus':response.status}
            if kind=='wbErrors':
                result.update(wb_error_report(payload, env))
            elif kind=='wbCards':
                result['shape']=vendor_shape(payload, env)
                result['testCards']=[{k:c.get(k) for k in ('nmID','vendorCode','title','createdAt','updatedAt','dimensions','sizes')} for c in (payload.get('cards') or []) if isinstance(c,dict) and c.get('vendorCode')=='сам'] if isinstance(payload,dict) else []
            else:
                result['shape']=vendor_shape(payload, env)
                result['testCards']=[{k:c.get(k) for k in ('id','offer_id','barcodes','sku','name','statuses','errors','is_archived','is_autoarchived')} for c in (payload.get('items') or []) if isinstance(c,dict) and str(c.get('id'))=='6975452207' and c.get('offer_id')=='сам'] if isinstance(payload,dict) else []
            reads[kind]=result
        except urllib.error.HTTPError as error:
            reads[kind]={'httpStatus':error.code}
    return reads

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--expected-commit',required=True)
    args=parser.parse_args()
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true':
            raise guards.PreflightError('github_actions_only')
        app=guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=guards.production_settings(app,args.expected_commit)
        import psycopg.rows
        with tempfile.TemporaryDirectory(prefix='ficusin-catalog-audit-') as folder:
            with guards.connect_production(env,folder) as connection:
                user=urllib.parse.unquote(urllib.parse.urlsplit(env['DATABASE_URL']).username)
                identity=connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()
                if identity!=('default_db',user,'on'):
                    raise guards.PreflightError('wrong_database_or_readonly')
                product=connection.execute("SELECT p.name,p.status,v.sku FROM products p JOIN product_variants v ON v.product_id=p.id WHERE p.id=11412 AND v.id=11412").fetchone()
                if not product or product[0]!='Бонсай самшит' or product[2]!='877':
                    raise guards.PreflightError('approved_test_product_changed')
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor:
                    cursor.execute(QUERY)
                    rows=cursor.fetchall()
        for row in rows:
            row['schema']=[a for a in row['schema'] or [] if a.get('required') or a.get('is_required') or any(s in str(a.get('name','')).lower() for s in ['вес','тн','масса'])]
        print(json.dumps({'ok':True,'readOnly':True,'databaseWrites':False,'providerWrites':False,'productId':11412,'productStatus':product[1],'receipts':rows,'vendorReadback':vendor_reads(env)},ensure_ascii=False,default=str))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':'test_product_publication_audit'})))
        return 1

if __name__=='__main__':
    raise SystemExit(main())
