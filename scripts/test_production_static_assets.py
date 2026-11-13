import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
import urllib.error

spec=importlib.util.spec_from_file_location("static_ops",Path(__file__).with_name("production-static-assets.py"))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
SHA="a"*40
HTML='<script type="module" src="/assets/app-test.js"></script><link rel="stylesheet" href="/assets/app-test.css">'


def app_fixture():
 return {"id":m.APP_ID,"name":m.APP_NAME,"branch_name":"main","commit_sha":SHA,"status":"active",
  "envs":{"APP_ENV":"production","S3_BUCKET":m.BUCKET,"S3_ACCESS_KEY":"PRIVATE_ACCESS_MUST_NOT_PRINT","S3_SECRET_KEY":"PRIVATE_SECRET_MUST_NOT_PRINT","PRIVATE_S3_BUCKET":"ficusin-private","SABY_SECRET_KEY":"VENDOR_SECRET","DATABASE_URL":"PRIVATE_DSN"}}

def bucket_fixture():
 return {"id":m.BUCKET_ID,"name":m.BUCKET,"type":"public","location":"ru-1","status":"created","is_allow_auto_upgrade":False,
 "disk_stats":{"size":1<<20,"used":1,"is_unlimited":False}}

class Provider:
 def __init__(self):self.value=app_fixture();self.metadata=bucket_fixture();self.patches=[];self.unknown=False
 def app(self):return copy.deepcopy(self.value)
 def bucket(self):return copy.deepcopy(self.metadata)
 def request(self,method,path,body=None):
  assert method=="PATCH" and path==f"/apps/{m.APP_ID}" and set(body)=={"envs"}
  self.patches.append(copy.deepcopy(body));self.value["envs"]=copy.deepcopy(body["envs"])
  if self.unknown:raise urllib.error.URLError("SECRET_DSNS_MUST_NOT_PRINT")
  return {}
class Storage:
 def __init__(self,existing=False):self.existing=existing
 def list_objects_v2(self,**args):
  key="site/releases/"+SHA+"/old" if self.existing else "products/PRIVATE_OBJECT_KEY"
  return {"IsTruncated":False,"Contents":[{"Key":key,"Size":11}]}

def build(source,dist):
 (dist/"assets").mkdir(parents=True)
 (dist/"index.html").write_text(HTML)
 (dist/"assets/app-test.js").write_bytes(b"test-js")
 (dist/"assets/app-test.css").write_bytes(b"test-css")

def publisher_fixture(calls):
 def publish(source,dist,prefix,path,settings,apply):
  calls.append(apply);files=[];total=0
  for name in ("assets/app-test.js","assets/app-test.css"):
   content=(dist/name).read_bytes();total+=len(content)
   files.append({"path":name,"key":prefix+"/"+name,"bytes":len(content),"sha256":hashlib.sha256(content).hexdigest(),"mime":"text/plain"})
  return {"prefix":prefix,"files":files,"bytes":total}
 return publish

class Tests(unittest.TestCase):
 def run_execute(self,provider=None,storage=None,apply=False,get_html=lambda:HTML,publisher=None,live_check=lambda paths:None):
  provider=provider or Provider();calls=[]
  with tempfile.TemporaryDirectory() as directory:
   report=m.execute(provider,storage or Storage(),Path(directory),directory,SHA,apply,builder=build,
    publisher=publisher or publisher_fixture(calls),get_html=get_html,live_check=live_check)
  return report,provider,calls

 def test_dryrun_builds_and_checks_without_upload_or_environment_patch(self):
  result,p,calls=self.run_execute()
  self.assertEqual(calls,[False]);self.assertEqual(p.patches,[]);self.assertTrue(result["dryRun"])
  self.assertFalse(result["uploadedVerified"]);self.assertEqual(result["fileCount"],2)
  encoded=json.dumps(result)
  for forbidden in ("PRIVATE_","VENDOR_SECRET","DATABASE_URL","sha256","SECRET_ACCESS"):
   self.assertNotIn(forbidden,encoded)

 def test_apply_upload_verify_then_env_only_patch_preserves_every_other_value(self):
  p=Provider();original=copy.deepcopy(p.value)
  result,p,calls=self.run_execute(p,apply=True)
  self.assertEqual(calls,[False,True]);self.assertEqual(len(p.patches),1)
  self.assertTrue(result["liveS3Confirmed"]);self.assertTrue(result["localFallbackRetained"])
  updated=p.patches[0]["envs"]
  for key,value in original["envs"].items():self.assertEqual(updated[key],value)
  self.assertEqual(set(updated)-set(original["envs"]),{"STATIC_ASSETS_BASE_URL"})
  self.assertEqual(p.value["commit_sha"],SHA);self.assertEqual(p.value["branch_name"],"main")

 def test_unknown_patch_only_reconciles_get_never_retries_patch(self):
  p=Provider();p.unknown=True
  result,p,_=self.run_execute(p,apply=True)
  self.assertTrue(result["ambiguousPatchReconciled"]);self.assertEqual(len(p.patches),1)

 def test_live_bundle_mismatch_or_nonempty_immutable_prefix_prevents_writes(self):
  for html,storage in ((HTML.replace("app-test","other"),Storage()),(HTML,Storage(existing=True))):
   p=Provider();calls=[]
   with self.assertRaises(m.AssetError),tempfile.TemporaryDirectory() as directory:
    m.execute(p,storage,Path(directory),directory,SHA,True,builder=build,publisher=publisher_fixture(calls),get_html=lambda:html)
   self.assertNotIn(True,calls);self.assertEqual(p.patches,[])

 def test_capacity_and_auto_upgrade_guard_do_not_raise_tariff(self):
  for size,used,upgrade in ((1,1,False),(1<<20,1,True)):
   p=Provider();p.metadata["disk_stats"].update(size=size,used=used);p.metadata["is_allow_auto_upgrade"]=upgrade
   with self.assertRaises(m.AssetError):self.run_execute(p,apply=True)
   self.assertEqual(p.patches,[])
  capacity=m.bucket_capacity(bucket_fixture());self.assertEqual(capacity["capacityBytes"],1<<30)

 def test_changed_verified_manifest_cannot_activate(self):
  calls=[];original=publisher_fixture(calls);p=Provider()
  def bad(*args):
   result=original(*args)
   if args[-1]:result["bytes"]+=1
   return result
  with self.assertRaises(m.AssetError):self.run_execute(p,apply=True,publisher=bad)
  self.assertEqual(p.patches,[])

 def test_child_environment_has_only_public_storage_secrets(self):
  env=m.publisher_env(app_fixture()["envs"])
  self.assertEqual(env["S3_BUCKET"],m.BUCKET)
  for forbidden in ("DATABASE_URL","PRIVATE_S3_BUCKET","SABY_SECRET_KEY","TIMEWEB_CLOUD_TOKEN"):
   self.assertNotIn(forbidden,env)
  self.assertEqual(env["GOENV"],"off");self.assertEqual(env["GOWORK"],"off")

 def test_provider_denies_mutations_in_dryrun_and_all_nonenv_operations(self):
  p=m.Provider("PRIVATE_TOKEN")
  for method,path,body in (("POST","/storages/buckets",{}),("PATCH",f"/apps/{m.APP_ID}",{"envs":{}}),("GET","/dbs/4192781",None)):
   with self.assertRaises(m.AssetError):p.request(method,path,body)

 def test_wait_for_restart_retries_only_read_confirmation(self):
  clock=[0];calls=[]
  def check(paths):
   calls.append(1)
   if len(calls)<3:raise m.AssetError("live_assets_not_confirmed_s3")
  m.wait_live_source({"/assets/a.js"},timeout=15,check=check,sleep=lambda n:clock.__setitem__(0,clock[0]+n),monotonic=lambda:clock[0])
  self.assertEqual(len(calls),3);self.assertEqual(clock[0],10)

 def test_clean_source_and_pinned_node24_guard(self):
  with tempfile.TemporaryDirectory() as directory:
   source=Path(directory);(source/"backend/cmd/publish-static-assets").mkdir(parents=True)
   (source/"backend/cmd/publish-static-assets/main.go").write_text("package main")
   docker=source/"Dockerfile";docker.write_text("FROM node:24-bookworm-slim@sha256:"+"a"*64+" AS frontend\n")
   def run(command,**kwargs):return subprocess.CompletedProcess(command,0,SHA.encode() if "rev-parse" in command else b"",b"PRIVATE_ERROR")
   self.assertEqual(m.check_source(source,SHA,run=run),source.resolve())
   docker.write_text("FROM node:latest AS frontend\n")
   with self.assertRaises(m.AssetError):m.check_source(source,SHA,run=run)

 def test_exact_app_revision_and_public_bucket_identity(self):
  app=app_fixture();app["commit_sha"]="b"*40
  with self.assertRaises(m.AssetError):m.check_app(app,SHA)
  bucket=bucket_fixture();bucket["id"]=603857
  with self.assertRaises(m.AssetError):m.bucket_capacity(bucket)

 def test_manifest_cli_checks_actual_files_hashes_and_excludes_shell(self):
  with tempfile.TemporaryDirectory() as directory:
   source=Path(directory);dist=source/"dist";build(source,dist);manifest=source/"manifest.json";calls=[]
   def run(command,**kwargs):
    calls.append(command)
    planned=publisher_fixture([])(source,dist,"site/releases/"+SHA,manifest,{},False)
    manifest.write_text(json.dumps({"dryRun":True,"verified":False,"manifest":planned}))
    return subprocess.CompletedProcess(command,0,b"PRIVATE_STDOUT",b"PRIVATE_STDERR")
   result=m.manifest_cli(source,dist,"site/releases/"+SHA,manifest,app_fixture()["envs"],False,run=run)
   self.assertEqual(len(result["files"]),2);self.assertEqual(len(calls),1)
   self.assertNotIn("--apply",calls[0])
   def bad(command,**kwargs):
    value=run(command,**kwargs);payload=json.loads(manifest.read_text());payload["manifest"]["files"][0]["sha256"]="0"*64;manifest.write_text(json.dumps(payload));return value
   with self.assertRaises(m.AssetError):m.manifest_cli(source,dist,"site/releases/"+SHA,manifest,app_fixture()["envs"],False,run=bad)

 def test_concurrent_environment_change_is_not_overwritten(self):
  p=Provider();original=p.app();p.value["envs"]["ROTATED_SETTING"]="PRIVATE_NEW_VALUE"
  with self.assertRaises(m.AssetError):m.patch_verified_environment(p,original["envs"],SHA,m.ENDPOINT+"/"+m.BUCKET+"/site/releases/"+SHA)
  self.assertEqual(p.patches,[])

if __name__=="__main__":unittest.main()
