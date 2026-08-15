-- New review media lives in private object storage. The legacy bytea column
-- remains readable during migration, so deploys are backward compatible.
ALTER TABLE product_review_photos
    ADD COLUMN IF NOT EXISTS object_key TEXT,
    ALTER COLUMN image DROP NOT NULL;

ALTER TABLE product_review_photos
    DROP CONSTRAINT IF EXISTS product_review_photos_content_type_check,
    DROP CONSTRAINT IF EXISTS product_review_photos_image_check;

ALTER TABLE product_review_photos
    ADD CONSTRAINT product_review_media_content_type_check
        CHECK (content_type IN ('image/jpeg','image/png','image/webp','video/mp4','video/webm')),
    ADD CONSTRAINT product_review_media_payload_check CHECK (
        (object_key IS NOT NULL AND image IS NULL)
        OR (object_key IS NULL AND image IS NOT NULL AND octet_length(image) <= 20971520)
    );

CREATE INDEX IF NOT EXISTS product_review_media_object_idx
    ON product_review_photos(object_key) WHERE object_key IS NOT NULL;
