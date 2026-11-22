CREATE VIEW procurement_plant_catalog_stock AS
WITH RECURSIVE plant_categories AS (
  SELECT id FROM categories WHERE slug='plants'
  UNION ALL
  SELECT child.id FROM categories child JOIN plant_categories parent ON parent.id=child.parent_id
)
SELECT variant.id AS variant_id, source.saby_id,
       CONCAT_WS(' · ', product.name, NULLIF(BTRIM(variant.label),'')) AS display_name,
       stock.balance, stock.synced_at
FROM products product
JOIN plant_categories category ON category.id=product.category_id
JOIN product_variants variant ON variant.product_id=product.id
JOIN saby_nomenclature source ON source.saby_id=variant.saby_id
LEFT JOIN LATERAL (
  SELECT SUM(GREATEST(inventory.available_qty-inventory.reserved_qty,0))::INTEGER AS balance,
         MAX(inventory.synced_at) AS synced_at
  FROM order_sale_inventory inventory
  JOIN warehouses warehouse ON warehouse.id=inventory.warehouse_id
      AND warehouse.saby_id='saby-ryazan-main' AND warehouse.is_active=1
  WHERE inventory.variant_id=variant.id AND source.missing_since IS NULL
    AND inventory.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours'
) stock ON 'stock'=ANY(product.saby_fields)
WHERE product.status='published' AND variant.is_active=1 AND variant.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM launch_hidden_extra_plant_variants hidden WHERE hidden.variant_id=variant.id);

CREATE TABLE procurement_plant_stock_days (
  variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
  observed_date DATE NOT NULL,
  in_stock BOOLEAN NOT NULL,
  PRIMARY KEY (variant_id, observed_date)
);
