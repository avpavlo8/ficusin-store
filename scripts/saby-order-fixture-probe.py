"""One owner-authorized create/cancel test, exclusively for fixture X380416519.

An explicit test mode may register a simulated external payment with nonFiscal=true.
Never calls a payment link or a fiscal payment operation.
Never retries a mutation. An unknown create result requires manual reconciliation.
"""

import hashlib
import json
import os
from pathlib import Path
import runpy
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

if os.environ.get("GITHUB_RUN_ATTEMPT", "1") != "1":
    raise SystemExit("Do not rerun a mutation job; reconcile the first attempt")

env = runpy.run_path(str(Path(__file__).with_name("saby-order-preflight.py")))
fixture = env["catalogue_fixture"]
assert fixture and fixture["nomNumber"] == "X380416519"
assert fixture["name"] == "ТЕСТ API Фикусин — резерв и отмена 26.10.2026"
assert fixture["balance"] == 1, "Expected exactly one disposable fixture unit"
assert str(env["test_price_id"]) == "19", "Expected the isolated fixture price list"

report_path = Path("fixture-order-result.json")
report = {"fixture": "X380416519", "priceListId": 19, "balanceBefore": fixture["balance"], "paymentRegistered": False}
payment_test = os.environ.get("SABY_FIXTURE_NONFISCAL_TEST") == "true"
report["nonFiscalPaymentTest"] = payment_test
if payment_test:
    assert env["test_bank_type"], "Explicit fixture-only custom bank type must be configured first"


def save():
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2))


def safe_text(value):
    text = str(value)
    for secret in (env["token"], *(os.environ.get(k) for k in ("SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY"))):
        if secret:
            text = text.replace(secret, "[redacted]")
    return text[:1500]


def call(method, path, payload=None):
    req = urllib.request.Request("https://api.sbis.ru" + path,
        data=json.dumps(payload, ensure_ascii=False).encode() if payload is not None else None,
        headers={"X-SBISAccessToken": env["token"], "Content-Type": "application/json"}, method=method)
    # Exactly one request. urllib does not retry timeouts or HTTP errors.
    try:
        with urllib.request.urlopen(req, timeout=45) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        try:
            body = json.loads(error.read())
        except (ValueError, UnicodeDecodeError):
            body = {}
        return error.code, {k: safe_text(v) for k, v in body.items() if k in ("message", "error", "code", "details")} if isinstance(body, dict) else {}


def balance():
    response = env["get"]("/retail/v2/nomenclature/list", pointId=env["point_id"], searchString="X380416519", withBalance="true", page=0, pageSize=50)
    rows = response.get("nomenclatures") or response.get("items") or []
    matches = [r for r in rows if r.get("nomNumber") == "X380416519" and r.get("name") == fixture["name"]]
    assert len(matches) == 1
    return matches[0].get("balance")


body = {
    "product": "delivery", "pointId": env["point_id"],
    "comment": "ТЕСТ API Фикусин 26.10.2026. Только X380416519. Без реального платежа и фискального чека. run=" + os.environ.get("GITHUB_RUN_ID", "local"),
    "customer": {"name": "ТЕСТ API Фикусин", "phone": "70000000000"},
    "datetime": (datetime.now(ZoneInfo("Europe/Moscow")) + timedelta(hours=2)).strftime("%Y-%m-%d %H:%M:%S"),
    "nomenclatures": [{"externalId": fixture["externalId"], "priceListId": 19, "count": 1, "cost": 1}],
    "delivery": {"isPickup": True, "paymentType": "cash"},
}
report["createAttempted"] = True
report["requestSha256"] = hashlib.sha256(json.dumps(body, sort_keys=True).encode()).hexdigest()
save()  # Durable evidence before the only create attempt.
order_id = None
try:
    status, created = call("POST", "/retail/order/create", body)
    report["createHTTP"] = status
    report["createResponseFields"] = sorted(created) if isinstance(created, dict) else type(created).__name__
    if status >= 400:
        report["createError"] = created
        save()
        print(json.dumps(report, ensure_ascii=False))
        raise SystemExit("Saby rejected the fixture order; no mutation retry")
    candidates = [created]
    if isinstance(created, dict):
        candidates += [created.get(k) for k in ("result", "order", "data")]
    for candidate in candidates:
        values = [candidate] if isinstance(candidate, str) else [candidate.get(k) for k in ("externalId", "orderId", "id")] if isinstance(candidate, dict) else []
        for value in values:
            try:
                order_id = str(uuid.UUID(str(value)))
                break
            except (ValueError, TypeError, AttributeError):
                pass
        if order_id:
            break
    if not order_id:
        report["unknownCreateResult"] = True
        raise RuntimeError("Create returned no recognized order UUID; reconcile manually")
    report["orderId"] = order_id
    save()
    _, state = call("GET", "/retail/order/" + order_id + "/state")
    report["stateAfterCreate"] = state
    report["balanceAfterCreate"] = balance()
    save()
    if payment_test:
        # Live API rejects the GET advertised in the public docs and explicitly
        # requires POST. The previous GET left payState=0 and was cancelled.
        payment_params = {"bankSum": 1, "bankType": env["test_bank_type"], "paymentType": "full", "nonFiscal": True}
        report["paymentAttempted"] = True
        report["paymentParameters"] = payment_params
        save()
        status, payment = call("POST", "/retail/order/" + order_id + "/register-payment", payment_params)
        report["paymentHTTP"] = status
        report["paymentResponse"] = payment
        save()
        time.sleep(5)
        _, state = call("GET", "/retail/order/" + order_id + "/state")
        report["stateAfterPayment"] = state
        report["paymentRegistered"] = isinstance(state, dict) and state.get("payState") == 200 and any(p.get("isClosed") is True for p in state.get("payments") or [])
        report["balanceAfterPayment"] = balance()
        save()
finally:
    if order_id:
        report["cancelAttempted"] = True
        save()
        status, cancelled = call("PUT", "/retail/order/" + order_id + "/cancel")
        report["cancelHTTP"] = status
        report["cancelResponse"] = cancelled
        if status >= 400:
            report["cancelError"] = cancelled
        time.sleep(3)
        _, state = call("GET", "/retail/order/" + order_id + "/state")
        report["stateAfterCancel"] = state
        report["balanceAfterCancel"] = balance()
    save()
    print(json.dumps(report, ensure_ascii=False))
if payment_test and (report.get("paymentHTTP", 500) >= 400 or
                     not isinstance(report.get("paymentResponse"), dict) or
                     report["paymentResponse"].get("successFlag") is not True):
    raise SystemExit("Nonfiscal external payment was rejected; inspect cancellation readback")
if payment_test and not report["paymentRegistered"]:
    raise SystemExit("Payment was queued, not confirmed. Do not replay registration")
if not isinstance(report.get("stateAfterCancel"), dict) or report["stateAfterCancel"].get("state") not in (42, 220):
    raise SystemExit("Cancellation was not confirmed. Reconcile this order; do not create another")
