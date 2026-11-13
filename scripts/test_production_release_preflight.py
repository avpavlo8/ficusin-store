import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
import urllib.error

SPEC = importlib.util.spec_from_file_location("production_preflight", Path(__file__).with_name("production-release-preflight.py"))
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def app_fixture():
    return {"id": module.APP_ID, "name": module.APP_NAME, "branch_name": "main", "commit_sha": module.EXPECTED_COMMIT,
            "envs": {"DATABASE_URL": "postgres://fixture:fixture-password@db.invalid/default_db?sslmode=require"}}


class Result:
    def __init__(self, rows):
        self.rows = rows

    def fetchone(self):
        return self.rows[0]

    def fetchall(self):
        return self.rows


class Connection:
    read_only = True

    def __init__(self, publications=True, mode=True, identity=None, shipping=(0, 0, 0)):
        self.publications, self.mode = publications, mode
        self.identity = identity or (module.DATABASE, "fixture", "on", "on")
        self.shipping, self.queries, self.closed = shipping, [], False

    def transaction(self):
        connection = self

        class Transaction:
            def __enter__(self):
                if connection.read_only is not True:
                    raise AssertionError("read-write transaction attempted")

            def __exit__(self, *args):
                return False
        return Transaction()

    def execute(self, query, params=()):
        sql = " ".join(query.split())
        if not sql.upper().startswith("SELECT "):
            raise AssertionError("SQL mutation attempted")
        self.queries.append((sql, params))
        if "current_setting('transaction_read_only')" in sql:
            return Result([self.identity])
        if "pg_get_userbyid" in sql:
            return Result([(True, True, False)])
        if "to_regclass('public.schema_migrations')" in sql:
            return Result([(True,)])
        if "max(name)" in sql:
            return Result([(201, "20261101_existing.sql")])
        if "to_regclass('public.catalog_editor_publications')" in sql:
            return Result([(self.publications,)])
        if "information_schema.columns" in sql:
            return Result([(self.mode,)])
        if "GROUP BY execution_mode,status" in sql:
            return Result([("disabled", "prepared", 2), ("mock", "processing", 1), ("live", "queued", 3)])
        if "GROUP BY status" in sql:
            return Result([("prepared", 2)])
        if "FROM public.orders" in sql:
            return Result([self.shipping])
        raise AssertionError("unexpected SQL")

    def close(self):
        self.closed = True


class Reader:
    def __init__(self, app=None, denied_backups=False):
        self.app = app or app_fixture()
        self.denied_backups, self.paths = denied_backups, []

    def get(self, path):
        self.paths.append(path)
        if path == f"/apps/{module.APP_ID}":
            return {"app": self.app}
        if path == f"/dbs/{module.DATABASE_ID}/backups":
            if self.denied_backups:
                raise urllib.error.HTTPError("https://api.invalid", 403, "forbidden", {}, None)
            return {"backups": [{"id": 123, "status": "completed", "created_at": "2026-11-13T10:00:00Z",
                                  "download_url": "https://private.invalid/secret", "customer": "MUST NOT PRINT"}]}
        raise AssertionError("unexpected Timeweb endpoint")


class PreflightTests(unittest.TestCase):
    def test_readonly_inventory_keeps_modes_and_whitelists_output(self):
        reader, connection = Reader(), Connection(shipping=(4, 2, 1))
        report = module.inspect(reader, lambda settings: connection, module.EXPECTED_COMMIT)
        self.assertTrue(connection.closed)
        self.assertEqual(reader.paths, [f"/apps/{module.APP_ID}", f"/dbs/{module.DATABASE_ID}/backups"])
        self.assertEqual(len(report["catalogPublications"]["pending"]), 3)
        self.assertEqual(report["migrations"]["count"], 201)
        self.assertTrue(report["shipping"]["exactWorkflowReadRequired"])
        self.assertEqual(report["shipping"]["autoTransitionCandidateUpperBound"], 1)
        encoded = json.dumps(report)
        for forbidden in ("fixture-password", "DATABASE_URL", "download_url", "MUST NOT PRINT", "private.invalid"):
            self.assertNotIn(forbidden, encoded)
        self.assertEqual(set(report["backups"]["backups"][0]), {"id", "status", "created_at"})

    def test_old_main_missing_table_is_honest(self):
        report = module.inspect(Reader(), lambda settings: Connection(publications=False), module.EXPECTED_COMMIT)
        self.assertEqual(report["catalogPublications"], {"tableExists": False, "pending": []})

    def test_old_table_without_execution_mode_is_not_invented(self):
        report = module.inspect(Reader(), lambda settings: Connection(mode=False), module.EXPECTED_COMMIT)
        self.assertFalse(report["catalogPublications"]["executionModeColumnExists"])
        self.assertIsNone(report["catalogPublications"]["pending"][0]["executionMode"])

    def test_backup_access_denied_is_explicit(self):
        report = module.inspect(Reader(denied_backups=True), lambda settings: Connection(), module.EXPECTED_COMMIT)
        self.assertEqual(report["backups"], {"available": False, "httpStatus": 403})

    def test_wrong_app_commit_environment_or_dsn_never_connects(self):
        for key, value in (("id", 280757), ("name", "other"), ("branch_name", "staging"), ("commit_sha", "0" * 40)):
            app = app_fixture()
            app[key] = value
            with self.subTest(key=key), self.assertRaises(module.PreflightError):
                module.inspect(Reader(app), lambda settings: self.fail("must not connect"), module.EXPECTED_COMMIT)
        for key, value in (("APP_ENV", "staging"), ("DATABASE_URL", "postgres://fixture:pass@db.invalid/ficusin_staging"),
                           ("DATABASE_URL", "postgres://fixture:pass@db.invalid/default_db?dbname=other")):
            app = app_fixture()
            app["envs"][key] = value
            with self.subTest(key=key, category="unsafe configuration"), self.assertRaises(module.PreflightError):
                module.inspect(Reader(app), lambda settings: self.fail("must not connect"), module.EXPECTED_COMMIT)

    def test_wrong_actual_database_or_writable_transaction_fails_closed(self):
        for identity in (("other", "fixture", "on", "on"), (module.DATABASE, "other", "on", "on"),
                         (module.DATABASE, "fixture", "off", "on"), (module.DATABASE, "fixture", "on", "off")):
            connection = Connection(identity=identity)
            with self.subTest(identity=identity), self.assertRaises(module.PreflightError):
                module.inspect(Reader(), lambda settings: connection, module.EXPECTED_COMMIT)
            self.assertTrue(connection.closed)

    def test_explicit_post_deploy_commit_supported(self):
        app = app_fixture()
        app["commit_sha"] = "a" * 40
        report = module.inspect(Reader(app), lambda settings: Connection(), "a" * 40)
        self.assertEqual(report["commit"], "a" * 40)

    def test_connection_enforces_readonly_tls_and_private_ca(self):
        class Driver:
            def connect(self, dsn, **kwargs):
                self.options = kwargs
                self.connection = Connection()
                self.connection.read_only = False
                return self.connection
        driver = Driver()
        settings = app_fixture()["envs"]
        settings["DATABASE_SSL_CA"] = "fixture certificate\\nline two"
        with tempfile.TemporaryDirectory() as directory:
            connection = module.connect_production(settings, directory, driver=driver)
            self.assertTrue(connection.read_only)
            self.assertFalse(driver.options["autocommit"])
            self.assertEqual(driver.options["sslmode"], "verify-full")
            self.assertIn("default_transaction_read_only=on", driver.options["options"])
            ca = Path(driver.options["sslrootcert"])
            self.assertEqual(stat.S_IMODE(ca.stat().st_mode), 0o600)
            self.assertEqual(ca.read_text(), "fixture certificate\nline two")

    def test_timeweb_transport_only_performs_two_allowed_gets(self):
        class Opener:
            def __init__(self):
                self.requests = []

            def open(self, request, **kwargs):
                self.requests.append(request)
                self_test.assertEqual(request.get_method(), "GET")
                self_test.assertIsNone(request.data)
                return io.BytesIO(b"{}")
        self_test = self
        opener = Opener()
        reader = module.TimewebReader("fixture-token", opener)
        reader.get(f"/apps/{module.APP_ID}")
        reader.get(f"/dbs/{module.DATABASE_ID}/backups")
        with self.assertRaises(module.PreflightError):
            reader.get(f"/dbs/{module.DATABASE_ID}/backups/create")
        self.assertEqual(len(opener.requests), 2)


if __name__ == "__main__":
    unittest.main()
