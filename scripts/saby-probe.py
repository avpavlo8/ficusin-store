import json, os, urllib.error, urllib.parse, urllib.request

def req(request):
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        body=error.read()
        try: return json.loads(body)
        except Exception: raise SystemExit("HTTP %d" % error.code)

def rpc(token, method, params):
    response=req(urllib.request.Request(
        "https://online.sbis.ru/service/?srv=1",
        data=json.dumps({"jsonrpc":"2.0","method":method,"params":params,"id":1},ensure_ascii=False).encode(),
        headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},
        method="POST"))
    if response.get("error"):
        error=response["error"]
        raise SystemExit(method+": "+str(error.get("details") or error.get("message")))
    return response.get("result")

def records(value, field):
    found=[]
    def walk(v):
        if isinstance(v,dict):
            schema=v.get("s") or []
            names=[x.get("n") for x in schema if isinstance(x,dict)]
            if field in v or field in names: found.append(v)
            if v.get("_type")=="recordset":
                for row in v.get("d") or []:
                    if isinstance(row,list): found.append({"_type":"record","s":schema,"d":row,"f":1})
            for k,x in v.items():
                if not (k=="d" and v.get("_type")=="recordset"): walk(x)
        elif isinstance(v,list):
            for x in v: walk(x)
    walk(value)
    return [x for x in found if get(x,field) is not None]

def get(rec,name):
    if name in rec:return rec[name]
    data=rec.get("d") or []
    for i,f in enumerate(rec.get("s") or []):
        if isinstance(f,dict) and f.get("n")==name:
            return data.get(name) if isinstance(data,dict) else (data[i] if i<len(data) else None)

def setf(rec,name,value):
    if name in rec:rec[name]=value;return True
    data=rec.get("d") or []
    for i,f in enumerate(rec.get("s") or []):
        if isinstance(f,dict) and f.get("n")==name:
            if isinstance(data,dict):data[name]=value
            else:data[i]=value
            return True
    return False

def record(values):
    return {"d":[v for v,t in values.values()],"s":[{"n":k,"t":t} for k,(v,t) in values.items()],"f":1}

def recordset(fields, rows):
    return {"s":[{"n":n,"t":t} for n,t in fields],
            "d":[[row.get(n) for n,t in fields] for row in rows]}

required=("SABY_APP_CLIENT_ID","SABY_APP_SECRET","SABY_SECRET_KEY","SABY_POINT_ID")
missing=[x for x in required if not os.environ.get(x)]
if missing:raise SystemExit("missing configuration: "+",".join(missing))
token=req(urllib.request.Request("https://online.sbis.ru/oauth/service/",
    data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),
    headers={"Content-Type":"application/json"},method="POST")).get("token")
if not token:raise SystemExit("no token")

catalog=req(urllib.request.Request("https://api.sbis.ru/retail/v2/nomenclature/list?"+urllib.parse.urlencode({
    "pointId":os.environ["SABY_POINT_ID"],"searchString":"Диффенбахия Reflector","pageSize":25,"page":0}),
    headers={"X-SBISAccessToken":token}))
products=[x for x in (catalog.get("nomenclatures") or catalog.get("items") or []) if not x.get("isParent")]
if not products:raise SystemExit("test product not found")
nom_id=int(products[0]["id"])

created=rpc(token,"РеалВх.Создать",{"Фильтр":{"ВызовИзБраузера":True},"ИмяМетода":"РеалВх.Список"})
docs=records(created,"@Документ")
if not docs:raise SystemExit("РеалВх.Создать did not return document")
doc=docs[0]
setf(doc,"Примечание","Тест поступления API Ficusin Store — не проводить")
written=rpc(token,"РеалВх.Записать",{"Запись":doc})
saved=records(written,"@Документ")
if saved:doc=saved[0]
doc_id=int(get(doc,"@Документ") or 0)
if not doc_id:raise SystemExit("РеалВх.Записать did not return id")

batch=rpc(token,"РеалВх.NomCreateWithSaveBatch",{
    "doc_rec":record({"@Документ":(doc_id,"Число целое")}),
    "rs":recordset([("Номенклатура","Число целое"),("КодЕГАИС","Строка"),("Количество","Число вещественное"),("Раздел","Число целое")],
                   [{"Номенклатура":nom_id,"КодЕГАИС":None,"Количество":1.0,"Раздел":None}]),
    "actions":record({"changed_document":(True,"Логическое")})})
positions=records(batch,"Номенклатура")
print("RECEIPT_PROBE_OK document_id=%d returned_positions=%d" % (doc_id,len(positions)))
