#!/usr/bin/env python3
"""Prepare, or explicitly publish, the exact deployed Ficusin public asset build.

GHA only; dry-run by default. Build uses the release Dockerfile's pinned Node24
frontend-dist target. No public/private bucket creation, ACL/tariff changes,
DB/catalog/vendor writes, source deletion or old-release cleanup.
"""
import argparse
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

APP_ID, APP_NAME, BUCKET_ID, BUCKET = 229223, "ficusin-store", 541777, "ficusin-photos"
API, SITE, ENDPOINT = "https://api.timeweb.cloud/api/v1", "https://ficusin.ru", "https://s3.twcstorage.ru"
MAX_RESPONSE, MAX_ASSET, MAX_RELEASE = 16 << 20, 64 << 20, 512 << 20


class AssetError(Exception):
    def __init__(self, code):
        self.safe_code = code if re.fullmatch(r"[a-z0-9_]{1,80}", code) else "asset_guard_failed"
        super().__init__()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise AssetError("unexpected_redirect")


class Provider:
    def __init__(self, token, apply=False, opener=None):
        if not token:
            raise AssetError("timeweb_token_missing")
        self.token, self.apply, self.patch_attempted = token, apply, False
        self.opener = opener or urllib.request.build_opener(NoRedirect())

    def request(self, method, path, body=None):
        allowed = method == "GET" and path in (f"/apps/{APP_ID}", f"/storages/buckets/{BUCKET_ID}")
        if method == "PATCH" and path == f"/apps/{APP_ID}":
            if not self.apply or self.patch_attempted or not isinstance(body, dict) or set(body) != {"envs"}:
                raise AssetError("environment_patch_not_allowed")
            self.patch_attempted = True
            allowed = True
        if not allowed:
            raise AssetError("provider_operation_not_allowed")
        req = urllib.request.Request(API + path, method=method,
            data=None if body is None else json.dumps(body, ensure_ascii=False).encode(),
            headers={"Authorization": "Bearer " + self.token, "Accept": "application/json", "Content-Type": "application/json;charset=utf-8"})
        with self.opener.open(req, timeout=30) as response:
            raw = response.read(MAX_RESPONSE + 1)
        if len(raw) > MAX_RESPONSE:
            raise AssetError("provider_response_too_large")
        result = json.loads(raw) if raw else {}
        if not isinstance(result, dict):
            raise AssetError("invalid_provider_response")
        return result

    def app(self): return self.request("GET", f"/apps/{APP_ID}").get("app")
    def bucket(self): return self.request("GET", f"/storages/buckets/{BUCKET_ID}").get("bucket")


def check_app(app, sha, require_active=True):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise AssetError("invalid_expected_commit")
    if not isinstance(app, dict) or app.get("id") != APP_ID or app.get("name") != APP_NAME:
        raise AssetError("wrong_application")
    branch = app.get("branch_name") or app.get("branch")
    if isinstance(branch, dict): branch = branch.get("name")
    if branch != "main" or app.get("commit_sha") != sha or require_active and app.get("status") != "active":
        raise AssetError("wrong_or_inactive_production_revision")
    env = app.get("envs")
    if not isinstance(env, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in env.items()):
        raise AssetError("invalid_environment")
    if (env.get("APP_ENV", "").strip() or "production") != "production":
        raise AssetError("not_production")
    if env.get("S3_BUCKET", "").strip() != BUCKET or (env.get("S3_ENDPOINT", "").strip() or ENDPOINT).rstrip("/") != ENDPOINT or (env.get("S3_REGION", "").strip() or "ru-1") != "ru-1":
        raise AssetError("unexpected_public_storage")
    if not env.get("S3_ACCESS_KEY", "").strip() or not env.get("S3_SECRET_KEY", "").strip():
        raise AssetError("public_storage_credentials_missing")
    return dict(env)


def bucket_capacity(bucket, apply=False):
    if (not isinstance(bucket, dict) or bucket.get("id") != BUCKET_ID or bucket.get("name") != BUCKET
            or bucket.get("type") != "public" or bucket.get("location") != "ru-1" or bucket.get("status") != "created"):
        raise AssetError("wrong_public_bucket")
    # Official SDK BucketDiskStats specifies size/used in KB, not MB/bytes:
    # github.com/timeweb-cloud/sdk-javascript/blob/main/docs/BucketDiskStats.md
    stats = bucket.get("disk_stats")
    if not isinstance(stats, dict) or stats.get("is_unlimited") is not False:
        raise AssetError("public_capacity_not_finite_verified")
    size, used = stats.get("size"), stats.get("used")
    if any(not isinstance(v, int) or isinstance(v, bool) or v < 0 for v in (size, used)) or size <= 0 or used > size:
        raise AssetError("invalid_public_capacity_metadata")
    if apply and bucket.get("is_allow_auto_upgrade") is not False:
        raise AssetError("public_auto_upgrade_not_verified_off")
    return {"capacityBytes": size * 1024, "providerUsedBytes": used * 1024,
            "autoUpgradeDisabled": bucket.get("is_allow_auto_upgrade") is False}


def tool_env():
    env = {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "LANG") if k in os.environ}
    env.update(GOENV="off", GOWORK="off", GOFLAGS="-mod=readonly", DOCKER_BUILDKIT="1", CI="true")
    return env


def capture(command, cwd=None, env=None, timeout=1800, run=subprocess.run):
    result = run(command, cwd=cwd, env=env or tool_env(), capture_output=True, timeout=timeout, check=False)
    if result.returncode != 0 or len(result.stdout) > MAX_RESPONSE or len(result.stderr) > MAX_RESPONSE:
        raise AssetError("captured_tool_failed")
    return result.stdout


def check_source(source, sha, run=subprocess.run):
    path = Path(source)
    if not path.is_absolute() or not path.is_dir() or path.is_symlink():
        raise AssetError("absolute_source_checkout_required")
    path = path.resolve()
    if capture(["git", "-C", str(path), "rev-parse", "HEAD"], timeout=30, run=run).decode().strip() != sha:
        raise AssetError("source_commit_mismatch")
    dirty = capture(["git", "-C", str(path), "status", "--porcelain", "--untracked-files=all", "--", "backend", "frontend", "public", "Dockerfile", ".dockerignore"], timeout=30, run=run)
    if dirty.strip(): raise AssetError("source_checkout_not_clean")
    docker = path / "Dockerfile"
    if not docker.is_file() or not re.search(r"^FROM node:24[-.][^\s]+@sha256:[0-9a-f]{64} AS frontend$", docker.read_text(), re.M):
        raise AssetError("pinned_node_24_build_missing")
    if not (path / "backend/cmd/publish-static-assets/main.go").is_file():
        raise AssetError("asset_publisher_missing")
    return path


def build_frontend(source, output, run=subprocess.run):
    # Same exact Node patch/image as Timeweb, not the runner's mutable Node24.
    capture(["docker", "build", "--target", "frontend-dist", "--output", "type=local,dest=" + str(output), str(source)],
            env=tool_env(), timeout=1800, run=run)
    if not (output / "index.html").is_file(): raise AssetError("frontend_build_missing_index")


class AssetLinks(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.links = set()
    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        value = values.get("src") if tag == "script" else values.get("href") if tag == "link" and values.get("rel") in ("stylesheet", "modulepreload") else None
        if not value: return
        parsed = urllib.parse.urlsplit(urllib.parse.urljoin(SITE + "/", value))
        if parsed.scheme == "https" and parsed.netloc == "ficusin.ru" and parsed.path.startswith("/assets/") and parsed.path.endswith((".js", ".css")):
            if parsed.query or parsed.fragment: raise AssetError("unexpected_live_asset_query")
            self.links.add(parsed.path)


def html_links(html):
    parser = AssetLinks(); parser.feed(html)
    if not parser.links: raise AssetError("html_asset_links_missing")
    return parser.links


def live_html(opener=None):
    opener = opener or urllib.request.build_opener(NoRedirect())
    request = urllib.request.Request(SITE + "/", headers={"Cache-Control": "no-cache", "Accept": "text/html"})
    with opener.open(request, timeout=30) as response:
        raw = response.read((1 << 20) + 1)
    if len(raw) > 1 << 20: raise AssetError("live_html_too_large")
    return raw.decode("utf-8")


def publisher_env(settings):
    env = tool_env()
    env.update(S3_BUCKET=BUCKET, S3_ENDPOINT=ENDPOINT, S3_REGION="ru-1",
               S3_ACCESS_KEY=settings["S3_ACCESS_KEY"].strip(), S3_SECRET_KEY=settings["S3_SECRET_KEY"].strip())
    return env


def manifest_cli(source, dist, prefix, manifest_path, settings, apply=False, run=subprocess.run):
    command = ["go", "run", "./cmd/publish-static-assets", "--dir", str(dist), "--prefix", prefix, "--manifest", str(manifest_path)]
    if apply: command.append("--apply")
    capture(command, cwd=source / "backend", env=publisher_env(settings), timeout=1850, run=run)
    if not manifest_path.is_file() or manifest_path.stat().st_size > MAX_RESPONSE: raise AssetError("manifest_output_invalid")
    result = json.loads(manifest_path.read_text())
    if result.get("dryRun") is not (not apply) or result.get("verified") is not apply: raise AssetError("manifest_contract_changed")
    manifest = result.get("manifest", {})
    if manifest.get("prefix") != prefix or not isinstance(manifest.get("files"), list) or not manifest["files"]:
        raise AssetError("manifest_empty_or_wrong_prefix")
    total, seen = 0, set()
    for entry in manifest["files"]:
        path, key, size, sha = (entry.get(k) for k in ("path", "key", "bytes", "sha256"))
        if (not isinstance(path, str) or path.startswith("/") or ".." in path or "\\" in path or path in seen
                or key != prefix + "/" + path or not isinstance(size, int) or isinstance(size, bool) or not 0 <= size <= MAX_ASSET
                or not isinstance(sha, str) or not re.fullmatch(r"[0-9a-f]{64}", sha)):
            raise AssetError("manifest_entry_invalid")
        if path in ("sw.js", "robots.txt") or path.endswith((".html", ".webmanifest")):
            raise AssetError("same_origin_shell_in_manifest")
        content = (dist / path).read_bytes()
        if len(content) != size or hashlib.sha256(content).hexdigest() != sha: raise AssetError("manifest_local_checksum_mismatch")
        seen.add(path); total += size
    if total != manifest.get("bytes") or total > MAX_RELEASE: raise AssetError("manifest_total_bound_exceeded")
    return manifest


def usage(storage, prefix):
    total, count, release_count, token, seen = 0, 0, 0, None, set()
    for _ in range(100):
        args = {"Bucket": BUCKET, "MaxKeys": 1000}
        if token: args["ContinuationToken"] = token
        reply = storage.list_objects_v2(**args)
        for item in reply.get("Contents", []):
            size = item.get("Size")
            if not isinstance(size, int) or isinstance(size, bool) or size < 0: raise AssetError("unknown_public_object_size")
            total += size; count += 1
            key = item.get("Key")
            if not isinstance(key, str): raise AssetError("unknown_public_object_key")
            release_count += key.startswith(prefix + "/")
        if reply.get("IsTruncated") is False: return {"bytes": total, "count": count, "targetReleaseObjects": release_count}
        token = reply.get("NextContinuationToken")
        if not isinstance(token, str) or not token or token in seen: raise AssetError("public_inventory_incomplete")
        seen.add(token)
    raise AssetError("public_inventory_limit")


def patch_verified_environment(provider, original, sha, base):
    if check_app(provider.app(), sha) != original: raise AssetError("environment_changed_before_patch")
    updated = dict(original); updated["STATIC_ASSETS_BASE_URL"] = base
    if any(updated[k] != v for k, v in original.items() if k != "STATIC_ASSETS_BASE_URL") or set(updated) - set(original) - {"STATIC_ASSETS_BASE_URL"}:
        raise AssetError("environment_preservation_failed")
    unknown = False
    try:
        provider.request("PATCH", f"/apps/{APP_ID}", {"envs": updated})
    except urllib.error.HTTPError as error:
        if error.code < 500: raise AssetError("environment_patch_rejected") from None
        unknown = True
    except Exception:
        unknown = True
    actual = check_app(provider.app(), sha, require_active=False)
    if actual != updated: raise AssetError("environment_patch_outcome_unverified")
    return unknown


def assert_live_source(paths, opener=None):
    opener = opener or urllib.request.build_opener(NoRedirect())
    for path in sorted(paths)[:2]:
        request = urllib.request.Request(SITE + path, headers={"Range": "bytes=0-0", "Cache-Control": "no-cache"})
        with opener.open(request, timeout=20) as response:
            response.read(1)
            if response.status not in (200, 206) or response.headers.get("X-Ficusin-Asset-Source") != "s3":
                raise AssetError("live_assets_not_confirmed_s3")


def wait_live_source(paths, timeout=600, check=assert_live_source, sleep=time.sleep, monotonic=time.monotonic):
    deadline=monotonic()+timeout
    while True:
        try:
            check(paths)
            return
        except AssetError as error:
            if error.safe_code!="live_assets_not_confirmed_s3":raise
        except urllib.error.HTTPError as error:
            if error.code<500:raise
        except urllib.error.URLError:
            pass
        if monotonic()>=deadline:raise AssetError("live_s3_confirmation_timeout")
        sleep(min(5,max(0,deadline-monotonic())))


def execute(provider, storage, source, directory, sha, apply=False, builder=build_frontend, publisher=manifest_cli,
            get_html=live_html, live_check=wait_live_source, diagnostics=None):
    diagnostics = diagnostics if diagnostics is not None else {}
    def phase(value): diagnostics["phase"] = value
    phase("application_and_public_bucket_guards")
    original = check_app(provider.app(), sha)
    capacity = bucket_capacity(provider.bucket(), apply=apply)
    prefix = "site/releases/" + sha
    base = ENDPOINT + "/" + BUCKET + "/" + prefix
    previous=original.get("STATIC_ASSETS_BASE_URL", "").strip()
    if previous and not re.fullmatch(re.escape(ENDPOINT+"/"+BUCKET+"/site/releases/")+r"[0-9a-f]{40}",previous):
        raise AssetError("unrecognized_static_release_already_active")
    phase("pinned_node_24_frontend_build")
    dist = Path(directory) / "frontend-dist"
    builder(source, dist)
    phase("live_html_asset_paths_match")
    expected_links = html_links((dist / "index.html").read_text())
    if html_links(get_html()) != expected_links: raise AssetError("live_bundle_does_not_match_exact_build")
    phase("asset_manifest_dry_run")
    manifest_path = Path(directory) / "asset-manifest.json"
    manifest = publisher(source, dist, prefix, manifest_path, original, False)
    phase("existing_public_capacity_inventory")
    existing = usage(storage, prefix)
    used = max(existing["bytes"], capacity["providerUsedBytes"])
    # Reserve an additional release-sized cushion against ordinary concurrent
    # photo uploads; no tariff increase is enabled or requested by this helper.
    if used + manifest["bytes"] * 2 > capacity["capacityBytes"]: raise AssetError("insufficient_public_bucket_capacity")
    diagnostics.update(fileCount=len(manifest["files"]),bytes=manifest["bytes"],uploadedVerified=False,environmentPatched=False)
    report = {"ok": True, "dryRun": not apply, "fileCount": len(manifest["files"]), "bytes": manifest["bytes"],
              "liveBundleMatches": True, "capacity": capacity, "existingStorage": existing,
              "uploadedVerified": False, "environmentPatched": False, "localFallbackRetained": True}
    if not apply: return report
    if existing["targetReleaseObjects"]: raise AssetError("immutable_target_prefix_not_empty")
    phase("preupload_revision_capacity_and_bundle_recheck")
    if check_app(provider.app(), sha) != original: raise AssetError("application_changed_before_upload")
    capacity = bucket_capacity(provider.bucket(), apply=True)
    existing = usage(storage, prefix)
    if existing["targetReleaseObjects"] or max(existing["bytes"], capacity["providerUsedBytes"]) + manifest["bytes"] * 2 > capacity["capacityBytes"]:
        raise AssetError("public_capacity_or_target_prefix_changed")
    if html_links(get_html()) != expected_links: raise AssetError("live_bundle_changed_before_upload")
    phase("publish_and_checksum_verify_public_assets")
    verified = publisher(source, dist, prefix, manifest_path, original, True)
    if verified != manifest: raise AssetError("verified_manifest_changed")
    diagnostics["uploadedVerified"]=True
    phase("activate_verified_static_release")
    diagnostics["environmentPatchAttempted"]=True
    unknown = patch_verified_environment(provider, original, sha, base)
    diagnostics["environmentPatched"]=True
    report.update(uploadedVerified=True, environmentPatched=True, ambiguousPatchReconciled=unknown)
    phase("live_s3_source_confirmation")
    live_check(expected_links)
    report["liveS3Confirmed"] = True
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    diagnostics = {"phase": "github_actions_guard"}
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true": raise AssetError("github_actions_only")
        source = check_source(os.environ.get("RELEASE_SOURCE_DIR", ""), args.expected_commit)
        provider = Provider(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""), args.apply)
        original = check_app(provider.app(), args.expected_commit)
        import boto3
        from botocore.config import Config
        storage = boto3.session.Session().client("s3", endpoint_url=ENDPOINT, region_name="ru-1",
            aws_access_key_id=original["S3_ACCESS_KEY"].strip(), aws_secret_access_key=original["S3_SECRET_KEY"].strip(),
            config=Config(signature_version="s3v4", connect_timeout=5, read_timeout=10,
                          retries={"total_max_attempts": 1}, s3={"addressing_style": "path"}))
        try:
            with tempfile.TemporaryDirectory(prefix="ficusin-production-static-assets-") as directory:
                report = execute(provider, storage, source, directory, args.expected_commit, args.apply, diagnostics=diagnostics)
        finally:
            storage.close()
        print(json.dumps(report, sort_keys=True))
        return 0
    except Exception as error:
        report = {"ok": False, "phase": diagnostics["phase"], "errorClass": type(error).__name__, "localFallbackRetained": True}
        for key in ("fileCount","bytes","uploadedVerified","environmentPatchAttempted","environmentPatched"):
            if key in diagnostics:report[key]=diagnostics[key]
        code = getattr(error, "safe_code", None)
        if isinstance(code, str) and re.fullmatch(r"[a-z0-9_]{1,80}", code): report["reason"] = code
        print(json.dumps(report, sort_keys=True))
        return 1


if __name__ == "__main__": raise SystemExit(main())
