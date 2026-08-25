-- Migration 055 replaced public Saby/name slugs with immutable numeric product
-- codes. The temporary mapping was not persisted, so already indexed links
-- lost their destination. Restore only aliases whose product identity is
-- documented by the historical procurement pilot and still unambiguous.
CREATE TABLE IF NOT EXISTS product_url_aliases (
  alias TEXT PRIMARY KEY,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK (alias = LOWER(BTRIM(alias)) AND alias <> '')
);

INSERT INTO product_url_aliases(alias,product_id)
SELECT mapping.alias,product.id
FROM (VALUES
  ('saby-1125',65::BIGINT), -- Бонсай Лигуструм
  ('saby-1140',84::BIGINT), -- Бонсай Фикус Retusa
  ('saby-1117',58::BIGINT), -- Бонсай Зелкова
  ('saby-1131',76::BIGINT), -- Бонсай Подокарпус
  ('saby-1173',178::BIGINT),-- Сансевиерия Цилиндрика Спагетти
  ('saby-3234',6::BIGINT),  -- Аглаонема Мария Кристина
  ('saby-3019',154::BIGINT) -- Непентес
) mapping(alias,product_code)
JOIN products product ON product.product_code=mapping.product_code
ON CONFLICT(alias) DO UPDATE SET product_id=EXCLUDED.product_id;

