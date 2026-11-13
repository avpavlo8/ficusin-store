"""Read one known existing product. Never writes catalogue data or logs tokens."""
import json
import os
import re
import urllib.request

def request(url, body, headers=None):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers={"Content-Type": "application/json", **(headers or {})}, method="POST")
    with urllib.request.urlopen(req, timeout=45) as response:
        return json.load(response)

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
