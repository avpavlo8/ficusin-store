#!/usr/bin/env python3
"""Inspect exactly two approved blocked publications. Never repair or replay them.

Business reads only: WB cards/errors, Saby native card read; auth is the sole
non-business POST. Raw DB rows, articles, IDs, vendor errors and secrets stay in
memory. Output is limited to classifications, booleans and counts.
"""
import argparse
from decimal import Decimal, InvalidOperation
import importlib.util
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.parse
import urllib.request

_spec = importlib.util.spec_from_file_location('reconciliation_guards', Path(__file__).with_name('production-release-preflight.py'))
guards = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(guards)
MAX_RESPONSE = 8 << 20
MAX_PAGES = 5
TARGET = 11412
QUERY = """SELECT p.channel,p.status,p.execution_mode,p.payload,p.progress,
 p.pending_payload,p.updated_at FROM public.catalog_editor_publications p
 JOIN public.product_variants v ON v.id=p.variant_id
 WHERE p.variant_id=11412 AND v.product_id=11412 AND p.channel IN ('saby','wb')
 ORDER BY p.channel"""
ENDPOINTS = {
 'wb_cards':'https://content-api.wildberries.ru/content/v2/get/cards/list',
 'wb_errors':'https://content-api.wildberries.ru/content/v2/cards/error/list',
 'saby_auth':'https://online.sbis.ru/oauth/service/',
 'saby_read':'https://online.sbis.ru/service/?srv=1',
}
CARD_FIELDS = ['CodeList','Images','ImageData','PriceData','Category','CategoryName','CategoryObject','Number','Name','NameBase','Description','Identifier','IsDraft','VAT','MeasureUnit','Path']


def require(condition, code):
    if not condition:
        raise guards.PreflightError(code)


def article(payload):
    channel = payload.get('channel') or {}
    market = payload.get('marketplace') or {}
    for value in (channel.get('article'), (market.get('wb') or {}).get('article'), market.get('article')):
        if value is not None:
            require(isinstance(value,str), 'invalid_saved_article')
            return value.strip()
    return ''


def validate_rows(rows):
    require(len(rows)==2 and {r['channel'] for r in rows}=={'saby','wb'}, 'approved_operations_missing')
    result = {r['channel']: r for r in rows}
    for row in rows:
        require(row['execution_mode']=='live' and isinstance(row['payload'],dict) and isinstance(row['progress'],dict), 'approved_operation_changed')
        require(row['payload'].get('variantId')==TARGET and row['payload'].get('productId')==TARGET, 'saved_identity_mismatch')
        require(row['progress'].get('createAttempted') is True, 'approved_operation_changed')
    wb, saby = result['wb'], result['saby']
    require(wb['status']=='failed' and wb['progress'].get('stage')=='reconcile' and not (wb['progress'].get('ids') or {}).get('productId') and not (wb['progress'].get('ids') or {}).get('uuid') and article(wb['payload']), 'approved_wb_operation_changed')
    ids=saby['progress'].get('ids') or {}
    require(saby['status']=='needs_attention' and saby['progress'].get('stage')=='send-save' and re.fullmatch('[1-9][0-9]{0,18}',str(ids.get('productId',''))) and re.fullmatch('[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}',str(ids.get('uuid',''))), 'approved_saby_operation_changed')
    return result


class VendorReader:
    def __init__(self, env, opener=None):
        self.env=env
        self.opener=opener or urllib.request.build_opener(guards.NoRedirect())
        self.token=None
        self.calls=0

    def read(self, kind, body):
        require(kind in ENDPOINTS, 'vendor_method_not_allowed')
        if kind.startswith('wb_'):
            token=self.env.get('CATALOG_WB_API_TOKEN','').strip() or self.env.get('WB_API_TOKEN','').strip()
            require(bool(token), 'wb_not_configured')
            headers={'Authorization':token}
        elif kind=='saby_auth':
            require(set(body)=={'app_client_id','app_secret','secret_key'} and all(body.values()), 'saby_not_configured')
            headers={}
        else:
            require(bool(self.token) and body.get('method')=='Nomenclature.Read' and body.get('protocol')==6, 'vendor_method_not_allowed')
            headers={'X-SBISAccessToken':self.token}
        request=urllib.request.Request(ENDPOINTS[kind],method='POST',data=json.dumps(body,ensure_ascii=False).encode('utf-8'),headers={
            'Content-Type':'application/json-rpc;charset=utf-8' if kind=='saby_read' else 'application/json;charset=utf-8', 'Accept':'application/json',**headers})
        self.calls+=1
        with self.opener.open(request,timeout=20) as response:
            require(response.status==200, 'vendor_read_rejected')
            raw=response.read(MAX_RESPONSE+1)
            charset=response.headers.get_content_charset() or 'utf-8'
        require(len(raw)<=MAX_RESPONSE, 'vendor_response_too_large')
        reply=json.loads(raw.decode(charset),parse_float=Decimal)
        require(isinstance(reply,dict) and not reply.get('error'), 'vendor_read_rejected')
        return reply

    def authenticate(self):
        reply=self.read('saby_auth',{'app_client_id':self.env.get('SABY_APP_CLIENT_ID',''),'app_secret':self.env.get('SABY_APP_SECRET',''),'secret_key':self.env.get('SABY_SECRET_KEY','')})
        self.token=reply.get('token')
        require(isinstance(self.token,str) and bool(self.token), 'saby_authentication_failed')


def error_page(reply, target):
    data=reply.get('data')
    if isinstance(data,dict):
        batches=data.get('items'); cursor=data.get('cursor') or {}
        require(isinstance(batches,list) and isinstance(cursor,dict), 'wb_error_schema_unknown')
        require('next' in cursor, 'wb_error_schema_unknown')
        more=cursor['next']
        require(isinstance(more,bool), 'wb_error_schema_unknown')
    else:
        require(isinstance(data,list), 'wb_error_schema_unknown')
        batches=data;cursor={};more=False
    matching=[]
    for batch in batches:
        require(isinstance(batch,dict), 'wb_error_schema_unknown')
        errors=batch.get('errors')
        if isinstance(errors,dict):
            messages=errors.get(target,[])
        elif batch.get('vendorCode')==target:
            messages=errors
        else:
            continue
        if isinstance(messages,str): messages=[messages]
        require(isinstance(messages,list) and all(isinstance(m,str) for m in messages), 'wb_error_schema_unknown')
        if messages:
            matching.append((messages,batch.get('updatedAt') or batch.get('createdAt')))
    return matching,more,cursor


def reconcile_wb(row, reader):
    target=article(row['payload']); barcode=row['payload'].get('barcode') or ''
    cursor={'limit':100};cards=[];cards_complete=False
    for _ in range(MAX_PAGES):
        reply=reader.read('wb_cards',{'settings':{'cursor':cursor,'filter':{'textSearch':target,'withPhoto':-1}}})
        page=reply.get('cards'); next_cursor=reply.get('cursor') or {}
        require(isinstance(page,list) and isinstance(next_cursor,dict), 'wb_cards_schema_unknown')
        cards.extend(c for c in page if isinstance(c,dict) and c.get('vendorCode')==target)
        if len(page)<100 or (isinstance(next_cursor.get('total'),int) and next_cursor['total']<100):
            cards_complete=True;break
        require(next_cursor.get('updatedAt') and isinstance(next_cursor.get('nmID'),int), 'wb_cards_cursor_missing')
        next_value={'limit':100,'updatedAt':next_cursor['updatedAt'],'nmID':next_cursor['nmID']}
        require(next_value!=cursor, 'wb_cards_cursor_stuck');cursor=next_value
    cursor={'limit':100};matches=[];errors_complete=False;scanned_batches=0;unattributed_mentions=0
    for _ in range(MAX_PAGES):
        reply=reader.read('wb_errors',{'cursor':cursor,'order':{'ascending':False}})
        page,more,next_cursor=error_page(reply,target)
        batches=reply['data']['items'] if isinstance(reply['data'],dict) else reply['data']
        scanned_batches+=len(batches)
        unattributed_mentions+=sum(isinstance(b.get('vendorCodes'),list) and target in b['vendorCodes'] and b.get('vendorCode')!=target and not (isinstance(b.get('errors'),dict) and target in b['errors']) for b in batches)
        matches.extend(page)
        if not more:errors_complete=True;break
        require(next_cursor.get('updatedAt') and next_cursor.get('batchUUID'), 'wb_error_cursor_missing')
        next_value={'limit':100,'updatedAt':next_cursor['updatedAt'],'batchUUID':next_cursor['batchUUID']}
        require(next_value!=cursor, 'wb_error_cursor_stuck');cursor=next_value
    owned=[];provider_barcode_matches=0
    for card in cards:
        codes=[code for size in card.get('sizes') or [] if isinstance(size,dict) for code in size.get('skus') or []]
        if barcode and barcode in codes and isinstance(card.get('nmID'),int) and card['nmID']>0:owned.append(card)
        sizes=card.get('sizes') or []
        first_codes=sizes[0].get('skus') or [] if sizes and isinstance(sizes[0],dict) else []
        if barcode and first_codes and first_codes[0]==barcode:provider_barcode_matches+=1
    tags=set()
    for messages,_ in matches:
        for message in messages:
            lower=message.lower()
            for tag,terms in {'tnved':('тнвэд','тн вэд','tnved'),'mandatory':('обязател','required'),'density':('density','габарит','вес')}.items():
                if any(term in lower for term in terms):tags.add(tag)
    classification='exact_card_barcode_ownership_confirmed' if cards_complete and len(cards)==len(owned)==1 else 'article_collision_ownership_unproven' if cards else 'no_exact_card_result_still_unknown'
    if not cards_complete or not errors_complete:classification='bounded_read_incomplete_result_unknown'
    return {'classification':classification,'cardsComplete':cards_complete,'errorsComplete':errors_complete,'exactCardCount':len(cards),'barcodeOwnedCardCount':len(owned),'providerFirstBarcodeMatchCount':provider_barcode_matches,'exactArticleErrorBatchCount':len(matches),'scannedErrorBatchCount':scanned_batches,'unattributedExactArticleBatchCount':unattributed_mentions,'errorTags':sorted(tags),'batchesWithTimestamp':sum(bool(stamp) for _,stamp in matches),
            'definitiveRejectionProven':False,'savedBatchCorrelationAvailable':False,'pendingPayload':row['pending_payload'] is not None,'replayAuthorized':False}


def native_record(value):
    require(isinstance(value,dict) and value.get('_type')=='record' and isinstance(value.get('s'),list) and isinstance(value.get('d'),list) and len(value['s'])==len(value['d']), 'saby_schema_unknown')
    names=[s.get('n') for s in value['s'] if isinstance(s,dict)]
    require(len(names)==len(value['s']) and all(isinstance(n,str) for n in names) and len(set(names))==len(names), 'saby_schema_unknown')
    return dict(zip(names,value['d']))


def native_rows(value):
    require(isinstance(value,dict) and value.get('_type')=='recordset' and isinstance(value.get('d'),list), 'saby_schema_unknown')
    return [native_record({'_type':'record','s':value.get('s'),'d':data}) for data in value['d']]


def native_price_matches_minor(price, minor):
    # The editor request contract is integer kopecks (priceMinor). Native
    # PriceData.Price is rubles. Missing/nullable values are missing evidence,
    # never zero and never an excuse to infer a differently named field.
    if isinstance(minor, bool) or not isinstance(minor, int) or minor <= 0 or price is None or isinstance(price, bool):
        return False
    try:
        value = Decimal(str(price))
        return value.is_finite() and value > 0 and value * 100 == minor
    except (InvalidOperation, ValueError, TypeError):
        return False


def saby_field_matches(card, request, progress, price_list):
    state=progress.get('native') or {};saby=request.get('saby') or {}
    fields={name: str(card.get(native,''))==str(expected) for name,native,expected in [
        ('name','Name',str(request.get('name','')).strip()),('category','Category',(saby.get('category') or {}).get('id','')),('folder','Folder',(saby.get('folder') or {}).get('id',''))]}
    fields['notDraft']=card.get('IsDraft') is False
    description=[['p',{'version':'2'},line] for line in str(request.get('description','')).strip().replace('\r\n','\n').split('\n')]
    fields['description']=card.get('Description')==description
    codes=native_rows(card.get('CodeList'))
    fields['barcode']=bool(request.get('barcode')) and any(c.get('Code')==request['barcode'] and c.get('Type')==2 for c in codes)
    images=native_rows(card.get('ImageData')); expected_images=state.get('imageIds') or []
    fields['images']=bool(expected_images) and len(expected_images)>=len(request.get('images') or []) and len(images)==len(expected_images) and all(c.get('id')==expected_images[i] and c.get('isMaster') is (i==0) for i,c in enumerate(images))
    if state.get('created') is True:
        prices=[p for p in native_rows(card.get('PriceData')) if p.get('PriceList')==price_list]
        fields['createdPrice']=len(prices)==1 and native_price_matches_minor(prices[0].get('Price'),request.get('priceMinor'))
    return fields


def reconcile_saby(row, reader, env):
    progress=row['progress']; request=row['payload'];ids=progress['ids'];expected=str(ids['productId'])
    reader.authenticate()
    price_list=int(env.get('SABY_PRICE_LIST_ID') or 6)
    require(0<price_list<2147483647, 'invalid_saby_price_list')
    option={'_type':'record','s':[{'n':'PriceList','t':{'n':'Массив','t':'Число целое'}}],'d':[[price_list]]}
    reply=reader.read('saby_read',{'jsonrpc':'2.0','protocol':6,'method':'Nomenclature.Read','params':{'Id':int(expected),'AdditionalFields':CARD_FIELDS,'Option':option},'id':1})
    card=native_record(reply.get('result'))
    require(str(card.get('Id'))==expected and isinstance(card.get('Identifier'),str) and card['Identifier'].lower()==ids['uuid'].lower(), 'saby_identity_mismatch')
    for required in [request.get('sabyUUID')]:
        require(not required or str(required).lower()==card['Identifier'].lower(), 'saby_identity_mismatch')
    expected_codes=[ids.get('offerId')]
    if (request.get('saby') or {}).get('mode')=='link' or request.get('action')=='update':expected_codes.append((request.get('saby') or {}).get('existingCode'))
    require(isinstance(card.get('Number'),str) and bool(card['Number']) and all(not code or card['Number']==code for code in expected_codes), 'saby_code_mismatch')
    fields=saby_field_matches(card,request,progress,price_list)
    state=progress.get('native') or {}
    selected_prices=[p for p in native_rows(card.get('PriceData')) if p.get('PriceList')==price_list] if state.get('created') is True else []
    price_evidence={'requestPriceMinorKnown':type(request.get('priceMinor')) is int and request['priceMinor']>0,'selectedNativePriceRowCount':len(selected_prices),'selectedNativePriceNotNull':len(selected_prices)==1 and selected_prices[0].get('Price') is not None}
    legacy=not str(state.get('preparedHash','')).startswith('json-v1:') or not str(state.get('preparedRequestHash','')).startswith('json-v1:')
    return {'classification':'identity_verified_legacy_receipt_requires_explicit_reprepare' if legacy else 'identity_verified_readback_requires_receipt_review','identityVerified':True,'frozenFieldMatches':fields,'allCheckedFrozenFieldsMatch':all(fields.values()),'legacyPreparedHashes':legacy,'priceEvidence':price_evidence,'pendingPayload':row['pending_payload'] is not None,'replayAuthorized':False}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--expected-commit',required=True);args=parser.parse_args()
    diagnostics={'phase':'catalog_readonly_reconciliation'}
    try:
        require(os.environ.get('GITHUB_ACTIONS')=='true','github_actions_only')
        app=guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{})
        env=guards.production_settings(app,args.expected_commit)
        import psycopg.rows
        with tempfile.TemporaryDirectory(prefix='ficusin-readonly-reconcile-') as directory:
            with guards.connect_production(env,directory) as connection:
                user=urllib.parse.unquote(urllib.parse.urlsplit(env['DATABASE_URL']).username)
                require(connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()==('default_db',user,'on'),'wrong_database_or_readonly')
                require(connection.execute("SELECT p.name FROM public.products p JOIN public.product_variants v ON v.product_id=p.id WHERE p.id=11412 AND v.id=11412 AND v.sku='877'").fetchone()==('Бонсай самшит',),'approved_product_changed')
                with connection.cursor(row_factory=psycopg.rows.dict_row) as cursor:
                    cursor.execute(QUERY); rows=validate_rows(cursor.fetchall())
                pricing=connection.execute("SELECT COUNT(*) total,COUNT(*) FILTER(WHERE economics#>>'{unified,enabled}'='true') unified_enabled FROM public.procurement_pricing_settings").fetchone()
                require(len(pricing)==2 and all(isinstance(n,int) and n>=0 for n in pricing),'pricing_inventory_shape_unknown')
        report={'readOnly':True,'databaseWrites':0,'businessWrites':0,'changesApplied':False,'pricingModels':{'total':pricing[0],'unifiedEnabled':pricing[1],'legacyModelConfigured':pricing[0]>pricing[1]}}
        for channel,fn in [('wb',lambda reader:reconcile_wb(rows['wb'],reader)),('saby',lambda reader:reconcile_saby(rows['saby'],reader,env))]:
            reader=VendorReader(env)
            try:
                report[channel]=fn(reader)
                report[channel]['readOK']=True
            except Exception as error:
                failure=guards.safe_failure(error,diagnostics)
                report[channel]={'readOK':False,'classification':'read_failed_result_unresolved','exceptionClass':failure['exceptionClass'],'reason':failure.get('reason','read_failed'),'httpStatus':failure.get('httpStatus'),'replayAuthorized':False}
            report[channel]['readCallsIncludingAuthentication']=reader.calls
        report['allReadsSucceeded']=all(report[ch]['readOK'] for ch in ('wb','saby'))
        print(json.dumps(report,sort_keys=True));return 0 if report['allReadsSucceeded'] else 1
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,diagnostics),sort_keys=True));return 1

if __name__=='__main__':raise SystemExit(main())
