"""Read only the three accounts visible at the supplied screenshot cutoff.

No contacts, names, credentials, or session token material belong in source or
public logs. stdout is piped directly into CMS encryption in the workflow.
"""
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime
from zoneinfo import ZoneInfo

import psycopg

MOSCOW = ZoneInfo("Europe/Moscow")


def timestamp(value):
    return value.astimezone(MOSCOW).isoformat(timespec="seconds") if value else None


def audit():
    request = urllib.request.Request(
        "https://api.timeweb.cloud/api/v1/apps/229223",
        headers={"Authorization": "Bearer " + os.environ["TIMEWEB_CLOUD_TOKEN"]},
        method="GET",
    )
    with urllib.request.urlopen(request, timeout=45) as response:
        app = json.load(response)["app"]
    assert app["id"] == 229223
    env = app["envs"]
    assert env.get("APP_ENV", "production") == "production"
    database = urllib.parse.urlsplit(env["DATABASE_URL"])
    assert database.scheme in ("postgres", "postgresql")
    assert database.path == "/default_db"
    with psycopg.connect(env["DATABASE_URL"], connect_timeout=20,
                         options="-c default_transaction_read_only=on -c statement_timeout=20000") as connection:
        actual_name, actual_user, readonly = connection.execute(
            "SELECT current_database(),current_user,current_setting('transaction_read_only')"
        ).fetchone()
        assert actual_name == urllib.parse.unquote(database.path[1:])
        assert actual_user == urllib.parse.unquote(database.username or "")
        assert readonly == "on"
        customers = connection.execute("""
            SELECT id,full_name,last_name,patronymic,created_at,consent_at,phone
            FROM customers
            WHERE created_at >= '2026-11-11 00:00:00+03'::timestamptz
              AND created_at <= '2026-11-12 19:02:00Z'::timestamptz
            ORDER BY created_at DESC,id DESC LIMIT 3
        """).fetchall()
        result = {"timezone": "Europe/Moscow", "readOnly": True, "customers": [], "orders": []}
        for customer_id, first_name, last_name, patronymic, created, consent, phone in customers:
            registration = connection.execute("""
                SELECT MIN(created_at),MAX(created_at),COUNT(*) FROM consent_events
                WHERE customer_id=%s AND event='registration'
            """, (customer_id,)).fetchone()
            sessions = connection.execute("""
                SELECT MIN(created_at),MAX(created_at),COUNT(*) FROM auth_sessions WHERE customer_id=%s
            """, (customer_id,)).fetchone()
            calls = connection.execute("""
                SELECT MIN(consumed_at),MAX(consumed_at),COUNT(consumed_at)
                FROM call_checks WHERE phone=%s AND consumed_at IS NOT NULL
            """, (phone,)).fetchone()
            result["customers"].append({
                "id": customer_id, "name": " ".join(part for part in (last_name,first_name,patronymic) if part),
                "createdAt": timestamp(created), "consentAt": timestamp(consent),
                "registrationConsentFirst": timestamp(registration[0]),
                "registrationConsentLast": timestamp(registration[1]), "registrationConsentCount": registration[2],
                "retainedSessionFirst": timestamp(sessions[0]), "retainedSessionLast": timestamp(sessions[1]),
                "retainedSessionCount": sessions[2], "verifiedCallFirst": timestamp(calls[0]),
                "verifiedCallLast": timestamp(calls[1]), "verifiedCallCount": calls[2],
            })
        customer_ids = [row[0] for row in customers]
        phones = [row[6] for row in customers]
        orders = connection.execute("""
            SELECT order_number,created_at,customer_id,status
            FROM orders WHERE customer_id=ANY(%s) OR phone=ANY(%s)
                OR order_number=ANY(%s)
            ORDER BY created_at,id
        """, (customer_ids, phones, ["0000-17","0000-18","0000-19","0000-20"])).fetchall()
        for number, created, owner, status in orders:
            result["orders"].append({"number": number, "createdAt": timestamp(created),
                                     "currentOwnerId": owner, "status": status})
    print(json.dumps(result, ensure_ascii=False), flush=True)
    print("Read-only customer audit succeeded; result encrypted for local recipient.", file=sys.stderr, flush=True)


try:
    audit()
except Exception as error:
    # Connection/API errors can embed connection details; emit only the type.
    print("Read-only customer audit failed:", type(error).__name__, file=sys.stderr, flush=True)
    sys.exit(1)
