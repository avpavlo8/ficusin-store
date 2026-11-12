-- Existing review keys refer to the original photo bucket. Never silently
-- reinterpret them as keys inside the new dedicated private bucket.
ALTER TABLE product_review_photos ADD COLUMN IF NOT EXISTS storage_scope TEXT NOT NULL DEFAULT 'legacy-public';
ALTER TABLE product_review_photos ADD CONSTRAINT review_storage_scope CHECK(storage_scope IN ('legacy-public','private')) NOT VALID;
