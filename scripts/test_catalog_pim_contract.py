import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def text(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


class CatalogPIMContractTest(unittest.TestCase):
    def test_product_and_sku_are_our_immutable_business_ids(self):
        migration = text("timeweb/migrations/056_catalog_pim_final.sql")
        self.assertIn("product_code BIGINT", migration)
        self.assertIn("products_product_code_uidx", migration)
        self.assertIn("Ficusin product_code is immutable", migration)
        self.assertIn("Ficusin SKU is immutable", migration)
        self.assertNotIn("FIC-000", migration)

    def test_orders_snapshot_product_variant_and_sku(self):
        migration = text("timeweb/migrations/056_catalog_pim_final.sql")
        service = text("backend/internal/order/service.go")
        for fragment in ("variant_id SET NOT NULL", "sku SET NOT NULL", "variant_snapshot", "order_items_product_fk"):
            self.assertIn(fragment, migration)
        self.assertIn("item.ProductID, item.VariantID, item.ID", service)
        self.assertIn("WHERE pv.sku = $1", service)

    def test_external_ids_are_not_catalogue_identity(self):
        pim = text("backend/internal/admin/catalog_pim.go")
        migration = text("timeweb/migrations/055_catalog_spu_sku_attributes.sql")
        self.assertIn('provider=="ficusin"', pim)
        self.assertIn("DELETE FROM product_external_ids WHERE provider = 'ficusin'", migration)

    def test_customer_and_technical_attributes_are_separated(self):
        public = text("backend/internal/catalog/postgres.go")
        pim = text("backend/internal/admin/catalog_pim.go")
        self.assertGreaterEqual(public.count("audience='customer'"), 2)
        self.assertIn("value_scope='product'", public)
        self.assertIn("value_scope='variant'", public)
        self.assertIn("Audience != \"customer\" && input.Audience != \"technical\"", pim)

    def test_attribute_inheritance_is_recursive_and_nearest_wins(self):
        pim = text("backend/internal/admin/catalog_pim.go")
        self.assertIn("WITH RECURSIVE ancestors", pim)
        self.assertIn("SELECT DISTINCT ON(id)", pim)
        self.assertIn("ORDER BY id,depth", pim)
        self.assertIn("source_category_name", pim)

    def test_enum_options_are_normalized(self):
        migration = text("timeweb/migrations/056_catalog_pim_final.sql")
        pim = text("backend/internal/admin/catalog_pim.go")
        self.assertIn("ALTER TABLE attribute_definitions DROP COLUMN IF EXISTS options", migration)
        self.assertIn("attribute_options", pim)
        self.assertIn("replaceAttributeOptions", pim)

    def test_cart_and_pdp_switch_on_sku(self):
        storefront = text("frontend/src/StorefrontPage.tsx")
        pdp = text("frontend/src/ProductPage.tsx")
        self.assertIn("cart[product.sku]", storefront)
        self.assertIn("[product.sku]", storefront)
        self.assertIn("cart[variant.sku]", pdp)
        self.assertIn("variant?.images?.length ? variant.images : product.images", pdp)
        self.assertIn("setActiveImage(0)", pdp)

    def test_filters_are_owner_configured_and_values_are_live(self):
        query = text("backend/internal/catalog/postgres.go")
        ui = text("frontend/src/StorefrontPage.tsx")
        admin_ui = text("frontend/src/AdminPim.tsx")
        self.assertIn("catalog_filters", query)
        self.assertIn("facetPopulation", ui)
        self.assertNotIn('const catalogFacets =', ui)
        self.assertIn("/api/v1/admin/catalog-filters", admin_ui)

    def test_role_boundaries_exist_for_system_definitions(self):
        pim = text("backend/internal/admin/catalog_pim.go")
        manage = text("backend/internal/admin/manage.go")
        self.assertIn("if actor.Role != RoleOwner", pim)
        self.assertGreaterEqual(manage.count("if actor.Role != RoleOwner"), 3)

    def test_variant_delete_is_safe_only_before_sale(self):
        pim = text("backend/internal/admin/catalog_pim.go")
        self.assertIn("SELECT EXISTS(SELECT 1 FROM order_items WHERE variant_id=$1)", pim)
        self.assertIn("проданный SKU можно только архивировать", pim)

    def test_dynamic_collections_are_live_pim_queries(self):
        migration = text("timeweb/migrations/057_dynamic_collection_rules.sql")
        public = text("backend/internal/catalog/postgres.go")
        admin = text("backend/internal/admin/collection_rules.go")
        ui = text("frontend/src/AdminCollectionsV2.tsx")
        self.assertIn("collection_rules_match_product", migration)
        self.assertIn("variant_attribute_values", migration)
        self.assertIn("product_attribute_values", migration)
        self.assertIn("collection_rules_match_product(product.id,collection.rules)", public)
        self.assertIn('Mode      string', admin)
        self.assertIn('mode: "manual" | "dynamic"', ui)
        self.assertIn("Все строки применяются одновременно (AND)", ui)

    def test_sku_media_has_repository_routes_and_admin_ui(self):
        repository = text("backend/internal/admin/variant_media.go")
        routes = text("backend/internal/httpapi/admin_catalog_routes.go")
        ui = text("frontend/src/VariantMediaManager.tsx")
        pim = text("frontend/src/AdminPim.tsx")
        self.assertIn("variant_id", repository)
        self.assertIn('GET /api/v1/admin/variants/{id}/media', routes)
        self.assertIn('POST /api/v1/admin/variants/{id}/media', routes)
        self.assertIn("Фото SKU имеют приоритет", ui)
        self.assertIn("VariantMediaManager", pim)

    def test_review_backfill_avoids_illegal_update_target_lateral_reference(self):
        migration = text("timeweb/migrations/055_catalog_spu_sku_attributes.sql")
        self.assertIn("WITH review_variant AS", migration)
        self.assertIn("WHERE review.id = match.review_id", migration)
        self.assertNotIn("FROM LATERAL (\n  SELECT item.variant_id", migration)

    def test_old_applied_migrations_keep_their_original_integrity_contracts(self):
        # Old applied migrations are superseded by 055/056, never edited in
        # place. The branch/base diff additionally verifies 044/046 are absent
        # from the changed-file set; these assertions pin stable original
        # markers so the repository test is not coupled to comment wording.
        migration_44 = text("timeweb/migrations/044_catalog_identity_attributes.sql")
        migration_46 = text("timeweb/migrations/046_catalog_identity_integrity.sql")
        migration_56 = text("timeweb/migrations/056_catalog_pim_final.sql")
        self.assertIn("FIC-", migration_44)
        self.assertIn("product_external_ids_product_level_uidx", migration_46)
        self.assertIn("validate_external_id_variant_owner", migration_46)
        self.assertIn("Migration 055 was an intermediate bridge", migration_56)


if __name__ == "__main__":
    unittest.main()
