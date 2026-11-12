"""Guarded storage-mode operation for the existing isolated staging app only."""
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

import psycopg

APP_ID = 280757
APP_PATH = f"/api/v1/apps/{APP_ID}"
DATABASE = "ficusin_staging_20261031"
DATABASE_USER = "ficusin_stage_20261031"
EXPECTED_SITE = "https://avpavlo8-ficusin-store-1b1b.twc1.net"
MODES = {
    "FILE_STORAGE_MODE": "mock",
    "CATALOG_PUBLICATION_MODE": "mock",
    "CATALOG_PUBLICATION_LIVE_ENABLED": "false",
}


def api(method, data=None):
    assert method in ("GET", "PATCH")
    request = urllib.request.Request(
        "https://api.timeweb.cloud" + APP_PATH,
        method=method,
        headers={"Authorization": "Bearer " + os.environ["TIMEWEB_CLOUD_TOKEN"],
                 "Content-Type": "application/json"},
        data=None if data is None else json.dumps(data).encode(),
    )
    try:
        with urllib.request.urlopen(request, timeout=45) as response:
            return json.load(response)["app"]
    except urllib.error.HTTPError as error:
        # Response bodies and environment dictionaries may contain credentials.
        raise RuntimeError(f"Staging app request returned HTTP {error.code}") from None


def guarded_env(app):
    assert app["id"] == APP_ID, "Unexpected application ID"
    assert app["name"] == "ficusin-staging", "Unexpected application name"
    env = app["envs"]
    assert isinstance(env, dict), "Unexpected environment representation"
    assert env.get("APP_ENV") == "staging", "Not a staging environment"
    assert env.get("SITE_URL", "").rstrip("/") == EXPECTED_SITE, "Unexpected staging origin"
    database = urllib.parse.urlsplit(env["DATABASE_URL"])
    assert database.scheme in ("postgres", "postgresql"), "Unexpected database URL type"
    assert urllib.parse.unquote(database.path) == "/" + DATABASE, "Unexpected database name"
    assert urllib.parse.unquote(database.username or "") == DATABASE_USER, "Unexpected database user"
    assert database.hostname, "Database host is missing"
    return env


def verify_actual_database(env):
    try:
        with psycopg.connect(env["DATABASE_URL"], connect_timeout=20,
                             options="-c default_transaction_read_only=on") as connection:
            name, user, readonly = connection.execute(
                "SELECT current_database(), current_user, current_setting('transaction_read_only')"
            ).fetchone()
            superuser, create_role, create_db = connection.execute(
                "SELECT rolsuper,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname=current_user"
            ).fetchone()
            assert name == DATABASE and user == DATABASE_USER and readonly == "on"
            assert not (superuser or create_role or create_db), "Staging database role is privileged"
    except Exception as error:
        # Driver errors can include connection details. Never log their contents.
        print("Actual staging database check failed:", type(error).__name__, flush=True)
        sys.exit(1)
    print("Actual staging database verified:", json.dumps({
        "database": name, "user": user, "readOnlyCheck": True,
        "superuser": False, "createRole": False, "createDatabase": False,
    }), flush=True)


def report(label, app, env):
    print(label, json.dumps({
        "appId": app["id"], "appName": app["name"], "status": app.get("status"),
        "branch": app.get("branch_name"), "APP_ENV": env["APP_ENV"],
        "database": DATABASE, "databaseUser": DATABASE_USER,
        "healthCheckEmpty": not app.get("health_check"),
        **{key: env.get(key, "<unset>") for key in MODES},
    }), flush=True)


before = api("GET")
envs_before = guarded_env(before)
verify_actual_database(envs_before)
report("Before", before, envs_before)
if os.environ.get("APPLY_STAGING_MODES") == "true":
    next_env = dict(envs_before)
    next_env.update(MODES)
    if next_env != envs_before or before.get("health_check"):
        api("PATCH", {"envs": next_env, "health_check": ""})
    after = api("GET")
    envs_after = guarded_env(after)
    assert all(envs_after.get(key) == value for key, value in MODES.items()), "Staging modes did not persist"
    assert not after.get("health_check"), "Unexpected container health check"
    assert {k: v for k, v in envs_after.items() if k not in MODES} == {
        k: v for k, v in envs_before.items() if k not in MODES
    }, "An unrelated environment setting changed"
    assert after.get("branch_name") == before.get("branch_name"), "Application branch changed"
    report("After", after, envs_after)
else:
    print("Read-only inspection complete; no environment changes requested.", flush=True)
