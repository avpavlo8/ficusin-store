import unittest

from saby_catalog_merge import (
    build_sales_product_ids,
    catalogue_ids_in_section,
    merge_catalog_items,
)


class MergeCatalogItemsTest(unittest.TestCase):
    def test_price_list_stock_overrides_zero_from_full_catalog(self):
        merged, conflicts = merge_catalog_items(
            [{"id": 42, "name": "Лиметта Росса", "balance": "0"}],
            [{"id": 42, "name": "Лиметта Росса", "balance": "5", "cost": 2990}],
        )
        self.assertEqual(merged[0]["balance"], "5")
        self.assertEqual(merged[0]["cost"], 2990)
        self.assertEqual(conflicts, 1)

    def test_price_list_zero_can_really_zero_store_stock(self):
        merged, conflicts = merge_catalog_items(
            [{"id": 42, "name": "Лиметта Росса", "balance": "7"}],
            [{"id": 42, "name": "Лиметта Росса", "balance": "0", "cost": 2990}],
        )
        self.assertEqual(merged[0]["balance"], "0")
        self.assertEqual(conflicts, 1)

    def test_price_list_balance_is_used_when_catalog_omits_it(self):
        merged, conflicts = merge_catalog_items(
            [{"id": 42, "name": "Лиметта Росса"}],
            [{"id": 42, "balance": "3", "cost": 2990}],
        )
        self.assertEqual(merged[0]["balance"], "3")
        self.assertEqual(conflicts, 0)

    def test_catalog_metadata_remains_authoritative(self):
        merged, _ = merge_catalog_items(
            [{"id": 42, "name": "Правильное название", "description": "Описание", "balance": 5}],
            [{"id": 42, "name": "Название из прайса", "description": "", "balance": 5, "cost": 2990}],
        )
        self.assertEqual(merged[0]["name"], "Правильное название")
        self.assertEqual(merged[0]["description"], "Описание")
        self.assertEqual(merged[0]["cost"], 2990)

    def test_item_absent_from_price_list_keeps_full_catalog_stock(self):
        merged, conflicts = merge_catalog_items(
            [{"id": 77, "name": "Справочный товар", "balance": 4}],
            [],
        )
        self.assertEqual(merged[0]["balance"], 4)
        self.assertEqual(conflicts, 0)

    def test_price_only_item_is_not_lost(self):
        merged, conflicts = merge_catalog_items([], [{"id": 99, "name": "Товар", "balance": 2, "cost": 100}])
        self.assertEqual(merged, [{"id": 99, "name": "Товар", "balance": 2, "cost": 100}])
        self.assertEqual(conflicts, 0)

    def test_sales_ids_use_numeric_catalogue_id_not_x_code(self):
        identifiers = build_sales_product_ids([
            {"id": 2971, "code": "X8999268", "nomNumber": "X8999268", "article": "PHAL-3"}
        ])
        self.assertEqual(identifiers["x8999268"], "2971")
        self.assertEqual(identifiers["2971"], "2971")
        self.assertEqual(identifiers["phal-3"], "2971")

    def test_sales_ids_include_saby_receipt_uuid(self):
        identifiers = build_sales_product_ids([
            {
                "id": 2971,
                "hierarchicalId": "059ac0b1-d01a-4c2d-b90b-f09e3af43318",
            }
        ])
        self.assertEqual(
            identifiers["059ac0b1-d01a-4c2d-b90b-f09e3af43318"], "2971"
        )

    def test_catalogue_section_selects_only_leaf_products(self):
        items = [
            {
                "id": 10,
                "name": "Комнатные растения",
                "isParent": True,
                "_ficusinSectionPath": [],
            },
            {
                "id": 11,
                "name": "Фикус",
                "isParent": False,
                "_ficusinSectionPath": ["Комнатные растения", "Фикусы"],
            },
            {
                "id": 12,
                "name": "Кашпо",
                "isParent": False,
                "_ficusinSectionPath": ["Кашпо"],
            },
        ]
        self.assertEqual(
            catalogue_ids_in_section(items, "комнатные РАСТЕНИЯ"), {"11"}
        )


if __name__ == "__main__":
    unittest.main()
