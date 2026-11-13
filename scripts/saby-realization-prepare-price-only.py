#!/usr/bin/env python3
"""One unsaved template probe on the fixed owned draft; NO save/post/stock write."""
import argparse
from decimal import Decimal
import importlib.util
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location("reads", Path(__file__).with_name("saby-realization-readback.py"))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)

DOC = 42807
UUID = "8d6e3870-40b3-4c82-8d2b-ccefd310637d"
VI = "1eacfa69-a5be-4790-8b9a-a198b3493220"
METHOD = "РеалИсх.DocNomCreate"
FLAGS = ("Проведен", "ЕстьПроводки", "ОшибкиПроведения", "НеактуальныеПроводки", "ВОчередиНаПроведение", "ВОчередиНаРасчетПроводок")


def fail(code):
    raise r.guards.PreflightError(code)


def fields(record):
    if not isinstance(record, dict) or record.get("_type") != "record":
        fail("native_record_required")
    schema, values = record.get("s"), record.get("d")
    if not isinstance(schema, list) or not isinstance(values, list) or len(schema) != len(values):
        fail("native_record_incomplete")
    names = [c.get("n") if isinstance(c, dict) else None for c in schema]
    if any(not isinstance(n, str) or not n for n in names) or len(set(names)) != len(names):
        fail("native_schema_ambiguous")
    return dict(zip(names, values))


def flags(record, name):
    data = fields(record)
    column = next((c for c in record["s"] if c["n"] == name), None)
    fmt = column.get("t") if column else None
    labels = fmt.get("s") if isinstance(fmt, dict) else None
    values = data.get(name)
    if not isinstance(labels, dict) or fmt.get("n") != "Флаги" or not isinstance(values, list) or len(labels) != len(values):
        fail("native_flags_missing")
    if any(not isinstance(k, str) or not k.isdecimal() or str(int(k)) != k for k in labels):
        fail("native_flags_invalid")
    names = [labels[k] for k in sorted(labels, key=int)]
    if any(not isinstance(n, str) for n in names) or len(set(names)) != len(names) or any(v is not None and type(v) is not bool for v in values):
        fail("native_flags_invalid")
    return dict(zip(names, values))


def number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float, Decimal)) or len(str(value)) > 40:
        fail("native_number_invalid")
    n = Decimal(str(value))
    if not n.is_finite():
        fail("native_number_invalid")
    return n


def read_baseline(rpc):
    raw = rpc("ДокОтгрИсх.Прочитать", {"ИдО": DOC, "ИмяМетода": "ДокОтгрИсх.Список"}, 6)
    data = fields(raw)
    expected = {"@Документ": DOC, "ИдентификаторДокумента": UUID, "ДокументРасширение.ИдентификаторВИ": VI,
                "ТипДокумента": 226, "ТипДокумента.ТипДокумента": "ДокОтгрИсх", "СкладскойДокумент.Склад": 283,
                "ДокументНашаОрганизация": 259, "Регламент": 122498, "Лицо1": 18655, "Лицо3": 259, "Дата": "2026-11-13"}
    for key, value in expected.items():
        if data.get(key) != value or isinstance(value, int) and type(data.get(key)) is not int:
            fail("fixed_test_identity_mismatch")
    if data.get("Удален") is not False:
        fail("fixed_test_deleted_or_unknown")
    state = flags(raw, "ДокументРасширение.Состояние")
    if any(k not in state or state[k] is not False for k in FLAGS):
        fail("fixed_test_not_unposted_idle")
    listing = rpc("РеалИсх.DocNomList", {
        "Фильтр": r.record([("СкладскойДокумент", "Число целое", DOC), ("Тип", "Число целое", 1),
                            ("ЭтоРасходы", "Логическое", False), ("Раздел", "Число целое", None)]),
        "Сортировка": None, "Навигация": r.record([("ЕстьЕще", "Логическое", True), ("Страница", "Число целое", 0),
                                                    ("РазмерСтраницы", "Число целое", 100)]), "ДопПоля": []}, 6)
    if not isinstance(listing, dict) or listing.get("_type") != "recordset" or not isinstance(listing.get("d"), list) or len(listing["d"]) != 1:
        fail("fixed_test_exact_one_line_required")
    row = {"_type": "record", "s": listing.get("s"), "d": listing["d"][0]}
    line = fields(row)
    expected_line = {"@НоменклатураДокумента": 175645, "Номенклатура": 3899, "СкладскойДокумент": DOC, "Склад": 284, "Тип": 1}
    if any(type(line.get(k)) is not int or line[k] != v for k, v in expected_line.items()) or line.get("Закрыто") is not False:
        fail("fixed_test_line_identity_mismatch")
    for key in ("Количество", "Цена", "СуммаЦен"):
        if number(line.get(key)) != Decimal(1):
            fail("fixed_test_line_values_mismatch")
    row_flags = flags(row, "Флаги")
    if "Удален" not in row_flags or row_flags["Удален"] is not None and row_flags["Удален"] is not False:
        fail("fixed_test_line_deleted")
    return {"identity": expected, "flags": {k: state[k] for k in FLAGS}, "line": expected_line,
            "quantity": number(line["Количество"]), "price": number(line["Цена"]), "sum": number(line["СуммаЦен"]), "closed": False}


def prepare_params():
    record = json.loads(Path(__file__).with_name("saby-realization-prepare-price-test-filter.json").read_text())
    data = fields(record)
    updates = {"Номенклатура": 3899, "СкладскойДокумент": DOC, "Количество": 1, "Цена": "1.01"}
    expected_types = {"Номенклатура": "Число целое", "СкладскойДокумент": "Число целое", "Количество": "Число целое", "Цена": "Строка"}
    if not updates.keys() <= data.keys():
        fail("captured_filter_incomplete")
    for i, column in enumerate(record["s"]):
        name = column["n"]
        if name in updates:
            if column["t"] != expected_types[name]:
                fail("captured_filter_type_changed")
            record["d"][i] = updates[name]
    return {"Фильтр": record, "ИмяМетода": None}


def request_prepare_once(params, token):
    # Fixed URL and fixed method. No redirect, auth-refresh, application retry,
    # save, posting, creation of a document, payments or stock endpoints.
    body = {"jsonrpc": "2.0", "protocol": 7, "method": METHOD, "params": params, "id": 1}
    request = urllib.request.Request(r.SERVICE, method="POST", data=json.dumps(body).encode(),
                                    headers={"Content-Type": "application/json-rpc;charset=utf-8", "Accept": "application/json", "X-SBISAccessToken": token})
    try:
        response = urllib.request.build_opener(r.guards.NoRedirect()).open(request, timeout=20)
    except urllib.error.HTTPError as error:
        error.close()
        fail("prepare_http_outcome_unknown")
    with response:
        content = response.read((8 << 20) + 1)
        if len(content) > 8 << 20:
            fail("prepare_response_too_large")
        return json.loads(content.decode(response.headers.get_content_charset() or "utf-8"), parse_float=Decimal)


def validate_template(answer):
    if not isinstance(answer, dict) or answer.get("error"):
        fail("prepare_rpc_outcome_unknown")
    raw = answer.get("result")
    line = fields(raw)
    if "@НоменклатураДокумента" not in line or line["@НоменклатураДокумента"] is not None or "Закрыто" not in line or line["Закрыто"] is not None:
        fail("prepare_did_not_return_unsaved_template")
    expected = {"Номенклатура": 3899, "СкладскойДокумент": DOC, "Склад": 284, "Тип": 1}
    if any(type(line.get(k)) is not int or line[k] != v for k, v in expected.items()) or number(line.get("Количество")) != Decimal(1):
        fail("prepare_template_identity_mismatch")
    price, total = number(line.get("Цена")), number(line.get("СуммаЦен"))
    for key in ("Цена", "СуммаЦен"):
        column = next(c for c in raw["s"] if c["n"] == key)
        if column["t"] != {"n": "Деньги", "p": 2}:
            fail("prepare_template_money_schema_changed")
    return {"templateUnsaved": True, "requestedPrice": "1.01", "returnedPrice": str(price), "returnedSum": str(total),
            "matchesRequestedPrice": price == total == Decimal("1.01")}


def probe(rpc, send, record_attempt, emit):
    before = read_baseline(rpc)
    params = prepare_params()
    record_attempt()
    failure = None
    summary = None
    try:
        summary = validate_template(send(params))
    except Exception as error:
        failure = error
    # Read-only recovery also runs after a lost/malformed response. No second
    # prepare and no save can be reached from this path.
    after = read_baseline(rpc)
    unchanged = before == after
    if summary is not None:
        emit({"preparePriceOnly": summary})
    emit({"persistedBaselineUnchanged": unchanged, "documentID": DOC, "savedLineID": 175645,
          "savedQuantity": "1", "savedPrice": "1", "postingRequested": False, "saveRequested": False, "retryPolicy": "read-only"})
    if not unchanged:
        fail("prepare_changed_persisted_baseline")
    if failure is not None:
        raise failure
    if not summary["matchesRequestedPrice"]:
        fail("prepare_filter_price_not_applied")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    args = parser.parse_args()
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("GITHUB_RUN_ATTEMPT") != "1":
            fail("first_github_attempt_only")
        app = r.guards.TimewebReader(os.environ.get("TIMEWEB_CLOUD_TOKEN", "")).get("/apps/229223").get("app", {})
        env = r.guards.production_settings(app, args.expected_commit)
        auth = r.request(r.AUTH, {k: env.get(n, "") for k, n in (("app_client_id", "SABY_APP_CLIENT_ID"), ("app_secret", "SABY_APP_SECRET"), ("secret_key", "SABY_SECRET_KEY"))})
        token = auth.get("token")
        if not token:
            fail("auth_failed")
        print("::add-mask::" + token)
        def attempt():
            path = Path("saby-realization-test-receipt.json")
            metadata = {"runId": os.environ.get("GITHUB_RUN_ID"), "action": "prepare-price-only", "documentID": DOC,
                        "documentUUID": UUID, "publicUUID": VI, "dedicatedTestNomenclatureID": 3899, "testQuantity": 1,
                        "requestedTemplatePrice": "1.01", "postingRequested": False, "saveRequested": False, "retryPolicy": "read-only"}
            with path.open("x") as receipt:
                json.dump(metadata, receipt)
            print(json.dumps({"attemptRecorded": metadata}), flush=True)
        probe(lambda method, params, protocol: r.rpc(method, params, token, protocol),
              lambda params: request_prepare_once(params, token), attempt,
              lambda item: print(json.dumps(item), flush=True))
        return 0
    except Exception as error:
        print(json.dumps(r.guards.safe_failure(error, {"phase": "owned_unsaved_price_prepare", "documentID": DOC, "retryPolicy": "read-only"})))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
