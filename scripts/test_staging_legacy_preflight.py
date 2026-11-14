import importlib.util
from pathlib import Path
import unittest
s=importlib.util.spec_from_file_location('stage',Path(__file__).with_name('staging-legacy-preflight.py'));m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class Tests(unittest.TestCase):
 def app(self):return {'id':280757,'name':'ficusin-staging','commit_sha':'a'*40,'branch_name':'staging','envs':{'APP_ENV':'staging','CATALOG_PUBLICATION_MODE':'mock','CATALOG_PUBLICATION_LIVE_ENABLED':'false','FILE_STORAGE_MODE':'mock','DATABASE_URL':'postgres://ficusin_stage_20261031:key@host/ficusin_staging_20261031'}}
 def test_exact_isolation(self):self.assertEqual(m.staging_settings(self.app(),'a'*40)['APP_ENV'],'staging')
 def test_reject_production_or_live(self):
  for field,val in [('id',229223),('commit_sha','b'*40),('branch_name','main')]:
   app=self.app();app[field]=val
   with self.assertRaises(m.guards.PreflightError):m.staging_settings(app,'a'*40)
  for field,val in [('APP_ENV','production'),('DATABASE_URL','postgres://u:k@host/default_db'),('CATALOG_PUBLICATION_MODE','live'),('FILE_STORAGE_MODE','s3'),('ORDER_REALIZATION_MODE','live')]:
   app=self.app();app['envs'][field]=val
   with self.assertRaises(m.guards.PreflightError):m.staging_settings(app,'a'*40)
if __name__=='__main__':unittest.main()
