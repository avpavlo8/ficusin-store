"""GHA-only, read-only inventory of Ficusin S3 access configuration.

Reads production app 229223 environment into memory; uses its EXISTING public
S3 credentials. Never creates objects/buckets/users, changes ACL/policies, or
patches application settings. Foreign bucket names, ACL owner IDs, full policies,
credentials and endpoint URLs are not logged. Unknown is never called private.
"""

import argparse
import json
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request

APP_ID = 229223
APP_NAME = "ficusin-store"
MAX_RESPONSE = 16 * 1024 * 1024
READ_METHODS = frozenset(("list_buckets", "get_bucket_acl", "get_bucket_policy_status", "get_bucket_policy"))
ALL_USERS = "http://acs.amazonaws.com/groups/global/AllUsers"
AUTHENTICATED_USERS = "http://acs.amazonaws.com/groups/global/AuthenticatedUsers"


class InspectError(Exception):
    """Fixed diagnostic codes only, never remote exception text."""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def mask(value):
    escaped = value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
    print("::add-mask::" + escaped, flush=True)


def production_settings(app):
    if not isinstance(app, dict) or app.get("id") != APP_ID or app.get("name") != APP_NAME:
        raise InspectError("wrong_application")
    env = app.get("envs")
    if not isinstance(env, dict):
        raise InspectError("invalid_environment")
    # config.Load treats an omitted/blank APP_ENV as production.
    if str(env.get("APP_ENV", "")).strip() not in ("", "production"):
        raise InspectError("not_production")
    if str(env.get("FILE_STORAGE_MODE", "s3")).strip() not in ("", "s3"):
        raise InspectError("unexpected_file_storage_mode")
    if str(env.get("LOCAL_PRIVATE_FILES_DIR", "")).strip():
        raise InspectError("unexpected_local_file_storage")
    for key in ("S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY"):
        if not isinstance(env.get(key), str) or not env[key].strip():
            raise InspectError("missing_existing_s3_configuration")
    endpoint = str(env.get("S3_ENDPOINT", "")).strip() or "https://s3.twcstorage.ru"
    parsed = urllib.parse.urlsplit(endpoint)
    if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise InspectError("unsafe_s3_endpoint")
    return env, endpoint.rstrip("/"), str(env.get("S3_REGION", "")).strip() or "ru-1"


def ours(name):
    # Never inspect or print another tenant/project's name merely because the
    # account-level key can list it. Match the brand as a complete name segment.
    return isinstance(name, str) and bool(re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,62}", name)) and bool(
        re.search(r"(?:^|[.-])ficusin(?:[.-]|$)", name)
    )


def acl_flags(reply):
    grants = reply.get("Grants")
    if not isinstance(grants, list):
        raise InspectError("invalid_acl")
    all_users = authenticated_users = False
    for grant in grants:
        if not isinstance(grant, dict) or not isinstance(grant.get("Grantee"), dict):
            raise InspectError("invalid_acl")
        grantee = grant["Grantee"]
        if grantee.get("Type") == "Group":
            all_users |= grantee.get("URI") == ALL_USERS
            authenticated_users |= grantee.get("URI") == AUTHENTICATED_USERS
    return all_users, authenticated_users


def policy_anonymous_allow(reply):
    policy = reply.get("Policy")
    if not isinstance(policy, str) or len(policy) > MAX_RESPONSE:
        raise InspectError("invalid_policy")
    try:
        policy = json.loads(policy)
    except (ValueError, TypeError):
        raise InspectError("invalid_policy") from None
    if not isinstance(policy, dict):
        raise InspectError("invalid_policy")
    statements = policy.get("Statement")
    if isinstance(statements, dict):
        statements = [statements]
    if not isinstance(statements, list):
        raise InspectError("invalid_policy")
    for statement in statements:
        if not isinstance(statement, dict):
            raise InspectError("invalid_policy")
        if statement.get("Effect") != "Allow":
            continue
        principal = statement.get("Principal")
        # This is deliberately conservative: a public Allow is reported even
        # when a condition or separate Deny could limit its effective scope.
        # Never misclassify a partially understood policy as a private bucket.
        if "NotPrincipal" in statement or principal == "*":
            return True
        if isinstance(principal, dict):
            for value in principal.values():
                if value == "*" or isinstance(value, list) and "*" in value:
                    return True
        elif isinstance(principal, list) and "*" in principal:
            return True
    return False


def error_state(error):
    response = getattr(error, "response", {})
    code = str(response.get("Error", {}).get("Code", "")) if isinstance(response, dict) else ""
    if code in ("AccessDenied", "AllAccessDisabled", "Unauthorized", "UnauthorizedAccess", "403"):
        return "access_denied"
    if code in ("NoSuchBucketPolicy", "NoSuchBucketPolicyStatus"):
        return "absent"
    if code in ("NotImplemented", "UnsupportedOperation", "MethodNotAllowed", "501", "405"):
        return "unsupported"
    if code == "NoSuchBucket":
        return "not_found"
    return "read_failed"


class Inventory:
    def __init__(self, client, deadline):
        self.client, self.deadline = client, deadline

    def read(self, method, **params):
        if method not in READ_METHODS:
            raise InspectError("write_method_rejected")
        if self.deadline - time.monotonic() < 10:
            raise InspectError("time_budget")
        return getattr(self.client, method)(**params)

    def bucket(self, name, public_bucket):
        if not ours(name):
            raise InspectError("foreign_bucket_rejected")
        states = {}
        all_users = authenticated_users = anonymous_allow = public_status = None
        try:
            all_users, authenticated_users = acl_flags(self.read("get_bucket_acl", Bucket=name))
            states["acl"] = "ok"
        except InspectError as error:
            if str(error) == "time_budget":
                raise
            states["acl"] = "invalid_response"
        except Exception as error:
            states["acl"] = error_state(error)
        try:
            status = self.read("get_bucket_policy_status", Bucket=name).get("PolicyStatus", {}).get("IsPublic")
            if not isinstance(status, bool):
                raise InspectError("invalid_policy_status")
            public_status, states["policyStatus"] = status, "ok"
        except InspectError as error:
            if str(error) == "time_budget":
                raise
            states["policyStatus"] = "invalid_response"
        except Exception as error:
            states["policyStatus"] = error_state(error)
            if states["policyStatus"] == "absent":
                public_status = False
        try:
            anonymous_allow = policy_anonymous_allow(self.read("get_bucket_policy", Bucket=name))
            states["policy"] = "ok"
        except InspectError as error:
            if str(error) == "time_budget":
                raise
            states["policy"] = "invalid_response"
        except Exception as error:
            states["policy"] = error_state(error)
            if states["policy"] == "absent":
                anonymous_allow = False
        evidence = (all_users, authenticated_users, anonymous_allow, public_status)
        is_public = True if any(value is True for value in evidence) else (
            False if all(value is False for value in evidence) else None
        )
        return {"name": name, "isPublic": is_public, "grantsAllUsers": all_users,
                "grantsAuthenticatedUsers": authenticated_users, "policyAnonymousAllow": anonymous_allow,
                "publicConfiguredMatch": name == public_bucket,
                "accessDenied": any(state == "access_denied" for state in states.values()),
                "readStates": states, "privateCandidate": is_public is False and name != public_bucket}

    def inspect(self, public_bucket, max_buckets):
        reply = self.read("list_buckets")
        buckets = reply.get("Buckets")
        if not isinstance(buckets, list):
            raise InspectError("invalid_bucket_list")
        names = []
        foreign = 0
        for bucket in buckets:
            name = bucket.get("Name") if isinstance(bucket, dict) else None
            if ours(name):
                names.append(name)
            else:
                foreign += 1
        names = sorted(set(names))
        result = {"buckets": [], "ignoredForeignBucketCount": foreign,
                  "ficusinBucketCount": len(names), "complete": not bool(reply.get("ContinuationToken"))}
        if len(names) > max_buckets:
            result["complete"] = False
        for name in names[:max_buckets]:
            try:
                result["buckets"].append(self.bucket(name, public_bucket))
            except InspectError as error:
                if str(error) != "time_budget":
                    raise
                result["complete"] = False
                result["stoppedByTimeBudget"] = True
                break
        return result


def fetch_app(token, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise InspectError("time_budget")
    request = urllib.request.Request(f"https://api.timeweb.cloud/api/v1/apps/{APP_ID}",
                                    headers={"Authorization": "Bearer " + token, "Accept": "application/json"})
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=min(20, remaining)) as response:
            raw = response.read(MAX_RESPONSE + 1)
        if len(raw) > MAX_RESPONSE:
            raise InspectError("provider_response_too_large")
        return json.loads(raw).get("app")
    except InspectError:
        raise
    except Exception:
        raise InspectError("provider_read_failed") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--max-buckets", type=int, default=50)
    parser.add_argument("--time-budget-seconds", type=int, default=300)
    args = parser.parse_args()
    if not 1 <= args.max_buckets <= 50 or not 15 <= args.time_budget_seconds <= 300:
        parser.error("max-buckets must be 1..50; time-budget-seconds must be 15..300")
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise InspectError("github_actions_only")
    token = os.environ.get("TIMEWEB_CLOUD_TOKEN", "")
    if not token:
        raise InspectError("missing_provider_credentials")
    mask(token)
    deadline = time.monotonic() + args.time_budget_seconds
    env, endpoint, region = production_settings(fetch_app(token, deadline))
    mask(env["S3_ACCESS_KEY"])
    mask(env["S3_SECRET_KEY"])
    import boto3
    from botocore.config import Config
    # Explicit credentials prevent accidental use of a runner role/default
    # profile. No other account, database or application is ever contacted.
    client = boto3.session.Session().client("s3", endpoint_url=endpoint, region_name=region,
        aws_access_key_id=env["S3_ACCESS_KEY"].strip(), aws_secret_access_key=env["S3_SECRET_KEY"].strip(),
        config=Config(signature_version="s3v4", connect_timeout=3, read_timeout=5,
                      retries={"total_max_attempts": 1}, s3={"addressing_style": "path"}))
    try:
        result = Inventory(client, deadline).inspect(env["S3_BUCKET"].strip(), args.max_buckets)
    finally:
        client.close()
    result.update({"appId": APP_ID, "appName": APP_NAME, "mode": "read-only",
                   "privateBucketConfigured": bool(str(env.get("PRIVATE_S3_BUCKET", "")).strip())})
    print(json.dumps(result, ensure_ascii=True), flush=True)
    return 0 if result["complete"] else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except InspectError as error:
        print(json.dumps({"error": str(error)}), flush=True)
        raise SystemExit(2) from None
    except Exception:
        print('{"error":"read_only_inspection_failed_no_sensitive_details_logged"}', flush=True)
        raise SystemExit(2) from None
