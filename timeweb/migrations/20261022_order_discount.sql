ALTER TABLE orders ADD COLUMN IF NOT EXISTS manager_discount_type TEXT NOT NULL DEFAULT 'fixed' CHECK(manager_discount_type IN ('fixed','percent'));
ALTER TABLE orders ADD COLUMN IF NOT EXISTS manager_discount_value NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(manager_discount_value>=0);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS manager_discount_amount NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(manager_discount_amount>=0);
