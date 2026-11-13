"""Read one known existing product. Never writes catalogue data or logs tokens."""
import json
import os
import re
import urllib.error
import urllib.request

def request(url, body, headers=None):
    content_type = "application/json-rpc;charset=utf-8" if "method" in body else "application/json;charset=utf-8"
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers={"Content-Type": content_type, "Accept":"application/json", **(headers or {})}, method="POST")
    try:
        response = urllib.request.urlopen(req, timeout=45)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        try:
            text = raw.decode(response.headers.get_content_charset() or "utf-8")
        except UnicodeDecodeError:
            text = raw.decode("cp1251")
        return json.loads(text)

def fields(record):
    schema = record.get("s", [])
    values = record.get("d", [])
    return {field.get("n"): values[i] for i, field in enumerate(schema) if i < len(values)}

token = request("https://online.sbis.ru/oauth/service/", {"app_client_id": os.environ["SABY_APP_CLIENT_ID"], "app_secret": os.environ["SABY_APP_SECRET"], "secret_key": os.environ["SABY_SECRET_KEY"]}).get("token")
if not token:
    raise SystemExit("No service token returned")
print("::add-mask::" + token)
result = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"Номенклатура.Прочитать", "params":{"ИдО":1101, "Расширение":None}, "id":1}, {"X-SBISAccessToken":token})
if result.get("error"):
    raise SystemExit("Read-only nomenclature RPC rejected: " + str(result["error"].get("code")))
record = result.get("result", {})
flat = fields(record)
if str(flat.get("@Номенклатура")) != "1101" or str(flat.get("НомНомер")) != "X269652448" or str(flat.get("Идентификатор")) != "0f56c2bd-2669-473f-9b94-875d521998a7":
    raise SystemExit("Identity mismatch; no metadata printed")
print("Identity verified for requested product")
print("Schema field names:", json.dumps(list(flat),ensure_ascii=False))
for name, value in flat.items():
    if re.search(r"categor|категор|folder|раздел|parent|иерарх|шаблон|template", name or "", re.I):
        if isinstance(value, (str, int, float, bool)) or value is None:
            print("Category/folder scalar:", json.dumps({"field":name,"value":value},ensure_ascii=False))
        elif isinstance(value, dict):
            print("Category/folder nested schema:", json.dumps({"field":name,"keys":list(fields(value)) if "s" in value else list(value)},ensure_ascii=False))
        elif isinstance(value, list):
            safe = value if len(value) <= 8 and all(v is None or isinstance(v,(str,int,float,bool)) for v in value) else {"items":len(value)}
            print("Category/folder identifier:", json.dumps({"field":name,"value":safe,"schema":next((s for s in record.get("s",[]) if s.get("n")==name),{})},ensure_ascii=False))

lookup = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"NomenclatureCategory.Read", "params":{"Id":1041,"AdditionalFields":["Name","Identifier"],"Option":None}, "id":2}, {"X-SBISAccessToken":token})
if lookup.get("error"):
    print("Category read rejected:",lookup["error"].get("code"),str(lookup["error"].get("message",""))[:600])
else:
    entity=fields(lookup.get("result",{}))
    print("Category response schema:",json.dumps(list(entity),ensure_ascii=False))
    if str(entity.get("Id")) == "1041":
        print("Verified category metadata:",json.dumps({"id":entity.get("Id"),"name":entity.get("Name"),"identifier":entity.get("Identifier")},ensure_ascii=False))
    else:
        print("Category identity not confirmed; no values printed")

lookup = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"Nomenclature.Read", "params":{"ИдО":3333,"ИмяМетода":None}, "id":3}, {"X-SBISAccessToken":token})
if lookup.get("error"):
    print("Folder read rejected:",lookup["error"].get("code"),str(lookup["error"].get("message",""))[:600])
else:
    entity=fields(lookup.get("result",{}))
    print("Folder response schema:",json.dumps(list(entity),ensure_ascii=False))
    if str(entity.get("Id")) == "3333":
        print("Verified folder metadata:",json.dumps({"id":entity.get("Id"),"name":entity.get("Name"),"isFolder":entity.get("IsFolder"),"isFolderUI":entity.get("IsFolderUI")},ensure_ascii=False))
    else:
        print("Folder identity not confirmed; no values printed")
