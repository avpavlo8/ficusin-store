import importlib.util
import io
import json
from pathlib import Path
import unittest
import urllib.error

SPEC = importlib.util.spec_from_file_location("marketplace_readiness", Path(__file__).with_name("production-marketplace-readiness.py"))
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def app_fixture():
    return {"id": module.guards.APP_ID, "name": "ficusin-store", "branch_name": "main",
            "commit_sha": module.guards.EXPECTED_COMMIT,
            "envs": {"DATABASE_URL": "postgres://fixture:private-password@db.invalid/default_db",
                     "CATALOG_WB_API_TOKEN": "catalog-wb-secret", "WB_API_TOKEN": "legacy-wb-secret",
                     "CATALOG_OZON_CLIENT_ID": "catalog-client-secret", "OZON_CLIENT_ID": "legacy-client-secret",
                     "CATALOG_OZON_API_KEY": "catalog-ozon-secret", "OZON_API_KEY": "legacy-ozon-secret"}}


class Timeweb:
    def __init__(self, app=None):
        self.app, self.paths = app if app is not None else app_fixture(), []

    def get(self, path):
        self.paths.append(path)
        if path != "/apps/229223":
            raise AssertionError("unexpected Timeweb endpoint")
        return {"app": self.app}


class Response(io.BytesIO):
    status = 200


class Opener:
    def __init__(self, responses):
        self.responses, self.requests = list(responses), []

    def open(self, request, timeout):
        self.requests.append((request, timeout))
        result = self.responses.pop(0)
        if isinstance(result, Exception):
            raise result
        return Response(result if isinstance(result, bytes) else json.dumps(result).encode())


class ReadinessTests(unittest.TestCase):
    def test_exact_readonly_requests_and_sanitized_aggregate_report(self):
        opener = Opener([{"data": [{"subjectID": 123, "subjectName": "PRIVATE NAME"}]},
                         {"result": [{"description_category_id": 456, "children": [{"type_id": 789}]}]}])
        timeweb = Timeweb()
        report = module.inspect(timeweb, module.CategoryReader(opener), module.guards.EXPECTED_COMMIT)
        self.assertEqual(timeweb.paths, ["/apps/229223"])
        self.assertEqual(len(opener.requests), 2)
        wb, ozon = [entry[0] for entry in opener.requests]
        self.assertEqual([entry[1] for entry in opener.requests], [20, 20])
        self.assertEqual((wb.get_method(), wb.full_url, wb.data), ("GET", module.WB_CATEGORIES, None))
        self.assertEqual(wb.get_header("Authorization"), "catalog-wb-secret")
        self.assertEqual((ozon.get_method(), ozon.full_url), ("POST", module.OZON_CATEGORIES))
        self.assertEqual(json.loads(ozon.data), {"language": "RU"})
        self.assertEqual(ozon.get_header("Client-id"), "catalog-client-secret")
        self.assertEqual(ozon.get_header("Api-key"), "catalog-ozon-secret")
        self.assertIsNone(ozon.get_header("Authorization"))
        self.assertIsNone(wb.get_header("Api-key"))
        self.assertTrue(report["categoriesReadable"])
        self.assertFalse(report["writePermissionsVerified"])
        self.assertEqual(report["channels"]["wb"]["count"], 1)
        self.assertEqual(report["channels"]["ozon"]["count"], 2)
        encoded = json.dumps(report)
        for forbidden in ("secret", "PRIVATE NAME", "subjectID", "type_id", "private-password", "DATABASE_URL"):
            self.assertNotIn(forbidden, encoded)

    def test_catalog_overrides_and_independent_legacy_fallback_match_application(self):
        settings = app_fixture()["envs"]
        settings["CATALOG_WB_API_TOKEN"] = " "
        settings["CATALOG_OZON_CLIENT_ID"] = ""
        self.assertEqual(module.effective_credentials(settings), {
            "wb": "legacy-wb-secret", "ozonClient": "legacy-client-secret", "ozonKey": "catalog-ozon-secret"})

    def test_unconfigured_channel_makes_no_request(self):
        opener = Opener([])
        result = module.inspect(Timeweb({**app_fixture(), "envs": {
            "DATABASE_URL": app_fixture()["envs"]["DATABASE_URL"]}}), module.CategoryReader(opener), module.guards.EXPECTED_COMMIT)
        self.assertEqual(opener.requests, [])
        self.assertFalse(result["categoriesReadable"])
        for channel in result["channels"].values():
            self.assertFalse(channel["configured"])
            self.assertEqual(channel["errorCode"], "not_configured")

    def test_wrong_app_or_commit_blocks_before_any_vendor_call(self):
        for field, value in (("id", 280757), ("name", "staging"), ("commit_sha", "0" * 40), ("branch_name", "staging")):
            app = app_fixture()
            app[field] = value
            opener = Opener([])
            with self.subTest(field=field), self.assertRaises(module.guards.PreflightError):
                module.inspect(Timeweb(app), module.CategoryReader(opener), module.guards.EXPECTED_COMMIT)
            self.assertEqual(opener.requests, [])

    def test_vendor_http_failure_is_single_attempt_and_does_not_echo_error(self):
        credentials = module.effective_credentials(app_fixture()["envs"])
        for status in (301, 401, 429, 500):
            opener = Opener([urllib.error.HTTPError(module.OZON_CATEGORIES, status, "SECRET ECHO", {}, None)])
            report = module.CategoryReader(opener).read("ozon", credentials)
            self.assertEqual(len(opener.requests), 1)
            self.assertEqual(report["httpStatus"], status)
            self.assertFalse(report["readOK"])
            self.assertEqual(report["errorCode"], "redirect_blocked" if status == 301 else "http_error")
            self.assertNotIn("SECRET", json.dumps(report))

    def test_no_redirect_handler_never_returns_a_forwarded_request(self):
        import urllib.request
        reader = module.CategoryReader()
        handlers = [handler for handler in reader.opener.handlers if isinstance(handler, module.guards.NoRedirect)]
        self.assertEqual(len(handlers), 1)
        request = urllib.request.Request(module.OZON_CATEGORIES, data=b"{}", headers={"Api-Key": "secret"})
        with self.assertRaises(module.guards.PreflightError):
            handlers[0].redirect_request(request, None, 302, "found", {}, "https://evil.invalid/")

    def test_invalid_empty_or_vendor_error_dictionary_fails_honestly(self):
        credentials = module.effective_credentials(app_fixture()["envs"])
        for payload, code in ((b"not-json", "invalid_json"), ({"result": []}, "empty_categories"),
                              ({"result": "wrong"}, "unexpected_schema"), ({"error": "SECRET ECHO"}, "vendor_error"),
                              ({"result": [{"children": "wrong"}]}, "unexpected_schema")):
            with self.subTest(code=code):
                report = module.CategoryReader(Opener([payload])).read("ozon", credentials)
                self.assertEqual(report["errorCode"], code)
                self.assertFalse(report["readOK"])
                self.assertNotIn("SECRET", json.dumps(report))

    def test_no_other_channel_or_endpoint_is_reachable(self):
        opener = Opener([])
        with self.assertRaises(module.guards.PreflightError):
            module.CategoryReader(opener).read("prices", module.effective_credentials(app_fixture()["envs"]))
        self.assertEqual(opener.requests, [])


if __name__ == "__main__":
    unittest.main()
