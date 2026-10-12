-- Flatten citrus and join the two carnivorous plants into one shop category.
-- Product moves happen before deleting leaves, so existing cards keep their
-- identity, prices, stock and marketplace mappings.
WITH indoor AS (SELECT id FROM categories WHERE slug='indoor-plants')
INSERT INTO categories(parent_id,name,slug,sort_order)
SELECT id,'Мясоеды','carnivorous-plants',85 FROM indoor
WHERE TRUE
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,
  parent_id=EXCLUDED.parent_id,sort_order=EXCLUDED.sort_order;

UPDATE products product SET category_id=target.id,updated_at=CURRENT_TIMESTAMP
FROM categories source,categories target
WHERE product.category_id=source.id
  AND source.slug IN ('venus-flytrap','nepenthes')
  AND target.slug='carnivorous-plants';

UPDATE products product SET category_id=citrus.id,updated_at=CURRENT_TIMESTAMP
FROM categories child,categories citrus
WHERE product.category_id=child.id AND child.parent_id=citrus.id
  AND citrus.slug='citrus';

-- A few flower cards had been placed at the plant root rather than at a
-- visible species category. Keep outdoor plants together; the rest use the
-- existing indoor taxonomy.
WITH plant_root AS (SELECT id FROM categories WHERE slug='plants')
INSERT INTO categories(parent_id,name,slug,sort_order)
SELECT id,'Садовые растения','garden-plants',20 FROM plant_root
WHERE TRUE
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,
  parent_id=EXCLUDED.parent_id,sort_order=EXCLUDED.sort_order;

WITH parents AS (SELECT id,slug FROM categories WHERE slug IN ('garden-plants','indoor-plants')),
     kinds(name,slug,parent_slug,sort_order) AS (VALUES
       ('Лилии','lilies','garden-plants',10),
       ('Спирея','spirea','garden-plants',20),
       ('Чубушник','philadelphus','garden-plants',30),
       ('Флебодиум','phlebodium','indoor-plants',495),
       ('Цикламен','cyclamen','indoor-plants',500)
     )
INSERT INTO categories(parent_id,name,slug,sort_order)
SELECT parents.id,kinds.name,kinds.slug,kinds.sort_order
FROM kinds JOIN parents ON parents.slug=kinds.parent_slug
WHERE TRUE
ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,
  parent_id=EXCLUDED.parent_id,sort_order=EXCLUDED.sort_order;

UPDATE products product SET category_id=target.id,updated_at=CURRENT_TIMESTAMP
FROM categories current_category,categories target
WHERE product.category_id=current_category.id AND current_category.slug='plants'
  AND target.slug=CASE
    WHEN lower(product.name) LIKE 'лили%' THEN 'lilies'
    WHEN lower(product.name) LIKE 'спире%' THEN 'spirea'
    WHEN lower(product.name) LIKE 'чубушник%' THEN 'philadelphus'
    WHEN lower(product.name) LIKE 'фаленопсис%' THEN 'orchid'
    WHEN lower(product.name) LIKE 'флебодиум%' THEN 'phlebodium'
    WHEN lower(product.name) LIKE 'цикламен%' THEN 'cyclamen'
    ELSE '' END;

-- Remove only empty leaves below these taxonomic branches. Other roots and
-- templates remain in place even on an empty fresh database.
DELETE FROM categories child USING categories parent
WHERE child.parent_id=parent.id
  AND parent.slug IN ('indoor-plants','citrus','garden-plants')
  AND NOT EXISTS(SELECT 1 FROM products product WHERE product.category_id=child.id)
  AND NOT EXISTS(SELECT 1 FROM categories descendant WHERE descendant.parent_id=child.id);
