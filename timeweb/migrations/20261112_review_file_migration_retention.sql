-- A verified object reference may coexist with legacy bytes during migration.
-- Source bytes are retained until a separately authorised retention cleanup.
ALTER TABLE product_review_photos DROP CONSTRAINT IF EXISTS product_review_media_payload_check;
ALTER TABLE product_review_photos ADD CONSTRAINT product_review_media_payload_check CHECK(image IS NOT NULL OR object_key IS NOT NULL) NOT VALID;
