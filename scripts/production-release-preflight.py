#!/usr/bin/env python3
"""Read-only release inventory for the exact Ficusin production app/database.

Never creates backups, changes ACLs, updates settings or executes SQL writes.
Only aggregate counts and whitelisted backup metadata may reach stdout.
"""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.error
import urllib.parse
import urllib.request

APP_ID = 229223
APP_NAME = "ficusin-store"
DATABASE_ID = 4192781
DATABASE = "default_db"
EXPECTED_COMMIT = "15989cc1f2d46baad54ce7ebbf379531c86c7f23"
API = "https://api.timeweb.cloud/api/v1"
MAX_RESPONSE = 4 << 20
PENDING = ("prepared", "queued", "processing")


class PreflightError(Exception):
    def __init__(self, code):
        self.safe_code = code if isinstance(code, str) and re.fullmatch(r"[a-z_]{1,80}", code) else "preflight_guard_failed"
        super().__init__()


def mark_phase(diagnostics, phase):
    if diagnostics is not None:
        diagnostics["phase"] = phase


def safe_failure(error, diagnostics):
    report = {"ok": False, "error": "production_preflight_failed",
              "phase": diagnostics.get("phase", "unknown"),
              "exceptionClass": type(error).__name__}
    if isinstance(error, PreflightError):
        report["reason"] = error.safe_code
    try:
        state = getattr(error, "sqlstate", None)
    except Exception:
        state = None
    if isinstance(state, str) and re.fullmatch(r"[0-9A-Z]{5}", state):
        report["sqlstate"] = state
    if isinstance(error, urllib.error.HTTPError) and isinstance(error.code, int) and 100 <= error.code <= 599:
        report["httpStatus"] = error.code
    return report


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise PreflightError("unexpected_timeweb_redirect")


class TimewebReader:
    def __init__(self, token, opener=None):
        if not token:
            raise PreflightError("timeweb_token_missing")
        self.token = token
        self.opener = opener or urllib.request.build_opener(NoRedirect())

    def get(self, path):
        if path not in (f"/apps/{APP_ID}", f"/dbs/{DATABASE_ID}/backups"):
            raise PreflightError("timeweb_path_not_allowed")
        request = urllib.request.Request(API + path, method="GET", headers={
            "Authorization": "Bearer " + self.token, "Accept": "application/json",
        })
        with self.opener.open(request, timeout=30) as response:
            raw = response.read(MAX_RESPONSE + 1)
        if len(raw) > MAX_RESPONSE:
            raise PreflightError("timeweb_response_too_large")
        result = json.loads(raw)
        if not isinstance(result, dict):
            raise PreflightError("invalid_timeweb_response")
        return result


def production_settings(app, expected_commit):
    if not re.fullmatch(r"[0-9a-f]{40}", expected_commit):
        raise PreflightError("invalid_expected_commit")
    if app.get("id") != APP_ID or app.get("name") != APP_NAME:
        raise PreflightError("wrong_application")
    branch = app.get("branch_name")
    if not branch:
        alternate = app.get("branch")
        branch = alternate.get("name") if isinstance(alternate, dict) else alternate
    if branch not in (None, "main") or app.get("commit_sha") != expected_commit:
        raise PreflightError("wrong_production_commit")
    settings = app.get("envs")
    if not isinstance(settings, dict) or (settings.get("APP_ENV", "").strip() or "production") != "production":
        raise PreflightError("wrong_environment")
    parsed = urllib.parse.urlsplit(settings.get("DATABASE_URL", ""))
    if (parsed.scheme not in ("postgres", "postgresql") or parsed.path != "/" + DATABASE
            or not parsed.hostname or not parsed.username or parsed.fragment):
        raise PreflightError("wrong_database_configuration")
    forbidden = {"dbname", "database", "host", "hostaddr", "user", "port", "service", "options"}
    if forbidden.intersection(urllib.parse.parse_qs(parsed.query)):
        raise PreflightError("ambiguous_database_configuration")
    return settings


def connect_production(settings, directory, driver=None, diagnostics=None):
    if driver is None:
        mark_phase(diagnostics, "database_driver_import")
        import psycopg as driver
    mark_phase(diagnostics, "database_tls_configuration")
    options = {
        "connect_timeout": 20, "autocommit": False,
        "options": "-c statement_timeout=10000 -c lock_timeout=3000 -c default_transaction_read_only=on",
    }
    verify = settings.get("DATABASE_SSL_VERIFY", "true").strip().lower() not in ("false", "0")
    options["sslmode"] = "verify-full" if verify else "require"
    ca = settings.get("DATABASE_SSL_CA", "").strip().strip('"\'').replace("\\n", "\n")
    if ca:
        path = Path(directory) / "production-root.crt"
        path.touch(mode=0o600)
        path.write_text(ca)
        options["sslrootcert"] = str(path)
    mark_phase(diagnostics, "database_connect")
    connection = driver.connect(settings["DATABASE_URL"], **options)
    mark_phase(diagnostics, "database_set_readonly")
    connection.read_only = True
    return connection


def database_report(connection, settings, diagnostics=None):
    mark_phase(diagnostics, "database_identity")
    expected_user = urllib.parse.unquote(urllib.parse.urlsplit(settings["DATABASE_URL"]).username)
    identity = connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only'),current_setting('default_transaction_read_only')").fetchone()
    if identity != (DATABASE, expected_user, "on", "on"):
        raise PreflightError("wrong_database_identity_or_readonly_state")
    mark_phase(diagnostics, "database_ownership")
    owner, schema_access, superuser = connection.execute("""
      SELECT pg_get_userbyid(d.datdba)=current_user,
        has_schema_privilege(current_user,'public','USAGE'),
        (SELECT rolsuper FROM pg_roles WHERE rolname=current_user)
      FROM pg_database d WHERE d.datname=current_database()
    """).fetchone()
    mark_phase(diagnostics, "migrations_table_presence")
    migrations_exist = connection.execute("SELECT to_regclass('public.schema_migrations') IS NOT NULL").fetchone()[0]
    migrations = {"tableExists": bool(migrations_exist)}
    if migrations_exist:
        mark_phase(diagnostics, "migrations_inventory")
        count, latest = connection.execute("SELECT count(*),max(name) FROM public.schema_migrations").fetchone()
        migrations.update(count=int(count), latest=latest)
    mark_phase(diagnostics, "publications_table_presence")
    publications_exist = connection.execute("SELECT to_regclass('public.catalog_editor_publications') IS NOT NULL").fetchone()[0]
    publications = {"tableExists": bool(publications_exist), "pending": []}
    if publications_exist:
        mark_phase(diagnostics, "publications_mode_column")
        has_mode = connection.execute("SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='catalog_editor_publications' AND column_name='execution_mode')").fetchone()[0]
        publications["executionModeColumnExists"] = bool(has_mode)
        mark_phase(diagnostics, "publications_pending_counts")
        if has_mode:
            rows = connection.execute("SELECT execution_mode,status,count(*) FROM public.catalog_editor_publications WHERE status=ANY(%s) GROUP BY execution_mode,status ORDER BY execution_mode,status", (list(PENDING),)).fetchall()
        else:
            rows = [(None, status, count) for status, count in connection.execute("SELECT status,count(*) FROM public.catalog_editor_publications WHERE status=ANY(%s) GROUP BY status ORDER BY status", (list(PENDING),)).fetchall()]
        publications["pending"] = [{"executionMode": mode if mode in (None, "disabled", "mock", "live") else "unknown", "status": status, "count": int(count)} for mode, status, count in rows]
    # This is explicitly a conservative superset, not a duplicated stock/box
    # evaluation. Zero proves no old flagged CDEK order can be unlocked; nonzero
    # requires reviewing the canonical workflow facts before deployment.
    mark_phase(diagnostics, "shipping_candidate_counts")
    paid_cdek, flagged_preorders, candidate_upper_bound = connection.execute("""
      SELECT count(*),count(*) FILTER(WHERE has_preorder=1),
        count(*) FILTER(WHERE has_preorder=1 AND delivery_fee_pending=0 AND delivery_repack_requested=0
          AND COALESCE(cdek_tariff_code,0)>0 AND COALESCE(cdek_office_code,'')<>''
          AND cdek_create_state NOT IN ('manual_review','unknown','registered'))
      FROM public.orders WHERE status IN ('new','confirmed') AND payment_status='paid'
        AND delivery_method='cdek' AND COALESCE(cdek_uuid,'')=''
    """).fetchone()
    return {
        "databaseIdentityVerified": True, "transactionReadOnly": True,
        "databaseOwner": bool(owner), "publicSchemaUsage": bool(schema_access), "superuser": bool(superuser),
        "migrations": migrations, "catalogPublications": publications,
        "shipping": {"paidNewConfirmedUnshippedCdekCount": int(paid_cdek),
                     "flaggedPreorderCount": int(flagged_preorders),
                     "autoTransitionCandidateUpperBound": int(candidate_upper_bound),
                     "exactWorkflowReadRequired": candidate_upper_bound > 0},
    }


def safe_backups(payload):
    backups = payload.get("backups")
    if not isinstance(backups, list):
        raise PreflightError("invalid_backup_list")
    safe = []
    for item in backups:
        if not isinstance(item, dict):
            continue
        identifier = item.get("id")
        if not isinstance(identifier, (int, str)) or isinstance(identifier, bool) or not re.fullmatch(r"[0-9a-fA-F-]{1,64}", str(identifier)):
            identifier = None
        status = item.get("status")
        if not isinstance(status, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,40}", status):
            status = None
        created = item.get("created_at")
        try:
            created = dt.datetime.fromisoformat(created.replace("Z", "+00:00")).isoformat()
        except (AttributeError, TypeError, ValueError):
            created = None
        safe.append({"id": identifier, "status": status, "created_at": created})
    return {"available": True, "count": len(safe), "backups": safe}


def inspect(reader, connection_factory, expected_commit, diagnostics=None):
    mark_phase(diagnostics, "timeweb_application_get")
    app = reader.get(f"/apps/{APP_ID}").get("app", {})
    mark_phase(diagnostics, "production_application_guards")
    settings = production_settings(app, expected_commit)
    mark_phase(diagnostics, "database_connect")
    connection = connection_factory(settings)
    try:
        if not connection.read_only:
            raise PreflightError("database_connection_not_readonly")
        mark_phase(diagnostics, "database_readonly_transaction")
        with connection.transaction():
            report = database_report(connection, settings, diagnostics)
    finally:
        connection.close()
    try:
        mark_phase(diagnostics, "timeweb_backup_list_get")
        payload = reader.get(f"/dbs/{DATABASE_ID}/backups")
        mark_phase(diagnostics, "backup_metadata_whitelist")
        backups = safe_backups(payload)
    except urllib.error.HTTPError as error:
        backups = {"available": False, "httpStatus": error.code}
    report.update(appId=APP_ID, appName=APP_NAME, commit=expected_commit, backups=backups)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", default=EXPECTED_COMMIT, help="Exact deployed main commit, including an explicitly selected post-release SHA")
    args = parser.parse_args()
    diagnostics = {"phase": "github_actions_guard"}
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise PreflightError("github_actions_only")
        mark_phase(diagnostics, "timeweb_reader_configuration")
        reader = TimewebReader(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""))
        with tempfile.TemporaryDirectory(prefix="ficusin-production-preflight-") as directory:
            report = inspect(reader, lambda settings: connect_production(settings, directory, diagnostics=diagnostics), args.expected_commit, diagnostics)
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))
        return 0
    except Exception as error:
        # Driver/HTTP exceptions may embed credentials, DSNs or server data.
        print(json.dumps(safe_failure(error, diagnostics)))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
