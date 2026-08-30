import json, os, urllib.error, urllib.request
def request(req):
 try:
  with urllib.request.urlopen(req,timeout=60) as response:return json.load(response)
 except urllib.error.HTTPError as error:return json.loads(error.read())
token=request(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST"))["token"]
body={"jsonrpc":"2.0","protocol":6,"method":"РеалВх.Создать","params":{"Фильтр":{"_type":"record","d":[224],"s":[{"n":"ТипДокумента","t":"Число целое"}]},"ИмяМетода":"РеалВх.Список"},"id":1}
response=request(urllib.request.Request("https://online.sbis.ru/service/?srv=1",data=json.dumps(body,ensure_ascii=False).encode(),headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},method="POST"))
if response.get("error"):
 print("ERROR",response["error"].get("details") or response["error"].get("message"));raise SystemExit(1)
doc=response.get("result")
print("CREATED",isinstance(doc,dict),sorted(doc)[:5] if isinstance(doc,dict) else "")
