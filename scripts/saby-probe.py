import json, os, urllib.error, urllib.parse, urllib.request

def req(request):
    try:
        with urllib.request.urlopen(request,timeout=60) as response:return json.load(response)
    except urllib.error.HTTPError as error:return json.loads(error.read())
def rpc(token,method,params):
    result=req(urllib.request.Request("https://online.sbis.ru/service/?srv=1",data=json.dumps({"jsonrpc":"2.0","method":method,"params":params,"id":1},ensure_ascii=False).encode(),headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},method="POST"))
    if result.get("error"):raise SystemExit(method+": "+str(result["error"].get("details") or result["error"]))
    return result.get("result")
def rec(values,types):
    return {"_type":"record","d":[values[k] for k in types],"s":[{"n":k,"t":types[k]} for k in types],"f":1}
def rs(rows,types):
    return {"_type":"recordset","d":[[row.get(k) for k in types] for row in rows],"s":[{"n":k,"t":types[k]} for k in types]}
def records(value,field):
    out=[]
    def walk(v):
        if isinstance(v,dict):
            schema=v.get("s") or []; names=[f.get("n") for f in schema if isinstance(f,dict)]
            if field in v or field in names:out.append(v)
            if v.get("_type")=="recordset":
                for row in v.get("d") or []:
                    if isinstance(row,list):out.append({"_type":"record","s":schema,"d":row,"f":1})
            for k,c in v.items():
                if not(k=="d" and v.get("_type")=="recordset"):walk(c)
        elif isinstance(v,list):
            for c in v:walk(c)
    walk(value);return out
def get(r,name):
    if name in r:return r[name]
    for i,f in enumerate(r.get("s") or []):
        if f.get("n")==name:
            d=r.get("d") or [];return d[i] if i<len(d) else None
def setf(r,name,value):
    if name in r:r[name]=value;return
    for i,f in enumerate(r.get("s") or []):
        if f.get("n")==name:r["d"][i]=value;return

token=req(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST")).get("token")
catalog=req(urllib.request.Request("https://api.sbis.ru/retail/v2/nomenclature/list?"+urllib.parse.urlencode({"pointId":os.environ["SABY_POINT_ID"],"searchString":"Диффенбахия Reflector","pageSize":25,"page":0}),headers={"X-SBISAccessToken":token}))
products=[x for x in (catalog.get("nomenclatures") or catalog.get("items") or []) if not x.get("isParent")]
if not products:raise SystemExit("product not found")
nom_id=int(products[0]["id"])
copied=rpc(token,"РеалВх.Копировать",{"ИдО":"38766","ИмяМетода":"РеалВх.Список"})
docs=records(copied,"@Документ")
if not docs:raise SystemExit("copy returned no document")
doc=docs[0]
doc_id=int(get(doc,"@Документ") or 0)
if not doc_id:raise SystemExit("copy returned no id")
added=rpc(token,"РеалВх.NomCreateWithSaveBatch",{
 "doc_rec":rec({"@Документ":doc_id},{"@Документ":"Число целое"}),
 "rs":rs([{"Номенклатура":nom_id,"КодЕГАИС":None,"Количество":1.0,"Раздел":None}],{"Номенклатура":"Число целое","КодЕГАИС":"Строка","Количество":"Число вещественное","Раздел":"Число целое"}),
 "actions":rec({"changed_document":True},{"changed_document":"Логическое"})})
print("RECEIPT_COPY_OK document_id=%d positions_in_response=%d"%(doc_id,len(records(added,"Номенклатура"))))
