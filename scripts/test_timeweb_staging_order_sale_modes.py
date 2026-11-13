import copy
import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('modes',Path(__file__).with_name('timeweb-staging-order-sale-modes.py'))
modes=importlib.util.module_from_spec(spec)
spec.loader.exec_module(modes)


def app():
    return {'id':280757,'name':'ficusin-staging','branch':'staging','status':'active','commit_sha':'a'*40,'health_check':'',
            'envs':{'APP_ENV':'staging','SITE_URL':modes.audit.SITE,'DATABASE_URL':'postgres://ficusin_stage_20261031:secret-not-for-output@db.invalid/ficusin_staging_20261031','OTHER_KEY':'preserved-secret'}}


def inspector(_app,emit_metadata=True):
    assert emit_metadata is False
    return {'databaseIsolated':True,'databaseReadOnly':True,'rolePrivileged':False,'activeWarehouses':[{'id':1,'name':'Main'}]}


class ModeTests(unittest.TestCase):
    def test_default_dry_run_never_writes_or_leaks(self):
        writes=[]
        report=modes.run(reader=app,inspector=inspector,writer=lambda *args:writes.append(args))
        self.assertEqual(writes,[])
        self.assertFalse(report['applyRequested']);self.assertFalse(report['applied'])
        self.assertNotIn('secret',str(report));self.assertNotIn('db.invalid',str(report))
        self.assertEqual(set(report['before']),set(modes.TARGET))
    def test_explicit_apply_preserves_every_unrelated_value_and_checks_after(self):
        current=app();writes=[]
        def read():return copy.deepcopy(current)
        def write(before,next_env):
            self.assertEqual(before['id'],280757)
            self.assertEqual(next_env['OTHER_KEY'],'preserved-secret')
            self.assertEqual({key for key in next_env if next_env.get(key)!=before['envs'].get(key)},set(modes.TARGET))
            writes.append(1);current['envs']=dict(next_env);current['health_check']=''
        report=modes.run(True,read,inspector,write)
        self.assertEqual(writes,[1]);self.assertTrue(report['applied'])
        self.assertEqual(report['after'],modes.TARGET)
        again=modes.run(True,read,inspector,write)
        self.assertEqual(writes,[1]);self.assertFalse(again['applied'])
    def test_wrong_target_and_warehouse_never_write(self):
        writes=[]
        for change in ('production','warehouse'):
            data=app()
            check=inspector
            if change=='production':data['id']=229223
            else:check=lambda *_args,**_kwargs:{'databaseIsolated':True,'databaseReadOnly':True,'rolePrivileged':False,'activeWarehouses':[{'id':2}]}
            with self.assertRaises(modes.audit.SafetyError):modes.run(True,lambda:data,check,lambda *args:writes.append(args))
        self.assertEqual(writes,[])
    def test_concurrent_configuration_change_aborts_without_overwrite(self):
        data=app();calls=0;writes=[]
        def read():
            nonlocal calls
            calls+=1;copy_app=copy.deepcopy(data)
            if calls>1:copy_app['envs']['OTHER_KEY']='new-value'
            return copy_app
        with self.assertRaises(modes.audit.SafetyError):modes.run(True,read,inspector,lambda *args:writes.append(args))
        self.assertEqual(writes,[])


if __name__=='__main__':unittest.main()
