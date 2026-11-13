#!/usr/bin/env python3
"""GHA-only single redeploy of the exact active production SHA; dry-run by default."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import urllib.request

spec = importlib.util.spec_from_file_location("redeploy_activation_guards", Path(__file__).with_name("production-publisher-activation.py"))
activation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(activation)
guards = activation.guards


class Provider:
    def __init__(self, token, apply=False, opener=None):
        self.reader = activation.Provider(token, opener=opener)
        self.apply, self.attempted = apply, False

    def get(self):
        return self.reader.get()

    def deploy(self, expected_commit):
        if not self.apply or self.attempted:
            raise guards.PreflightError("redeploy_not_allowed")
        # Set before I/O: a lost response never permits a second POST.
        self.attempted = True
        request = urllib.request.Request(guards.API + "/apps/229223/deploy", method="POST",
            data=json.dumps({"commit_sha": expected_commit}).encode(),
            headers={"Authorization": "Bearer " + self.reader.token, "Content-Type": "application/json", "Accept": "application/json"})
        with self.reader.opener.open(request, timeout=30) as response:
            response.read(guards.MAX_RESPONSE + 1)


def check_app(app, sha):
    env = activation.check_app(app, sha)
    if any(env.get(key) != value for key, value in activation.FLAGS.items()):
        raise guards.PreflightError("live_publication_flags_not_enabled")
    return env


def redeploy(provider, expected_commit, apply=False):
    env = check_app(provider.get(), expected_commit)
    if check_app(provider.get(), expected_commit) != env:
        raise guards.PreflightError("environment_changed_during_preflight")
    if apply:
        provider.deploy(expected_commit)
    return {"deployRequested": apply, "commit": expected_commit}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise guards.PreflightError("github_actions_only")
        result = redeploy(Provider(os.environ.get("TIMEWEB_CLOUD_TOKEN", ""), args.apply), args.expected_commit, args.apply)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception as error:
        print(json.dumps(guards.safe_failure(error, {"phase": "production_exact_redeploy"})))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
