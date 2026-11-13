#!/usr/bin/env python3
"""Enable only future explicit editor publications on the deployed production SHA.

GHA only. Dry-run unless --apply. No vendor requests, SQL writes or job promotion.
The only permitted mutation is one app env PATCH changing the two live flags.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import urllib.request

def sibling(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result

guards = sibling("activation_guards", "production-release-preflight.py")
marketplaces = sibling("activation_credentials", "production-marketplace-readiness.py")
FLAGS = {"CATALOG_PUBLICATION_MODE": "live", "CATALOG_PUBLICATION_LIVE_ENABLED": "true"}
MIGRATION = "20261113_catalog_editor_saby_publication.sql"


class Provider:
    def __init__(self, token, apply=False, opener=None):
        if not token:
            raise guards.PreflightError("timeweb_token_missing")
        self.token, self.apply, self.patch_attempted = token, apply, False
        self.opener = opener or urllib.request.build_opener(guards.NoRedirect())
        self.last_environment = None

    def get(self):
        result = self._request("GET")
        app = result.get("app", {})
        self.last_environment = dict(app["envs"]) if isinstance(app.get("envs"), dict) else None
        return app

    def patch(self, updated):
        if (not self.apply or self.patch_attempted or self.last_environment is None
                or updated != {**self.last_environment, **FLAGS}):
            raise guards.PreflightError("environment_patch_not_allowed")
        self.patch_attempted = True
        return self._request("PATCH", {"envs": updated})

    def _request(self, method, body=None):
        request = urllib.request.Request(guards.API + f"/apps/{guards.APP_ID}", method=method,
            data=None if body is None else json.dumps(body, ensure_ascii=False).encode(),
            headers={"Authorization": "Bearer " + self.token, "Accept": "application/json",
                     "Content-Type": "application/json;charset=utf-8"})
        with self.opener.open(request, timeout=30) as response:
            raw = response.read(guards.MAX_RESPONSE + 1)
        if len(raw) > guards.MAX_RESPONSE:
            raise guards.PreflightError("timeweb_response_too_large")
        result = json.loads(raw) if raw else {}
        if not isinstance(result, dict):
            raise guards.PreflightError("invalid_timeweb_response")
        return result


def check_app(app, expected_commit):
    if expected_commit == guards.EXPECTED_COMMIT:
        raise guards.PreflightError("old_production_revision_not_eligible")
    env = guards.production_settings(app, expected_commit)
    branch = app.get("branch_name") or app.get("branch")
    if isinstance(branch, dict):
        branch = branch.get("name")
    if branch != "main" or app.get("status") != "active":
        raise guards.PreflightError("production_not_active_main")
    if any(not isinstance(k, str) or not isinstance(v, str) for k, v in env.items()):
        raise guards.PreflightError("invalid_environment")
    credentials = marketplaces.effective_credentials(env)
    if not all(credentials.values()) or not all(env.get(k, "").strip() for k in ("SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY")):
        raise guards.PreflightError("publication_credentials_missing")
    if env.get("PRIVATE_S3_BUCKET", "").strip() != "ficusin-private":
        raise guards.PreflightError("wrong_private_bucket")
    if env.get("FILE_STORAGE_MODE", "").strip() not in ("", "s3") or env.get("LOCAL_PRIVATE_FILES_DIR", "").strip():
        raise guards.PreflightError("non_production_file_storage")
    if (env.get("CATALOG_PUBLICATION_MODE", "").strip() or "disabled") not in ("disabled", "live"):
        raise guards.PreflightError("unexpected_publication_mode")
    return dict(env)


def queue_inventory(connection, env):
    with connection.transaction():
        identity = connection.execute("SELECT current_database(),current_user,current_setting('transaction_read_only'),current_setting('default_transaction_read_only')").fetchone()
        import urllib.parse
        user = urllib.parse.unquote(urllib.parse.urlsplit(env["DATABASE_URL"]).username)
        if identity != (guards.DATABASE, user, "on", "on"):
            raise guards.PreflightError("wrong_database_identity_or_readonly_state")
        migrated = connection.execute("SELECT EXISTS(SELECT 1 FROM public.schema_migrations WHERE name=%s)", (MIGRATION,)).fetchone()[0]
        if not migrated:
            raise guards.PreflightError("native_publisher_migration_missing")
        rows = connection.execute("""SELECT execution_mode,status,count(*) FROM public.catalog_editor_publications
            WHERE status IN ('prepared','queued','processing','needs_attention')
            GROUP BY execution_mode,status ORDER BY execution_mode,status""").fetchall()
        result = []
        for mode, status, count in rows:
            if mode not in ("disabled", "mock", "live") or status not in ("prepared", "queued", "processing", "needs_attention") or not isinstance(count, int) or count < 0:
                raise guards.PreflightError("unexpected_queue_inventory")
            result.append({"mode": mode, "status": status, "count": count})
        if any(row["mode"] == "live" and row["count"] for row in result):
            raise guards.PreflightError("live_publication_queue_not_empty")
        return result


def activate(provider, connect, expected_commit, apply=False):
    env = check_app(provider.get(), expected_commit)
    connection = connect(env)
    try:
        pending = queue_inventory(connection, env)
    finally:
        connection.close()
    report = {"appId": guards.APP_ID, "commit": expected_commit, "dryRun": not apply,
              "pendingPublications": pending, "changes": FLAGS, "vendorWritesPerformed": False,
              "writePermissionsVerified": False, "patchAttempted": False}
    # Re-read immediately before the mutation, so a concurrent config change or
    # release is never overwritten with the first snapshot. No automatic retry.
    fresh = check_app(provider.get(), expected_commit)
    if fresh != env:
        raise guards.PreflightError("environment_changed_during_preflight")
    if all(env.get(key) == value for key, value in FLAGS.items()):
        report["alreadyEnabled"] = True
    elif apply:
        provider.patch({**fresh, **FLAGS})
        report["patchAttempted"] = True
        # PATCH can trigger deployment; do not claim the runtime has switched.
        report["runtimeVerificationRequired"] = True
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise guards.PreflightError("github_actions_only")
        provider = Provider(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""), args.apply)
        with tempfile.TemporaryDirectory(prefix="ficusin-publisher-activation-") as directory:
            report = activate(provider, lambda env: guards.connect_production(env, directory), args.expected_commit, args.apply)
        print(json.dumps(report, sort_keys=True))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error, {"phase": "publisher_activation"})))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
