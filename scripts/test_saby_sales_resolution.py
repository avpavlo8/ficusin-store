import unittest

from saby_catalog_merge import build_sales_product_ids, resolve_sales_product_id


class SalesResolutionTest(unittest.TestCase):
    def setUp(self):
        self.ids = build_sales_product_ids([
            {"id": 2971, "hierarchicalId": 8842, "nomNumber": "X8999268"}
        ])
        self.catalogue_ids = {"2971"}

    def test_falls_back_when_receipt_uuid_is_not_in_catalogue(self):
        position = {
            "NomenclatureUUID": "stale-receipt-uuid",
            "Nomenclature": 8842,
            "NomenclatureNumber": "X8999268",
        }
        self.assertEqual(
            resolve_sales_product_id(position, self.ids, self.catalogue_ids),
            "2971",
        )

    def test_rejects_position_outside_selected_section(self):
        self.assertEqual(
            resolve_sales_product_id(
                {"NomenclatureNumber": "X8999268"}, self.ids, {"9999"}
            ),
            "",
        )


if __name__ == "__main__":
    unittest.main()
