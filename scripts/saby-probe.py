import json, os, urllib.error, urllib.request

def request(req):
    try:
        with urllib.request.urlopen(req,timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return json.loads(error.read())

token=request(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST"))["token"]

def rpc(method,params):
    result=request(urllib.request.Request("https://online.sbis.ru/service/?srv=1",data=json.dumps({"jsonrpc":"2.0","method":method,"params":params,"id":1},ensure_ascii=False).encode(),headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},method="POST"))
    if result.get("error"):
        print(method,"ERROR",result["error"].get("details") or result["error"].get("message"))
        raise SystemExit(1)
    print(method,"OK")
    return result.get("result")

def rec(values, types=None):
    types=types or {}
    return {"_type":"record","d":list(values.values()),"s":[{"n":name,"t":types.get(name, "Логическое" if isinstance(value,bool) else "Число целое" if isinstance(value,int) else "Строка")} for name,value in values.items()]}

copied=rpc("РеалВх.Копировать",{"ИдО":"38766","ИмяМетода":"РеалВх.Список"})
doc=copied
if isinstance(copied,dict) and copied.get("_type")!="record":
    for value in copied.values():
        if isinstance(value,dict) and value.get("_type")=="record":
            doc=value;break
if not isinstance(doc,dict) or doc.get("_type")!="record":
    print("COPY_NO_RECORD",type(copied).__name__, sorted(copied)[:15] if isinstance(copied,dict) else "")
    raise SystemExit(1)

row=rec({"Номенклатура":"X8999268","КодЕГАИС":"","Количество":1,"Раздел":None},{"Номенклатура":"Строка","КодЕГАИС":"Строка","Количество":"Число вещественное","Раздел":"Строка"})
rows={"_type":"recordset","d":[row["d"]],"s":row["s"]}
actions=rec({"changed_document":True})
result=rpc("РеалВх.NomCreateWithSaveBatch",{"doc_rec":doc,"rs":rows,"actions":actions})
print("BATCH_RESULT",type(result).__name__, sorted(result)[:15] if isinstance(result,dict) else "")
