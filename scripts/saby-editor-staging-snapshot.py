"""Verify Saby links and optionally cache editor metadata in isolated staging.

GHA only. Default: read-only dry run; --apply permits ONLY the cache UPSERT.
No product, variant, mirror, draft, ACL, session or environment changes. Saby
calls are restricted to authentication and the three verified read methods.
Names and raw records are never printed. Existing cache survives every failure.
"""

import argparse
from collections import Counter
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

APP_ID = 280757
APP_NAME = "ficusin-staging"
DATABASE = "ficusin_staging_20261031"
LOGIN = "ficusin_stage_20261031"
MAX_RESPONSE = 16 * 1024 * 1024
MAX_CANDIDATES = 10000
AUTH_URL = "https://online.sbis.ru/oauth/service/"
RPC_URL = "https://online.sbis.ru/service/?srv=1"
READ_METHODS = frozenset(("Номенклатура.Прочитать", "NomenclatureCategory.Read", "Nomenclature.Read"))

# Use only active, explicit Saby code/UUID mappings. A storefront category or
# name match must never invent an external identity. Include nonnumeric links
# in uniqueness checks, even though they cannot themselves be fetched by ID.
LINKS_SQL = """
SELECT d.variant_id, d.product_id, d.saby_id,
       ARRAY(SELECT DISTINCT m.external_id FROM public.product_external_ids m
             WHERE m.product_id=d.product_id
               AND (m.variant_id=d.variant_id OR m.variant_id IS NULL)
               AND m.provider='saby' AND m.status='active' AND m.id_type='code'),
       ARRAY(SELECT DISTINCT m.external_id FROM public.product_external_ids m
             WHERE m.product_id=d.product_id
               AND (m.variant_id=d.variant_id OR m.variant_id IS NULL)
               AND m.provider='saby' AND m.status='active' AND m.id_type='alias')
FROM public.canonical_product_directory d
WHERE d.active AND NULLIF(BTRIM(d.saby_id),'') IS NOT NULL
ORDER BY d.variant_id LIMIT 10001
"""
CACHE_SQL = """
INSERT INTO public.catalog_editor_references(channel,kind,scope,payload,fetched_at)
VALUES('saby','mock/linked-card',%s,%s::jsonb,CURRENT_TIMESTAMP)
ON CONFLICT(channel,kind,scope) DO UPDATE
SET payload=EXCLUDED.payload,fetched_at=CURRENT_TIMESTAMP
"""


class SnapshotError(Exception):
    """A fixed diagnostic code only; never include server messages/secrets."""


class BudgetExpired(SnapshotError):
    pass


@dataclass(frozen=True)
class Link:
    variant_id: int
    product_id: int
    id: str
    code: str
    uuid: str


def numeric_id(value):
    if isinstance(value, bool):
        raise SnapshotError("invalid_id")
    text = str(value)
    if not re.fullmatch(r"[1-9][0-9]{0,18}", text) or int(text) > 9223372036854775807:
        raise SnapshotError("invalid_id")
    return text


def uuid_id(value):
    try:
        text = str(value)
        result = str(uuid.UUID(text))
        return result if text.lower() == result else None
    except (ValueError, TypeError, AttributeError):
        return None


def eligible_links(rows):
    """Resolve only unique canonical ID + explicit code + active UUID alias."""
    if len(rows) > MAX_CANDIDATES:
        raise SnapshotError("too_many_link_rows")
    prepared = []
    all_ids, all_codes, all_uuids = Counter(), Counter(), Counter()
    for variant, product, remote, codes, aliases in rows:
        remote = str(remote).strip()
        codes = {str(code).strip() for code in codes if str(code).strip()}
        aliases = {parsed for value in aliases if (parsed := uuid_id(value))}
        prepared.append((variant, product, remote, codes, aliases))
        all_ids[remote] += 1
        all_codes.update(codes)
        all_uuids.update(aliases)
    result, skipped = [], 0
    for variant, product, remote, codes, aliases in prepared:
        try:
            remote = numeric_id(remote)
        except SnapshotError:
            skipped += 1
            continue
        if len(codes) != 1 or len(aliases) != 1:
            skipped += 1
            continue
        code, alias = next(iter(codes)), next(iter(aliases))
        if all_ids[remote] != 1 or all_codes[code] != 1 or all_uuids[alias] != 1:
            skipped += 1
            continue
        result.append(Link(variant, product, remote, code, alias))
    return result, skipped


def record_fields(record):
    if not isinstance(record, dict) or record.get("_type") != "record":
        raise SnapshotError("invalid_record")
    schema, values = record.get("s"), record.get("d")
    if not isinstance(schema, list) or not isinstance(values, list) or len(schema) != len(values):
        raise SnapshotError("invalid_record")
    fields = {}
    for column, value in zip(schema, values):
        if not isinstance(column, dict) or not isinstance(column.get("n"), str) or column["n"] in fields:
            raise SnapshotError("invalid_record")
        fields[column["n"]] = (column, value)
    return fields


def field(fields, name):
    if name not in fields:
        raise SnapshotError("missing_field")
    return fields[name][1]


def hierarchy_ref(fields, name):
    if name not in fields:
        raise SnapshotError("missing_reference")
    schema, value = fields[name]
    if schema.get("t") != "Идентификатор" or schema.get("s") != "Иерархия":
        raise SnapshotError("invalid_reference_schema")
    if not isinstance(value, list) or len(value) != 1:
        raise SnapshotError("invalid_reference")
    return numeric_id(value[0])


def name_field(fields, key):
    name = field(fields, key)
    if not isinstance(name, str) or not name.strip() or len(name) > 1000:
        raise SnapshotError("invalid_name")
    return name.strip()


def verify_card(record, link):
    fields = record_fields(record)
    if (numeric_id(field(fields, "@Номенклатура")) != link.id
            or field(fields, "НомНомер") != link.code
            or uuid_id(field(fields, "Идентификатор")) != link.uuid):
        raise SnapshotError("identity_mismatch")
    return hierarchy_ref(fields, "Категория"), hierarchy_ref(fields, "Раздел")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Never forward service/Timeweb tokens to an unexpected destination.
        return None


def mask(value):
    escaped = value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
    print("::add-mask::" + escaped, flush=True)


class Reader:
    def __init__(self, deadline, delay):
        self.deadline, self.delay, self.last_call = deadline, delay, 0.0
        self.opener = urllib.request.build_opener(NoRedirect())
        self.token = None
        self.categories, self.folders = {}, {}

    def remaining(self):
        remaining = self.deadline - time.monotonic()
        if remaining < 1:
            raise BudgetExpired("time_budget")
        return remaining

    def json_request(self, url, body=None, headers=None):
        pause = self.delay - (time.monotonic() - self.last_call)
        if pause > 0:
            if self.remaining() <= pause + 1:
                raise BudgetExpired("time_budget")
            time.sleep(pause)
        self.last_call = time.monotonic()
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
        request = urllib.request.Request(url, data=data, headers={
            "Accept": "application/json", "Content-Type": "application/json;charset=utf-8", **(headers or {})
        }, method="GET" if body is None else "POST")
        try:
            with self.opener.open(request, timeout=min(30, self.remaining())) as response:
                raw = response.read(MAX_RESPONSE + 1)
                charset = response.headers.get_content_charset() or "utf-8"
            if len(raw) > MAX_RESPONSE:
                raise SnapshotError("response_too_large")
            try:
                text = raw.decode(charset)
            except UnicodeDecodeError:
                text = raw.decode("cp1251")
            result = json.loads(text)
            if not isinstance(result, dict):
                raise SnapshotError("invalid_json_shape")
            return result
        except SnapshotError:
            raise
        except Exception:
            # urllib/database exceptions can contain credentials, response bodies
            # or remote error details. None belong in CI logs.
            raise SnapshotError("request_failed") from None

    def authenticate(self):
        body = {"app_client_id": os.environ["SABY_APP_CLIENT_ID"],
                "app_secret": os.environ["SABY_APP_SECRET"], "secret_key": os.environ["SABY_SECRET_KEY"]}
        token = self.json_request(AUTH_URL, body).get("token")
        if not isinstance(token, str) or not token:
            raise SnapshotError("authentication_failed")
        mask(token)
        self.token = token

    def rpc(self, method, params):
        if method not in READ_METHODS or not self.token:
            raise SnapshotError("rpc_not_allowed")
        reply = self.json_request(RPC_URL, {
            "jsonrpc": "2.0", "protocol": 6, "method": method, "params": params, "id": 1,
        }, {"X-SBISAccessToken": self.token, "Content-Type": "application/json-rpc;charset=utf-8"})
        if reply.get("error") or not isinstance(reply.get("result"), dict):
            raise SnapshotError("rpc_rejected")
        return reply["result"]

    def snapshot(self, link):
        category_id, folder_id = verify_card(self.rpc("Номенклатура.Прочитать", {
            "ИдО": int(link.id), "Расширение": None,
        }), link)
        if category_id not in self.categories:
            category = record_fields(self.rpc("NomenclatureCategory.Read", {
                "Id": int(category_id), "AdditionalFields": ["Name", "Identifier"], "Option": None,
            }))
            if numeric_id(field(category, "Id")) != category_id:
                raise SnapshotError("category_identity_mismatch")
            self.categories[category_id] = name_field(category, "Name")
        if folder_id not in self.folders:
            folder = record_fields(self.rpc("Nomenclature.Read", {"ИдО": int(folder_id), "ИмяМетода": None}))
            if numeric_id(field(folder, "@Номенклатура")) != folder_id:
                raise SnapshotError("folder_identity_mismatch")
            self.folders[folder_id] = name_field(folder, "Наименование")
        return {"id": link.id, "code": link.code, "uuid": link.uuid,
                "category": {"id": category_id, "name": self.categories[category_id]},
                "folder": {"id": folder_id, "name": self.folders[folder_id]}}


def stage_settings(reader):
    app = reader.json_request(f"https://api.timeweb.cloud/api/v1/apps/{APP_ID}", headers={
        "Authorization": "Bearer " + os.environ["TIMEWEB_CLOUD_TOKEN"],
    }).get("app", {})
    if app.get("id") != APP_ID or app.get("name") != APP_NAME:
        raise SnapshotError("wrong_application")
    env = app.get("envs", {})
    if not isinstance(env, dict) or env.get("APP_ENV") != "staging":
        raise SnapshotError("wrong_environment")
    if env.get("CATALOG_PUBLICATION_MODE") != "mock" or str(env.get("CATALOG_PUBLICATION_LIVE_ENABLED", "")).lower() != "false":
        raise SnapshotError("unsafe_publication_mode")
    parsed = urllib.parse.urlsplit(env.get("DATABASE_URL", ""))
    if (parsed.scheme not in ("postgres", "postgresql") or parsed.path != "/" + DATABASE
            or urllib.parse.unquote(parsed.username or "") != LOGIN or not parsed.hostname):
        raise SnapshotError("wrong_database_configuration")
    return env


def connect_stage(settings, directory):
    import psycopg  # Only the GHA operational entry point needs this dependency.
    options = {"connect_timeout": 20, "autocommit": True,
               "options": "-c statement_timeout=10000 -c lock_timeout=3000 -c default_transaction_read_only=on"}
    verify = str(settings.get("DATABASE_SSL_VERIFY", "true")).lower() not in ("false", "0")
    options["sslmode"] = "verify-full" if verify else "require"
    ca = settings.get("DATABASE_SSL_CA", "").strip().strip('"\'').replace("\\n", "\n")
    if ca:
        path = Path(directory) / "staging-root.crt"
        path.touch(mode=0o600)
        path.write_text(ca)
        options["sslrootcert"] = str(path)
    return psycopg.connect(settings["DATABASE_URL"], **options)


def validate_database(connection):
    if connection.execute("SELECT current_database(),current_user").fetchone() != (DATABASE, LOGIN):
        raise SnapshotError("wrong_database_identity")
    role = connection.execute("SELECT rolsuper,rolcreatedb,rolcreaterole FROM pg_roles WHERE rolname=current_user").fetchone()
    if role != (False, False, False):
        raise SnapshotError("unsafe_database_role")


def load_links(connection):
    return eligible_links(connection.execute(LINKS_SQL).fetchall())


def cache_snapshot(connection, link, payload):
    # No network work inside this short transaction. Lock the existing source
    # identity rows so concurrent edits cannot change them between check/upsert.
    with connection.transaction():
        connection.execute("SET TRANSACTION READ WRITE")
        validate_database(connection)
        connection.execute("SELECT id FROM public.products WHERE id=%s FOR SHARE", (link.product_id,)).fetchall()
        connection.execute("SELECT id FROM public.product_variants WHERE id=%s FOR SHARE", (link.variant_id,)).fetchall()
        connection.execute("SELECT id FROM public.product_external_ids WHERE product_id=%s AND provider='saby' AND status='active' FOR SHARE", (link.product_id,)).fetchall()
        current, _ = load_links(connection)
        if link not in current:
            raise SnapshotError("link_changed")
        connection.execute(CACHE_SQL, (link.id, json.dumps(payload, ensure_ascii=False)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true", help="Write verified snapshots to the isolated staging editor cache")
    parser.add_argument("--max-items", type=int, default=1000)
    parser.add_argument("--time-budget-seconds", type=int, default=600)
    args = parser.parse_args()
    if not 1 <= args.max_items <= 1000 or not 1 <= args.time_budget_seconds <= 600:
        parser.error("max-items must be 1..1000; time-budget-seconds must be 1..600")
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise SnapshotError("github_actions_only")
    for key in ("SABY_APP_CLIENT_ID", "SABY_APP_SECRET", "SABY_SECRET_KEY", "TIMEWEB_CLOUD_TOKEN"):
        if not os.environ.get(key):
            raise SnapshotError("missing_credentials")
        mask(os.environ[key])
    reader = Reader(time.monotonic() + args.time_budget_seconds, 0.1)
    stats = Counter()
    requested = {"id": "1101", "eligible": False, "identityVerified": False, "cached": False}
    settings = stage_settings(reader)
    with tempfile.TemporaryDirectory(prefix="ficusin-stage-saby-") as temporary:
        with connect_stage(settings, temporary) as connection:
            validate_database(connection)
            links, stats["skippedIdentity"] = load_links(connection)
            stats["eligible"] = len(links)
            requested["eligible"] = any(link.id == "1101" for link in links)
            reader.authenticate()
            for link in links[:args.max_items]:
                try:
                    reader.remaining()
                    stats["attempted"] += 1
                    payload = reader.snapshot(link)
                    stats["verified"] += 1
                    if link.id == "1101":
                        requested["identityVerified"] = True
                    if args.apply:
                        reader.remaining()
                        cache_snapshot(connection, link, payload)
                        stats["cached"] += 1
                        if link.id == "1101":
                            requested["cached"] = True
                except BudgetExpired:
                    stats["budgetExpired"] += 1
                    break
                except SnapshotError as error:
                    stats[str(error)] += 1
                except Exception:
                    stats["database_or_processing_error"] += 1
                if stats["attempted"] % 50 == 0:
                    print(json.dumps({"progress": dict(stats)}), flush=True)
            stats["notAttempted"] = len(links) - stats["attempted"]
    complete = stats["notAttempted"] == 0 and stats["verified"] == stats["eligible"]
    print(json.dumps({"mode": "apply" if args.apply else "dry-run", "complete": complete,
                      "counts": dict(stats), "requested": requested}, ensure_ascii=True), flush=True)
    return 0 if complete and (not args.apply or stats["cached"] == stats["verified"]) else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SnapshotError as error:
        print(json.dumps({"error": str(error)}), flush=True)
        raise SystemExit(2) from None
    except Exception:
        print('{"error":"operation_failed_no_sensitive_details_logged"}', flush=True)
        raise SystemExit(2) from None
