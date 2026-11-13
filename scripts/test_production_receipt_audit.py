import datetime
import importlib.util
import io
import json
from pathlib import Path
import unittest
import urllib.error

s=importlib.util.spec_from_file_location('fiscal_audit',Path(__file__).with_name('production-receipt-audit.py'))
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
ID='aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'

def row():
 return dict(provider_payment_id=ID,amount_minor=19900,receipt_status='',settlement_status='',settlement_reference=False,
  effective_status='completed',receipt_enabled=None,request_modes=[],created_at=datetime.datetime(2026,8,5),receipt_checked_at=None,
  income_warning=True,settlement_warning=True)

class Reader:
 def __init__(self): self.calls=0
 def get(self,path,params=None):
  self.calls+=1
  if path.startswith('/payments/'):
   return {'ok':True,'httpStatus':200},{'id':ID,'status':'succeeded','test':True,'amount':{'value':'199.00','currency':'RUB'},'metadata':{'email':'DO_NOT_LOG_CUSTOMER'}}
  return {'ok':True,'httpStatus':200},{'items':[{'payment_id':ID,'type':'payment','status':'succeeded','items':[{'payment_mode':'full_payment','description':'DO_NOT_LOG_NAME'}]}]}

class Tests(unittest.TestCase):
 def test_aggregate_overlap_and_no_customer_payment_ids_or_amounts(self):
  report=m.reconcile([row()],Reader())
  self.assertEqual(report['warnings'],{'income':1,'settlement':1});self.assertEqual(report['paymentCount'],1)
  self.assertTrue(all(g['providerTest'] is True and g['providerReceipt']=='absent' and g['amountMatches'] for g in report['groups']))
  text=json.dumps(report)
  for secret in (ID,'DO_NOT_LOG','199.00','provider_payment_id','amount_minor'):self.assertNotIn(secret,text)
  self.assertFalse(report['databaseWrites']);self.assertFalse(report['providerWrites'])

 def test_missing_or_failed_get_never_fabricates_a_receipt_status(self):
  class Failed:
   def get(self,path,params=None):
    if path=='/receipts':return {'ok':True,'httpStatus':200},{'items':[]}
    return {'ok':False,'httpStatus':404},None
  result=m.reconcile([row()],Failed())
  self.assertEqual(result['groups'][0]['providerRead']['httpStatus'],404)
  self.assertNotIn('providerReceipt',result['groups'][0])
  self.assertTrue(result['groups'][0]['providerReceipts']['complete'])
  self.assertEqual(result['groups'][0]['providerReceipts']['receipts'],[])

 def test_fixed_get_only_url_and_denied_paths(self):
  seen=[]
  class Response(io.BytesIO):
   status=200
  class Opener:
   def open(self,request,timeout):
    seen.append((request.method,request.full_url));return Response(b'{}')
  reader=m.YooReader('SHOP_SECRET','KEY_SECRET',Opener())
  reader.get('/payments/'+ID);reader.get('/receipts',{'payment_id':ID,'limit':100})
  self.assertTrue(all(method=='GET' and url.startswith(m.BASE+'/') for method,url in seen))
  for path in ('/payments/'+ID+'/capture','/refunds','https://evil.invalid/'):
   with self.assertRaises(m.guards.PreflightError):reader.get(path)
  with self.assertRaises(m.guards.PreflightError):reader.get('/receipts',{'unexpected':'secret'})

 def test_receipt_filter_identity_must_match(self):
  class Bad:
   def get(self,*args):return {'ok':True,'httpStatus':200},{'items':[{'payment_id':'different'}]}
  with self.assertRaises(m.guards.PreflightError):m.receipt_list(Bad(),ID)

 def test_exact_amount_and_currency_comparison(self):
  self.assertTrue(m.amount_matches({'amount':{'value':'0.01','currency':'RUB'}},1))
  self.assertFalse(m.amount_matches({'amount':{'value':'199.00','currency':'USD'}},19900))
  self.assertFalse(m.amount_matches({'amount':{'value':'NaN','currency':'RUB'}},19900))

if __name__=='__main__':unittest.main()
