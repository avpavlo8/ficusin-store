"""Owner-authorized one-product CML probe through the existing service connection."""
import base64, json, os, time, urllib.error, urllib.parse, urllib.request, uuid
import xml.etree.ElementTree as E

MARKER='FICUSIN-API-TEST-20261031'
NAME='ТЕСТ API Фикусин — НЕ ПРОДАВАТЬ '+MARKER
NS=uuid.uuid5(uuid.NAMESPACE_URL,'https://ficusin.ru/probe/20261031/01a18c82-a931-7a50-b35a-973a7b3bdb18')
uid=lambda key:str(uuid.uuid5(NS,key))
GUID=uid('nomenclature')
token=''
report={'probe':MARKER,'guid':GUID,'point':278,'priceList':6}

def emit(stage,value): print(json.dumps({'stage':stage,'result':value},ensure_ascii=False),flush=True)
def request(url,payload=None,auth=True):
    headers={'Content-Type':'application/json;charset=utf-8','Accept':'application/json'}
    if auth: headers['X-SBISAccessToken']=token
    req=urllib.request.Request(url,data=json.dumps(payload,ensure_ascii=False).encode() if payload is not None else None,headers=headers)
    try:
        with urllib.request.urlopen(req,timeout=45) as f:return json.load(f)
    except urllib.error.HTTPError as err:
        text=err.read(4096).decode('utf-8','replace')
        for secret in [token]+[os.environ.get(k,'') for k in ['SABY_APP_CLIENT_ID','SABY_APP_SECRET','SABY_SECRET_KEY']]:
            if secret:text=text.replace(secret,'[redacted]')
        raise RuntimeError(f'HTTP {err.code}: {text[:1000]}') from None
def rows(value):
    if isinstance(value,list):return value
    if isinstance(value,dict):
        for key in ['nomenclatures','items','result','data','priceLists','pricelists']:
            if key in value:return rows(value[key])
    return []
def find(priced=False):
    q={'pointId':278,'searchString':MARKER,'pageSize':25,'page':0,'withBalance':'true','withBarcode':'true'}
    if priced:q['priceListId']=6
    data=request('https://api.sbis.ru/retail/v2/nomenclature/list?'+urllib.parse.urlencode(q))
    return [r for r in rows(data) if not r.get('isParent') and (r.get('article')==MARKER or r.get('nomNumber')==MARKER or MARKER in r.get('name',''))]
def wait_find():
    for i in range(6):
        result=find()
        if result:return result
        if i<5:time.sleep(3)
    return []
def snapshot(items):return [{k:r.get(k) for k in ['id','hierarchicalId','name','article','nomNumber','cost','balance','published','attributes']} for r in items]
def cml(price_name,pot='12',height='35',price='1234.56'):
    root=E.Element('КоммерческаяИнформация',ВерсияСхемы='2.07',ДатаФормирования='2026-10-31')
    def add(p,k,v=None,**attrs):
        e=E.SubElement(p,k,attrs)
        if v is not None:e.text=str(v)
        return e
    classifier=add(root,'Классификатор');add(classifier,'Ид',uid('classifier'));add(classifier,'Наименование','Тест API Фикусин')
    props=add(classifier,'Свойства')
    for key,label in [('pot','Диаметр горшка'),('height','Высота')]:
        prop=add(props,'Свойство');add(prop,'Ид',uid(key));add(prop,'Наименование',label);add(prop,'ТипЗначений','Число')
    cat=add(root,'Каталог',СодержитТолькоИзменения='true');add(cat,'Ид',uid('catalog'));add(cat,'ИдКлассификатора',uid('classifier'));add(cat,'Наименование','Тест API Фикусин')
    item=add(add(cat,'Товары'),'Товар');add(item,'Ид',GUID);add(item,'Артикул',MARKER);add(item,'Наименование',NAME)
    add(item,'Описание','Тест интеграции. Не продавать. Без складского остатка.')
    add(item,'БазоваяЕдиница','шт',Код='796',НаименованиеПолное='Штука',МеждународноеСокращение='PCE')
    props=add(item,'ЗначенияСвойств')
    for key,value in [('pot',pot),('height',height)]:
        prop=add(props,'ЗначенияСвойства');add(prop,'Ид',uid(key));add(prop,'Значение',value)
    if price_name:
        offers=add(root,'ПакетПредложений',СодержитТолькоИзменения='true');add(offers,'Ид',uid('offers'));add(offers,'ИдКаталога',uid('catalog'));add(offers,'ИдКлассификатора',uid('classifier'))
        pt=add(add(offers,'ТипыЦен'),'ТипЦены');add(pt,'Ид','6');add(pt,'Наименование',price_name);add(pt,'Валюта','RUB')
        offer=add(add(offers,'Предложения'),'Предложение');add(offer,'Ид',GUID);add(offer,'Наименование',NAME)
        val=add(add(offer,'Цены'),'Цена');add(val,'ИдТипаЦены','6');add(val,'ЦенаЗаЕдиницу',price);add(val,'Валюта','RUB');add(val,'Единица','шт');add(val,'Коэффициент','1')
    assert len(root.findall('.//Товар'))==1 and root.find('.//Товар/Артикул').text==MARKER
    assert root.find('.//Количество') is None
    return E.tostring(root,encoding='utf-8',xml_declaration=True)
def send(raw):
    return request('https://online.sbis.ru/service/?srv=1',{'jsonrpc':'2.0','protocol':3,'method':'СБИС.ИмпортНоменклатурыИзCML','params':{'Параметр':{'Файл':base64.b64encode(raw).decode()}},'id':1})
def main():
    global token
    assert os.environ.get('GITHUB_REF_NAME')=='probe/saby-card-api-20261031' and os.environ.get('SABY_PROBE_SEARCH')==MARKER,'Explicit probe branch/marker required'
    assert os.environ.get('SABY_POINT_ID')=='278','Unexpected point'
    token=request('https://online.sbis.ru/oauth/service/',{'app_client_id':os.environ['SABY_APP_CLIENT_ID'],'app_secret':os.environ['SABY_APP_SECRET'],'secret_key':os.environ['SABY_SECRET_KEY']},False).get('token','')
    assert token,'Service authentication returned no token'
    print('::add-mask::'+token,flush=True);emit('service_auth','ok')
    price_data=request('https://api.sbis.ru/retail/nomenclature/price-list?'+urllib.parse.urlencode({'pointId':278,'actualDate':'2026-10-31','page':0,'pageSize':100}))
    emit('price_list_rows', rows(price_data))
    baseline=request('https://api.sbis.ru/retail/v2/nomenclature/list?'+urllib.parse.urlencode({'pointId':278,'searchString':'Аглаонема D12 в асс.','pageSize':25,'page':0}))
    emit('connection_catalog_check', [{'id':r.get('id'),'name':r.get('name'),'article':r.get('article'),'cost':r.get('cost')} for r in rows(baseline) if not r.get('isParent')][:2])
    emit('public_api_version',request('https://online.sbis.ru/service/?srv=1',{'jsonrpc':'2.0','protocol':3,'method':'СБИС.ИнформацияОВерсии','params':{},'id':1}))
    selected=[r for r in rows(price_data) if str(r.get('id'))=='6']
    emit('price_list_schema',{'keys':list(price_data) if isinstance(price_data,dict) else [],'selected':[{'id':r.get('id'),'name':r.get('name')} for r in selected]})
    price_name=selected[0].get('name') if selected else None
    report['price_payload_included']=bool(price_name)
    before=find();emit('before',snapshot(before))
    assert len(before)<=1,'Existing duplicate test entries; refusing writes'
    raw=cml(price_name)
    # Match the encoding in the official downloadable request exactly.
    raw=raw.decode('utf-8').replace("encoding='utf-8'",'encoding="UTF-16"').encode('utf-16')
    if not before:
        response=send(raw);emit('create_response',response)
        if response.get('error'):
            report['creation']='rejected';emit('after_rejected_create',snapshot(find()));return
        first=wait_find()
    else:first=before
    emit('after_create',snapshot(first))
    if len(first)!=1:report['creation']='unknown_no_retry';return
    report['creation']='confirmed';report['test_id']=first[0].get('id')
    emit('price_readback',snapshot(find(True)))
    response=send(raw);emit('repeat_response',response)
    if response.get('error'):report['repeat']='rejected';return
    after=wait_find();emit('after_repeat',snapshot(after))
    report['same_id_after_repeat']=len(after)==1 and after[0].get('id')==first[0].get('id')
    if not report['same_id_after_repeat']:return
    response=send(cml(price_name,'13','37','1278.90'));emit('update_response',response)
    if response.get('error'):return
    emit('after_update',snapshot(wait_find()));emit('updated_price_readback',snapshot(find(True)))
try:main()
except Exception as err:report['error']=str(err)[:1400]
finally:emit('summary',report)
