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
        self.assertEqual(inventory.storage_flags(env),{'privateConfigured':True,'publicConfigured':True,'distinctBuckets':False,'objectsWritten':0})
        self.assertNotIn('SECRET',str(inventory.storage_flags(env)))

if __name__=='__main__':unittest.main()

class ReadOnlyObjectTests(unittest.TestCase):
    def setUp(self):
        spec=importlib.util.spec_from_file_location('object_reader',Path(__file__).with_name('readonly-private-object-verification.py'))
        self.reader=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.reader)
    def test_exact_get_escaping_and_signed_authorization(self):
        class Response:
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,limit):return b'original bytes'
        class Opener:
            def open(inner,request,timeout):
                self.assertEqual(request.method,'GET')
                self.assertEqual(request.full_url,'https://s3.twcstorage.ru/private/old/a%20%2B%20b.png')
                self.assertIn('AWS4-HMAC-SHA256 Credential=access/',request.get_header('Authorization'))
                self.assertIsNone(request.data)
                return Response()
        self.assertEqual(self.reader.signed_get({'PRIVATE_S3_BUCKET':'private','S3_ACCESS_KEY':'access','S3_SECRET_KEY':'secret'},'old/a + b.png',Opener()),b'original bytes')
    def test_non_timeweb_or_insecure_configuration_blocked(self):
        for endpoint in ['http://s3.twcstorage.ru','https://evil.example','https://s3.twcstorage.ru/path','https://user:pass@s3.twcstorage.ru','https://s3.twcstorage.ru?key=x']:
            with self.assertRaises(ValueError):self.reader.signed_get({'PRIVATE_S3_ENDPOINT':endpoint},'key')
