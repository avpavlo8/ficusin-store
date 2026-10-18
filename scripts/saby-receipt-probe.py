"""Read an existing receipt; log field names, types and counts, never values.

Only two fixed read methods are permitted. No document creation, row writes,
posting, credentials, personal data or attachment URLs are emitted.
"""
import json
import os
import urllib.request
import urllib.error
import uuid
import re


def request(url, payload, token=None):
    headers = {"Content-Type": "application/json-rpc;charset=utf-8"}
    if token:
        headers["X-SBISAccessToken"] = token
    req = urllib.request.Request(url, data=json.dumps(payload).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return {"httpStatus": error.code}


def shape(value, depth=0):
    if depth > 12:
        return type(value).__name__
    if isinstance(value, dict):
        if isinstance(value.get("s"), list) and isinstance(value.get("d"), list):
            fields, data = value["s"], value["d"]
            recordset = value.get("_type") == "recordset" or bool(data and isinstance(data[0], list))
            sample = data[0] if recordset and data else data if not recordset else []
            return {"protocol": value.get("_type"), "rowCount": len(data) if recordset else 1,
                    "fields": [{"name": f.get("n"), "type": f.get("t"),
                                "valueShape": shape(sample[i], depth+1) if i < len(sample) else "absent"}
                               for i, f in enumerate(fields)],
                    "other": {k: shape(v, depth+1) for k,v in value.items() if k not in ("d","s","_type")}}
        return {k: shape(v, depth+1) for k,v in value.items()}
    if isinstance(value, list):
        return {"count": len(value), "first": shape(value[0], depth+1) if value else None}
    return type(value).__name__


receipt_id = int(os.environ["SABY_RECEIPT_ID"])
assert receipt_id > 0
guid = str(uuid.UUID(os.environ["SABY_RECEIPT_GUID"]))
auth = request("https://online.sbis.ru/oauth/service/", {
    "app_client_id": os.environ["SABY_APP_CLIENT_ID"],
    "app_secret": os.environ["SABY_APP_SECRET"],
    "secret_key": os.environ["SABY_SECRET_KEY"],
})
token = auth["token"]
for method, params in (
    ("ДокОтгрВх.Прочитать", {"ИдО": receipt_id, "ИмяМетода": "ДокОтгрВх.Список"}),
    ("СБИС.ПрочитатьДокумент", {"Документ": {"Идентификатор": guid}}),
    ("РеалВх.DocNomList", {
        "Фильтр": {"_type":"record", "d":[receipt_id, 0, False, None], "s":[{"n":"СкладскойДокумент","t":"Число целое"},{"n":"Тип","t":"Число целое"},{"n":"ЭтоРасходы","t":"Логическое"},{"n":"Раздел","t":"Число целое"}]},
        "Сортировка": None,
        "Навигация": {"_type":"record", "d":[True,0,1000], "s":[{"n":"ЕстьЕще","t":"Логическое"},{"n":"Страница","t":"Число целое"},{"n":"РазмерСтраницы","t":"Число целое"}]},
        "ДопПоля": []}),
):
    result = request("https://online.sbis.ru/service/?srv=1", {
        "jsonrpc": "2.0", "protocol": 6, "id": 1, "method": method, "params": params,
    }, token)
    print(method)
    print("httpStatus:",result.get("httpStatus"))
    print(json.dumps(shape(result), ensure_ascii=False, indent=2))
    if "error" in result:
        error = result["error"]
        msg = str(error.get("message", ""))
        print("errorCategories:", {word: word in msg.lower() for word in ("найден", "доступ", "аргумент", "параметр", "метод", "документ")})
        # Runtime/signature errors only. Never log a service-supplied document,
        # a request body or arbitrary data from the exception details.
        if any(term in msg.lower() for term in ("метод", "аргумент", "параметр")):
            msg = re.sub(r"[0-9]+(?:[-.][0-9a-z]+)*", "#", msg, flags=re.I)
            for secret in (token, os.environ["SABY_APP_SECRET"], os.environ["SABY_SECRET_KEY"]):
                msg = msg.replace(secret, "[redacted]")
            print("signatureError:", msg[:600])
    if method == "ДокОтгрВх.Прочитать" and "result" in result:
        record = result["result"]
        fields, data = record.get("s",[]), record.get("d",[])
        for i, field in enumerate(fields):
            if field.get("n") == "ДокументРасширение.Состояние":
                print("postedBit10:", data[i][10] if isinstance(data[i], list) and len(data[i]) > 10 else None)
