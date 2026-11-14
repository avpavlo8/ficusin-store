"""Bounded signed GET only; never log keys, contents, credentials, or URLs."""
import datetime
import hashlib
import hmac
import re
import urllib.parse
import urllib.request

MAX_OBJECT = 64 << 20

def signed_get(env, key, opener=None):
    endpoint=(env.get('PRIVATE_S3_ENDPOINT') or env.get('S3_ENDPOINT') or 'https://s3.twcstorage.ru').rstrip('/')
    parsed=urllib.parse.urlsplit(endpoint)
    if parsed.scheme!='https' or not parsed.hostname or not (parsed.hostname=='s3.twcstorage.ru' or parsed.hostname.endswith('.twcstorage.ru')) or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ('','/'):
        raise ValueError('unsafe_object_endpoint')
    bucket=env.get('PRIVATE_S3_BUCKET','')
    access=env.get('PRIVATE_S3_ACCESS_KEY') or env.get('S3_ACCESS_KEY','')
    secret=env.get('PRIVATE_S3_SECRET_KEY') or env.get('S3_SECRET_KEY','')
    region=env.get('PRIVATE_S3_REGION') or env.get('S3_REGION') or 'ru-1'
    if not bucket or not access or not secret or not isinstance(key,str) or not key or len(key)>2048 or not re.fullmatch(r'[a-zA-Z0-9._-]+',bucket):
        raise ValueError('unsafe_object_configuration')
    path=urllib.parse.quote('/'+bucket+'/'+key.lstrip('/'),safe='/-_.~')
    now=datetime.datetime.now(datetime.timezone.utc);date=now.strftime('%Y%m%d');stamp=now.strftime('%Y%m%dT%H%M%SZ')
    empty=hashlib.sha256(b'').hexdigest();signed='host;x-amz-content-sha256;x-amz-date'
    canonical='\n'.join(['GET',path,'','host:'+parsed.netloc+'\nx-amz-content-sha256:'+empty+'\nx-amz-date:'+stamp+'\n',signed,empty])
    scope=date+'/'+region+'/s3/aws4_request'
    message='\n'.join(['AWS4-HMAC-SHA256',stamp,scope,hashlib.sha256(canonical.encode()).hexdigest()])
    def sign(k,v):return hmac.new(k,v.encode(),hashlib.sha256).digest()
    signing=sign(sign(sign(sign(('AWS4'+secret).encode(),date),region),'s3'),'aws4_request')
    signature=hmac.new(signing,message.encode(),hashlib.sha256).hexdigest()
    request=urllib.request.Request(endpoint+path,method='GET',headers={'X-Amz-Date':stamp,'X-Amz-Content-Sha256':empty,'Authorization':'AWS4-HMAC-SHA256 Credential='+access+'/'+scope+', SignedHeaders='+signed+', Signature='+signature})
    if opener is None:
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self,*args,**kwargs):raise ValueError('unexpected_object_redirect')
        opener=urllib.request.build_opener(NoRedirect())
    with opener.open(request,timeout=30) as response:
        data=response.read(MAX_OBJECT+1)
    if not data or len(data)>MAX_OBJECT:raise ValueError('invalid_object_size')
    return data
