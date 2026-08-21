#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def patch(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if old in text:
        target.write_text(text.replace(old, new, 1), encoding="utf-8")
        print(f"patched {path}")
        return
    if new in text:
        print(f"already patched {path}")
        return
    raise RuntimeError(f"expected fragment not found in {path}: {old[:120]!r}")


# Storefront cart identity is SKU, while product.id remains the PRODUCT code
# used by the public URL and favorites.
patch(
    "frontend/src/StorefrontPage.tsx",
    "type Product = {\n  id: string;\n  name: string;",
    "type Product = {\n  id: string;\n  sku: string;\n  name: string;",
)
patch(
    "frontend/src/StorefrontPage.tsx",
    "const inCart = cart[product.id] ?? 0;",
    "const inCart = cart[product.sku] ?? 0;",
)
patch(
    "frontend/src/StorefrontPage.tsx",
    "[product.id]: Math.min(\n        product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20,\n        (current[product.id] ?? 0) + 1,\n      ),",
    "[product.sku]: Math.min(\n        product.stock && product.stock > 0 ? Math.min(product.stock, 20) : 20,\n        (current[product.sku] ?? 0) + 1,\n      ),",
)
patch(
    "frontend/src/StorefrontPage.tsx",
    "const nextQuantity = Math.max(0, Math.min(maximum, (current[product.id] || 0) + delta));\n    if (nextQuantity === 0) { const next = { ...current }; delete next[product.id]; return next; }\n    return { ...current, [product.id]: nextQuantity };",
    "const nextQuantity = Math.max(0, Math.min(maximum, (current[product.sku] || 0) + delta));\n    if (nextQuantity === 0) { const next = { ...current }; delete next[product.sku]; return next; }\n    return { ...current, [product.sku]: nextQuantity };",
)

# Public catalogue identity is products.product_code. Keep the repository
# method name for this small patch to avoid changing every mock at once; its
# implementation and all public data now use product_code.
patch(
    "backend/internal/catalog/postgres.go",
    "SELECT id, slug, name, latin_name, short_description, description, care_instructions,",
    "SELECT id, product_code::TEXT, name, latin_name, short_description, description, care_instructions,",
)
patch(
    "backend/internal/catalog/postgres.go",
    "FROM products WHERE slug = $1 AND status = 'published' LIMIT 1",
    "FROM products WHERE product_code::TEXT = $1 AND status = 'published' LIMIT 1",
)
patch(
    "backend/internal/catalog/postgres.go",
    "product.slug, default_variant.sku, product.name, product.latin_name, 'Растения',",
    "product.product_code::TEXT, default_variant.sku, product.name, product.latin_name, 'Растения',",
)

# Keep the established aliases in the popularity query so focused tests remain
# readable while the query itself continues to use variant_id as the relation.
patch(
    "backend/internal/catalog/postgres.go",
    "SELECT variant.product_id, SUM(item.quantity * CASE\n\t\t\tWHEN orders.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days' THEN 1.0\n\t\t\tWHEN orders.created_at >= CURRENT_TIMESTAMP - INTERVAL '90 days' THEN 0.5\n\t\t\tWHEN orders.created_at >= CURRENT_TIMESTAMP - INTERVAL '365 days' THEN 0.2\n\t\t\tELSE 0.05 END)::DOUBLE PRECISION AS score\n\t\tFROM order_items item\n\t\tJOIN product_variants variant ON variant.id=item.variant_id\n\t\tJOIN orders ON orders.id=item.order_id\n\t\tWHERE orders.status <> 'cancelled' AND (orders.status='completed' OR orders.payment_status='paid')",
    "SELECT variant.product_id, SUM(oi.quantity * CASE\n\t\t\tWHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days' THEN 1.0\n\t\t\tWHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '90 days' THEN 0.5\n\t\t\tWHEN o.created_at >= CURRENT_TIMESTAMP - INTERVAL '365 days' THEN 0.2\n\t\t\tELSE 0.05 END)::DOUBLE PRECISION AS score\n\t\tFROM order_items oi\n\t\tJOIN product_variants variant ON variant.id=oi.variant_id\n\t\tJOIN orders o ON o.id=oi.order_id\n\t\tWHERE o.status <> 'cancelled' AND (o.status='completed' OR o.payment_status='paid')",
)

# Checkout resolves the exact SKU instead of a PRODUCT and its first variant.
patch(
    "backend/internal/order/service.go",
    "type purchasableItem struct {\n\tID        string\n\tVariantID int64\n\tName      string",
    "type purchasableItem struct {\n\tID        string // immutable SKU snapshot\n\tProductID int64\n\tVariantID int64\n\tVariantLabel string\n\tHeightCM *int\n\tPotDiameterCM *int\n\tName      string",
)
patch(
    "backend/internal/order/service.go",
    "SELECT p.slug, p.name, pv.id, pv.base_price_minor,\n\t\t\t\tCOALESCE(pv.package_length_cm, 0), COALESCE(pv.package_width_cm, 0),\n\t\t\t\tCOALESCE(pv.package_height_cm, 0), COALESCE(pv.package_weight_grams, 0)\n\t\t\tFROM products p\n\t\t\tJOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1\n\t\t\tWHERE p.slug = $1 AND p.status = 'published'\n\t\t\tORDER BY pv.id\n\t\t\tLIMIT 1\n\t\t`, requested.ID).Scan(\n\t\t\t&item.ID, &item.Name, &item.VariantID, &priceMinor,\n\t\t\t&item.Parcel.LengthCM, &item.Parcel.WidthCM,\n\t\t\t&item.Parcel.HeightCM, &item.Parcel.WeightGrams,\n\t\t)",
    "SELECT pv.sku, p.id, p.name, pv.id, pv.label, pv.height_cm, pv.pot_diameter_cm, pv.base_price_minor,\n\t\t\t\tCOALESCE(pv.package_length_cm, 0), COALESCE(pv.package_width_cm, 0),\n\t\t\t\tCOALESCE(pv.package_height_cm, 0), COALESCE(pv.package_weight_grams, 0)\n\t\t\tFROM products p\n\t\t\tJOIN product_variants pv ON pv.product_id = p.id AND pv.is_active = 1 AND pv.archived_at IS NULL\n\t\t\tWHERE pv.sku = $1 AND p.status = 'published'\n\t\t\tLIMIT 1\n\t\t`, requested.ID).Scan(\n\t\t\t&item.ID, &item.ProductID, &item.Name, &item.VariantID, &item.VariantLabel,\n\t\t\t&item.HeightCM, &item.PotDiameterCM, &priceMinor,\n\t\t\t&item.Parcel.LengthCM, &item.Parcel.WidthCM,\n\t\t\t&item.Parcel.HeightCM, &item.Parcel.WeightGrams,\n\t\t)",
)
patch(
    "backend/internal/order/service.go",
    "INSERT INTO order_items (\n\t\t\t\torder_id, product_id, variant_id, product_name, unit_price, quantity,\n\t\t\t\tis_preorder, reserved_qty\n\t\t\t) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)\n\t\t`, orderID, item.ID, item.VariantID, item.Name, item.Price, item.Quantity,\n\t\t\tboolToInt(item.Preorder), item.Reserved)",
    "INSERT INTO order_items (\n\t\t\t\torder_id, product_id, variant_id, sku, product_name, variant_label, variant_snapshot,\n\t\t\t\tunit_price, quantity, is_preorder, reserved_qty\n\t\t\t) VALUES ($1, $2, $3, $4, $5, $6,\n\t\t\t\tjsonb_strip_nulls(jsonb_build_object('heightCm',$7::INTEGER,'potDiameterCm',$8::INTEGER)),\n\t\t\t\t$9, $10, $11, $12)\n\t\t`, orderID, item.ProductID, item.VariantID, item.ID, item.Name, item.VariantLabel,\n\t\t\titem.HeightCM, item.PotDiameterCM, item.Price, item.Quantity,\n\t\t\tboolToInt(item.Preorder), item.Reserved)",
)

# Product creation lets the database allocate immutable numeric SKU. Slug is
# retained only as internal descriptive metadata during the transition.
patch(
    "backend/internal/admin/catalogue.go",
    "VALUES ($1, $2, 'FIC-' || LPAD(nextval('ficusin_sku_seq')::TEXT, 6, '0'),\n\t\t\t'Основной вариант', $3, $4, $5, $6, $7, $8, $9, 1, CURRENT_TIMESTAMP)",
    "VALUES ($1, $2, DEFAULT,\n\t\t\t'Основной вариант', $3, $4, $5, $6, $7, $8, $9, 1, CURRENT_TIMESTAMP)",
)

# Reviews are verified through the real order PRODUCT FK and retain the exact
# purchased variant/SKU context.
patch(
    "backend/internal/reviews/reviews.go",
    "var productID, orderID int64\n\terr = tx.QueryRow(ctx, `SELECT p.id,o.id FROM products p JOIN order_items oi ON oi.product_id=p.slug JOIN orders o ON o.id=oi.order_id WHERE p.slug=$1 AND o.customer_id=$2 AND o.status='completed' ORDER BY o.created_at DESC LIMIT 1`, slug, customerID).Scan(&productID,&orderID)",
    "var productID, orderID, variantID int64\n\tvar purchasedSKU string\n\terr = tx.QueryRow(ctx, `SELECT p.id,o.id,oi.variant_id,oi.sku FROM products p JOIN order_items oi ON oi.product_id=p.id JOIN orders o ON o.id=oi.order_id WHERE p.product_code::TEXT=$1 AND o.customer_id=$2 AND o.status='completed' ORDER BY o.created_at DESC LIMIT 1`, slug, customerID).Scan(&productID,&orderID,&variantID,&purchasedSKU)",
)
patch(
    "backend/internal/reviews/reviews.go",
    "`INSERT INTO product_reviews(product_id,customer_id,order_id,rating,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id`, productID,customerID,orderID,input.Rating,strings.TrimSpace(input.Text)",
    "`INSERT INTO product_reviews(product_id,customer_id,order_id,variant_id,purchased_sku,rating,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING id`, productID,customerID,orderID,variantID,purchasedSKU,input.Rating,strings.TrimSpace(input.Text)",
)
patch(
    "backend/internal/reviews/reviews.go",
    "`SELECT r.id,p.name,p.slug,r.rating,r.body,r.status,r.created_at::text FROM product_reviews r JOIN products p ON p.id=r.product_id WHERE r.customer_id=$1 ORDER BY r.created_at DESC`",
    "`SELECT r.id,p.name,p.product_code::TEXT,r.rating,r.body,r.status,r.created_at::text FROM product_reviews r JOIN products p ON p.id=r.product_id WHERE r.customer_id=$1 ORDER BY r.created_at DESC`",
)

# Admin order API now exposes the relational product id and the SKU snapshot.
patch(
    "backend/internal/admin/admin.go",
    "type OrderItem struct {\n\tProductID   string  `json:\"productId\"`\n\tProductName string  `json:\"productName\"`",
    "type OrderItem struct {\n\tProductID   int64   `json:\"productId\"`\n\tSKU         string  `json:\"sku\"`\n\tVariantLabel string `json:\"variantLabel\"`\n\tProductName string  `json:\"productName\"`",
)
patch(
    "backend/internal/admin/manage.go",
    "SELECT order_id, product_id, product_name, unit_price::DOUBLE PRECISION, quantity\n\t\tFROM order_items WHERE order_id = ANY($1::bigint[]) ORDER BY id",
    "SELECT order_id, product_id, sku, variant_label, product_name, unit_price::DOUBLE PRECISION, quantity\n\t\tFROM order_items WHERE order_id = ANY($1::bigint[]) ORDER BY id",
)
patch(
    "backend/internal/admin/manage.go",
    "itemRows.Scan(&orderID, &item.ProductID, &item.ProductName,\n\t\t\t&item.UnitPrice, &item.Quantity)",
    "itemRows.Scan(&orderID, &item.ProductID, &item.SKU, &item.VariantLabel, &item.ProductName,\n\t\t\t&item.UnitPrice, &item.Quantity)",
)
patch(
    "frontend/src/adminTypes.ts",
    "items: Array<{ productId: string; productName: string; unitPrice: number; quantity: number }>;\n",
    "items: Array<{ productId: number; sku: string; variantLabel: string; productName: string; unitPrice: number; quantity: number }>;\n",
)

# Remove the stale comment that documents the legacy order relation.
patch(
    "backend/internal/order/journal.go",
    "\t\t-- order_items.product_id is the legacy public slug (TEXT), while\n\t\t-- products.id is BIGINT. The variant is the real relational key and\n\t\t-- is present on every order created by the current checkout.\n\t\tJOIN product_variants pv ON pv.id = oi.variant_id\n\t\tLEFT JOIN products p ON p.id = pv.product_id",
    "\t\tJOIN product_variants pv ON pv.id = oi.variant_id\n\t\tLEFT JOIN products p ON p.id = oi.product_id",
)

marker = ROOT / "docs" / "catalog-model-v2.md"
marker.write_text(
    """# Catalogue model v2\n\nThe catalogue uses explicit Ficusin-owned identities.\n\n- PRODUCT: `products.id` internal FK + immutable numeric `product_code` for `/product/{code}`.\n- SKU: `product_variants.id` internal FK + immutable numeric `sku` for the sellable unit.\n- Cart keys are SKUs. Order rows snapshot PRODUCT id, variant id, SKU, label and core variant characteristics.\n- Saby/WB/Ozon identifiers are external mappings, never catalogue identity.\n- Product and variant attributes are separate; enum options are normalized.\n- Reviews belong to PRODUCT and remember the purchased variant/SKU.\n""",
    encoding="utf-8",
)
print("catalogue identity/cart refactor applied")
