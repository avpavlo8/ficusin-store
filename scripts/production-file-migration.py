#!/usr/bin/env python3
"""GHA-only, explicit migration of legacy Ficusin private files to approved S3.

Default dry-run. Explicit --apply leases one file of at most 64 MiB per
Go invocation within a 512 MiB batch budget. Never deploys, creates goods, changes ACL/env/roles,
migrates finance, or removes bytea. Child output is captured as aggregates.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.parse
import urllib.request

spec = importlib.util.spec_from_file_location("file_migration_guards", Path(__file__).with_name("production-release-preflight.py"))
guards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guards)

APP_ID, APP_NAME = 229223, "ficusin-store"
BUCKET_ID, BUCKET, PRESET = 603857, "ficusin-private", 5143
CAPACITY, MAX_TOTAL, MAX_FILE, LIMIT = 1 << 30, 512 << 20, 64 << 20, 1000
KINDS = ("avatars", "returns", "procurement", "reviews", "chat")
SOURCES = {
    "avatars": ("customers", "avatar_object_key", "avatar_image"),
    "returns": ("marketplace_return_photos", "object_key", "image"),
    "procurement": ("procurement_documents", "object_key", "content"),
    "reviews": ("product_review_photos", "object_key", "image"),
    "chat": ("support_messages", "object_key", "image_data"),
}
MAX_OUTPUT = 16 << 20


class MigrationError(Exception):
    def __init__(self, code):
        self.safe_code = code if re.fullmatch(r"[a-z_]{1,80}", code) else "migration_guard_failed"
        super().__init__()


class Reader:
    def __init__(self, token, opener=None):
        if not token:
            raise MigrationError("missing_timeweb_token")
        self.token = token
        self.opener = opener or urllib.request.build_opener(guards.NoRedirect())

    def get(self, path):
        if path not in (f"/apps/{APP_ID}", f"/storages/buckets/{BUCKET_ID}"):
            raise MigrationError("provider_path_not_allowed")
        req = urllib.request.Request(guards.API + path, method="GET", headers={
            "Authorization": "Bearer " + self.token, "Accept": "application/json"})
        with self.opener.open(req, timeout=30) as response:
            raw = response.read(MAX_OUTPUT + 1)
        if len(raw) > MAX_OUTPUT:
            raise MigrationError("provider_response_too_large")
        result = json.loads(raw)
        if not isinstance(result, dict):
            raise MigrationError("invalid_provider_response")
        return result


def check_app(app, expected_commit):
    settings = guards.production_settings(app, expected_commit)
    branch = app.get("branch_name") or app.get("branch")
    if isinstance(branch, dict):
        branch = branch.get("name")
    if branch != "main" or app.get("status") != "active":
        raise MigrationError("production_not_active_main")
    if any(not isinstance(k, str) or not isinstance(v, str) for k, v in settings.items()):
        raise MigrationError("invalid_environment")
    if settings.get("PRIVATE_S3_BUCKET", "").strip() != BUCKET:
        raise MigrationError("wrong_private_bucket")
    if settings.get("FILE_STORAGE_MODE", "s3").strip() not in ("", "s3") or settings.get("LOCAL_PRIVATE_FILES_DIR", "").strip():
        raise MigrationError("non_s3_file_storage")
    endpoint = settings.get("PRIVATE_S3_ENDPOINT", "").strip() or settings.get("S3_ENDPOINT", "").strip() or "https://s3.twcstorage.ru"
    region = settings.get("PRIVATE_S3_REGION", "").strip() or settings.get("S3_REGION", "").strip() or "ru-1"
    if endpoint.rstrip("/") != "https://s3.twcstorage.ru" or region != "ru-1":
        raise MigrationError("unexpected_private_storage_location")
    access = settings.get("PRIVATE_S3_ACCESS_KEY", "").strip() or settings.get("S3_ACCESS_KEY", "").strip()
    secret = settings.get("PRIVATE_S3_SECRET_KEY", "").strip() or settings.get("S3_SECRET_KEY", "").strip()
    if not access or not secret:
        raise MigrationError("missing_private_storage_credentials")
    return dict(settings), {"endpoint": endpoint.rstrip("/"), "region": region, "access": access, "secret": secret}


def check_bucket(bucket):
    if (not isinstance(bucket, dict) or bucket.get("id") != BUCKET_ID or bucket.get("name") != BUCKET
            or bucket.get("type") != "private" or bucket.get("storage_class") != "cold"
            or bucket.get("preset_id") != PRESET or bucket.get("location") != "ru-1"
            or bucket.get("status") != "created" or bucket.get("is_allow_auto_upgrade") is not False):
        raise MigrationError("private_bucket_outside_approved_scope")


def check_source(source, expected_commit, run=subprocess.run):
    if not re.fullmatch(r"[0-9a-f]{40}", expected_commit):
        raise MigrationError("invalid_expected_commit")
    source = Path(source)
    if not source.is_absolute() or not source.is_dir() or source.is_symlink():
        raise MigrationError("source_must_be_absolute_checkout")
    source = source.resolve()
    def git(*args):
        result = run(["git", "-C", str(source), *args], capture_output=True, timeout=30, check=False)
        if result.returncode != 0 or len(result.stdout) > MAX_OUTPUT:
            raise MigrationError("source_git_check_failed")
        return result.stdout.decode("utf-8", errors="strict").strip()
    if git("rev-parse", "HEAD") != expected_commit or git("status", "--porcelain", "--untracked-files=all", "--", "backend", "timeweb/migrations"):
        raise MigrationError("source_revision_or_cleanliness_mismatch")
    command = source / "backend/cmd/migrate-file-assets/main.go"
    if not command.is_file() or command.is_symlink():
        raise MigrationError("migration_command_missing")
    migrations = sorted(p.name for p in (source / "timeweb/migrations").glob("*.sql") if p.is_file() and not p.is_symlink())
    if not migrations:
        raise MigrationError("migration_sources_missing")
    return source, migrations


def inspect_database(connection, settings, migrations):
    user = urllib.parse.unquote(urllib.parse.urlsplit(settings["DATABASE_URL"]).username)
    identity = connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only')").fetchone()
    if identity != ("default_db", user, "on") or not connection.read_only:
        raise MigrationError("wrong_database_identity_or_readonly")
    applied = {row[0] for row in connection.execute("SELECT name FROM public.schema_migrations").fetchall()}
    if not set(migrations).issubset(applied):
        raise MigrationError("release_migrations_not_applied")
    inventory = {}
    for kind in KINDS:
        table, key, data = SOURCES[kind]  # fixed compile-time SQL identifiers
        row = connection.execute(f"SELECT count(*),COALESCE(sum(octet_length({data})),0),COALESCE(max(octet_length({data})),0),count(*) FILTER(WHERE octet_length({data})=0) FROM (SELECT {data} FROM public.{table} WHERE NULLIF({key},'') IS NULL AND {data} IS NOT NULL ORDER BY id LIMIT %s) files", (LIMIT,)).fetchone()
        count, size, largest, empty = (int(v) for v in row)
        if min(count, size, largest, empty) < 0 or largest > MAX_FILE or count > LIMIT:
            raise MigrationError("legacy_file_bound_exceeded")
        if empty:
            raise MigrationError("legacy_empty_file_requires_review")
        inventory[kind] = {"count": count, "bytes": size}
    if sum(item["bytes"] for item in inventory.values()) > MAX_TOTAL:
        raise MigrationError("migration_total_exceeds_cap")
    return inventory


def storage_usage(storage):
    # Only existing approved bucket metadata; no canary, ACL mutation or keys in logs.
    acl = storage.get_bucket_acl(Bucket=BUCKET)
    grants = acl.get("Grants")
    if not isinstance(grants, list):
        raise MigrationError("invalid_private_acl")
    for grant in grants:
        grantee = grant.get("Grantee", {})
        if grantee.get("Type") == "Group" and grantee.get("URI") in (
                "http://acs.amazonaws.com/groups/global/AllUsers", "http://acs.amazonaws.com/groups/global/AuthenticatedUsers"):
            raise MigrationError("broad_private_bucket_acl")
    try:
        policy_reply = storage.get_bucket_policy(Bucket=BUCKET)
    except Exception as error:
        response = getattr(error, "response", {})
        if response.get("Error", {}).get("Code") != "NoSuchBucketPolicy":
            raise
    else:
        policy = json.loads(policy_reply.get("Policy", ""))
        statements = policy.get("Statement", [])
        if isinstance(statements, dict):
            statements = [statements]
        if not isinstance(statements, list):
            raise MigrationError("unknown_private_bucket_policy")
        for statement in statements:
            if statement.get("Effect") != "Allow":
                continue
            principal = statement.get("Principal")
            values = principal.values() if isinstance(principal, dict) else [principal]
            if "NotPrincipal" in statement or any(v == "*" or isinstance(v, list) and "*" in v for v in values):
                raise MigrationError("anonymous_private_bucket_policy")
    size, count, continuation = 0, 0, None
    seen = set()
    for _ in range(100):
        args = {"Bucket": BUCKET, "MaxKeys": 1000}
        if continuation:
            args["ContinuationToken"] = continuation
        reply = storage.list_objects_v2(**args)
        for item in reply.get("Contents", []):
            value = item.get("Size")
            if not isinstance(value, int) or isinstance(value, bool) or value < 0:
                raise MigrationError("unknown_existing_storage_size")
            size += value
            count += 1
        if size > CAPACITY:
            raise MigrationError("existing_storage_exceeds_capacity")
        if reply.get("IsTruncated") is False:
            return {"count": count, "bytes": size}
        continuation = reply.get("NextContinuationToken")
        if not isinstance(continuation, str) or not continuation or continuation in seen:
            raise MigrationError("incomplete_private_storage_inventory")
        seen.add(continuation)
    raise MigrationError("private_storage_inventory_limit")


def child_environment(settings, storage, directory, apply):
    # Do not pass Timeweb, Saby, marketplaces, mail, payment or runner AWS secrets.
    env = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "CGO_ENABLED", "LANG") if key in os.environ}
    parsed = urllib.parse.urlsplit(settings["DATABASE_URL"])
    query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True)
    verify = settings.get("DATABASE_SSL_VERIFY", "true").strip().lower() not in ("false", "0")
    query["sslmode"] = ["verify-full" if verify else "require"]
    ca = settings.get("DATABASE_SSL_CA", "").strip().strip('"\'').replace("\\n", "\n")
    if ca:
        path = Path(directory) / "migration-root.crt"
        path.touch(mode=0o600, exist_ok=True)
        path.write_text(ca)
        os.chmod(path, 0o600)
        query["sslrootcert"] = [str(path)]
    query["connect_timeout"] = ["20"]
    options = "-c statement_timeout=1800000 -c lock_timeout=3000"
    if not apply:
        options += " -c default_transaction_read_only=on"
    query["options"] = [options]
    dsn = urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query, doseq=True)))
    env.update(GOWORK="off", GOENV="off", APP_ENV="production", FILE_STORAGE_MODE="s3", CATALOG_PUBLICATION_MODE="disabled",
               CATALOG_PUBLICATION_LIVE_ENABLED="false", DATABASE_URL=dsn,
               PRIVATE_S3_BUCKET=BUCKET, PRIVATE_S3_ENDPOINT=storage["endpoint"], PRIVATE_S3_REGION=storage["region"],
               PRIVATE_S3_ACCESS_KEY=storage["access"], PRIVATE_S3_SECRET_KEY=storage["secret"])
    return env


def run_cli(source, env, apply, run=subprocess.run, *, kind=None):
    if kind is not None and kind not in KINDS or apply and kind is None:
        raise MigrationError("migration_kind_not_allowed")
    kinds = (kind,) if kind is not None else KINDS
    limit = 1 if apply else LIMIT
    command = ["go", "run", "./cmd/migrate-file-assets", "--kinds", ",".join(kinds), "--limit", str(limit)]
    if apply:
        command.append("--apply")
    child = run(command, cwd=source / "backend", env=env, capture_output=True, timeout=1850, check=False)
    # stdout includes private row IDs/checksums; stderr may include a DSN. Neither
    # is forwarded, including on process failure, JSON failure or timeout.
    if len(child.stdout) > MAX_OUTPUT or len(child.stderr) > MAX_OUTPUT:
        raise MigrationError("child_output_bound_exceeded")
    try:
        payload = json.loads(child.stdout)
    except Exception:
        raise MigrationError("migration_child_invalid_output") from None
    if not isinstance(payload, dict) or payload.get("dryRun") is not (not apply) or payload.get("sourceBytesDeleted") is not False:
        raise MigrationError("migration_child_contract_changed")
    files = payload.get("files")
    if not isinstance(files, list) or len(files) > len(kinds) * limit:
        raise MigrationError("migration_child_file_bound_exceeded")
    allowed = {"copied-verified-source-retained", "changed-concurrently-skipped"} if apply else {"would-copy"}
    aggregate = {kind: {"count": 0, "bytes": 0, "copied": 0, "skipped": 0} for kind in KINDS}
    seen = set()
    for record in files:
        if not isinstance(record, dict):
            raise MigrationError("migration_child_record_invalid")
        kind, identity, size, sha, status = (record.get(k) for k in ("kind", "id", "bytes", "sha256", "status"))
        if (kind not in kinds or not isinstance(identity, int) or isinstance(identity, bool) or identity <= 0
                or not isinstance(size, int) or isinstance(size, bool) or not 0 <= size <= MAX_FILE
                or not isinstance(sha, str) or not re.fullmatch(r"[0-9a-f]{64}", sha) or status not in allowed
                or (kind, identity) in seen):
            raise MigrationError("migration_child_record_invalid")
        seen.add((kind, identity))
        item = aggregate[kind]
        item["count"] += 1
        item["bytes"] += size
        item["copied"] += status == "copied-verified-source-retained"
        item["skipped"] += status == "changed-concurrently-skipped"
        if item["count"] > limit or sum(i["bytes"] for i in aggregate.values()) > (MAX_FILE if apply else MAX_TOTAL):
            raise MigrationError("migration_child_total_exceeds_cap")
    return aggregate, child.returncode == 0 and "error" not in payload


def execute(reader, connect, storage, source, migrations, expected_commit, directory, apply=False, runner=run_cli, diagnostics=None):
    diagnostics = diagnostics if diagnostics is not None else {}
    def phase(value):
        diagnostics["phase"] = value
    phase("production_guards")
    settings, private = check_app(reader.get(f"/apps/{APP_ID}").get("app", {}), expected_commit)
    check_bucket(reader.get(f"/storages/buckets/{BUCKET_ID}").get("bucket"))
    phase("release_migrations_and_legacy_inventory")
    with connect(settings) as connection:
        inventory = inspect_database(connection, settings, migrations)
    phase("private_storage_capacity_and_privacy")
    usage = storage_usage(storage)
    total = sum(item["bytes"] for item in inventory.values())
    if usage["bytes"] + total > CAPACITY:
        raise MigrationError("migration_exceeds_private_bucket_capacity")
    phase("migration_dry_run")
    planned, ok = runner(source, child_environment(settings, private, directory, False), False)
    if not ok or any((planned[kind]["count"], planned[kind]["bytes"]) != (inventory[kind]["count"], inventory[kind]["bytes"]) for kind in KINDS):
        raise MigrationError("dry_run_incomplete_or_source_changed")
    report = {"ok": True, "dryRun": not apply, "sourceBytesDeleted": False, "financeExcluded": True,
              "existingStorage": usage, "files": planned, "failed": 0}
    if not apply:
        return report
    phase("migration_apply")
    applied = {kind: {"count": 0, "bytes": 0, "copied": 0, "skipped": 0} for kind in KINDS}
    report.update(files=applied, plannedFiles=planned, complete=False, calls=0,
                  chargedBytes=0, uncertainBytes=0, stopReason="complete")
    env = child_environment(settings, private, directory, True)
    # The deployed Save rejects >64 MiB BEFORE PUT. One explicit kind and
    # --limit 1 therefore turn this reservation into a hard per-call ceiling,
    # including a concurrent replacement after the read-only preflight.
    for kind in KINDS:
        while True:
            if report["chargedBytes"] + MAX_FILE > MAX_TOTAL:
                report["stopReason"] = "batch_budget"
                return report
            if report["calls"] >= len(KINDS) * (LIMIT + 1):
                report["stopReason"] = "call_limit"
                return report
            usage = storage_usage(storage)
            if usage["bytes"] + MAX_FILE >= CAPACITY:
                report["stopReason"] = "storage_capacity"
                report["existingStorage"] = usage
                return report
            # Hold the whole reservation until the single-row result is known.
            report["chargedBytes"] += MAX_FILE
            report["calls"] += 1
            try:
                copied, ok = runner(source, env, True, kind=kind)
            except Exception:
                # PUT/commit acknowledgement may have been lost. Retain the
                # reservation, never replay automatically, never expose output.
                report.update(ok=False, failed=1, stopReason="child_result_unknown")
                report["uncertainBytes"] += MAX_FILE
                return report
            if not ok:
                report.update(ok=False, failed=1, stopReason="child_failed")
                report["uncertainBytes"] += MAX_FILE
                return report
            actual = sum(item["bytes"] for item in copied.values())
            if actual > MAX_FILE or any(copied[other]["count"] for other in KINDS if other != kind) or copied[kind]["count"] > 1:
                report.update(ok=False, failed=1, stopReason="child_contract_changed")
                report["uncertainBytes"] += MAX_FILE
                return report
            report["chargedBytes"] -= MAX_FILE - actual
            for field in ("count", "bytes", "copied", "skipped"):
                applied[kind][field] += copied[kind][field]
            if copied[kind]["count"] == 0:
                break
    report["complete"] = True
    return report



def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    diagnostics = {"phase": "github_actions_guard"}
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise MigrationError("github_actions_only")
        source, migrations = check_source(os.environ.get("RELEASE_SOURCE_DIR", ""), args.expected_commit)
        reader = Reader(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""))
        settings, private = check_app(reader.get(f"/apps/{APP_ID}").get("app", {}), args.expected_commit)
        import boto3
        from botocore.config import Config
        storage = boto3.session.Session().client("s3", endpoint_url=private["endpoint"], region_name=private["region"],
            aws_access_key_id=private["access"], aws_secret_access_key=private["secret"],
            config=Config(signature_version="s3v4", connect_timeout=5, read_timeout=10,
                          retries={"total_max_attempts": 1}, s3={"addressing_style": "path"}))
        try:
            with tempfile.TemporaryDirectory(prefix="ficusin-production-file-migration-") as directory:
                report = execute(reader, lambda env: guards.connect_production(env, directory), storage,
                                 source, migrations, args.expected_commit, directory, args.apply,
                                 diagnostics=diagnostics)
        finally:
            storage.close()
        print(json.dumps(report, sort_keys=True))
        return 0 if report["ok"] else 2
    except Exception as error:
        # Fixed identifiers only. Never str(error), child stdout/stderr, SDK/DB
        # responses, source rows, DSNs, object keys, file hashes or credentials.
        report = {"ok": False, "failed": 1, "phase": diagnostics["phase"],
                  "errorClass": type(error).__name__, "sourceBytesDeleted": False, "dryRun": not args.apply}
        code = getattr(error, "safe_code", None)
        if isinstance(code, str) and re.fullmatch(r"[a-z_]{1,80}", code):
            report["reason"] = code
        print(json.dumps(report, sort_keys=True))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
