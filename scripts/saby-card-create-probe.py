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
    return E.tostring(root,encoding='utf-16',xml_declaration=True)
def send(raw):
    return request('https://online.sbis.ru/service/?srv=1',{'jsonrpc':'2.0','protocol':3,'method':'СБИС.ИмпортНоменклатурыИзCML','params':{'Параметр':{'Файл':base64.b64encode(raw).decode()}},'id':1})
def send_flat(raw):
    return request('https://online.sbis.ru/service/?srv=1',{'jsonrpc':'2.0','protocol':3,'method':'СБИС.ИмпортНоменклатурыИзCML','params':{'Файл':base64.b64encode(raw).decode()},'id':1})
def main():
    global token
    assert os.environ.get('GITHUB_REF_NAME')=='probe/saby-card-api-20261031' and os.environ.get('SABY_PROBE_SEARCH')==MARKER
    assert os.environ.get('SABY_POINT_ID')=='278'
    token=request('https://online.sbis.ru/oauth/service/',{'app_client_id':os.environ['SABY_APP_CLIENT_ID'],'app_secret':os.environ['SABY_APP_SECRET'],'secret_key':os.environ['SABY_SECRET_KEY']},False).get('token','')
    assert token
    print('::add-mask::'+token,flush=True);emit('service_auth','ok')
    before=find();emit('before_full_test_row',before)
    assert len(before)==1 and before[0].get('id')==3952,'Known isolated card required'
    root=E.fromstring(cml('ТЕСТ API Фикусин — НЕ ПРОДАВАТЬ','14','38','1299.90'))
    for path in ['./ПакетПредложений/ТипыЦен/ТипЦены/Ид','./ПакетПредложений/Предложения/Предложение/Цены/Цена/ИдТипаЦены']:
        root.find(path).text=uid('isolated-price')
    raw=('<?xml version="1.0" encoding="UTF-16"?>\r\n'+E.tostring(root,encoding='unicode')).encode('utf-16')
    emit('roundtrip_expected',{'id':3952,'name':NAME,'pot':'14','height':'38','price':1299.90})
    emit('roundtrip_write_response',send(raw))
    time.sleep(3)
    emit('roundtrip_repeat_response',send(raw))
    final=[]
    for attempt in range(20):
        final=find()
        emit('roundtrip_readback_'+str(attempt),snapshot(final))
        if len(final)>1:break
        if len(final)==1:
            item=final[0];attrs=item.get('attributes',{})
            if item.get('id')==3952 and attrs.get('Высота')=='38' and attrs.get('Диаметр горшка')=='14' and abs((item.get('cost') or 0)-1299.90)<0.001 and item.get('name')==NAME:break
        time.sleep(4)
    emit('final_full_test_row',final)
    report['final_test_count']=len(final)
    report['same_id_after_repeat']=len(final)==1 and final[0].get('id')==3952
    report['attributes_updated']=report['same_id_after_repeat'] and final[0].get('attributes',{}).get('Высота')=='38' and final[0].get('attributes',{}).get('Диаметр горшка')=='14'
    report['price_updated']=report['same_id_after_repeat'] and abs((final[0].get('cost') or 0)-1299.90)<0.001

try:main()
except Exception as err:report['error']=str(err)[:1400]
finally:emit('summary',report)
