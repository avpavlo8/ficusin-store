import json, os, urllib.request
token=json.load(urllib.request.urlopen(urllib.request.Request("https://online.sbis.ru/oauth/service/",data=json.dumps({"app_client_id":os.environ["SABY_APP_CLIENT_ID"],"app_secret":os.environ["SABY_APP_SECRET"],"secret_key":os.environ["SABY_SECRET_KEY"]}).encode(),headers={"Content-Type":"application/json"},method="POST")))["token"]
os.makedirs("/tmp/saby-modules",exist_ok=True)
base="https://cdn.sbis.ru/static/resources/"
paths={
 "Types/serializer.min.js":"309ef622dae8e3d383b41db9e51c1630",
 "Types/entity.min.js":"309ef622dae8e3d383b41db9e51c1630",
 "Types/collection.min.js":"309ef622dae8e3d383b41db9e51c1630",
 "Types/source.min.js":"309ef622dae8e3d383b41db9e51c1630",
 "TransportCore/transport.min.js":"2f303808ce92ace6b813398a7370f0d0",\n "Browser/Transport.min.js":"1176a2de9445136329dd3209f17a9827",\n "BrowserTransport/Transport.min.js":"1176a2de9445136329dd3209f17a9827",\n "BrowserTransport/bundle.min.js":"1176a2de9445136329dd3209f17a9827",\n "BrowserTransport.min.js":"1176a2de9445136329dd3209f17a9827",
}
for path,h in paths.items():
    try:
        data=urllib.request.urlopen(urllib.request.Request(base+path+"?x_module="+h,headers={"X-SBISAccessToken":token}),timeout=60).read()
    except Exception as error:
        print("MISS",path,type(error).__name__);continue
    open("/tmp/saby-modules/"+path.replace("/","__"),"wb").write(data)
    print("OK",path,len(data))
