import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("cycle", Path(__file__).with_name("saby-owned-row-cycle.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def app():
    return {"id": 229223, "name": "ficusin-store", "branch_name": "main", "status": "active", "commit_sha": m.PRODUCTION_SHA,
            "envs": {"APP_ENV": "production", "DATABASE_URL": "postgres://test:test@db.invalid/default_db", **{k: "synthetic-" + k for k in m.SECRET_KEYS}}}


class RunnerTests(unittest.TestCase):
    def test_default_plan_has_no_network_no_subprocess(self):
        def forbidden(*args, **kwargs):
            self.fail("plan touched a remote/build process")
        self.assertFalse(m.execute(None, None, None, reader=forbidden, run=forbidden)["applied"])

    def test_restore_plan_is_fixed_to_acknowledged_row_and_one_price(self):
        plan = m.execute(None, None, None, restore_acknowledged_row=True)["plan"]
        self.assertEqual(plan["acknowledgedStartRowID"], 175676)
        self.assertEqual(plan["startPrice"], "1.01")
        self.assertEqual(plan["prices"], ["1.00"])
        self.assertFalse(plan["postingRequested"])

    def test_prod_identity_is_fixed_and_only_saby_credentials_escape(self):
        self.assertEqual(set(m.validate_app(app())), set(m.SECRET_KEYS))
        for field, value in (("id", 280757), ("commit_sha", "a" * 40), ("status", "deploying"), ("branch_name", "staging")):
            data = app()
            data[field] = value
            with self.assertRaises(m.guards.PreflightError):
                m.validate_app(data)

    def test_child_environment_excludes_all_other_credentials(self):
        with patch.dict(os.environ, {"TIMEWEB_CLOUD_TOKEN": "never", "DATABASE_URL": "never", "SABY_APP_SECRET": "inherited-never", "GOFLAGS": "never", "GOWORK": "never"}):
            env = m.child_environment(app()["envs"])
        self.assertNotIn("TIMEWEB_CLOUD_TOKEN", env)
        self.assertNotIn("DATABASE_URL", env)
        self.assertNotIn("GOFLAGS", env)
        self.assertEqual(env["GOWORK"], "off")
        self.assertEqual(env["SABY_APP_SECRET"], "synthetic-SABY_APP_SECRET")

    def test_apply_refuses_non_first_attempt_before_remote(self):
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "true", "GITHUB_RUN_ATTEMPT": "2"}):
            with self.assertRaises(m.guards.PreflightError):
                m.execute(None, None, "/unreachable", True)

    def test_archive_rejects_links_and_traversal(self):
        for name, kind in (("../outside", tarfile.REGTYPE), ("backend/link", tarfile.SYMTYPE)):
            out = io.BytesIO()
            with tarfile.open(fileobj=out, mode="w") as archive:
                entry = tarfile.TarInfo(name)
                entry.type = kind
                archive.addfile(entry)
            with tempfile.TemporaryDirectory() as directory, self.assertRaises(m.guards.PreflightError):
                m.extract_backend(out.getvalue(), directory)

    def test_unknown_child_output_is_never_exposed_and_never_retried(self):
        out = io.BytesIO()
        with tarfile.open(fileobj=out, mode="w") as archive:
            info = tarfile.TarInfo("backend/go.mod")
            content = b"module fake\n"
            info.size = len(content)
            archive.addfile(info, io.BytesIO(content))
        calls = []
        def run(cmd, **kwargs):
            calls.append(cmd)
            if cmd[0] == "git":
                value = b"a" * 40 if cmd[1] == "rev-parse" else b"" if cmd[1] == "status" else out.getvalue()
                return subprocess.CompletedProcess(cmd, 0, value, b"")
            if cmd[:2] == ["go", "build"]:
                self.assertNotIn("SABY_APP_SECRET", kwargs["env"])
                return subprocess.CompletedProcess(cmd, 0, b"", b"")
            self.assertNotIn("DATABASE_URL", kwargs["env"])
            self.assertNotIn("TIMEWEB_CLOUD_TOKEN", kwargs["env"])
            return subprocess.CompletedProcess(cmd, 1, b"sensitive-native-response", b"secret-from-error")
        class Reader:
            def get(self, path):
                if path != "/apps/229223":
                    raise AssertionError(path)
                return {"app": app()}
        capture = io.StringIO()
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {"GITHUB_ACTIONS": "true", "GITHUB_RUN_ATTEMPT": "1"}), contextlib.redirect_stdout(capture):
            with self.assertRaises(m.guards.PreflightError):
                m.execute(directory, "a" * 40, Path(directory) / "journal", True, Reader(), run, restore_acknowledged_row=True)
        self.assertNotIn("sensitive-native-response", capture.getvalue())
        self.assertNotIn("secret-from-error", capture.getvalue())
        self.assertEqual(len(calls), 5)  # three local git reads, one build, one execution
        self.assertEqual(calls[-1][-1], "--restore-acknowledged-row")


if __name__ == "__main__":
    unittest.main()
