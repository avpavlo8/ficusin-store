#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if old not in text:
        if new in text:
            print("already", path)
            return
        raise RuntimeError(f"fragment not found in {path}: {old[:100]!r}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")
    print("patched", path)


def between(path: str, start: str, end: str, body: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    left = text.find(start)
    right = text.find(end, left + len(start)) if left >= 0 else -1
    if left < 0 or right < 0:
        if body in text:
            print("already", path)
            return
        raise RuntimeError(f"markers not found in {path}: {start!r} .. {end!r}")
    target.write_text(text[:left] + body + "\n\n" + text[right:], encoding="utf-8")
    print("replaced section", path)


# Product-scoped values are validated only against the effective category
# schema and normalized enum options. Variant-scoped values never enter this
# table and no writes are mirrored into legacy physical columns.
product_save = r'''func saveProductAttributes(ctx context.Context, tx pgx.Tx, productID int64, attributes map[string]any) error {
	for code, value := range attributes {
		code = strings.TrimSpace(code)
		if code == "" { continue }
		raw, err := json.Marshal(value)
		if err != nil { return fmt.Errorf("encode product attribute %s: %w", code, err) }
		if string(raw) == "null" || string(raw) == `""` || string(raw) == "[]" {
			if _, err := tx.Exec(ctx, `DELETE FROM product_attribute_values value USING attribute_definitions definition
				WHERE value.attribute_id=definition.id AND value.product_id=$1 AND definition.code=$2`, productID, code); err != nil {
				return fmt.Errorf("clear product attribute %s: %w", code, err)
			}
			continue
		}
		tag, err := tx.Exec(ctx, `
			WITH RECURSIVE ancestors AS (
				SELECT category_id AS id, 0 AS depth FROM products WHERE id=$1
				UNION ALL
				SELECT category.parent_id, ancestor.depth+1
				FROM categories category JOIN ancestors ancestor ON category.id=ancestor.id
				WHERE category.parent_id IS NOT NULL
			), candidates AS (
				SELECT definition.id,definition.data_type,definition.value_scope,assignment.is_excluded,ancestor.depth
				FROM ancestors ancestor
				JOIN category_attributes assignment ON assignment.category_id=ancestor.id
				JOIN attribute_definitions definition ON definition.id=assignment.attribute_id
				WHERE definition.code=$2 AND definition.is_active
				UNION ALL
				SELECT definition.id,definition.data_type,definition.value_scope,FALSE,1000000
				FROM attribute_definitions definition
				WHERE definition.code=$2 AND definition.is_active AND definition.is_global
			), effective AS (
				SELECT DISTINCT ON(id) id,data_type,value_scope,is_excluded
				FROM candidates ORDER BY id,depth
			)
			INSERT INTO product_attribute_values(product_id,attribute_id,value,source,updated_at)
			SELECT $1,effective.id,$3::jsonb,'local',CURRENT_TIMESTAMP
			FROM effective
			WHERE effective.value_scope='product' AND NOT effective.is_excluded
			  AND CASE effective.data_type
				WHEN 'number' THEN jsonb_typeof($3::jsonb)='number'
				WHEN 'boolean' THEN jsonb_typeof($3::jsonb)='boolean'
				WHEN 'enum' THEN jsonb_typeof($3::jsonb)='string' AND EXISTS(
					SELECT 1 FROM attribute_options option WHERE option.attribute_id=effective.id
					AND option.code=($3::jsonb#>>'{}') AND option.is_active)
				WHEN 'multi_enum' THEN jsonb_typeof($3::jsonb)='array' AND NOT EXISTS(
					SELECT 1 FROM jsonb_array_elements_text($3::jsonb) selected
					WHERE NOT EXISTS(SELECT 1 FROM attribute_options option WHERE option.attribute_id=effective.id
						AND option.code=selected AND option.is_active))
				ELSE jsonb_typeof($3::jsonb)='string' END
			ON CONFLICT(product_id,attribute_id) DO UPDATE SET value=EXCLUDED.value,
				source='local',updated_at=CURRENT_TIMESTAMP
		`, productID, code, string(raw))
		if err != nil { return fmt.Errorf("save product attribute %s: %w", code, err) }
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: атрибут %s не разрешён для PRODUCT или имеет неверное значение", ErrInvalidInput, code)
		}
	}
	return nil
}'''
between("backend/internal/admin/catalogue.go", "func saveProductAttributes(", "// validateRequiredAttributes", product_save)

# Required PRODUCT values should not mistake required SKU dimensions for a
# missing card-wide value.
replace(
    "backend/internal/admin/catalogue.go",
    "JOIN attribute_definitions d ON d.id=ca.attribute_id\n\t\t\tORDER BY d.id,a.depth",
    "JOIN attribute_definitions d ON d.id=ca.attribute_id AND d.value_scope='product' AND d.is_active\n\t\t\tORDER BY d.id,a.depth",
)

# The legacy category-schema adapter stays for the existing product form, but
# reads the normalized effective PIM schema instead of the dropped JSON options.
legacy_schema = r'''func (repository *PostgresRepository) ListCategoryAttributes(ctx context.Context, categoryID int64) ([]CategoryAttribute, error) {
	items, err := repository.EffectiveCategoryAttributes(ctx, categoryID)
	if err != nil { return nil, err }
	result := make([]CategoryAttribute, 0, len(items))
	for _, definition := range items {
		if !definition.Active || definition.Excluded { continue }
		options := make([]string, 0, len(definition.Options))
		for _, option := range definition.Options { if option.Active { options = append(options, option.Code) } }
		result = append(result, CategoryAttribute{
			Code: definition.Code, Name: definition.Name, DataType: definition.DataType,
			Unit: definition.Unit, Options: options, Audience: definition.Audience,
			Required: definition.Required, Filterable: definition.Filterable,
			ShowOnPDP: definition.ShowOnPDP, Badge: definition.Badge, SortOrder: definition.SortOrder,
		})
	}
	return result, nil
}'''
between("backend/internal/admin/manage.go", "func (repository *PostgresRepository) ListCategoryAttributes(", "func (repository *PostgresRepository) CreateCategory", legacy_schema)

# Catalogue structure is owner-only. Managers edit products/SKUs, not system
# definitions or category topology.
replace("backend/internal/admin/manage.go", "if !Can(actor.Role,PermissionProductsEdit){return Category{},ErrForbidden}\n\tinput.Name", "if actor.Role != RoleOwner{return Category{},ErrForbidden}\n\tinput.Name")
replace("backend/internal/admin/manage.go", "if !Can(actor.Role,PermissionProductsEdit){return Category{},ErrForbidden}\n\t_,err:=repository.pool.Exec", "if actor.Role != RoleOwner{return Category{},ErrForbidden}\n\t_,err:=repository.pool.Exec")
replace("backend/internal/admin/manage.go", "if !Can(actor.Role,PermissionProductsEdit){return ErrForbidden}\n\tvar children,products int", "if actor.Role != RoleOwner{return ErrForbidden}\n\tvar children,products int")

# Exact SKU checkout SQL formatting also keeps the existing query-safety test
# readable.
replace("backend/internal/catalog/postgres.go", "(o.status='completed' OR o.payment_status='paid')", "(o.status = 'completed' OR o.payment_status = 'paid')")

# Product variants carry their own gallery and archived SKUs never reach PDP.
replace(
    "backend/internal/catalog/catalog.go",
    "WholesaleMinQty int     `json:\"wholesaleMinQty\"`\n\tAttributes      []ProductAttribute `json:\"attributes\"`",
    "WholesaleMinQty int     `json:\"wholesaleMinQty\"`\n\tImages          []string `json:\"images\"`\n\tAttributes      []ProductAttribute `json:\"attributes\"`",
)
replace(
    "backend/internal/catalog/postgres.go",
    "pv.height_cm, pv.pot_diameter_cm, pv.wholesale_min_qty,\n\t\t\tCOALESCE((",
    "pv.height_cm, pv.pot_diameter_cm, pv.wholesale_min_qty,\n\t\t\tCOALESCE((SELECT jsonb_agg(COALESCE(mirror.large_url,media.object_key) ORDER BY media.is_primary DESC,media.sort_order,media.id)\n\t\t\t\tFROM product_media media LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key\n\t\t\t\tWHERE media.variant_id=pv.id),'[]'::jsonb),\n\t\t\tCOALESCE((",
)
replace(
    "backend/internal/catalog/postgres.go",
    "WHERE pv.product_id = $1 AND pv.is_active = 1",
    "WHERE pv.product_id = $1 AND pv.is_active = 1 AND pv.archived_at IS NULL",
)
replace(
    "backend/internal/catalog/postgres.go",
    "var attributes []byte\n\t\tif err := variantRows.Scan(&variant.ID, &variant.SKU, &variant.Label, &priceMinor,\n\t\t\t&variant.Stock, &variant.HeightCM, &variant.PotDiameterCM,\n\t\t\t&variant.WholesaleMinQty, &attributes); err != nil {",
    "var attributes, images []byte\n\t\tif err := variantRows.Scan(&variant.ID, &variant.SKU, &variant.Label, &priceMinor,\n\t\t\t&variant.Stock, &variant.HeightCM, &variant.PotDiameterCM,\n\t\t\t&variant.WholesaleMinQty, &images, &attributes); err != nil {",
)
replace(
    "backend/internal/catalog/postgres.go",
    "variant.Price = float64(priceMinor) / 100\n\t\tif err := json.Unmarshal(attributes, &variant.Attributes); err != nil {",
    "variant.Price = float64(priceMinor) / 100\n\t\tif err := json.Unmarshal(images, &variant.Images); err != nil { variantRows.Close(); return ProductDetail{}, fmt.Errorf(\"decode variant images: %w\", err) }\n\t\tif err := json.Unmarshal(attributes, &variant.Attributes); err != nil {",
)

# Public product-list variant selection also ignores archived variants.
replace(
    "backend/internal/catalog/postgres.go",
    "WHERE variant.product_id=product.id AND variant.is_active=1",
    "WHERE variant.product_id=product.id AND variant.is_active=1 AND variant.archived_at IS NULL",
)

# Admin variant creation/copy omits SKU explicitly so PostgreSQL can allocate
# the immutable sequence default. DEFAULT inside INSERT..SELECT is invalid.
replace(
    "backend/internal/admin/catalog_pim.go",
    "INSERT INTO product_variants(product_id,sku,label,base_price_minor,wholesale_min_qty,is_active,updated_at) SELECT $1,DEFAULT,BTRIM($2),$3,$4,$5,CURRENT_TIMESTAMP",
    "INSERT INTO product_variants(product_id,label,base_price_minor,wholesale_min_qty,is_active,updated_at) SELECT $1,BTRIM($2),$3,$4,$5,CURRENT_TIMESTAMP",
)
replace(
    "backend/internal/admin/catalog_pim.go",
    "INSERT INTO product_variants(product_id,sku,label,base_price_minor,price_override_minor,wholesale_min_qty,is_active,updated_at) SELECT product_id,DEFAULT,label || ' — копия',base_price_minor,price_override_minor,wholesale_min_qty,1,CURRENT_TIMESTAMP",
    "INSERT INTO product_variants(product_id,label,base_price_minor,price_override_minor,wholesale_min_qty,is_active,updated_at) SELECT product_id,label || ' — копия',base_price_minor,price_override_minor,wholesale_min_qty,1,CURRENT_TIMESTAMP",
)

# New products seed variant-scoped dimensions into variant_attribute_values;
# the database trigger from migration 055 is deliberately removed by 056.
replace(
    "backend/internal/admin/catalogue.go",
    "if err := saveProductAttributes(ctx, tx, id, input.Attributes); err != nil {\n\t\treturn Product{}, err\n\t}\n\tif err := validateRequiredAttributes",
    "if err := saveProductAttributes(ctx, tx, id, input.Attributes); err != nil {\n\t\treturn Product{}, err\n\t}\n\tvar variantID int64\n\tif err := tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE product_id=$1 ORDER BY id LIMIT 1`, id).Scan(&variantID); err != nil { return Product{}, err }\n\tvariantAttributes := map[string]any{}\n\tif input.HeightCM != nil { variantAttributes[\"height_cm\"] = *input.HeightCM }\n\tif input.PotDiameterCM != nil { variantAttributes[\"pot_diameter_cm\"] = *input.PotDiameterCM }\n\tif input.PackageLengthCM != nil { variantAttributes[\"package_length_cm\"] = *input.PackageLengthCM }\n\tif input.PackageWidthCM != nil { variantAttributes[\"package_width_cm\"] = *input.PackageWidthCM }\n\tif input.PackageHeightCM != nil { variantAttributes[\"package_height_cm\"] = *input.PackageHeightCM }\n\tif input.PackageWeightGrams != nil { variantAttributes[\"package_weight_grams\"] = *input.PackageWeightGrams }\n\tif err := saveVariantPIMValues(ctx, tx, id, variantID, variantAttributes); err != nil { return Product{}, err }\n\tif err := validateRequiredAttributes",
)

# UI: variant-specific galleries change immediately with the selected SKU.
replace(
    "frontend/src/product/types.ts",
    "heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number;\n  attributes: ProductAttribute[];",
    "heightCm?: number; potDiameterCm?: number; wholesaleMinQty: number; images: string[];\n  attributes: ProductAttribute[];",
)
replace(
    "frontend/src/ProductPage.tsx",
    "variants: (item.variants || []).map((variant) => ({ ...variant, attributes: variant.attributes || [] }))",
    "variants: (item.variants || []).map((variant) => ({ ...variant, images: variant.images || [], attributes: variant.attributes || [] }))",
)
replace(
    "frontend/src/ProductPage.tsx",
    "const customerAttributes = [...product.attributes, ...(variant?.attributes || [])].filter((item) => item.showInCharacteristics !== false);",
    "const customerAttributes = [...product.attributes, ...(variant?.attributes || [])].filter((item) => item.showInCharacteristics !== false);\n  const gallery = variant?.images?.length ? variant.images : product.images;",
)
replace(
    "frontend/src/ProductPage.tsx",
    "<ProductGallery images={product.images} name={product.name} active={activeImage} onSelect={setActiveImage} />",
    "<ProductGallery images={gallery} name={product.name} active={Math.min(activeImage, Math.max(gallery.length - 1, 0))} onSelect={setActiveImage} />",
)
replace(
    "frontend/src/ProductPage.tsx",
    "onVariant={(id) => { setSelectedID(id); setQuantity(1); }}",
    "onVariant={(id) => { setSelectedID(id); setQuantity(1); setActiveImage(0); }}",
)

# Tests that intentionally documented the old slug relation now document the
# explicit PRODUCT FK and numeric test fixtures.
replace("backend/internal/httpapi/admin_order_adjustments_test.go", 'ProductID: "azaliya-d9"', "ProductID: 101")
replace("backend/internal/httpapi/admin_order_adjustments_test.go", 'ProductID: "aglaonema-mariya"', "ProductID: 102")
replace(
    "backend/internal/order/journal_test.go",
    "LEFT JOIN products p ON p.id = pv.product_id",
    "LEFT JOIN products p ON p.id = oi.product_id",
)

print("PIM follow-up applied")
