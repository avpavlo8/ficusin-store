import json, os, urllib.error, urllib.request

def req(request):
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return json.loads(error.read())

def rpc(token, method, params):
    response=req(urllib.request.Request("https://online.sbis.ru/service/?srv=1",
        data=json.dumps({"jsonrpc":"2.0","method":method,"params":params,"id":1},ensure_ascii=False).encode(),
        headers={"Content-Type":"application/json-rpc; charset=utf-8","X-SBISAccessToken":token},method="POST"))
    if response.get("error"): raise SystemExit(str(response["error"].get("details") or response["error"]))
    return response.get("result")

token=req(urllib.request.Request("https://online.sbis.ru/oauth/service/",
    data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),
    headers={"Content-Type":"application/json"},method="POST")).get("token")
result=rpc(token,"СБИС.ПрочитатьДокумент",{"Документ":{"Идентификатор":"b3e70b39-6c41-4ea1-8acd-e63afd5b0ee5"},"ДопПоля":"ДополнительныеПоля,Расширение"})
def walk(v,path=""):
    if isinstance(v,dict):
        for k,x in v.items():
            p=path+"."+k if path else k
            if isinstance(x,(int,float)) or (isinstance(x,str) and x.isdigit()):
                print(p,":",x)
            elif isinstance(x,(dict,list)): walk(x,p)
    elif isinstance(v,list):
        for i,x in enumerate(v): walk(x,path+"[]")
walk(result)
