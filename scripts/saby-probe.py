import json, os, urllib.parse, urllib.request
def load(req):
 with urllib.request.urlopen(req,timeout=60) as response:return json.load(response)
token=load(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST"))["token"]
query=urllib.parse.urlencode({"pointId":os.environ["SABY_POINT_ID"],"withBalance":"true","withBarcode":"true","pageSize":1000,"searchString":"X8999268"})
page=load(urllib.request.Request("https://api.sbis.ru/retail/v2/nomenclature/list?"+query,headers={"X-SBISAccessToken":token}))
rows=page.get("nomenclatures") or page.get("items") or page.get("result") or []
for row in rows:
 text=" ".join(str(row.get(k,"")) for k in ("id","hierarchicalId","code","article","name"))
 if "X8999268" in text:
  print(json.dumps(row,ensure_ascii=False,sort_keys=True))
  break
else: print("NOT_FOUND",len(rows))
