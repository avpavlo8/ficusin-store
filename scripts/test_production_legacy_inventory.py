import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('legacy_inventory',Path(__file__).with_name('production-legacy-data-inventory.py'))
inventory=importlib.util.module_from_spec(spec);spec.loader.exec_module(inventory)

class Cursor:
    def __init__(self):self.queries=[]
    def execute(self,sql):self.queries.append(sql)
    def fetchall(self):return []

class Tests(unittest.TestCase):
    def test_missing_sources_are_visible_without_data_reads(self):
        cursor=Cursor();report=inventory.inspect(cursor)
        self.assertEqual(len(cursor.queries),1)
        self.assertTrue(all(not row['available'] for row in report['files'].values()))
    def test_rejects_database_mutations_even_inside_cte(self):
        for sql in ['DELETE FROM orders','WITH x AS (UPDATE orders SET status=1 RETURNING *) SELECT * FROM x','SELECT 1; DROP TABLE orders']:
            with self.assertRaises(inventory.guards.PreflightError):inventory.select(Cursor(),sql)
    def test_private_configuration_matches_runtime_defaults(self):
        flags=inventory.storage_flags({'PRIVATE_S3_BUCKET':'private','S3_BUCKET':'public','S3_ACCESS_KEY':'key','S3_SECRET_KEY':'secret'})
        self.assertTrue(flags['privateConfigured']);self.assertTrue(flags['publicConfigured']);self.assertTrue(flags['distinctBuckets'])
    def test_configuration_returns_flags_never_keys(self):
        env={prefix+key:'SECRET' for prefix in ['S3_','PRIVATE_S3_'] for key in ['ENDPOINT','REGION','BUCKET','ACCESS_KEY','SECRET_KEY']}
        self.assertEqual(inventory.storage_flags(env),{'privateConfigured':True,'publicConfigured':True,'distinctBuckets':False,'objectsReadOrWritten':0})
        self.assertNotIn('SECRET',str(inventory.storage_flags(env)))

if __name__=='__main__':unittest.main()
