import unittest

from saby_catalog_merge import merge_catalog_items


class MergeCatalogItemsTest(unittest.TestCase):
    def test_price_list_cannot_zero_catalog_stock(self):
        merged, conflicts = merge_catalog_items(
            [{"id": 42, "name": "Лиметта Росса", "balance": "7"}],
            [{"id": 42, "name": "Лиметта Росса", "balance": "0", "cost": 2990}],
        )
        self.assertEqual(merged[0]["balance"], "7")
        self.assertEqual(merged[0]["cost"], 2990)
        self.assertEqual(conflicts, 1)

    def test_price_list_balance_is_fallback_when_catalog_omits_it(self):
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

    def test_price_only_item_is_not_lost(self):
        merged, conflicts = merge_catalog_items([], [{"id": 99, "name": "Товар", "balance": 2, "cost": 100}])
        self.assertEqual(merged, [{"id": 99, "name": "Товар", "balance": 2, "cost": 100}])
        self.assertEqual(conflicts, 0)


if __name__ == "__main__":
    unittest.main()
