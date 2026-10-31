"""Fail closed unless this PR commit has a successful staging deployment."""
import json
import os
import time
import urllib.request

repository = os.environ['GITHUB_REPOSITORY']
sha = os.environ['CANDIDATE_SHA']
url = os.environ.get('STAGING_BASE_URL','').rstrip('/')
if not url:
    raise SystemExit('Staging is not provisioned: configure STAGING_BASE_URL and verify this candidate before production')

def get(path):
    request = urllib.request.Request('https://api.github.com'+path,headers={'Authorization':'Bearer '+os.environ['GITHUB_TOKEN'],'Accept':'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'})
    with urllib.request.urlopen(request,timeout=20) as response:
        return json.load(response)

for attempt in range(40):
    deployments = get(f'/repos/{repository}/deployments?sha={sha}&environment=staging&per_page=100')
    for deployment in deployments:
        if deployment['sha'] != sha or deployment['environment'] != 'staging':
            continue
        statuses = get(f'/repos/{repository}/deployments/{deployment["id"]}/statuses?per_page=1')
        if statuses and statuses[0]['state']=='success' and statuses[0].get('environment_url','').rstrip('/')==url:
            print('Exact PR commit verified on staging:',sha)
            raise SystemExit(0)
    print('Waiting for successful staging verification of',sha,flush=True)
    time.sleep(15)
raise SystemExit('This commit has not passed staging. Push this exact candidate to staging and wait for verification; do not merge production')
