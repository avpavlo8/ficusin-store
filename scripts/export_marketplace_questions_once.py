#!/usr/bin/env python3
"""One-shot export for catalogue research. Never stores API credentials."""
import json, os, sys, urllib.parse, urllib.request, urllib.error

OUT = {"wildberries": [], "ozon": [], "errors": []}

def request(url, *, headers, payload=None):
    body = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url, data=body, headers=headers,
                                 method="GET" if body is None else "POST")
    with urllib.request.urlopen(req, timeout=45) as response:
        return json.load(response)

def wb():
    token = os.getenv("WB_API_TOKEN", "").strip()
    if not token:
        OUT["errors"].append("WB_API_TOKEN is not configured in GitHub Actions")
        return
    for answered in (False, True):
        skip = 0
        while True:
            query = urllib.parse.urlencode({"isAnswered": str(answered).lower(), "take": 5000,
                                            "skip": skip, "order": "dateDesc"})
            data = request("https://feedbacks-api.wildberries.ru/api/v1/questions?" + query,
                           headers={"Authorization": token})
            questions = data.get("data", {}).get("questions", [])
            for item in questions:
                product = item.get("productDetails") or {}
                answer = item.get("answer") or {}
                OUT["wildberries"].append({"text": item.get("text", ""),
                    "answer": answer.get("text", ""), "createdDate": item.get("createdDate", ""),
                    "nmId": product.get("nmId"), "productName": product.get("productName", "")})
            if len(questions) < 5000: break
            skip += len(questions)

def ozon():
    client = os.getenv("OZON_CLIENT_ID", "").strip()
    key = os.getenv("OZON_API_KEY", "").strip()
    if not client or not key:
        OUT["errors"].append("OZON_CLIENT_ID/OZON_API_KEY are not configured in GitHub Actions")
        return
    headers = {"Client-Id": client, "Api-Key": key, "Content-Type": "application/json"}
    last_id = ""
    for _ in range(100):
        data = request("https://api-seller.ozon.ru/v1/question/list", headers=headers,
                       payload={"filter": {"status": "ALL"}, "last_id": last_id, "limit": 100})
        result = data.get("result") or data
        questions = result.get("questions") or result.get("items") or []
        for item in questions:
            OUT["ozon"].append({"text": item.get("text") or item.get("question_text") or "",
                "answer": item.get("answer_text") or "", "createdDate": item.get("created_at") or "",
                "sku": item.get("sku"), "productName": item.get("product_name") or ""})
        new_last = result.get("last_id") or ""
        if not questions or not new_last or new_last == last_id: break
        last_id = new_last

for name, loader in (("wildberries", wb), ("ozon", ozon)):
    try: loader()
    except urllib.error.HTTPError as error:
        OUT["errors"].append(f"{name}: HTTP {error.code}: {error.read().decode(errors='replace')[:500]}")
    except Exception as error:
        OUT["errors"].append(f"{name}: {type(error).__name__}: {error}")

with open("marketplace-questions.json", "w", encoding="utf-8") as file:
    json.dump(OUT, file, ensure_ascii=False, indent=2)
print(json.dumps({"wildberries": len(OUT["wildberries"]), "ozon": len(OUT["ozon"]),
                  "errors": OUT["errors"]}, ensure_ascii=False))
if OUT["errors"] and not (OUT["wildberries"] or OUT["ozon"]): sys.exit(2)
