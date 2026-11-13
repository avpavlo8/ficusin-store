import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("migration_ops", Path(__file__).with_name("production-file-migration.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
SHA = "a" * 40


def app_fixture():
    return {"id": m.APP_ID, "name": m.APP_NAME, "branch_name": "main", "commit_sha": SHA, "status": "active",
            "envs": {"DATABASE_URL": "postgres://fixture:DO_NOT_LOG_PASSWORD@db.invalid/default_db?sslmode=require",
                     "APP_ENV": "production", "PRIVATE_S3_BUCKET": m.BUCKET,
                     "S3_ACCESS_KEY": "DO_NOT_LOG_KEY", "S3_SECRET_KEY": "DO_NOT_LOG_SECRET",
                     "DATABASE_SSL_VERIFY": "false", "SABY_SECRET_KEY": "VENDOR_SECRET_MUST_NOT_PASS"}}


def bucket_fixture():
    return {"id": m.BUCKET_ID, "name": m.BUCKET, "type": "private", "storage_class": "cold", "preset_id": m.PRESET,
            "location": "ru-1", "status": "created", "is_allow_auto_upgrade": False}


def aggregate_fixture():
    return {kind: {"count": 1 if kind == "avatars" else 0, "bytes": 17 if kind == "avatars" else 0,
                   "copied": 0, "skipped": 0} for kind in m.KINDS}


class Result:
    def __init__(self, one=None, all_rows=None):
        self.one, self.all_rows = one, all_rows
    def fetchone(self): return self.one
    def fetchall(self): return self.all_rows


class Connection:
    read_only = True
    def __init__(self, missing=False, oversized=False):
        self.missing, self.oversized = missing, oversized
    def __enter__(self): return self
    def __exit__(self, *args): return None
    def execute(self, sql, params=None):
        if not sql.lstrip().startswith("SELECT") or "finance" in sql:
            raise AssertionError("unexpected SQL write/financial access")
        if "current_database()" in sql:
            return Result(("default_db", "fixture", "on"))
        if "schema_migrations" in sql:
            return Result(all_rows=[] if self.missing else [("new.sql",)])
        if "public.customers" in sql:
            size = m.MAX_TOTAL + 1 if self.oversized else 17
            return Result((1, size, 17, 0))
        return Result((0, 0, 0, 0))


class Reader:
    def __init__(self, app=None, bucket=None):
        self.app, self.bucket, self.paths = app or app_fixture(), bucket or bucket_fixture(), []
    def get(self, path):
        self.paths.append(path)
        if path == f"/apps/{m.APP_ID}": return {"app": self.app}
        if path == f"/storages/buckets/{m.BUCKET_ID}": return {"bucket": self.bucket}
        raise AssertionError("unexpected provider access")


class Storage:
    def get_bucket_acl(self, **args):
        assert args == {"Bucket": m.BUCKET}
        return {"Grants": [{"Grantee": {"Type": "CanonicalUser"}}]}
    def get_bucket_policy(self, **args):
        return {"Policy": json.dumps({"Statement": []})}
    def list_objects_v2(self, **args):
        return {"IsTruncated": False, "Contents": [{"Size": 23, "Key": "PRIVATE_KEY_MUST_NOT_LOG"}]}


class Tests(unittest.TestCase):
    def test_dryrun_only_aggregates_no_apply(self):
        calls = []
        def runner(source, env, apply):
            calls.append(apply)
            self.assertNotIn("SABY_SECRET_KEY", env)
            self.assertNotIn("TIMEWEB_CLOUD_TOKEN", env)
            self.assertNotIn("S3_BUCKET", env)
            self.assertNotIn("S3_SECRET_KEY", env)
            self.assertEqual(env["PRIVATE_S3_BUCKET"], m.BUCKET)
            self.assertIn("default_transaction_read_only%3Don", env["DATABASE_URL"])
            return aggregate_fixture(), True
        with tempfile.TemporaryDirectory() as directory:
            result = m.execute(Reader(), lambda _: Connection(), Storage(), Path(directory), ["new.sql"], SHA,
                               directory, runner=runner)
        self.assertEqual(calls, [False])
        self.assertTrue(result["dryRun"])
        self.assertFalse(result["sourceBytesDeleted"])
        encoded = json.dumps(result)
        for forbidden in ("DO_NOT_LOG", "PRIVATE_KEY", "sha256", "DATABASE_URL", "VENDOR_SECRET", "fixture"):
            self.assertNotIn(forbidden, encoded)
        self.assertEqual(result["files"]["avatars"]["bytes"], 17)

    def test_apply_one_kind_one_row_releases_unused_reservation(self):
        calls = []
        copied = False
        def runner(source, env, apply, *, kind=None):
            nonlocal copied
            calls.append((apply, kind))
            if not apply:
                return aggregate_fixture(), True
            self.assertNotIn("default_transaction_read_only", env["DATABASE_URL"])
            result = {k: {"count": 0, "bytes": 0, "copied": 0, "skipped": 0} for k in m.KINDS}
            if kind == "avatars" and not copied:
                copied = True
                result[kind].update(count=1, bytes=17, copied=1)
            return result, True
        with tempfile.TemporaryDirectory() as directory:
            report = m.execute(Reader(), lambda _: Connection(), Storage(), Path(directory), ["new.sql"], SHA,
                               directory, apply=True, runner=runner)
        self.assertTrue(report["complete"])
        self.assertFalse(report["dryRun"])
        self.assertEqual(report["chargedBytes"], 17)
        self.assertEqual(report["files"]["avatars"]["copied"], 1)
        self.assertFalse(report["sourceBytesDeleted"])
        self.assertEqual(calls, [(False, None), (True, "avatars"), (True, "avatars")] + [(True, k) for k in m.KINDS[1:]])

    def test_wrong_revision_environment_bucket_and_auto_upgrade_rejected(self):
        for field, value in (("commit_sha", "b" * 40), ("branch_name", "staging"), ("name", "other")):
            app = app_fixture(); app[field] = value
            with self.assertRaises(Exception): m.check_app(app, SHA)
        app = app_fixture(); app["envs"]["APP_ENV"] = "staging"
        with self.assertRaises(Exception): m.check_app(app, SHA)
        for field, value in (("id", 541777), ("type", "public"), ("preset_id", 5127), ("is_allow_auto_upgrade", True)):
            bucket = bucket_fixture(); bucket[field] = value
            with self.assertRaises(m.MigrationError): m.check_bucket(bucket)

    def test_missing_release_migrations_or_excess_bytes_stop_before_cli(self):
        for connection in (Connection(missing=True), Connection(oversized=True)):
            with tempfile.TemporaryDirectory() as directory, self.assertRaises(m.MigrationError):
                m.execute(Reader(), lambda _: connection, Storage(), Path(directory), ["new.sql"], SHA,
                          directory, runner=lambda *args: self.fail("CLI executed before guards"))

    def test_go_child_private_records_and_stderr_never_forwarded(self):
        payload = {"dryRun": True, "sourceBytesDeleted": False,
                   "files": [{"kind": "avatars", "id": 987654321, "bytes": 17, "sha256": "a" * 64, "status": "would-copy"}]}
        def run(command, **kwargs):
            self.assertNotIn("finance", ",".join(command))
            self.assertNotIn("--apply", command)
            self.assertTrue(kwargs["capture_output"])
            return subprocess.CompletedProcess(command, 0, json.dumps(payload).encode(), b"PRIVATE_DSN_AND_ERROR_TEXT")
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            result, ok = m.run_cli(Path("/source"), {}, False, run=run)
        self.assertTrue(ok)
        self.assertEqual(stdout.getvalue(), "")
        for forbidden in ("987654321", "PRIVATE_DSN", "sha256", "a" * 64):
            self.assertNotIn(forbidden, json.dumps(result))
        payload["error"] = "DO_NOT_LOG_PASSWORD_FILE_ID_AND_SHA"
        result, ok = m.run_cli(Path("/source"), {}, False, run=run)
        self.assertFalse(ok)
        self.assertNotIn("DO_NOT_LOG", json.dumps(result))

    def test_source_exact_sha_and_dirty_checkout_guard(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            (source / "backend/cmd/migrate-file-assets").mkdir(parents=True)
            (source / "backend/cmd/migrate-file-assets/main.go").write_text("package main")
            (source / "timeweb/migrations").mkdir(parents=True)
            (source / "timeweb/migrations/new.sql").write_text("SELECT 1;")
            def run(command, **kwargs):
                output = SHA.encode() if "rev-parse" in command else b""
                return subprocess.CompletedProcess(command, 0, output, b"secret stderr")
            self.assertEqual(m.check_source(source, SHA, run=run)[1], ["new.sql"])
            def dirty(command, **kwargs):
                return subprocess.CompletedProcess(command, 0, SHA.encode() if "rev-parse" in command else b"?? backend/extra.go", b"")
            with self.assertRaises(m.MigrationError): m.check_source(source, SHA, run=dirty)

    def test_capacity_and_broad_acl_never_pass(self):
        class PublicStorage(Storage):
            def get_bucket_acl(self, **args):
                return {"Grants": [{"Grantee": {"Type": "Group", "URI": "http://acs.amazonaws.com/groups/global/AllUsers"}}]}
        with self.assertRaises(m.MigrationError): m.storage_usage(PublicStorage())
        class FullStorage(Storage):
            def list_objects_v2(self, **args):
                return {"IsTruncated": False, "Contents": [{"Size": m.CAPACITY}]}
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(m.MigrationError):
            m.execute(Reader(), lambda _: Connection(), FullStorage(), Path(directory), ["new.sql"], SHA,
                      directory, runner=lambda *args: self.fail("CLI executed without capacity"))

    def test_apply_command_is_one_row_and_rejects_unapproved_kinds_before_process(self):
        payload = {"dryRun": False, "sourceBytesDeleted": False,
                   "files": [{"kind": "chat", "id": 123456789, "bytes": 17, "sha256": "b" * 64, "status": "changed-concurrently-skipped"}]}
        def run(command, **kwargs):
            self.assertEqual(command[-1], "--apply")
            self.assertEqual(command[command.index("--kinds") + 1], "chat")
            self.assertEqual(command[command.index("--limit") + 1], "1")
            self.assertTrue(kwargs["capture_output"])
            return subprocess.CompletedProcess(command, 0, json.dumps(payload).encode(), b"PRIVATE_SECRET")
        captured = io.StringIO()
        with contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
            result, ok = m.run_cli(Path("/source"), {}, True, run=run, kind="chat")
        self.assertTrue(ok)
        self.assertEqual(result["chat"], {"count": 1, "bytes": 17, "copied": 0, "skipped": 1})
        self.assertEqual(captured.getvalue(), "")
        self.assertNotIn("123456789", json.dumps(result))
        for kind in (None, "finance", "avatars,chat"):
            with self.assertRaises(m.MigrationError):
                m.run_cli(Path("/source"), {}, True, run=lambda *a, **k: self.fail("process started"), kind=kind)
        payload["files"].append(payload["files"][0])
        with self.assertRaises(m.MigrationError):
            m.run_cli(Path("/source"), {}, True, run=run, kind="chat")
        payload["files"] = []
        payload["sourceBytesDeleted"] = True
        with self.assertRaises(m.MigrationError):
            m.run_cli(Path("/source"), {}, True, run=run, kind="chat")

    def test_batch_reserves_before_put_and_charges_skipped_bytes_too(self):
        attempts = 0
        def runner(source, env, apply, *, kind=None):
            nonlocal attempts
            if not apply: return aggregate_fixture(), True
            self.assertLess(attempts * m.MAX_FILE, m.MAX_TOTAL)
            attempts += 1
            result = {k: {"count": 0, "bytes": 0, "copied": 0, "skipped": 0} for k in m.KINDS}
            result[kind].update(count=1, bytes=m.MAX_FILE, skipped=1)
            return result, True
        with tempfile.TemporaryDirectory() as directory:
            report = m.execute(Reader(), lambda _: Connection(), Storage(), Path(directory), ["new.sql"], SHA,
                               directory, apply=True, runner=runner)
        self.assertEqual(attempts, 8)
        self.assertEqual(report["chargedBytes"], m.MAX_TOTAL)
        self.assertEqual(report["stopReason"], "batch_budget")
        self.assertEqual(report["files"]["avatars"]["skipped"], 8)
        self.assertFalse(report["complete"])

    def test_many_small_files_refund_reservation_and_retry_cas_skip_safely(self):
        attempts = 0
        def runner(source, env, apply, *, kind=None):
            nonlocal attempts
            if not apply: return aggregate_fixture(), True
            result = {k: {"count": 0, "bytes": 0, "copied": 0, "skipped": 0} for k in m.KINDS}
            if kind == "avatars" and attempts < 20:
                attempts += 1
                result[kind].update(count=1, bytes=17, skipped=int(attempts == 1), copied=int(attempts != 1))
            return result, True
        with tempfile.TemporaryDirectory() as directory:
            report = m.execute(Reader(), lambda _: Connection(), Storage(), Path(directory), ["new.sql"], SHA,
                               directory, apply=True, runner=runner)
        self.assertTrue(report["complete"])
        self.assertEqual(attempts, 20)
        self.assertEqual(report["chargedBytes"], 340)
        self.assertEqual(report["files"]["avatars"]["skipped"], 1)
        self.assertEqual(report["files"]["avatars"]["copied"], 19)

    def test_actual_storage_capacity_checked_before_every_apply(self):
        class GrowingStorage(Storage):
            scans = 0
            def list_objects_v2(self, **args):
                self.scans += 1
                size = 23 if self.scans <= 2 else m.CAPACITY - m.MAX_FILE
                return {"IsTruncated": False, "Contents": [{"Size": size}]}
        attempts = []
        def runner(source, env, apply, *, kind=None):
            if not apply: return aggregate_fixture(), True
            attempts.append(kind)
            return aggregate_fixture(), True
        storage = GrowingStorage()
        with tempfile.TemporaryDirectory() as directory:
            report = m.execute(Reader(), lambda _: Connection(), storage, Path(directory), ["new.sql"], SHA,
                               directory, apply=True, runner=runner)
        self.assertEqual(attempts, ["avatars"])
        self.assertEqual(storage.scans, 3)
        self.assertEqual(report["stopReason"], "storage_capacity")

    def test_unknown_or_failed_apply_never_retried_and_masks_partial_output(self):
        for failure in ("timeout", "failed"):
            calls = []
            def runner(source, env, apply, *, kind=None):
                if not apply: return aggregate_fixture(), True
                calls.append(kind)
                if failure == "timeout":
                    raise subprocess.TimeoutExpired("PRIVATE_DSN", 1, output=b"PRIVATE_ROW_ID", stderr=b"PRIVATE_SECRET")
                return aggregate_fixture(), False
            captured = io.StringIO()
            with tempfile.TemporaryDirectory() as directory, contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
                report = m.execute(Reader(), lambda _: Connection(), Storage(), Path(directory), ["new.sql"], SHA,
                                   directory, apply=True, runner=runner)
            self.assertEqual(calls, ["avatars"])
            self.assertFalse(report["ok"])
            self.assertEqual(report["uncertainBytes"], m.MAX_FILE)
            self.assertEqual(report["chargedBytes"], m.MAX_FILE)
            self.assertFalse(report["sourceBytesDeleted"])
            self.assertNotIn("PRIVATE", json.dumps(report) + captured.getvalue())

    def test_cli_unknown_kind_or_output_contract_rejected(self):
        payload = {"dryRun": True, "sourceBytesDeleted": False, "files": [{"kind": "finance", "id": 1, "bytes": 1, "sha256": "a" * 64, "status": "would-copy"}]}
        with self.assertRaises(m.MigrationError):
            m.run_cli(Path("/source"), {}, False, run=lambda cmd, **_: subprocess.CompletedProcess(cmd, 0, json.dumps(payload).encode(), b""))


if __name__ == "__main__": unittest.main()
