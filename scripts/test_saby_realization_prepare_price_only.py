import copy
from decimal import Decimal
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("price_probe", Path(__file__).with_name("saby-realization-prepare-price-only.py"))
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)


def fixtures():
    header = p.r.record([(key, "Число целое" if isinstance(value, int) else "Строка", value) for key, value in {
        "@Документ": p.DOC, "ИдентификаторДокумента": p.UUID, "ДокументРасширение.ИдентификаторВИ": p.VI,
        "ТипДокумента": 226, "ТипДокумента.ТипДокумента": "ДокОтгрИсх", "СкладскойДокумент.Склад": 283,
        "ДокументНашаОрганизация": 259, "Регламент": 122498, "Лицо1": 18655, "Лицо3": 259, "Дата": "2026-11-13", "Удален": False}.items()])
    # Reversed schema insertion order proves flags are read by numeric ordinal.
    header["s"].append({"n": "ДокументРасширение.Состояние", "t": {"n": "Флаги", "s": {str(i): p.FLAGS[i] for i in reversed(range(6))}}})
    header["d"].append([False] * 6)
    line = p.r.record([(key, "Число целое", value) for key, value in {
        "@НоменклатураДокумента": 175645, "Номенклатура": 3899, "СкладскойДокумент": p.DOC,
        "Склад": 284, "Тип": 1, "Количество": 1, "Цена": 1, "СуммаЦен": 1, "Закрыто": False}.items()])
    for column in line["s"]:
        if column["n"] in ("Цена", "СуммаЦен"):
            column["t"] = {"n": "Деньги", "p": 2}
    line["s"].append({"n": "Флаги", "t": {"n": "Флаги", "s": {"0": "Удален"}}})
    line["d"].append([False])
    template = copy.deepcopy(line)
    for index, column in enumerate(template["s"]):
        if column["n"] in ("@НоменклатураДокумента", "Закрыто"):
            template["d"][index] = None
        if column["n"] in ("Цена", "СуммаЦен"):
            template["d"][index] = Decimal("1.01")
    return header, line, template


def change(record, name, value):
    for index, column in enumerate(record["s"]):
        if column["n"] == name:
            record["d"][index] = value
            return
    raise AssertionError("missing fixture field")


class ProbeTests(unittest.TestCase):
    def harness(self, edit=None, reply_edit=None, send_error=None, after_edit=None):
        header, line, template = fixtures()
        if edit:
            edit(header, line)
        if reply_edit:
            reply_edit(template)
        calls, attempts, reports, sent = [], [], [], []

        def rpc(method, params, protocol):
            calls.append(method)
            self.assertEqual(protocol, 6)
            if method == "ДокОтгрИсх.Прочитать":
                self.assertEqual(params["ИдО"], p.DOC)
                result = copy.deepcopy(header)
                if after_edit and calls.count(method) > 1:
                    after_edit(result)
                return result
            self.assertEqual(method, "РеалИсх.DocNomList")
            self.assertEqual(p.r.fields(params["Фильтр"])["СкладскойДокумент"], p.DOC)
            return {"_type": "recordset", "s": copy.deepcopy(line["s"]), "d": [copy.deepcopy(line["d"])]}

        def send(params):
            sent.append(copy.deepcopy(params))
            if send_error:
                raise send_error
            return {"result": copy.deepcopy(template)}

        return lambda: p.probe(rpc, send, lambda: attempts.append(True), reports.append), calls, attempts, reports, sent

    def test_exact_unsaved_price_then_unchanged_saved_row(self):
        run, calls, attempts, reports, sent = self.harness()
        run()
        self.assertEqual(len(sent), 1)
        self.assertEqual(attempts, [True])
        params = sent[0]
        self.assertIsNone(params["ИмяМетода"])
        filter_data = p.fields(params["Фильтр"])
        self.assertEqual({k: filter_data[k] for k in ("Номенклатура", "СкладскойДокумент", "Количество", "Цена")},
                         {"Номенклатура": 3899, "СкладскойДокумент": p.DOC, "Количество": 1, "Цена": "1.01"})
        self.assertEqual(calls.count("ДокОтгрИсх.Прочитать"), 2)
        self.assertEqual(calls.count("РеалИсх.DocNomList"), 2)
        self.assertTrue(reports[0]["preparePriceOnly"]["matchesRequestedPrice"])
        self.assertTrue(reports[-1]["persistedBaselineUnchanged"])

    def test_wrong_price_is_reported_without_fixing_or_saving(self):
        run, calls, attempts, reports, sent = self.harness(reply_edit=lambda t: (change(t, "Цена", Decimal("0.00")), change(t, "СуммаЦен", Decimal("0.00"))))
        with self.assertRaises(p.r.guards.PreflightError) as caught:
            run()
        self.assertEqual(caught.exception.safe_code, "prepare_filter_price_not_applied")
        self.assertEqual(reports[0]["preparePriceOnly"]["returnedPrice"], "0.00")
        self.assertFalse(reports[0]["preparePriceOnly"]["matchesRequestedPrice"])
        self.assertTrue(reports[-1]["persistedBaselineUnchanged"])
        self.assertEqual(len(sent), 1)

    def test_lost_response_only_runs_readback_without_retry(self):
        run, calls, attempts, reports, sent = self.harness(send_error=TimeoutError("fixture"))
        with self.assertRaises(TimeoutError):
            run()
        self.assertEqual(len(sent), 1)
        self.assertEqual(calls.count("ДокОтгрИсх.Прочитать"), 2)
        self.assertEqual(calls.count("РеалИсх.DocNomList"), 2)
        self.assertTrue(reports[-1]["persistedBaselineUnchanged"])

    def test_posted_foreign_or_changed_baseline_never_prepares(self):
        cases = [lambda h, l: change(h, "Лицо1", 1), lambda h, l: change(l, "Цена", 2),
                 lambda h, l: change(l, "@НоменклатураДокумента", 1), lambda h, l: change(l, "Закрыто", True),
                 lambda h, l: change(h, "ДокументРасширение.Состояние", [True] + [False] * 5),
                 lambda h, l: change(h, "ДокументРасширение.Состояние", [None] + [False] * 5),
                 lambda h, l: change(h, "ИдентификаторДокумента", p.VI)]
        for edit in cases:
            with self.subTest(edit=cases.index(edit)):
                run, calls, attempts, reports, sent = self.harness(edit=edit)
                with self.assertRaises(p.r.guards.PreflightError):
                    run()
                self.assertFalse(sent)
                self.assertFalse(attempts)

    def test_saved_or_foreign_template_is_not_accepted(self):
        for edit in [lambda t: change(t, "@НоменклатураДокумента", 175645), lambda t: change(t, "Склад", 283),
                     lambda t: change(t, "Номенклатура", 1), lambda t: change(t, "Закрыто", False)]:
            run, calls, attempts, reports, sent = self.harness(reply_edit=edit)
            with self.assertRaises(p.r.guards.PreflightError):
                run()
            self.assertEqual(len(sent), 1)
            self.assertEqual(calls.count("РеалИсх.DocNomList"), 2)

    def test_changed_post_state_after_prepare_is_a_failure(self):
        run, calls, attempts, reports, sent = self.harness(after_edit=lambda h: change(h, "ДокументРасширение.Состояние", [True] + [False] * 5))
        with self.assertRaises(p.r.guards.PreflightError):
            run()
        self.assertEqual(len(sent), 1)

    def test_once_transport_has_only_fixed_method_no_redirect_and_no_retry(self):
        class Opener:
            def open(self, request, timeout):
                body = json.loads(request.data)
                self.assertions(request, body, timeout)
                raise TimeoutError("fixture")
        opener = Opener()
        def assertions(request, body, timeout):
            self.assertEqual(request.full_url, p.r.SERVICE)
            self.assertEqual(body["method"], "РеалИсх.DocNomCreate")
            self.assertEqual(body["protocol"], 7)
            self.assertEqual(timeout, 20)
        opener.assertions = assertions
        with patch.object(p.urllib.request, "build_opener", return_value=opener) as build:
            with self.assertRaises(TimeoutError):
                p.request_prepare_once(p.prepare_params(), "fixture-token")
            self.assertEqual(build.call_count, 1)
            self.assertIsInstance(build.call_args.args[0], p.r.guards.NoRedirect)


if __name__ == "__main__":
    unittest.main()
