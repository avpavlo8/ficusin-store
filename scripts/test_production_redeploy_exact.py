import copy
import importlib.util
import io
import json
from pathlib import Path
import unittest
import urllib.error

spec = importlib.util.spec_from_file_location("redeploy", Path(__file__).with_name("production-redeploy-exact.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
SHA = "a" * 40


def app_fixture():
    return {"id": 229223, "name": "ficusin-store", "branch": "main", "status": "active", "commit_sha": SHA,
            "envs": {"DATABASE_URL": "postgres://fixture:password@db.invalid/default_db",
                     "WB_API_TOKEN": "wb-secret", "OZON_CLIENT_ID": "client-secret", "OZON_API_KEY": "ozon-secret",
                     "SABY_APP_CLIENT_ID": "saby-client", "SABY_APP_SECRET": "saby-secret", "SABY_SECRET_KEY": "saby-key",
                     "PRIVATE_S3_BUCKET": "ficusin-private", **module.activation.FLAGS}}


class Opener:
    def __init__(self, apps=None, error=None):
        self.apps = apps if apps is not None else [app_fixture(), app_fixture()]
        self.error, self.requests = error, []

    def open(self, request, timeout):
        self.requests.append(request)
        if timeout != 30: raise AssertionError("unexpected timeout")
        if request.get_method() == "GET":
            return io.BytesIO(json.dumps({"app": self.apps.pop(0)}).encode())
        if self.error: raise self.error
        return io.BytesIO(b'{"deploy_id":"DO NOT PRINT"}')


class Tests(unittest.TestCase):
    def test_default_dryrun_two_readonly_gets(self):
        opener = Opener()
        result = module.redeploy(module.Provider("secret", opener=opener), SHA)
        self.assertEqual(result, {"deployRequested": False, "commit": SHA})
        self.assertEqual([r.get_method() for r in opener.requests], ["GET", "GET"])

    def test_apply_single_exact_deploy_post_safe_output(self):
        opener = Opener()
        result = module.redeploy(module.Provider("secret", True, opener), SHA, True)
        self.assertEqual(result, {"deployRequested": True, "commit": SHA})
        self.assertEqual([r.get_method() for r in opener.requests], ["GET", "GET", "POST"])
        request = opener.requests[-1]
        self.assertEqual(request.full_url, module.guards.API + "/apps/229223/deploy")
        self.assertEqual(json.loads(request.data), {"commit_sha": SHA})
        self.assertNotIn("secret", json.dumps(result))
        self.assertNotIn("DO NOT PRINT", json.dumps(result))

    def test_identity_flags_private_bucket_and_fresh_snapshot_guards(self):
        for field, value in (("id", 280757), ("name", "other"), ("branch", "staging"), ("status", "deploying"), ("commit_sha", "b" * 40),
                             ("PRIVATE_S3_BUCKET", "other"), ("CATALOG_PUBLICATION_MODE", "disabled"), ("CATALOG_PUBLICATION_LIVE_ENABLED", "false")):
            app = app_fixture()
            (app if field in app else app["envs"])[field] = value
            opener = Opener([app])
            with self.subTest(field=field), self.assertRaises(module.guards.PreflightError):
                module.redeploy(module.Provider("secret", True, opener), SHA, True)
            self.assertEqual([r.get_method() for r in opener.requests], ["GET"])
        fresh = copy.deepcopy(app_fixture())
        fresh["envs"]["OTHER"] = "changed"
        opener = Opener([app_fixture(), fresh])
        with self.assertRaises(module.guards.PreflightError):
            module.redeploy(module.Provider("secret", True, opener), SHA, True)
        self.assertEqual([r.get_method() for r in opener.requests], ["GET", "GET"])

    def test_unknown_post_never_replayed_and_redirects_blocked(self):
        opener = Opener(error=urllib.error.HTTPError("https://api.invalid", 500, "SECRET ERROR", {}, None))
        provider = module.Provider("secret", True, opener)
        with self.assertRaises(urllib.error.HTTPError):
            module.redeploy(provider, SHA, True)
        with self.assertRaises(module.guards.PreflightError):
            provider.deploy(SHA)
        self.assertEqual([r.get_method() for r in opener.requests], ["GET", "GET", "POST"])
        handler = next(h for h in module.Provider("secret").reader.opener.handlers if isinstance(h, module.guards.NoRedirect))
        with self.assertRaises(module.guards.PreflightError):
            handler.redirect_request(opener.requests[-1], None, 302, "redirect", {}, "https://evil.invalid/")


if __name__ == "__main__":
    unittest.main()
