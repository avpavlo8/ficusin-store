import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('smoke',Path(__file__).with_name('timeweb-staging-order-sale-smoke.py'))
smoke=importlib.util.module_from_spec(spec);spec.loader.exec_module(smoke)
SHA='a'*40
SECRET='credential-that-must-never-be-printed'

def app():
    return {'id':280757,'name':'ficusin-staging','branch':'staging','status':'active','commit_sha':SHA,'envs':{
        'APP_ENV':'staging','SITE_URL':smoke.audit.SITE,
        'DATABASE_URL':'postgresql://ficusin_stage_20261031:'+SECRET+'@isolated.invalid/ficusin_staging_20261031',
        'FILE_STORAGE_MODE':'mock','CATALOG_PUBLICATION_MODE':'mock','CATALOG_PUBLICATION_LIVE_ENABLED':'false',
        'ORDER_REALIZATION_MODE':'mock','ORDER_REALIZATION_LIVE_ENABLED':'false','ORDER_REALIZATION_SITE_WAREHOUSE_ID':'1'}}

def inspect(*args,**kwargs):
    return {'dispatchControl':{'executionMode':'mock','enabled':True,'siteWarehouseId':1},'activeWarehouses':[{'id':1,'name':'Основной склад'}]}

class SmokeSafety(unittest.TestCase):
    def test_default_is_read_only_and_does_not_launch_or_need_candidate(self):
        writes=[]
        result=smoke.execute(SHA,'/absent',reader=app,inspector=inspect,launcher=lambda *_:writes.append(True))
        self.assertFalse(result['applied']);self.assertEqual(writes,[])
    def test_every_target_guard_precedes_fixture_runner(self):
        for mutate in (lambda a:a.update(id=229223),lambda a:a.update(branch='main'),lambda a:a.update(commit_sha='b'*40),lambda a:a['envs'].update(APP_ENV='production'),lambda a:a['envs'].update(ORDER_REALIZATION_MODE='live'),lambda a:a['envs'].update(ORDER_REALIZATION_LIVE_ENABLED='true'),lambda a:a['envs'].update(DATABASE_URL='postgres://wrong@host/prod')):
            value=app();mutate(value);writes=[]
            with self.assertRaises(smoke.audit.SafetyError):smoke.execute(SHA,'/absent',True,reader=lambda:value,inspector=inspect,launcher=lambda *_:writes.append(True))
            self.assertEqual(writes,[])
    def test_actual_control_disabled_or_wrong_warehouse_is_rejected(self):
        for mode,enabled,wh in [('live',True,1),('mock',False,1),('mock',True,2)]:
            with self.assertRaises(smoke.audit.SafetyError):smoke.preflight(app(),SHA,lambda *_a,**_k:{'dispatchControl':{'executionMode':mode,'enabled':enabled,'siteWarehouseId':wh},'activeWarehouses':[{'id':1}]})
    def test_concurrent_deploy_or_secret_env_drift_prevents_fixture(self):
        old=app();fresh=app();fresh['envs']['UNRELATED']='new';reads=iter([old,fresh]);writes=[]
        with self.assertRaisesRegex(smoke.audit.SafetyError,'changed_during_preflight'):
            smoke.execute(SHA,'/absent',True,reader=lambda:next(reads),inspector=inspect,launcher=lambda *_:writes.append(True))
        self.assertFalse(writes)
    def test_deployment_during_worker_observation_cannot_report_pass(self):
        old=app();changed=app();changed['commit_sha']='b'*40;reads=iter([old,old,changed]);calls=[]
        with self.assertRaisesRegex(smoke.audit.SafetyError,'changed_during_smoke'):
            smoke.execute(SHA,'/absent',True,reader=lambda:next(reads),inspector=inspect,launcher=lambda *a:(calls.append(a) or {'result':'passed'}))
        self.assertEqual(len(calls),1)
    def test_apply_calls_only_guarded_launcher_exactly_once(self):
        calls=[]
        result=smoke.execute(SHA,'/absent',True,reader=app,inspector=inspect,launcher=lambda *a:(calls.append(a) or {'result':'passed'}))
        self.assertEqual(len(calls),1);self.assertEqual(calls[0][2],SHA);self.assertEqual(result,{'result':'passed'})
    def test_raw_child_output_and_environment_secrets_never_escape(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            candidate=Path(directory);(candidate/'backend').mkdir();seen=[]
            def run(args,**kw):
                seen.append(kw)
                return subprocess.CompletedProcess(args,1,SECRET+'\n'+json.dumps({'probe':'staging_sale_smoke','result':'mock_worker_timeout','cleanup':'retained','orderId':42}),SECRET)
            output=io.StringIO()
            with patch.object(smoke,'candidate_head',return_value=SHA),patch.dict(smoke.os.environ,{'TIMEWEB_CLOUD_TOKEN':SECRET,'SABY_PASSWORD':SECRET}),contextlib.redirect_stdout(output):
                with self.assertRaisesRegex(smoke.audit.SafetyError,'details_masked'):smoke.launch(candidate,app()['envs'],SHA,run)
            self.assertNotIn(SECRET,output.getvalue());self.assertIn('retained',output.getvalue())
            self.assertNotIn('TIMEWEB_CLOUD_TOKEN',seen[0]['env']);self.assertNotIn('SABY_PASSWORD',seen[0]['env'])
            self.assertEqual(list((candidate/'backend').iterdir()),[])
    def test_success_requires_pass_and_confirmed_cleanup(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            candidate=Path(directory);(candidate/'backend').mkdir()
            for result in ({'result':'passed','cleanup':'retained'},{'result':'mock_worker_timeout','cleanup':'removed'}):
                with patch.object(smoke,'candidate_head',return_value=SHA),contextlib.redirect_stdout(io.StringIO()):
                    with self.assertRaises(smoke.audit.SafetyError):smoke.launch(candidate,app()['envs'],SHA,lambda args,**kw:subprocess.CompletedProcess(args,0,json.dumps({'probe':'staging_sale_smoke',**result}),''))

if __name__=='__main__':unittest.main()
