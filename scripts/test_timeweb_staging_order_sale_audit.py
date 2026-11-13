import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('audit', Path(__file__).with_name('timeweb-staging-order-sale-audit.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


def app():
    return {'id':280757,'name':'ficusin-staging','branch_name':'staging','envs':{
        'APP_ENV':'staging','SITE_URL':audit.SITE,
        'DATABASE_URL':'postgres://ficusin_stage_20261031:not-for-output@db.invalid/ficusin_staging_20261031'}}


class Cursor:
    def __init__(self, value): self.value = value
    def fetchone(self): return self.value
    def fetchall(self): return self.value


class Connection:
    def __init__(self): self.queries=[]
    def __enter__(self): return self
    def __exit__(self,*args): return False
    def execute(self,query):
        self.queries.append(query)
        if not query.startswith('SELECT '): raise AssertionError('non-read SQL')
        if 'current_database' in query:return Cursor((audit.DATABASE,audit.LOGIN,'on'))
        if 'pg_roles' in query:return Cursor((False,False,False))
        if 'warehouses' in query:return Cursor([(7,'Test warehouse')])
        if 'to_regclass' in query:return Cursor((False,))
        if 'public.settings' in query:return Cursor(('0',))
        raise AssertionError('unexpected SQL')


class AuditTest(unittest.TestCase):
    def test_wrong_app_database_or_role_fails_before_connect(self):
        for key,value in [('id',229223),('branch_name','main')]:
            data=app();data[key]=value
            with self.assertRaises(audit.SafetyError):audit.guard(data)
        for key,value in [('APP_ENV','production'),('DATABASE_URL','postgres://gen_user:x@db.invalid/default_db')]:
            data=app();data['envs'][key]=value
            with self.assertRaises(audit.SafetyError):audit.guard(data)
    def test_alternate_branch_metadata_shape_does_not_relax_database_guard(self):
        data=app();data['branch']=data.pop('branch_name')
        self.assertEqual(audit.branch_name(data),'staging')
        self.assertEqual(audit.guard(data)['APP_ENV'],'staging')
        data['envs']['DATABASE_URL']='postgres://gen_user:x@db.invalid/default_db'
        with self.assertRaises(audit.SafetyError):audit.guard(data)
    def test_only_read_only_sql_and_safe_report(self):
        connection=Connection()
        def connect(dsn,**kwargs):
            self.assertIn('default_transaction_read_only=on',kwargs['options'])
            self.assertIn('statement_timeout=10000',kwargs['options'])
            self.assertEqual(kwargs['sslmode'],'verify-full')
            return connection
        with patch.object(audit,'read_app',return_value=app()):result=audit.inspect(connect)
        self.assertEqual(result['activeWarehouses'],[{'id':7,'name':'Test warehouse'}])
        self.assertNotIn('not-for-output',str(result))
        self.assertNotIn('db.invalid',str(result))
        self.assertTrue(all(q.startswith('SELECT ') for q in connection.queries))
        self.assertEqual(result['orderAutoCancelHours'],0)
        self.assertFalse(result['orderAutoCancelEnabled'])
        self.assertEqual(result['orderAutoCancelSource'],'stored')
    def test_expiry_default_disabled_numeric_and_invalid_are_safe(self):
        for row,hours,source,enabled in [(None,24,'default',True),(('24',),24,'stored',True),((' 0 ',),0,'stored',False),(('-1',),-1,'stored',False),(('+12',),12,'stored',True),(('misplaced-secret',),24,'invalid_fallback',True),(('9223372036854775808',),24,'invalid_fallback',True)]:
            result=audit.safe_expiry(row)
            self.assertEqual(result,{'orderAutoCancelHours':hours,'orderAutoCancelSource':source,'orderAutoCancelEnabled':enabled})
            self.assertNotIn('secret',str(result))
    def test_malformed_flags_cannot_leak_misplaced_secret(self):
        report=audit.safe_flags({'APP_ENV':'staging','ORDER_REALIZATION_MODE':'secret-text','ORDER_REALIZATION_SITE_WAREHOUSE_ID':'secret-value'})
        self.assertNotIn('secret',str(report))
        self.assertEqual(report['ORDER_REALIZATION_MODE'],'<invalid>')


if __name__=='__main__':unittest.main()
