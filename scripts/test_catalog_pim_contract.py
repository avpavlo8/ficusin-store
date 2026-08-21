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

    def test_old_applied_migrations_are_not_rewritten(self):
        # Their old concepts are deliberately superseded by 055/056, not edited in place.
        migration_44 = text("timeweb/migrations/044_catalog_identity_attributes.sql")
        migration_46 = text("timeweb/migrations/046_catalog_identity_integrity.sql")
        migration_56 = text("timeweb/migrations/056_catalog_pim_final.sql")
        self.assertIn("FIC-", migration_44)
        self.assertIn("legacy", migration_46.lower())
        self.assertIn("Migration 055 was an intermediate bridge", migration_56)


if __name__ == "__main__":
    unittest.main()
