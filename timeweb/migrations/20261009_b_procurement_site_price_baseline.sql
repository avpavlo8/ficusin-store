-- Keep the site's price at calculation time so channel approvals can be made independently.
ALTER TABLE procurement_order_lines
    ADD COLUMN IF NOT EXISTS baseline_site_price_minor BIGINT;
