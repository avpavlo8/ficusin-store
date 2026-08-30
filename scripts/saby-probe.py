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

guid="01a05168-f67a-7c55-8cd3-bdb6026b0e18"
result=rpc("ДокОтгрВх.Прочитать",{"ИдО":guid,"ИмяМетода":"ДокОтгрВх.Список"})
expected={3604,3605,2971,2542}
found=set()
def walk(value):
    if isinstance(value,dict):
        for child in value.values(): walk(child)
    elif isinstance(value,list):
        for child in value: walk(child)
    elif isinstance(value,(int,float)) and int(value) in expected: found.add(int(value))
walk(result)
print("DOCUMENT_GUID="+guid)
print("READ_RESULT_TYPE="+type(result).__name__)
print("NOMENCLATURE_IDS_FOUND="+",".join(map(str,sorted(found))))
