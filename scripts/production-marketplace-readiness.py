#!/usr/bin/env python3
"""Read category dictionaries once per seller API. This is not write-scope proof.

Only the two exact read-only category requests from editor_references.go are
reachable. No catalogue creation, price changes, SQL or app mutations exist here.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location("production_release_guards", Path(__file__).with_name("production-release-preflight.py"))
guards = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(guards)

WB_CATEGORIES = "https://content-api.wildberries.ru/content/v2/object/all?locale=ru&limit=1000&offset=0&name="
OZON_CATEGORIES = "https://api-seller.ozon.ru/v1/description-category/tree"
MAX_RESPONSE = 8 << 20


def effective_credentials(settings):
    def effective(preferred, fallback):
        return (settings.get(preferred, "").strip() or settings.get(fallback, "").strip())
    return {"wb": effective("CATALOG_WB_API_TOKEN", "WB_API_TOKEN"),
            "ozonClient": effective("CATALOG_OZON_CLIENT_ID", "OZON_CLIENT_ID"),
            "ozonKey": effective("CATALOG_OZON_API_KEY", "OZON_API_KEY")}


def category_count(channel, payload):
    if not isinstance(payload, dict):
        raise guards.PreflightError("unexpected_schema")
    if payload.get("error") or payload.get("errors"):
        raise guards.PreflightError("vendor_error")
    rows = payload.get("data" if channel == "wb" else "result")
    if not isinstance(rows, list) or any(not isinstance(row, dict) for row in rows):
        raise guards.PreflightError("unexpected_schema")
    if channel == "wb":
        return len(rows)
    # Count dictionary tree nodes, not seller products or item identifiers.
    count, stack = 0, list(rows)
    while stack:
        row = stack.pop()
        count += 1
        children = row.get("children", [])
        if count > 100000 or not isinstance(children, list) or any(not isinstance(child, dict) for child in children):
            raise guards.PreflightError("unexpected_schema")
        stack.extend(children)
    return count


class CategoryReader:
    def __init__(self, opener=None):
        self.opener = opener or urllib.request.build_opener(guards.NoRedirect())

    def read(self, channel, credentials):
        if channel not in ("wb", "ozon"):
            raise guards.PreflightError("channel_not_allowed")
        configured = bool(credentials["wb"]) if channel == "wb" else bool(credentials["ozonClient"] and credentials["ozonKey"])
        result = {"configured": configured, "readOK": False, "count": None, "httpStatus": None, "errorCode": None}
        if not configured:
            result["errorCode"] = "not_configured"
            return result
        if channel == "wb":
            request = urllib.request.Request(WB_CATEGORIES, method="GET", headers={"Authorization": credentials["wb"], "Accept": "application/json"})
        elif channel == "ozon":
            request = urllib.request.Request(OZON_CATEGORIES, method="POST", data=json.dumps({"language": "RU"}).encode(), headers={
                "Client-Id": credentials["ozonClient"], "Api-Key": credentials["ozonKey"],
                "Content-Type": "application/json", "Accept": "application/json",
            })
        else:
            raise guards.PreflightError("channel_not_allowed")
        try:
            with self.opener.open(request, timeout=20) as response:
                result["httpStatus"] = response.status
                raw = response.read(MAX_RESPONSE + 1)
            if len(raw) > MAX_RESPONSE:
                raise guards.PreflightError("response_too_large")
            count = category_count(channel, json.loads(raw))
            result["count"] = count
            if count <= 0:
                raise guards.PreflightError("empty_categories")
            result["readOK"] = True
        except urllib.error.HTTPError as error:
            result["httpStatus"] = error.code
            result["errorCode"] = "redirect_blocked" if 300 <= error.code < 400 else "http_error"
        except guards.PreflightError as error:
            result["errorCode"] = "redirect_blocked" if error.safe_code == "unexpected_timeweb_redirect" else error.safe_code
        except (ValueError, UnicodeError):
            result["errorCode"] = "invalid_json"
        except Exception:
            # Never stringify vendor errors: they can echo authentication data.
            result["errorCode"] = "request_failed"
        return result


def inspect(timeweb_reader, category_reader, expected_commit):
    app = timeweb_reader.get(f"/apps/{guards.APP_ID}").get("app", {})
    settings = guards.production_settings(app, expected_commit)
    credentials = effective_credentials(settings)
    report = {"appId": guards.APP_ID, "commit": expected_commit,
              "credentialsPresent": {"wbToken": bool(credentials["wb"]), "ozonClientId": bool(credentials["ozonClient"]), "ozonApiKey": bool(credentials["ozonKey"])},
              "writePermissionsVerified": False,
              "channels": {channel: category_reader.read(channel, credentials) for channel in ("wb", "ozon")}}
    report["categoriesReadable"] = all(result["readOK"] for result in report["channels"].values())
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", default=guards.EXPECTED_COMMIT)
    args = parser.parse_args()
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise guards.PreflightError("github_actions_only")
        reader = guards.TimewebReader(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""))
        report = inspect(reader, CategoryReader(), args.expected_commit)
        print(json.dumps(report, sort_keys=True))
        return 0 if report["categoriesReadable"] else 1
    except Exception as error:
        print(json.dumps(guards.safe_failure(error, {"phase": "marketplace_readiness_guards"})))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
