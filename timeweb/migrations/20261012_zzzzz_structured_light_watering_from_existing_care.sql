-- Structure existing published care guidance into the two filter fields.
-- Each row was reviewed against the live care text and passport on 2026-10-12.
-- Keep price, stock, SKU, height, photos and marketplace links unchanged.
CREATE TEMP TABLE curated_existing_care_labels (product_code bigint PRIMARY KEY, expected_name text NOT NULL, light text NOT NULL, water text NOT NULL) ON COMMIT DROP;
INSERT INTO curated_existing_care_labels(product_code,expected_name,light,water) VALUES
  (185,'Драцена Сандера «Лаки Бамбук»','diffused','moderate'),
  (476,'Кактус','sunny','rare'),
  (482,'Сингониум','diffused','moderate'),
  (489,'Пальма Ликуала','diffused','moderate'),
  (510,'Глоксиния в горшке','diffused','moderate'),
  (522,'Фикус Биннендийка Amstel','diffused','moderate'),
  (537,'Флорариум','diffused','rare'),
  (549,'Бонсай можжевельник','sunny','moderate'),
  (554,'Бонсай Муррайя','diffused','moderate'),
  (570,'Платицериум Оленерогий','diffused','moderate'),
  (722,'Петуния ампельная','sunny','frequent'),
  (729,'Композиция из растений с арекой','diffused','moderate'),
  (731,'Аспарагус','diffused','moderate'),
  (735,'Композиция с Монстерой','diffused','moderate'),
  (741,'Композиция из растений','diffused','moderate'),
  (761,'Монстера Альба','diffused','moderate'),
  (767,'Бонсай Дуб','sunny','moderate'),
  (773,'Орхидея (микс)','diffused','moderate'),
  (787,'Бонсай Ficus Mi','diffused','moderate'),
  (801,'Педилантус','sunny','moderate'),
  (809,'Композиция из растений с бамбуком','diffused','moderate'),
  (830,'Панданус','diffused','moderate');
UPDATE products AS product
SET light_level=CASE WHEN COALESCE(product.light_level,'')='' THEN seed.light ELSE product.light_level END,
    watering=CASE WHEN COALESCE(product.watering,'')='' THEN seed.water ELSE product.watering END,
    updated_at=CURRENT_TIMESTAMP
FROM curated_existing_care_labels AS seed
WHERE product.product_code=seed.product_code AND product.name=seed.expected_name
  AND product.catalog_section='plants' AND product.status='published'
  AND NULLIF(BTRIM(product.care_instructions),'') IS NOT NULL
  AND (COALESCE(product.light_level,'')='' OR COALESCE(product.watering,'')='');
