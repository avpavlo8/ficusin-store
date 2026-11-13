#!/usr/bin/env python3
"""Prepare, or explicitly apply, the capped Ficusin private S3 rollout.

Default dry-run: GET only. --apply permits one exact approved 2.20 RUB cold preset creation,
private canary bytes, deletion of that canary, and an env-only app PATCH.
No database/business writes, public ACL changes, security users or tariff
fallbacks. Credentials remain in memory; only fixed status fields are logged.
"""
import argparse
from decimal import Decimal, InvalidOperation
import hashlib
import importlib.util
import json
import os
import re
from pathlib import Path
import secrets
import statistics
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

spec = importlib.util.spec_from_file_location("storage_inspect", Path(__file__).with_name("production-private-storage-inspect.py"))
inspection = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inspection)

APP_ID = 229223
APP_NAME = "ficusin-store"
EXPECTED_COMMIT = "15989cc1f2d46baad54ce7ebbf379531c86c7f23"
TARGET = "ficusin-private"
PRESET = 5143
MAX_PRICE = Decimal("2.20")
API = "https://api.timeweb.cloud/api/v1"
ALLOWED_ENV_CHANGES = frozenset(("PRIVATE_S3_BUCKET", "PRIVATE_S3_ACCESS_KEY", "PRIVATE_S3_SECRET_KEY"))
STATE = {"phase": "startup", "created": False, "createOutcomeUnknown": False, "canaryCleanupVerified": False, "canaryWriteAttempted": False, "environmentPatched": False}


class ProvisionError(Exception):
    def __init__(self, code):
        self.code = code
        super().__init__()


def check_app(app):
    env, endpoint, region = inspection.production_settings(app)
    branch = app.get("branch_name")
    if branch is None:
        candidate = app.get("branch")
        branch = candidate.get("name") if isinstance(candidate, dict) else candidate
    if branch != "main" or app.get("commit_sha") != EXPECTED_COMMIT:
        raise ProvisionError("production_revision_changed")
    if env["S3_BUCKET"].strip() != "ficusin-photos" or region != "ru-1":
        raise ProvisionError("unexpected_public_storage")
    if any(not isinstance(key, str) or not isinstance(value, str) for key, value in env.items()):
        raise ProvisionError("environment_not_string_map")
    if env.get("PRIVATE_S3_BUCKET", "").strip() not in ("", TARGET):
        raise ProvisionError("different_private_bucket_already_configured")
    if env.get("PRIVATE_S3_ENDPOINT", "").strip().rstrip("/") not in ("", endpoint):
        raise ProvisionError("different_private_endpoint_configured")
    if env.get("PRIVATE_S3_REGION", "").strip() not in ("", region):
        raise ProvisionError("different_private_region_configured")
    return dict(env), endpoint, region


def check_preset(reply):
    presets = reply.get("storages_presets")
    if not isinstance(presets, list):
        raise ProvisionError("invalid_preset_list")
    matches = [p for p in presets if isinstance(p, dict) and p.get("id") == PRESET]
    if len(matches) != 1:
        raise ProvisionError("exact_preset_unavailable")
    preset = matches[0]
    try:
        price = Decimal(str(preset.get("price")))
    except InvalidOperation:
        raise ProvisionError("invalid_preset_price") from None
    if (not price.is_finite() or price < 0 or price > MAX_PRICE
            or preset.get("disk") != 1024 or preset.get("location") != "ru-1"
            or preset.get("storage_class") != "cold"):
        raise ProvisionError("preset_exceeds_exact_approved_scope")
    return {"id": PRESET, "disk": 1024, "priceMonthlyRub": str(price), "location": "ru-1", "storage_class": "cold"}


def check_bucket(bucket, require_no_auto_upgrade=True):
    if (not isinstance(bucket, dict) or bucket.get("name") != TARGET
            or not isinstance(bucket.get("id"), int) or isinstance(bucket["id"], bool) or bucket["id"] <= 0
            or bucket.get("type") != "private" or bucket.get("preset_id") != PRESET
            or bucket.get("location") != "ru-1" or bucket.get("storage_class") != "cold"
            or bucket.get("status") != "created"):
        raise ProvisionError("target_bucket_outside_approved_scope")
    # Official Create/UpdateStorageRequest does NOT document a way to set this
    # flag. Never invent a PATCH field or enable uploads when it is unknown/on.
    if require_no_auto_upgrade and bucket.get("is_allow_auto_upgrade") is not False:
        raise ProvisionError("auto_upgrade_unverified_or_enabled")
    return bucket


class Provider:
    def __init__(self, token, apply, deadline, opener=None):
        self.token, self.apply, self.deadline = token, apply, deadline
        self.opener = opener or urllib.request.build_opener(inspection.NoRedirect())
        self.target_id = None
        self.create_attempted = self.patch_attempted = False

    def request(self, method, path, body=None):
        reads = (f"/apps/{APP_ID}", "/presets/storages", "/storages/buckets")
        target_path = f"/storages/buckets/{self.target_id}" if self.target_id else None
        allowed = method == "GET" and (path in reads or path == target_path)
        if method == "POST" and path == "/storages/buckets":
            if not self.apply or self.create_attempted or body != {"name": TARGET, "type": "private", "preset_id": PRESET}:
                raise ProvisionError("create_not_allowed")
            self.create_attempted = True  # Before transport: no ambiguous retry.
            allowed = True
        if method == "PATCH" and path == f"/apps/{APP_ID}":
            if not self.apply or self.patch_attempted or not isinstance(body, dict) or set(body) != {"envs"}:
                raise ProvisionError("environment_patch_not_allowed")
            self.patch_attempted = True
            allowed = True
        if not allowed:
            raise ProvisionError("provider_operation_not_allowed")
        remaining = self.deadline - time.monotonic()
        if remaining < 2:
            raise ProvisionError("time_budget")
        request = urllib.request.Request(API + path, method=method,
            data=None if body is None else json.dumps(body, ensure_ascii=False).encode(),
            headers={"Authorization": "Bearer " + self.token, "Accept": "application/json",
                     "Content-Type": "application/json;charset=utf-8", "Cache-Control": "no-cache"})
        with self.opener.open(request, timeout=min(30, remaining)) as response:
            raw = response.read(inspection.MAX_RESPONSE + 1)
        if len(raw) > inspection.MAX_RESPONSE:
            raise ProvisionError("provider_response_too_large")
        result = json.loads(raw) if raw else {}
        if not isinstance(result, dict):
            raise ProvisionError("invalid_provider_response")
        return result

    def app(self):
        return self.request("GET", f"/apps/{APP_ID}").get("app")

    def lookup(self):
        reply = self.request("GET", "/storages/buckets")
        buckets = reply.get("buckets")
        if not isinstance(buckets, list):
            raise ProvisionError("invalid_bucket_list")
        total = reply.get("meta", {}).get("total")
        if isinstance(total, int) and total > len(buckets):
            raise ProvisionError("incomplete_bucket_list")
        matches = [b for b in buckets if isinstance(b, dict) and b.get("name") == TARGET]
        if len(matches) > 1:
            raise ProvisionError("ambiguous_target_bucket")
        if not matches:
            return None, len(buckets)
        check_bucket(matches[0], require_no_auto_upgrade=False)
        self.target_id = matches[0]["id"]
        bucket = self.request("GET", f"/storages/buckets/{self.target_id}").get("bucket")
        return check_bucket(bucket, require_no_auto_upgrade=False), len(buckets)

    def create_once(self):
        try:
            self.request("POST", "/storages/buckets", {"name": TARGET, "type": "private", "preset_id": PRESET})
        except urllib.error.HTTPError as error:
            STATE["createHTTPStatus"] = error.code
            try:
                failure = json.loads(error.read(8192))
                code = failure.get("error_code")
                if isinstance(code, str) and re.fullmatch(r"[A-Za-z0-9_.-]{1,100}", code):
                    STATE["createErrorCode"] = code
                message = failure.get("message")
                if isinstance(message, str):
                    message = re.sub(r"https?://\S+", "[url]", message)
                    message = re.sub(r"[A-Za-z0-9+/=_-]{32,}", "[opaque]", message)
                    STATE["createValidationMessage"] = message[:500]
            except Exception:
                pass
            if error.code < 500:
                # Includes unavailable tariff, conflict, auth and quota: stop.
                raise ProvisionError("create_rejected_no_tariff_fallback") from None
            STATE["createOutcomeUnknown"] = True
        except ProvisionError:
            raise
        except Exception:
            STATE["createOutcomeUnknown"] = True
        # Only GET reconciliation after a possible timeout/5xx. Never POST again.
        for attempt in range(3):
            bucket, _ = self.lookup()
            if bucket:
                STATE["created"] = True
                return bucket
            if attempt < 2:
                time.sleep(1)
        raise ProvisionError("create_outcome_unresolved_no_retry")


def error_code(error):
    response = getattr(error, "response", {})
    return response.get("Error", {}).get("Code") if isinstance(response, dict) else None


def is_not_found(error):
    return error_code(error) in ("404", "NoSuchKey", "NotFound")


def verify_privacy(client):
    all_users, authenticated = inspection.acl_flags(client.get_bucket_acl(Bucket=TARGET))
    if all_users or authenticated:
        raise ProvisionError("private_bucket_has_broad_acl")
    try:
        policy = client.get_bucket_policy(Bucket=TARGET)
    except Exception as error:
        if error_code(error) != "NoSuchBucketPolicy":
            raise
    else:
        if inspection.policy_anonymous_allow(policy):
            raise ProvisionError("private_bucket_has_anonymous_policy_allow")


def check_anonymous_denied(endpoint, key):
    url = endpoint + "/" + TARGET + "/" + urllib.parse.quote(key, safe="/")
    request = urllib.request.Request(url, method="GET", headers={"Cache-Control": "no-cache"})
    try:
        with urllib.request.build_opener(inspection.NoRedirect()).open(request, timeout=10) as response:
            response.read(1)
        raise ProvisionError("anonymous_canary_access_allowed")
    except urllib.error.HTTPError as error:
        if error.code not in (403, 404):
            raise ProvisionError("anonymous_canary_denial_unverified") from None


def canary(client, endpoint):
    STATE["canaryWriteAttempted"] = False
    key = "ops-canary/private-storage/" + uuid.uuid4().hex + ".bin"
    body = b"Ficusin private storage canary\n" + secrets.token_bytes(1024 * 1024)
    try:
        client.head_object(Bucket=TARGET, Key=key)
    except Exception as error:
        if not is_not_found(error):
            raise
    else:
        raise ProvisionError("canary_key_already_exists")
    STATE["canaryCleanupVerified"] = False
    try:
        STATE["canaryWriteAttempted"] = True
        client.put_object(Bucket=TARGET, Key=key, Body=body, ContentType="application/octet-stream",
                          ACL="private", CacheControl="private, no-store")
        started = time.perf_counter()
        reply = client.get_object(Bucket=TARGET, Key=key)
        stream = reply["Body"]
        try:
            actual = stream.read(len(body) + 1)
        finally:
            stream.close()
        if len(actual) != len(body) or hashlib.sha256(actual).digest() != hashlib.sha256(body).digest():
            raise ProvisionError("canary_checksum_mismatch")
        durations = [time.perf_counter() - started]
        for _ in range(2):
            started = time.perf_counter()
            reply = client.get_object(Bucket=TARGET, Key=key)
            with reply["Body"] as stream:
                repeated = stream.read(len(body) + 1)
            durations.append(time.perf_counter() - started)
            if hashlib.sha256(repeated).digest() != hashlib.sha256(body).digest():
                raise ProvisionError("canary_repeat_checksum_mismatch")
        started = time.perf_counter()
        partial = client.get_object(Bucket=TARGET, Key=key, Range="bytes=0-65535")
        with partial["Body"] as stream:
            partial_bytes = stream.read(65537)
        range_seconds = time.perf_counter() - started
        if partial.get("ResponseMetadata", {}).get("HTTPStatusCode") != 206 or partial_bytes != body[:65536]:
            raise ProvisionError("canary_range_read_mismatch")
        STATE["speedCheck"] = {"bytes":len(body),"reads":len(durations),"medianReadMs":round(statistics.median(durations)*1000),"maxReadMs":round(max(durations)*1000),"rangeReadMs":round(range_seconds*1000)}
        if statistics.median(durations) > 1.0 or max(durations) > 2.0 or range_seconds > 1.5:
            raise ProvisionError("cold_storage_speed_below_required_threshold")
        check_anonymous_denied(endpoint, key)
    finally:
        # The absence check preceded any PUT. Delete only this invocation's
        # random key, including when a PUT timeout has an unknown outcome.
        client.delete_object(Bucket=TARGET, Key=key)
        try:
            reply = client.get_object(Bucket=TARGET, Key=key)
        except Exception as error:
            if not is_not_found(error):
                raise ProvisionError("canary_cleanup_unverified") from None
            STATE["canaryCleanupVerified"] = True
        else:
            reply["Body"].close()
            raise ProvisionError("canary_still_exists_after_delete")


def make_client(endpoint, region, access, secret):
    import boto3
    from botocore.config import Config
    inspection.mask(access)
    inspection.mask(secret)
    return boto3.session.Session().client("s3", endpoint_url=endpoint, region_name=region,
        aws_access_key_id=access, aws_secret_access_key=secret,
        config=Config(signature_version="s3v4", connect_timeout=3, read_timeout=10,
                      retries={"total_max_attempts": 1}, s3={"addressing_style": "path"}))


def select_verified_credentials(env, bucket, endpoint, region):
    inherited = (env.get("PRIVATE_S3_ACCESS_KEY", "").strip() or env["S3_ACCESS_KEY"].strip(),
                 env.get("PRIVATE_S3_SECRET_KEY", "").strip() or env["S3_SECRET_KEY"].strip())
    choices = [("inherited", inherited)]
    dedicated = (str(bucket.get("access_key", "")).strip(), str(bucket.get("secret_key", "")).strip())
    if all(dedicated) and dedicated != inherited:
        choices.append(("bucket_existing", dedicated))
    for index, (source, pair) in enumerate(choices):
        client = make_client(endpoint, region, *pair)
        try:
            verify_privacy(client)
            STATE["phase"] = "private_canary"
            canary(client, endpoint)
            return source, pair
        except ProvisionError:
            raise
        except Exception as error:
            if STATE["canaryWriteAttempted"] and not STATE["canaryCleanupVerified"]:
                raise ProvisionError("canary_cleanup_unverified") from None
            if inspection.error_state(error) != "access_denied" or index == len(choices) - 1:
                raise
            # No role/key is created or expanded. A second attempt is only with
            # the bucket's already existing credentials returned by the provider.
        finally:
            client.close()
    raise ProvisionError("no_verified_existing_credentials")


def expected_env(initial, source, pair):
    desired = dict(initial)
    desired["PRIVATE_S3_BUCKET"] = TARGET
    if source == "bucket_existing":
        desired["PRIVATE_S3_ACCESS_KEY"], desired["PRIVATE_S3_SECRET_KEY"] = pair
    changed = {key for key in set(initial) | set(desired) if initial.get(key) != desired.get(key)}
    if not changed <= ALLOWED_ENV_CHANGES:
        raise ProvisionError("unexpected_environment_change")
    return desired, sorted(changed)


def configure_environment(provider, initial, desired):
    current, _, _ = check_app(provider.app())
    if current != initial:
        raise ProvisionError("environment_changed_concurrently")
    if current == desired:
        return
    try:
        provider.request("PATCH", f"/apps/{APP_ID}", {"envs": desired})
    except urllib.error.HTTPError as error:
        if error.code < 500:
            raise ProvisionError("environment_patch_rejected") from None
    except ProvisionError:
        raise
    except Exception:
        pass  # Unknown result: GET verification only, never retry PATCH.
    for attempt in range(3):
        actual, _, _ = check_app(provider.app())
        if actual == desired:
            STATE["environmentPatched"] = True
            return
        if actual != initial:
            raise ProvisionError("environment_patch_changed_other_values")
        if attempt < 2:
            time.sleep(1)
    raise ProvisionError("environment_patch_outcome_unverified")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise ProvisionError("github_actions_only")
    token = os.environ.get("TIMEWEB_CLOUD_TOKEN", "")
    if not token:
        raise ProvisionError("timeweb_token_missing")
    inspection.mask(token)
    provider = Provider(token, args.apply, time.monotonic() + 600)
    STATE["phase"] = "production_identity"
    initial, endpoint, region = check_app(provider.app())
    for key, value in initial.items():
        if any(part in key for part in ("TOKEN", "KEY", "SECRET", "PASSWORD", "DATABASE_URL", "SSL_CA")) and len(value) >= 8:
            inspection.mask(value)
    STATE["phase"] = "price_cap"
    preset = check_preset(provider.request("GET", "/presets/storages"))
    STATE["phase"] = "target_lookup"
    bucket, original_count = provider.lookup()
    report = {"mode": "apply" if args.apply else "dry-run", "target": TARGET, "preset": preset,
              "originalBucketCount": original_count, "wouldCreate": bucket is None,
              "autoUpgradeControl": "must_be_explicitly_false; unsupported API flag will not be guessed"}
    if not args.apply:
        report["autoUpgradeVerifiedDisabled"] = bucket is not None and bucket.get("is_allow_auto_upgrade") is False
        report["noWritesPerformed"] = True
        print(json.dumps(report), flush=True)
        return 0
    STATE["phase"] = "target_create_once"
    if bucket is None:
        bucket = provider.create_once()
    check_bucket(bucket)
    # Recheck current published price immediately before writing canary bytes.
    check_preset(provider.request("GET", "/presets/storages"))
    STATE["phase"] = "private_acl_policy"
    source, pair = select_verified_credentials(initial, bucket, endpoint, region)
    desired, changed = expected_env(initial, source, pair)
    STATE["phase"] = "environment_patch"
    configure_environment(provider, initial, desired)
    report.update({"bucketId": bucket["id"], "credentialsSource": source,
                   "changedEnvironmentKeys": changed, "canaryVerified": True,
                   "autoUpgradeVerifiedDisabled": True, **STATE})
    report["ok"] = True
    print(json.dumps(report), flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        report = {"ok": False, **STATE, "error": error.code if isinstance(error, ProvisionError) else "private_storage_provision_failed",
                  "diagnostic": inspection.safe_error(error, STATE["phase"])}
        print(json.dumps(report), flush=True)
        raise SystemExit(2) from None
