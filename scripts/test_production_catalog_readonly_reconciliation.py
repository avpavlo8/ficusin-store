import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('catalog_reconciliation',Path(__file__).with_name('production-catalog-readonly-reconciliation.py'))
mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)


def record(fields):
    return {'_type':'record','s':[{'n':name} for name in fields],'d':list(fields.values())}


def rows(fields):
    names=list(fields[0]) if fields else ['id']
    return {'_type':'recordset','s':[{'n':name} for name in names],'d':[[field[n] for n in names] for field in fields]}


def fixture():
    payload={'variantId':11412,'productId':11412,'barcode':'private-barcode','name':'Private product','description':'one\ntwo','saby':{'category':{'id':'1041'},'folder':{'id':'3333'}},'images':['private-url']}
    wb={'channel':'wb','status':'failed','execution_mode':'live','payload':{**payload,'channel':{'article':'private-article'}},'progress':{'stage':'reconcile','createAttempted':True,'ids':{}},'pending_payload':None}
    progress={'stage':'send-save','createAttempted':True,'ids':{'productId':'3980','uuid':'00000000-0000-0000-0000-000000000001','offerId':'private-code'},'native':{'imageIds':['private-image-id'],'created':True,'preparedHash':'old-hash','preparedRequestHash':'old-request-hash'}}
    saby={'channel':'saby','status':'needs_attention','execution_mode':'live','payload':payload,'progress':progress,'pending_payload':{**payload,'name':'new name'}}
    return [wb,saby]


class Reader:
    def __init__(self,sequence):self.sequence=list(sequence);self.requests=[]
    def read(self,kind,body):
        self.requests.append((kind,body));expected,result=self.sequence.pop(0)
        if kind!=expected:raise AssertionError('unexpected method')
        return result
    def authenticate(self):self.requests.append(('saby_auth',{}))


class Tests(unittest.TestCase):
    def test_only_exact_two_unchanged_operations_accepted(self):
        self.assertEqual(set(mod.validate_rows(fixture())),{'wb','saby'})
        for change in ['status','variant','uuid','create','mode']:
            f=fixture()
            if change=='status':f[0]['status']='queued'
            if change=='variant':f[0]['payload']['variantId']=99
            if change=='uuid':f[1]['progress']['ids']['uuid']=''
            if change=='create':f[0]['progress']['createAttempted']=False
            if change=='mode':f[1]['execution_mode']='mock'
            with self.assertRaises(mod.guards.PreflightError):mod.validate_rows(f)
        with self.assertRaises(mod.guards.PreflightError):mod.validate_rows(fixture()[:1])

    def test_explicit_empty_article_does_not_inherit_common(self):
        self.assertEqual(mod.article({'channel':{'article':''},'marketplace':{'article':'old'}}),'')
        self.assertEqual(mod.article({'channel':{'article':None},'marketplace':{'wb':{'article':' own '},'article':'old'}}),'own')

    def test_wb_exact_article_and_barcode_provenance(self):
        reader=Reader([('wb_cards',{'cards':[{'nmID':7,'vendorCode':'private-article','sizes':[{'skus':['private-barcode']}]}],'cursor':{'total':1}}),('wb_errors',{'data':{'items':[{'errors':{'private-article':['ТНВЭД неверный','обязательный атрибут']},'updatedAt':'timestamp'}],'cursor':{'next':False}}})])
        result=mod.reconcile_wb(fixture()[0],reader)
        self.assertEqual(result['classification'],'exact_card_barcode_ownership_confirmed')
        self.assertEqual(result['errorTags'],['mandatory','tnved'])
        self.assertEqual(result['providerFirstBarcodeMatchCount'],1)
        self.assertFalse(result['replayAuthorized']);self.assertFalse(result['definitiveRejectionProven'])
        self.assertNotIn('private-',json.dumps(result))
        self.assertEqual(reader.requests[0][1]['settings']['filter']['textSearch'],'private-article')

    def test_wb_random_existing_article_is_not_our_card(self):
        reader=Reader([('wb_cards',{'cards':[{'nmID':7,'vendorCode':'private-article','sizes':[{'skus':['different']}]}]}),('wb_errors',{'data':[]})])
        result=mod.reconcile_wb(fixture()[0],reader)
        self.assertEqual(result['classification'],'article_collision_ownership_unproven');self.assertEqual(result['barcodeOwnedCardCount'],0)

    def test_wb_later_barcode_provenance_does_not_claim_provider_first_barcode_match(self):
        reader=Reader([('wb_cards',{'cards':[{'nmID':7,'vendorCode':'private-article','sizes':[{'skus':['other','private-barcode']}]}]}),('wb_errors',{'data':[]})])
        result=mod.reconcile_wb(fixture()[0],reader)
        self.assertEqual(result['barcodeOwnedCardCount'],1)
        self.assertEqual(result['providerFirstBarcodeMatchCount'],0)

    def test_wb_missing_card_and_exact_old_errors_does_not_authorize_create(self):
        reader=Reader([('wb_cards',{'cards':[]}),('wb_errors',{'data':[{'vendorCode':'other','errors':['private-article']},{'vendorCode':'private-article','errors':['ТН ВЭД']} ]})])
        result=mod.reconcile_wb(fixture()[0],reader)
        self.assertEqual(result['classification'],'no_exact_card_result_still_unknown');self.assertEqual(result['exactArticleErrorBatchCount'],1)
        self.assertFalse(result['definitiveRejectionProven']);self.assertFalse(result['savedBatchCorrelationAvailable'])

    def test_wb_bounded_search_never_claims_absence_from_first_page(self):
        seq=[]
        for i in range(mod.MAX_PAGES):seq.append(('wb_cards',{'cards':[{'vendorCode':'other'}]*100,'cursor':{'total':100,'updatedAt':'stamp'+str(i),'nmID':i+1}}))
        seq.append(('wb_errors',{'data':[]}))
        result=mod.reconcile_wb(fixture()[0],Reader(seq))
        self.assertFalse(result['cardsComplete']);self.assertEqual(result['classification'],'bounded_read_incomplete_result_unknown')

    def test_unrecognized_wb_schema_fails_closed(self):
        for result in [{'data':None},{'data':{}},{'data':{'items':{},'cursor':{}}}]:
            with self.assertRaises(mod.guards.PreflightError):mod.error_page(result,'article')

    def test_saby_exact_native_identity_and_readback_only(self):
        request=fixture()[1]['payload'];request['priceMinor']=123
        card={'Id':3980,'Identifier':'00000000-0000-0000-0000-000000000001','Number':'private-code','Name':'Private product','Category':1041,'Folder':3333,'IsDraft':False,'Description':[['p',{'version':'2'},'one'],['p',{'version':'2'},'two']], 'CodeList':rows([{'Code':'private-barcode','Type':2}]),'ImageData':rows([{'id':'private-image-id','isMaster':True}]),'PriceData':rows([{'PriceList':6,'Price':1.23}])}
        row=fixture()[1];row['payload']=request
        reader=Reader([('saby_read',{'result':record(card)})])
        result=mod.reconcile_saby(row,reader,{})
        self.assertTrue(result['identityVerified']);self.assertTrue(result['allCheckedFrozenFieldsMatch'])
        self.assertEqual(result['classification'],'identity_verified_legacy_receipt_requires_explicit_reprepare')
        self.assertFalse(result['replayAuthorized']);self.assertNotIn('private-',json.dumps(result))
        self.assertEqual(reader.requests[1][1]['method'],'Nomenclature.Read')
        self.assertEqual(reader.requests[1][1]['params']['Id'],3980)
        for field,newvalue in [('Id',3981),('Identifier','00000000-0000-0000-0000-000000000099'),('Number','other')]:
            wrong=copy.deepcopy(card);wrong[field]=newvalue
            with self.assertRaises(mod.guards.PreflightError):mod.reconcile_saby(row,Reader([('saby_read',{'result':record(wrong)})]),{})

    def test_null_or_missing_prices_are_unconfirmed_not_conversion_errors(self):
        for price,minor in [(None,123),(1.23,None),(1.23,0),(None,None),('NaN',123),('Infinity',123),('invalid',123),(True,100),(1,True)]:
            self.assertFalse(mod.native_price_matches_minor(price,minor))
        self.assertTrue(mod.native_price_matches_minor('1.23',123))
        self.assertFalse(mod.native_price_matches_minor('1.23',124))
        request=fixture()[1]['payload']
        card={'Name':'Private product','Category':1041,'Folder':3333,'IsDraft':False,'Description':[],
              'CodeList':rows([]),'ImageData':rows([]),'PriceData':rows([{'PriceList':6,'Price':None}])}
        for minor in [None,123]:
            if minor is not None:request['priceMinor']=minor
            fields=mod.saby_field_matches(card,request,fixture()[1]['progress'],6)
            self.assertFalse(fields['createdPrice'])
        card['PriceData']=rows([{'PriceList':6}])
        self.assertFalse(mod.saby_field_matches(card,request,fixture()[1]['progress'],6)['createdPrice'])

    def test_duplicate_saby_schema_rejected(self):
        with self.assertRaises(mod.guards.PreflightError):mod.native_record({'_type':'record','s':[{'n':'Id'},{'n':'Id'}],'d':[1,2]})

    def test_vendor_transport_fixed_read_allowlist_no_sensitive_output(self):
        class Headers:
            def get_content_charset(self):return 'utf-8'
        class Response:
            status=200;headers=Headers()
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,limit):return b'{"result":{}}'
        class Opener:
            def __init__(self):self.requests=[]
            def open(self,request,timeout):self.requests.append(request);return Response()
        opener=Opener();reader=mod.VendorReader({'WB_API_TOKEN':'secret-token'},opener)
        reader.read('wb_cards',{'settings':{}})
        self.assertEqual(opener.requests[0].full_url,mod.ENDPOINTS['wb_cards'])
        self.assertEqual(opener.requests[0].method,'POST')
        for kind,body in [('wb_update',{}),('saby_create',{}),('saby_read',{'method':'Nomenclature.Update','protocol':6})]:
            with self.assertRaises(mod.guards.PreflightError):reader.read(kind,body)
        self.assertEqual(len(opener.requests),1)

if __name__=='__main__':unittest.main()
