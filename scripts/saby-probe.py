import json, os, re, urllib.error, urllib.request
token=json.load(urllib.request.urlopen(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST")))["token"]
os.makedirs("/tmp/saby-transport",exist_ok=True)
hashes={"Browser":"0be9805ed98858df248b6a2dfb76ba78","BrowserTransport":"1176a2de9445136329dd3209f17a9827"}
patterns=[
 "static/resources/{m}/{m}.min.js","static/resources/{m}.min.js","static/resources/{m}-min.js",
 "static/resources/{m}/library.min.js","static/resources/{m}/bundle.min.js","static/resources/{m}/transport.min.js",
 "static/resources/{m}/Transport.min.js","static/{m}/{m}.min.js","static/{m}.min.js",
 "cdn/{m}/{m}.min.js","cdn/{m}.min.js",
]
hosts=["https://cdn.sbis.ru/","https://online.sbis.ru/"]
for module,h in hashes.items():
 for host in hosts:
  for pattern in patterns:
   url=host+pattern.format(m=module)+"?x_module="+h
   try:
    data=urllib.request.urlopen(urllib.request.Request(url,headers={"X-SBISAccessToken":token}),timeout=20).read()
   except Exception:
    continue
   name=re.sub(r"[^A-Za-z0-9_.-]","_",url.split("?")[0])
   open("/tmp/saby-transport/"+name,"wb").write(data)
   print("OK",url,len(data))
for url in ["https://online.sbis.ru/page/purchases?org=g-1","https://online.sbis.ru/"]:
 try:
  data=urllib.request.urlopen(urllib.request.Request(url,headers={"X-SBISAccessToken":token}),timeout=30).read()
 except Exception as error:
  print("PAGE_MISS",url,type(error).__name__); continue
 open("/tmp/saby-transport/page.html","wb").write(data)
 print("PAGE_OK",url,len(data))
 for match in sorted(set(re.findall(rb'[^"\' ]*BrowserTransport[^"\' ]*',data))):
  print("REF",match[:500].decode("utf-8","replace"))
