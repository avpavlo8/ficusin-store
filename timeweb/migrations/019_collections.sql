-- Hand-made collections: "неприхотливые", "в тень", "безопасно котам".
--
-- Assembled by a person, not by a rule over attributes: the manager knows
-- that a particular ficus is fussy despite its label, and a query never
-- will. Membership is an explicit list.
CREATE TABLE IF NOT EXISTS collections (
  id BIGSERIAL PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  -- note is the one line under the title on the storefront tab.
  note TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_active SMALLINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS collection_products (
  collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (collection_id, product_id)
);

CREATE INDEX IF NOT EXISTS collection_products_product_idx
  ON collection_products (product_id);

-- Three to start with, so the storefront has something to show before
-- anyone opens the panel. Empty collections are hidden from customers.
INSERT INTO collections (slug, title, note, sort_order)
VALUES
  ('easy', 'Неприхотливые', 'Простят забытый полив', 10),
  ('shade', 'В тень', 'Живут вдали от окна', 20),
  ('pet-safe', 'Безопасно питомцам', 'Не токсичны для кошек и собак', 30)
ON CONFLICT (slug) DO NOTHING;
