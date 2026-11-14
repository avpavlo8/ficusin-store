"""Verify the exact candidate on the isolated staging origin, without writes."""
import hashlib
import base64
import json
import os
import pathlib
import re
import time
import urllib.error
import urllib.parse
import urllib.request

base = os.environ['STAGING_BASE_URL'].rstrip('/')
target = urllib.parse.urlsplit(base)
if target.scheme != 'https' or target.username or target.password or target.port or target.path or target.query or target.fragment or not (target.hostname.endswith('.twc1.net') or target.hostname == 'staging.ficusin.ru'):
    raise SystemExit('Only the isolated staging HTTPS origin is allowed')

root = pathlib.Path(__file__).resolve().parents[1]
frontend = root / 'frontend/dist'
index = (frontend / 'index.html').read_text()
expected_js = re.search(r'src="(/assets/[^"]+\.js)"', index).group(1)
expected_css = re.search(r'href="(/assets/[^"]+\.css)"', index).group(1)
# Match shell sort of full paths; Path ordering compares path components.
files = sorted((p for p in (root/'backend').rglob('*') if p.is_file() and (p.suffix == '.go' or p.name in ('go.mod','go.sum'))), key=lambda p: p.as_posix())
checksums = [hashlib.sha256(p.read_bytes()).hexdigest() for p in files]
checksums += [hashlib.sha256((frontend/'index.html').read_bytes()).hexdigest(),hashlib.sha256((root/'Dockerfile').read_bytes()).hexdigest()]
version = hashlib.sha256(('\n'.join(checksums)+'\n').encode()).hexdigest()[:16]

def read(path):
    authorization = base64.b64encode(('staging:'+os.environ['STAGING_ACCESS_TOKEN']).encode()).decode()
    request = urllib.request.Request(base+path,headers={'Cache-Control':'no-cache','Authorization':'Basic '+authorization})
    with urllib.request.urlopen(request,timeout=20) as response:
        if response.headers.get('X-Ficusin-Environment') != 'staging':
            raise RuntimeError('Target is not running in isolated staging mode')
        if 'noindex' not in response.headers.get('X-Robots-Tag',''):
            raise RuntimeError('Staging indexing guard missing')
        return response.read()

print('candidate',os.environ.get('GITHUB_SHA',''), 'expected version',version,flush=True)
for attempt in range(40):
    try:
        health = json.loads(read('/api/v1/health'))
        ready = json.loads(read('/api/v1/ready'))
        if health.get('status') != 'ok' or ready.get('status') != 'ok' or health.get('version') != version:
            raise RuntimeError('Waiting for exact backend revision')
        html = read('/').decode()
        if expected_js not in html or expected_css not in html:
            raise RuntimeError('Waiting for exact frontend revision')
        for asset in (expected_js,expected_css):
            if read(asset) != (frontend/asset.lstrip('/')).read_bytes():
                raise RuntimeError('Candidate asset bytes differ')
        if 'Disallow: /' not in read('/robots.txt').decode():
            raise RuntimeError('robots.txt does not prohibit staging indexing')
        catalog = json.loads(read('/api/v1/catalog'))
        if not catalog.get('products'):
            raise RuntimeError('No catalog products for browser verification')
        print('Verified exact staging candidate',version,'products',len(catalog['products']),flush=True)
        break
    except (urllib.error.URLError,RuntimeError,ValueError,TimeoutError,ConnectionError) as error:
        print('attempt',attempt+1,str(error),flush=True)
        if attempt == 39:
            raise
        time.sleep(15)
