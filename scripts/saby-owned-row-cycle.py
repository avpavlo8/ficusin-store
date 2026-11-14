#!/usr/bin/env python3
"""Prepare/run ONE fixed unposted test-row cycle through pinned product helpers.

Default is a local plan, with no remote access. --apply is explicit, GHA first
attempt only, and requires exact production/source SHAs. No posting or stock
writes, no DB connection, no application/environment mutation. Retain journal.
"""
import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

spec = importlib.util.spec_from_file_location("row_cycle_guards", Path(__file__).with_name("production-release-preflight.py"))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)

PRODUCTION_SHA = "4a3f4dd4799b881d2e367d93a47c13e7a3f77273"
SECRET_KEYS = ("SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY")
SUMMARY = {"documentID": 42807, "nativeUUID": "8d6e3870-40b3-4c82-8d2b-ccefd310637d",
           "publicUUID": "1eacfa69-a5be-4790-8b9a-a198b3493220", "nomenclatureID": 3899,
           "quantity": "1", "prices": ["1.01", "1.00"], "postingRequested": False,
           "documentCreationRequested": False, "retryPolicy": "read-only"}


def fail(code):
    raise guards.PreflightError(code)


def child_environment(values=None):
    result = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH") if key in os.environ}
    result.update({"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOTELEMETRY": "off"})
    if values is not None:
        for key in SECRET_KEYS:
            value = values.get(key)
            if not isinstance(value, str) or not value.strip():
                fail("missing_saby_credentials")
            result[key] = value
    return result


def validate_app(app):
    env = guards.production_settings(app, PRODUCTION_SHA)
    branch = app.get("branch_name") or app.get("branch")
    if isinstance(branch, dict):
        branch = branch.get("name")
    if branch != "main" or app.get("status") != "active":
        fail("production_not_active_main")
    # Deliberately return only these credentials, never the database URL or
    # production publication configuration to a runner that can write a row.
    return {key: env.get(key) for key in SECRET_KEYS}


def source_archive(source, expected_sha, run=subprocess.run):
    source = Path(source).resolve(strict=True)
    if not re.fullmatch(r"[0-9a-f]{40}", expected_sha):
        fail("invalid_source_commit")
    head = run(["git", "rev-parse", "HEAD"], cwd=source, capture_output=True, check=True).stdout.decode().strip()
    dirty = run(["git", "status", "--porcelain"], cwd=source, capture_output=True, check=True).stdout
    if head != expected_sha or dirty.strip():
        fail("source_not_exact_clean_commit")
    data = run(["git", "archive", "HEAD", "backend"], cwd=source, capture_output=True, check=True).stdout
    if not data or len(data) > 64 << 20:
        fail("source_archive_invalid")
    return data


def extract_backend(data, directory):
    if any(Path(directory).iterdir()):
        fail("archive_target_not_empty")
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        for member in archive.getmembers():
            parts = Path(member.name).parts
            if not parts or parts[0] != "backend" or ".." in parts or member.name.startswith("/") or not (member.isfile() or member.isdir()):
                fail("unsafe_source_archive")
        # All members are validated regular files/directories below backend;
        # target is a fresh empty tempdir, so neither archive nor existing
        # symlinks can redirect extraction (also compatible with Python 3.9).
        archive.extractall(directory)


def execute(source, source_sha, journal, apply=False, reader=None, run=subprocess.run):
    if not apply:
        return {"ok": True, "applied": False, "plan": SUMMARY, "productionSHA": PRODUCTION_SHA}
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("GITHUB_RUN_ATTEMPT") != "1":
        fail("first_github_attempt_only")
    journal = Path(journal).absolute()
    if journal.exists() or journal.is_symlink() or not journal.parent.is_dir():
        fail("journal_already_exists_or_invalid")
    archive = source_archive(source, source_sha, run)
    # Build in an isolated archive: no temporary Go package ever appears in the
    # product checkout, and only committed source at the supplied SHA is used.
    with tempfile.TemporaryDirectory(prefix="saby-owned-row-cycle-") as directory:
        root = Path(directory)
        extract_backend(archive, root)
        backend = root / "backend"
        command = backend / "cmd/owned-row-cycle"
        command.mkdir(parents=True, exist_ok=False)
        command.joinpath("main.go").write_bytes(Path(__file__).with_name("saby-owned-row-cycle").joinpath("main.go").read_bytes())
        binary = root / "owned-row-cycle"
        result = run(["go", "build", "-o", str(binary), "./cmd/owned-row-cycle"], cwd=backend,
                     env=child_environment(), capture_output=True, timeout=240)
        if result.returncode:
            fail("pinned_runner_build_failed")
        reader = reader or guards.TimewebReader(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""))
        values = validate_app(reader.get("/apps/229223").get("app", {}))
        for value in values.values():
            if isinstance(value, str) and value:
                print("::add-mask::" + value, flush=True)
        result = run([str(binary), "--journal", str(journal)], cwd=root,
                     env=child_environment(values), capture_output=True, timeout=300)
        # No raw helper stderr/stdout is ever printed, including exceptions.
        if result.returncode:
            fail("owned_row_cycle_stopped_read_only_recovery")
        if len(result.stdout) > 4096:
            fail("unexpected_runner_output")
        report = json.loads(result.stdout)
        expected = {"ok": True, "documentID": 42807, "finalUnposted": True, "finalQuantity": "1", "finalPrice": "1.00", "postingRequested": False}
        if report != expected or not journal.is_file() or journal.stat().st_size == 0:
            fail("unverified_runner_completion")
        return {**expected, "applied": True, "sourceSHA": source_sha, "retryPolicy": "read-only"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path)
    parser.add_argument("--source-sha")
    parser.add_argument("--journal", type=Path, default=Path("saby-owned-row-cycle-journal.jsonl"))
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        if args.apply and (args.source_root is None or args.source_sha is None):
            fail("pinned_source_required")
        print(json.dumps(execute(args.source_root, args.source_sha, args.journal, args.apply)), flush=True)
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error, {"phase": "fixed_unposted_test_row_cycle"})), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
