"""Read-only preflight for the owner-approved disposable Saby fixture.

No orders, payments, documents, settings or balances are changed.
Only fixture values and capability counts are printed to public CI logs.
"""

import json
import os
from datetime import datetime
from zoneinfo import ZoneInfo
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
test_bank_type = None
for product in ("retail", "delivery"):
    result = get("/retail/point/list", pointId=point_id, product=product, withPrices="true")
    points = result.get("salesPoints") or []
    print(product + "_point_result_fields", sorted(result))
    print(product + "_point_result_count", len(points))
    matched = [p for p in points if str(p.get("id")) == str(point_id)]
    print(product + "_point_found", len(matched) == 1)
    if len(matched) == 1:
        print(product + "_payment_kind_count", len(matched[0].get("paymentKinds") or []))
        for kind in matched[0].get("paymentKinds") or []:
            if kind.get("name") == "ТЕСТ API Фикусин — внешняя оплата":
                test_bank_type = kind.get("id")
                print("fixture_bank_type", test_bank_type)
        print(product + "_point_fields", sorted(matched[0]))
        delivery_settings = matched[0].get("deliverySettings")
        print(product + "_delivery_settings_type", type(delivery_settings).__name__)
        if isinstance(delivery_settings, dict):
            print(product + "_delivery_settings_fields", sorted(delivery_settings))
            print(product + "_delivery_settings_flags", {k: v for k, v in delivery_settings.items() if isinstance(v, bool)})

prices = get("/retail/nomenclature/price-list", pointId=point_id,
             actualDate=datetime.now(ZoneInfo("Europe/Moscow")).strftime("%Y-%m-%d"), page=0, pageSize=100)
print("price_list_result_fields", sorted(prices) if isinstance(prices, dict) else "list")
price_rows = prices if isinstance(prices, list) else next((v for v in prices.values() if isinstance(v, list)), [])
test_price_id = None
for price in price_rows:
    if str(price.get("id")) == "6":
        print("price_list_6_name", price.get("name"))
    if price.get("name") == "ТЕСТ API Фикусин 26.10.2026":
        test_price_id = price.get("id")
        print("fixture_price_list_id", test_price_id)

catalogue_fixture = None
for price_list in dict.fromkeys((None, 6, test_price_id)):
    params = dict(pointId=point_id, searchString=CODE, withBalance="true", page=0, pageSize=50)
    if price_list:
        params["priceListId"] = price_list
    result = get("/retail/v2/nomenclature/list", **params)
    rows = result.get("nomenclatures") or result.get("items") or []
    if not isinstance(rows, list):
        print("Unexpected catalogue response shape")
        continue
    fixtures = [r for r in rows if r.get("name") == NAME and r.get("nomNumber") == CODE and not r.get("isParent")]
    print("fixture_in_" + ("price_list_" + str(price_list) if price_list else "catalogue"), len(fixtures) == 1)
    for row in fixtures:
        if price_list is None:
            catalogue_fixture = row
        print("fixture_balance", row.get("balance"))
        print("fixture_fields", sorted(row))

if os.environ.get("SABY_FIXTURE_RECONCILE") == "true":
    # Fixed owner-authorized fixture order only. Never a write or a payment retry.
    order_id = "01a177ee-1ac4-780f-82d7-045b9fdfba13"
    state = get("/retail/order/" + order_id + "/state")
    print("fixture_order_state", json.dumps(state, ensure_ascii=False))
    order = get("/retail/order/" + order_id)
    print("fixture_order_readback_fields", sorted(order))
    print("fixture_order_number", order.get("number"), order.get("orderNumber"))
    print("fixture_order_position_fields", [sorted(r) for r in order.get("nomenclatures") or []])
