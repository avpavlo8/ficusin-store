import importlib.util
import io
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('catalog_audit', Path(__file__).with_name('production-catalog-publication-audit.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
ENV = {'CATALOG_WB_API_TOKEN': 'fixture-wb-secret', 'CATALOG_OZON_CLIENT_ID': 'fixture-client', 'CATALOG_OZON_API_KEY': 'fixture-ozon-secret'}


class Response(io.BytesIO):
    status = 200


class Opener:
    def __init__(self, responses): self.responses, self.requests = list(responses), []
    def open(self, request, timeout):
        if timeout != 25: raise AssertionError('unexpected timeout')
        self.requests.append(request)
        return Response(json.dumps(self.responses.pop(0)).encode())


class AuditTests(unittest.TestCase):
    def test_current_batches_match_exact_article_and_keep_timestamp_evidence(self):
        payload = {'data': {'items': [
            {'errors': {'другой': ['PRIVATE OTHER MESSAGE']}, 'updatedAt': '2026-11-12T08:00:00Z'},
            {'errors': {'сам': ['Неверный ТНВЭД'], 'чужой': ['PRIVATE OTHER MESSAGE']},
             'createdAt': '2026-11-13T08:00:00Z', 'updatedAt': '2026-11-13T09:00:00Z', 'batchUUID': 'fixture-test-batch'}],
            'cursor': {'next': True, 'updatedAt': 'DO NOT PRINT UNRELATED CURSOR', 'batchUUID': 'UNRELATED CURSOR UUID'}}, 'error': False, 'errorText': ''}
        result = module.wb_error_report(payload, ENV)
        self.assertEqual(result['parserFormat'], 'data.items')
        self.assertEqual(result['batchCountScanned'], 2)
        self.assertTrue(result['morePages'])
        self.assertEqual(result['shape']['itemCounts']['dataItems'], 2)
        self.assertEqual(len(result['testErrors']), 1)
        self.assertEqual(result['testErrors'][0]['errors'], ['Неверный ТНВЭД'])
        self.assertEqual(result['testErrors'][0]['updatedAt'], '2026-11-13T09:00:00Z')
        for private in ('PRIVATE OTHER MESSAGE', 'UNRELATED CURSOR', 'чужой', 'другой'):
            self.assertNotIn(private, json.dumps(result, ensure_ascii=False))

    def test_legacy_array_errors_are_not_lost(self):
        result = module.wb_error_report({'data': [{'vendorCode': 'чужой', 'errors': ['PRIVATE']}, {'vendorCode': 'сам', 'errors': ['Ошибка старого формата'], 'updatedAt': '2026-11-13T09:00:00Z'}]}, ENV)
        self.assertEqual(result['parserFormat'], 'data[]')
        self.assertEqual(result['shape']['dataType'], 'array')
        self.assertEqual(result['testErrors'][0]['errors'], ['Ошибка старого формата'])
        self.assertNotIn('PRIVATE', json.dumps(result))

    def test_http_200_vendor_error_is_visible_and_redacted(self):
        payload = {'error': True, 'errorText': 'Denied fixture-wb-secret https://private.invalid/token user@example.invalid\r\nline', 'data': None}
        result = module.wb_error_report(payload, ENV)
        self.assertTrue(result['shape']['errorFlag'])
        self.assertEqual(result['parserFormat'], 'unsupported')
        self.assertEqual(result['shape']['dataType'], 'null')
        encoded = json.dumps(result)
        for private in ('fixture-wb-secret', 'private.invalid', 'user@example.invalid', '\\r', '\\n'):
            self.assertNotIn(private, encoded)

    def test_unexpected_shape_is_not_reported_as_a_recognized_empty_page(self):
        for payload in ({'data': {'errors': []}}, {'items': []}, [], {'data': {'items': None}}):
            result = module.wb_error_report(payload, ENV)
            self.assertEqual(result['parserFormat'], 'unsupported')
            self.assertIsNone(result['batchCountScanned'])
        recognized = module.wb_error_report({'data': {'items': [], 'cursor': {'next': False}}}, ENV)
        self.assertEqual(recognized['parserFormat'], 'data.items')
        self.assertEqual(recognized['batchCountScanned'], 0)

    def test_three_fixed_read_requests_and_card_shapes_only(self):
        opener = Opener([{'data': {'items': []}, 'error': False}, {'cards': [], 'cursor': {'total': 0}}, {'items': [{'id': 6975452207, 'offer_id': 'сам', 'barcodes': [], 'statuses': {'is_created': False}, 'errors': [{'code': 'INCORRECT_DENSITY'}]}, {'id': 8, 'offer_id': 'чужой', 'name': 'PRIVATE OTHER CARD'}]}])
        report = module.vendor_reads(ENV, opener)
        self.assertEqual([r.get_method() for r in opener.requests], ['POST', 'POST', 'POST'])
        self.assertEqual([r.full_url for r in opener.requests], [
            'https://content-api.wildberries.ru/content/v2/cards/error/list',
            'https://content-api.wildberries.ru/content/v2/get/cards/list',
            'https://api-seller.ozon.ru/v3/product/info/list'])
        self.assertEqual(json.loads(opener.requests[1].data)['settings']['filter']['textSearch'], 'сам')
        self.assertEqual(json.loads(opener.requests[2].data), {'product_id': [6975452207]})
        self.assertIsNone(opener.requests[2].get_header('Authorization'))
        self.assertEqual(report['wbCards']['shape']['itemCounts']['rootCards'], 0)
        self.assertEqual(len(report['ozonCard']['testCards']), 1)
        self.assertNotIn('PRIVATE OTHER CARD', json.dumps(report))


if __name__ == '__main__':
    unittest.main()
