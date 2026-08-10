-- Закупки живут в собственной предметной области. Инвойс сначала становится
-- черновиком и очередью проверок; ни остатки, ни цены на живых каналах этим
-- импортом не меняются.
CREATE TABLE IF NOT EXISTS procurement_suppliers (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('international', 'domestic')),
  country_code TEXT NOT NULL DEFAULT '',
  default_currency TEXT NOT NULL CHECK (default_currency IN ('EUR', 'USD', 'RUB')),
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS procurement_suppliers_name_unique
  ON procurement_suppliers (LOWER(name));

-- Одна карточка СБИС может иметь сколько угодно названий у одного поставщика.
-- matched_saby_id ссылается на полный справочник СБИС, а не только на товары,
-- уже опубликованные на сайте.
CREATE TABLE IF NOT EXISTS procurement_supplier_aliases (
  id BIGSERIAL PRIMARY KEY,
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id) ON DELETE CASCADE,
  raw_name TEXT NOT NULL,
  normalized_name TEXT NOT NULL DEFAULT '',
  supplier_article TEXT NOT NULL DEFAULT '',
  pot_diameter_cm NUMERIC(6,1),
  height_cm NUMERIC(6,1),
  matched_saby_id TEXT REFERENCES saby_nomenclature(saby_id) ON DELETE SET NULL,
  match_status TEXT NOT NULL DEFAULT 'unmatched'
    CHECK (match_status IN ('unmatched', 'suggested', 'confirmed', 'new_product', 'ignored')),
  confidence NUMERIC(5,4) NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
  availability_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (availability_status IN ('available', 'unknown', 'check', 'temporarily_unavailable', 'discontinued')),
  unavailable_since DATE,
  check_after DATE,
  occurrences INTEGER NOT NULL DEFAULT 0,
  last_seen_at DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS procurement_supplier_alias_unique
  ON procurement_supplier_aliases (
    supplier_id,
    LOWER(raw_name),
    COALESCE(supplier_article, ''),
    COALESCE(pot_diameter_cm, -1),
    COALESCE(height_cm, -1)
  );
CREATE INDEX IF NOT EXISTS procurement_alias_review_idx
  ON procurement_supplier_aliases (match_status, supplier_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS procurement_alias_availability_idx
  ON procurement_supplier_aliases (availability_status, check_after);

CREATE TABLE IF NOT EXISTS procurement_orders (
  id BIGSERIAL PRIMARY KEY,
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id),
  order_number TEXT NOT NULL DEFAULT '',
  document_number TEXT NOT NULL DEFAULT '',
  document_date DATE,
  source_kind TEXT NOT NULL DEFAULT 'manual'
    CHECK (source_kind IN ('recommendation', 'manual', 'invoice', 'payment_invoice')),
  currency TEXT NOT NULL CHECK (currency IN ('EUR', 'USD', 'RUB')),
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'ordered', 'invoice_received', 'review', 'ready_to_receive', 'received', 'cancelled')),
  exchange_rate NUMERIC(14,6),
  trolley_cost_currency NUMERIC(14,2) NOT NULL DEFAULT 0,
  delivery_to_ryazan_rub NUMERIC(14,2) NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_orders_status_idx
  ON procurement_orders (status, created_at DESC);

CREATE TABLE IF NOT EXISTS procurement_order_lines (
  id BIGSERIAL PRIMARY KEY,
  procurement_order_id BIGINT NOT NULL REFERENCES procurement_orders(id) ON DELETE CASCADE,
  supplier_alias_id BIGINT REFERENCES procurement_supplier_aliases(id) ON DELETE SET NULL,
  saby_id TEXT REFERENCES saby_nomenclature(saby_id) ON DELETE SET NULL,
  raw_name TEXT NOT NULL,
  supplier_article TEXT NOT NULL DEFAULT '',
  ordered_qty INTEGER NOT NULL DEFAULT 0 CHECK (ordered_qty >= 0),
  invoiced_qty INTEGER CHECK (invoiced_qty >= 0),
  unit_price NUMERIC(14,4),
  load_unit TEXT NOT NULL DEFAULT '',
  customer_request BOOLEAN NOT NULL DEFAULT FALSE,
  match_status TEXT NOT NULL DEFAULT 'unmatched'
    CHECK (match_status IN ('unmatched', 'suggested', 'confirmed', 'new_product', 'ignored')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_order_lines_order_idx
  ON procurement_order_lines (procurement_order_id, id);
CREATE INDEX IF NOT EXISTS procurement_order_lines_match_idx
  ON procurement_order_lines (match_status, procurement_order_id);

-- Сюда позже попадут как клиентские товары «под заказ», так и идеи продавцов.
-- Они учитываются алгоритмом рекомендаций, но сами по себе закупку не создают.
CREATE TABLE IF NOT EXISTS procurement_requests (
  id BIGSERIAL PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('customer_order', 'staff_recommendation')),
  saby_id TEXT REFERENCES saby_nomenclature(saby_id) ON DELETE SET NULL,
  requested_name TEXT NOT NULL,
  quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),
  customer_order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'included', 'fulfilled', 'cancelled')),
  notes TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS procurement_requests_open_idx
  ON procurement_requests (status, kind, created_at);
