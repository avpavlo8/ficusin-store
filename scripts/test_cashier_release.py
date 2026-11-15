import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('cashier_release',Path(__file__).with_name('cashier-release.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

def prod():
    return {'id':229223,'name':'ficusin-store','branch_name':'main','commit_sha':m.SOURCE,'status':'active',
      'envs':{'APP_ENV':'production','DATABASE_URL':'postgres://fixture:NEVER_PRINT@db.invalid/default_db',
      'SABY_APP_CLIENT_ID':'private-client','SABY_APP_SECRET':'private-app','SABY_SECRET_KEY':'private-key',
      'UNRELATED':'must-survive'}}

def stage():
    return {'id':280757,'branch_name':'staging','commit_sha':m.STAGE_OLD,'status':'active','health_check':'',
      'envs':{'APP_ENV':'staging','DATABASE_URL':'postgres://ficusin_stage_20261031:NEVER_PRINT@db.invalid/ficusin_staging_20261031',
      'STAGING_ACCESS_TOKEN':'private-stage'}}

class Fake:
    def __init__(self,app): self.app=copy.deepcopy(app);self.calls=[];self.gets=0
    def request(self,method,path,body=None):
        self.calls.append((method,path,copy.deepcopy(body)))
        if method=='GET': self.gets+=1;return {'app':copy.deepcopy(self.app)}
        if method=='PATCH': self.app['envs']=copy.deepcopy(body['envs'])
        return {}

class ReleaseTests(unittest.TestCase):
    def test_only_required_posting_flags_decide_unposted(self):
        names=['Проведен','ЕстьПроводки','ОшибкиПроведения','НеактуальныеПроводки','ВОчередиНаПроведение','ВОчередиНаРасчетПроводок','ИнойФлаг']
        raw={'s':[{'n':'ДокументРасширение.Состояние','t':{'n':'Флаги','s':{str(i):n for i,n in enumerate(names)}}}]}
        doc={'ДокументРасширение.Состояние':[None,False,False,False,None,False,True]}
        self.assertTrue(m.unposted(raw,doc))
        doc['ДокументРасширение.Состояние'][0]=True
        self.assertFalse(m.unposted(raw,doc))
        self.assertFalse(m.unposted({},doc))
    def test_backup_id_read_is_allowed(self):
        import io
        class Response(io.BytesIO):
            def __enter__(self):return self
            def __exit__(self,*args):self.close()
        class Opener:
            def open(self,req,timeout):return Response(b'{"backup":{"id":42}}')
        p=m.Provider('fixture');p.opener=Opener()
        self.assertEqual(p.request('GET','/dbs/4192781/backups/42'),{'backup':{'id':42}})
    def test_activate_changes_only_six_flags(self):
        p=Fake(prod())
        with patch.object(m,'database',return_value={'migrationApplied':True}): result=m.activate(p,m.SOURCE)
        self.assertTrue(result['patched'])
        self.assertEqual(p.app['envs'],{**prod()['envs'],**m.FLAGS})
        self.assertEqual([x[0] for x in p.calls],['GET','GET','PATCH','GET'])
    def test_enabled_is_not_written_again(self):
        app=prod();app['envs'].update(m.FLAGS);p=Fake(app)
        with patch.object(m,'database',return_value={}): result=m.activate(p,m.SOURCE)
        self.assertFalse(result['patched']);self.assertTrue(all(x[0]=='GET' for x in p.calls))
    def test_concurrent_environment_change_stops_patch(self):
        class Changed(Fake):
            def request(self,method,path,body=None):
                if self.gets==1:self.app['envs']['UNRELATED']='changed'
                return super().request(method,path,body)
        p=Changed(prod())
        with patch.object(m,'database',return_value={}),self.assertRaises(m.g.PreflightError):m.activate(p,m.SOURCE)
        self.assertTrue(all(x[0]=='GET' for x in p.calls))
    def test_database_barrier_stops_activation(self):
        p=Fake(prod())
        with patch.object(m,'database',side_effect=m.g.PreflightError('cashier_migration_missing')),self.assertRaises(m.g.PreflightError):m.activate(p,m.SOURCE)
        self.assertEqual(len(p.calls),1)
    def test_stage_deploy_preserves_environment_and_branch(self):
        p=Fake(stage());before=copy.deepcopy(p.app)
        result=m.deploy_stage(p)
        self.assertTrue(result['branchUntouched']);self.assertEqual(p.app,before)
        self.assertEqual(p.calls[-1],('POST','/apps/280757/deploy',{'commit_sha':m.SOURCE}))
    def test_stage_rejects_prod_database(self):
        app=stage();app['envs']['DATABASE_URL']=prod()['envs']['DATABASE_URL'];p=Fake(app)
        with self.assertRaises(m.g.PreflightError):m.deploy_stage(p)
        self.assertEqual(len(p.calls),1)
    def test_stage_rejects_concurrent_unrecognized_commit(self):
        app=stage();app['commit_sha']='a'*40;p=Fake(app)
        with self.assertRaises(m.g.PreflightError):m.deploy_stage(p)
        self.assertEqual(len(p.calls),1)
    def test_unknown_patch_is_not_replayed(self):
        class Lost(Fake):
            def request(self,method,path,body=None):
                result=super().request(method,path,body)
                if method=='PATCH':raise TimeoutError()
                return result
        p=Lost(prod())
        with patch.object(m,'database',return_value={}),self.assertRaises(TimeoutError):m.activate(p,m.SOURCE)
        self.assertEqual(sum(x[0]=='PATCH' for x in p.calls),1)

if __name__=='__main__':unittest.main()
