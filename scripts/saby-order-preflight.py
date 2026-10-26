"""Read-only preflight for the owner-approved disposable Saby fixture.

No orders, payments, documents, settings or balances are changed.
Only fixture values and capability counts are printed to public CI logs.
"""

import json
import os
import urllib.error
import urllib.parse
import urllib.request

CODE = "X380416519"
NAME = "ТЕСТ API Фикусин — резерв и отмена 26.10.2026"


def request(url, token=None, data=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-SBISAccessToken"] = token
    req = urllib.request.Request(
        url, data=json.dumps(data).encode() if data is not None else None,
        headers=headers, method="POST" if data is not None else "GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=45) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        # Do not expose vendor response bodies or request headers in CI.
        print("endpoint", urllib.parse.urlparse(url).path, "HTTP", error.code)
        return {}


token = request("https://online.sbis.ru/oauth/service/", data={
    "app_client_id": os.environ["SABY_APP_CLIENT_ID"],
    "app_secret": os.environ["SABY_APP_SECRET"],
    "secret_key": os.environ["SABY_SECRET_KEY"],
}).get("token")
if not token:
    raise SystemExit("Service authentication failed")
print("service_auth_ok", True)


def get(path, **params):
    return request("https://api.sbis.ru" + path + "?" + urllib.parse.urlencode(params), token)


point_id = int(os.environ.get("SABY_POINT_ID") or 278)
for product in ("retail", "delivery"):
    result = get("/retail/point/list", pointId=point_id, product=product, withPrices="true")
    points = result.get("salesPoints") or []
    matched = [p for p in points if p.get("id") == point_id]
    print(product + "_point_found", len(matched) == 1)
    if len(matched) == 1:
        print(product + "_payment_kind_count", len(matched[0].get("paymentKinds") or []))
        print(product + "_point_fields", sorted(matched[0]))

for price_list in (None, 6):
    params = dict(pointId=point_id, searchString=CODE, withBalance="true", page=0, pageSize=50)
    if price_list:
        params["priceListId"] = price_list
    result = get("/retail/v2/nomenclature/list", **params)
    rows = result.get("nomenclatures") or result.get("items") or []
    if not isinstance(rows, list):
        print("Unexpected catalogue response shape")
        continue
    fixtures = [r for r in rows if r.get("name") == NAME and r.get("code") == CODE and not r.get("isParent")]
    print("fixture_in_" + ("price_list_6" if price_list else "catalogue"), len(fixtures) == 1)
    for row in fixtures:
        print("fixture_balance", row.get("balance"))
        print("fixture_fields", sorted(row))
