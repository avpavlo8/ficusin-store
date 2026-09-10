-- Preserve supplier packaging separately from the ordered plant quantity.
ALTER TABLE procurement_order_lines
  ADD COLUMN IF NOT EXISTS package_count INTEGER CHECK (package_count > 0),
  ADD COLUMN IF NOT EXISTS units_per_package INTEGER CHECK (units_per_package > 0),
  ADD COLUMN IF NOT EXISTS supplier_category TEXT NOT NULL DEFAULT '';
