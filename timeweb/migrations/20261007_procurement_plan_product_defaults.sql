-- Current supplier terms are shared by open procurement plans. Order lines
-- retain planned and invoiced prices as historical document values.
CREATE TABLE IF NOT EXISTS procurement_product_defaults (
  supplier_id BIGINT NOT NULL REFERENCES procurement_suppliers(id) ON DELETE CASCADE,
  saby_id TEXT NOT NULL REFERENCES saby_nomenclature(saby_id) ON DELETE CASCADE,
  currency TEXT NOT NULL CHECK (currency IN ('EUR','USD','RUB')),
  pot_diameter_cm NUMERIC(10,2) NOT NULL DEFAULT -1,
  height_cm NUMERIC(10,2) NOT NULL DEFAULT -1,
  category TEXT NOT NULL DEFAULT '',
  supplier_article TEXT NOT NULL DEFAULT '',
  unit_price NUMERIC(14,4),
  units_per_package INTEGER NOT NULL DEFAULT 1 CHECK (units_per_package > 0),
  price_source TEXT NOT NULL DEFAULT 'plan' CHECK (price_source IN ('plan','invoice')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
  PRIMARY KEY (supplier_id, saby_id, pot_diameter_cm, height_cm)
);
