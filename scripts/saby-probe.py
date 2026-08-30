"""One-off live verification of the production warehouse receipt contract."""
import json, os, urllib.parse, urllib.request

def request_json(request):
    with urllib.request.urlopen(request, timeout=60) as response: return json.load(response)

token = request_json(urllib.request.Request("https://online.sbis.ru/oauth/service/", data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(), headers={"Content-Type":"application/json"}, method="POST"))["token"]

def rpc(method, params):
    response = request_json(urllib.request.Request("https://online.sbis.ru/service/?srv=1", data=json.dumps({"jsonrpc":"2.0","protocol":6,"method":method,"params":params,"id":1}, ensure_ascii=False).encode(), headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token}, method="POST"))
    if response.get("error"): raise RuntimeError(response["error"].get("details") or response["error"].get("message"))
    return response.get("result")

def record(values):
    types={bool:"Логическое",int:"Число целое",float:"Число вещественное"}
    return {"_type":"record","d":list(values.values()),"s":[{"n":name,"t":types.get(type(value),"Строка")} for name,value in values.items()]}

def field(item, name):
    for index,spec in enumerate(item.get("s",[])):
        if spec.get("n")==name: return item.get("d",[])[index]
    return item.get(name)

def catalogue_product(code):
    query=urllib.parse.urlencode({"pointId":os.environ["SABY_POINT_ID"],"searchString":code,"pageSize":50,"page":0})
    result=request_json(urllib.request.Request("https://api.sbis.ru/retail/v2/nomenclature/list?"+query,headers={"X-SBISAccessToken":token}))
    items=result.get("nomenclatures") or result.get("items") or []
    matches=[item for item in items if not item.get("isParent") and item.get("nomNumber")==code]
    if len(matches)!=1: raise RuntimeError(f"{code}: expected one catalogue product, got {len(matches)}")
    return int(matches[0]["id"]),matches[0].get("name",code)

requested=[("X941618637",3.0),("X450485643",2.0),("X8999268",40.0),("X2413914",20.0)]
resolved=[(catalogue_product(code),quantity,code) for code,quantity in requested]
document=rpc("РеалВх.Создать",{"Фильтр":record({"ТипДокумента":224}),"ИмяМетода":"РеалВх.Список"})
document["_type"]="record"
for index,spec in enumerate(document["s"]):
    if spec.get("n")=="Примечание": document["d"][index]="Ficusin Store, контроль поступления после исправления"
schema=[{"n":"Номенклатура","t":"Число целое"},{"n":"КодЕГАИС","t":"Строка"},{"n":"Количество","t":"Число вещественное"},{"n":"Раздел","t":"Строка"}]
rows=[[product_id,"",quantity,None] for ((product_id,_),quantity,_) in resolved]
added=rpc("РеалВх.NomCreateWithSaveBatch",{"doc_rec":document,"rs":{"_type":"recordset","d":rows,"s":schema},"actions":record({"changed_document":True})})
if not isinstance(added,list) or len(added)!=len(rows): raise RuntimeError(f"Saby returned {len(added) if isinstance(added,list) else 0} of {len(rows)} rows")
guid=str(field(document,"ИдентификаторДокумента") or "").strip()
if not guid: raise RuntimeError("Saby did not return a document GUID")
print("DOCUMENT_GUID="+guid)
print("DOCUMENT_URL=https://ret.saby.ru/opendoc.html?guid="+urllib.parse.quote(guid)+"&f3=259&client=43033516")
print("POSITIONS_ADDED="+str(len(added)))
for ((product_id,name),quantity,code) in resolved: print(f"POSITION={product_id}|{code}|{quantity:g}|{name}")
