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
 return response.get("result")
doc=rpc("РеалВх.Создать",{"Фильтр":{"_type":"record","d":[224],"s":[{"n":"ТипДокумента","t":"Число целое"}]},"ИмяМетода":"РеалВх.Список"})
print("CREATED", isinstance(doc,dict), sorted(doc)[:5] if isinstance(doc,dict) else "")\nraise SystemExit(0)\nfields=doc.get("s") or []; data=doc.get("d") or []
for i,field in enumerate(fields):
 name=field.get("n","")
 if any(word in name for word in ("Тип","Регламент","Документ","Склад","Контрагент","Лицо")):
  value=data[i] if i<len(data) else None
  print(name,field.get("t"),json.dumps(value,ensure_ascii=False)[:500])
