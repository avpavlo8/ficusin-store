import json, os, urllib.error, urllib.request
def request(req):
 try:
  with urllib.request.urlopen(req,timeout=60) as response:return json.load(response)
 except urllib.error.HTTPError as error:return json.loads(error.read())
token=request(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST"))["token"]
def rpc(method,params):
 response=request(urllib.request.Request("https://online.sbis.ru/service/?srv=1",data=json.dumps({"jsonrpc":"2.0","protocol":6,"method":method,"params":params,"id":1},ensure_ascii=False).encode(),headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},method="POST"))
 if response.get("error"):
  print(method,"ERROR",response["error"].get("details") or response["error"].get("message"));raise SystemExit(1)
 print(method,"OK");return response.get("result")
def rec(values,types=None):
 types=types or {}
 return {"_type":"record","d":list(values.values()),"s":[{"n":name,"t":types.get(name,"Логическое" if isinstance(value,bool) else "Число целое" if isinstance(value,int) else "Строка")} for name,value in values.items()]}
doc=rpc("РеалВх.Копировать",{"ИдО":"38766","ИмяМетода":"РеалВх.Список"})
if isinstance(doc,dict) and "d" in doc and "s" in doc:doc["_type"]="record"
row=rec({"Номенклатура":"X8999268","КодЕГАИС":"","Количество":1.0,"Раздел":None},{"Номенклатура":"Строка","КодЕГАИС":"Строка","Количество":"Число вещественное","Раздел":"Строка"})
rows={"_type":"recordset","d":[row["d"]],"s":row["s"]}
actions=rec({"changed_document":True})
result=rpc("РеалВх.NomCreateWithSaveBatch",{"doc_rec":doc,"rs":rows,"actions":actions})
print("RESULT",type(result).__name__,sorted(result)[:12] if isinstance(result,dict) else "")
