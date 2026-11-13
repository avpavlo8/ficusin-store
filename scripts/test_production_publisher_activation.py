import copy
import importlib.util
import io
import json
from pathlib import Path
import unittest
import urllib.error

SPEC = importlib.util.spec_from_file_location("publisher_activation", Path(__file__).with_name("production-publisher-activation.py"))
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)
SHA = "a" * 40


def app_fixture():
    return {"id": 229223, "name": "ficusin-store", "branch": "main", "status": "active", "commit_sha": SHA,
            "envs": {"DATABASE_URL": "postgres://fixture:password@db.invalid/default_db",
                     "WB_API_TOKEN": "wb-secret", "OZON_CLIENT_ID": "client-secret", "OZON_API_KEY": "ozon-secret",
                     "SABY_APP_CLIENT_ID": "saby-client", "SABY_APP_SECRET": "saby-secret", "SABY_SECRET_KEY": "saby-key",
                     "PRIVATE_S3_BUCKET": "ficusin-private", "UNCHANGED": "exact\nvalue"}}


class Provider:
    def __init__(self, app=None, fresh=None):
        self.app = app if app is not None else app_fixture()
        self.fresh, self.gets, self.patches = fresh, 0, []

    def get(self):
        self.gets += 1
        return copy.deepcopy(self.fresh if self.gets > 1 and self.fresh is not None else self.app)

    def patch(self, env):
        self.patches.append(env)


class Rows:
    def __init__(self, rows): self.rows = rows
    def fetchone(self): return self.rows[0]
    def fetchall(self): return self.rows


class Connection:
    def __init__(self, pending=None, migrated=True, readonly=True):
        self.pending = pending if pending is not None else [("disabled", "prepared", 7), ("mock", "queued", 2)]
        self.migrated, self.readonly, self.closed, self.queries = migrated, readonly, False, []

    def transaction(self):
        class Transaction:
            def __enter__(self): pass
            def __exit__(self, *args): return False
        return Transaction()

    def execute(self, query, params=()):
        sql = " ".join(query.split())
        if not sql.startswith("SELECT "):
            raise AssertionError("SQL write attempted")
        self.queries.append((sql, params))
        if "current_database()" in sql:
            return Rows([("default_db", "fixture", "on" if self.readonly else "off", "on")])
        if "schema_migrations" in sql:
            if params != (module.MIGRATION,): raise AssertionError("wrong migration guard")
            return Rows([(self.migrated,)])
        if "catalog_editor_publications" in sql:
            return Rows(self.pending)
        raise AssertionError("unexpected SQL")

    def close(self): self.closed = True


class ActivationTests(unittest.TestCase):
    def test_default_dryrun_queries_only_and_keeps_disabled_mock_jobs(self):
        provider, connection = Provider(), Connection()
        report = module.activate(provider, lambda env: connection, SHA)
        self.assertTrue(connection.closed)
        self.assertEqual(provider.gets, 2)
        self.assertEqual(provider.patches, [])
        self.assertTrue(report["dryRun"])
        self.assertFalse(report["patchAttempted"])
        self.assertFalse(report["vendorWritesPerformed"])
        self.assertFalse(report["writePermissionsVerified"])
        self.assertEqual(len(report["pendingPublications"]), 2)
        encoded = json.dumps(report)
        for forbidden in ("secret", "password", "UNCHANGED", "exact", "DATABASE_URL"):
            self.assertNotIn(forbidden, encoded)

    def test_apply_changes_exactly_two_flags_preserves_every_other_env(self):
        provider = Provider()
        report = module.activate(provider, lambda env: Connection(), SHA, apply=True)
        self.assertEqual(provider.patches, [{**app_fixture()["envs"], **module.FLAGS}])
        self.assertTrue(report["patchAttempted"])
        self.assertTrue(report["runtimeVerificationRequired"])

    def test_old_sha_wrong_app_branch_status_block_before_database(self):
        for key, value in (("id", 280757), ("name", "other"), ("branch", "staging"), ("status", "deploying"), ("commit_sha", "b" * 40)):
            app = app_fixture()
            app[key] = value
            with self.subTest(key=key), self.assertRaises(module.guards.PreflightError):
                module.activate(Provider(app), lambda env: self.fail("must not connect"), SHA, True)
        with self.assertRaises(module.guards.PreflightError):
            module.activate(Provider(), lambda env: self.fail("must not connect"), module.guards.EXPECTED_COMMIT, True)

    def test_required_keys_storage_environment_must_be_valid(self):
        for key, value in (("WB_API_TOKEN", ""), ("OZON_CLIENT_ID", ""), ("OZON_API_KEY", ""),
                           ("SABY_APP_CLIENT_ID", ""), ("SABY_APP_SECRET", ""), ("SABY_SECRET_KEY", ""),
                           ("PRIVATE_S3_BUCKET", "ficusin-photos"), ("APP_ENV", "staging"),
                           ("CATALOG_PUBLICATION_MODE", "mock"), ("FILE_STORAGE_MODE", "mock")):
            app = app_fixture()
            app["envs"][key] = value
            with self.subTest(key=key), self.assertRaises(module.guards.PreflightError):
                module.activate(Provider(app), lambda env: self.fail("must not connect"), SHA, True)

    def test_catalog_specific_credentials_are_valid_without_legacy_keys(self):
        app = app_fixture()
        for preferred, legacy in (("CATALOG_WB_API_TOKEN", "WB_API_TOKEN"), ("CATALOG_OZON_CLIENT_ID", "OZON_CLIENT_ID"), ("CATALOG_OZON_API_KEY", "OZON_API_KEY")):
            app["envs"][preferred] = app["envs"].pop(legacy)
        module.activate(Provider(app), lambda env: Connection(), SHA, True)

    def test_any_live_unfinished_job_or_missing_migration_blocks_patch(self):
        fixtures = [Connection(pending=[("live", status, 1)]) for status in ("prepared", "queued", "processing", "needs_attention")]
        fixtures.extend([Connection(migrated=False), Connection(readonly=False)])
        for connection in fixtures:
            provider = Provider()
            with self.subTest(pending=connection.pending), self.assertRaises(module.guards.PreflightError):
                module.activate(provider, lambda env: connection, SHA, True)
            self.assertTrue(connection.closed)
            self.assertEqual(provider.patches, [])

    def test_fresh_environment_change_or_release_change_is_not_overwritten(self):
        for change in ("env", "sha"):
            fresh = app_fixture()
            if change == "env": fresh["envs"]["OTHER"] = "new"
            else: fresh["commit_sha"] = "b" * 40
            provider = Provider(fresh=fresh)
            with self.assertRaises(module.guards.PreflightError):
                module.activate(provider, lambda env: Connection(), SHA, True)
            self.assertEqual(provider.patches, [])

    def test_already_enabled_is_noop(self):
        app = app_fixture()
        app["envs"].update(module.FLAGS)
        provider = Provider(app)
        report = module.activate(provider, lambda env: Connection(pending=[]), SHA, True)
        self.assertTrue(report["alreadyEnabled"])
        self.assertEqual(provider.patches, [])

    def test_transport_only_one_guarded_app_patch_no_retries_or_redirects(self):
        class Opener:
            def __init__(self): self.requests = []
            def open(self, request, timeout):
                self.requests.append(request)
                self_test.assertEqual(timeout, 30)
                if request.get_method() == "GET":
                    return io.BytesIO(json.dumps({"app": app_fixture()}).encode())
                raise urllib.error.HTTPError(request.full_url, 500, "SECRET", {}, None)
        self_test = self
        opener = Opener()
        provider = module.Provider("timeweb-secret", True, opener)
        provider.get()
        with self.assertRaises(module.guards.PreflightError):
            provider.patch({**app_fixture()["envs"], "OTHER": "not allowed", **module.FLAGS})
        with self.assertRaises(urllib.error.HTTPError):
            provider.patch({**app_fixture()["envs"], **module.FLAGS})
        with self.assertRaises(module.guards.PreflightError):
            provider.patch({**app_fixture()["envs"], **module.FLAGS})
        self.assertEqual([r.get_method() for r in opener.requests], ["GET", "PATCH"])
        self.assertEqual({r.full_url for r in opener.requests}, {module.guards.API + "/apps/229223"})
        self.assertEqual(set(json.loads(opener.requests[1].data)), {"envs"})
        self.assertTrue(any(isinstance(h, module.guards.NoRedirect) for h in module.Provider("secret").opener.handlers))


if __name__ == "__main__":
    unittest.main()
