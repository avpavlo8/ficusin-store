-- Immutable new migration. Existing bytea remains readable and migration keeps
-- source bytes until a separately authorised retention cleanup.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS avatar_object_key TEXT;
ALTER TABLE marketplace_return_photos ADD COLUMN IF NOT EXISTS object_key TEXT,
 ALTER COLUMN image DROP NOT NULL;
ALTER TABLE procurement_documents ADD COLUMN IF NOT EXISTS object_key TEXT,
 ALTER COLUMN content DROP NOT NULL;
ALTER TABLE finance_imports ADD COLUMN IF NOT EXISTS object_key TEXT,
 ALTER COLUMN source_file DROP NOT NULL;
-- The app writes only object references for new files. Transitional rows may
-- contain both a verified S3 reference and legacy bytes for rollback.
ALTER TABLE marketplace_return_photos ADD CONSTRAINT return_photo_payload_present
 CHECK(image IS NOT NULL OR object_key IS NOT NULL) NOT VALID;
ALTER TABLE procurement_documents ADD CONSTRAINT procurement_document_payload_present
 CHECK(content IS NOT NULL OR object_key IS NOT NULL) NOT VALID;
ALTER TABLE finance_imports ADD CONSTRAINT finance_import_payload_present
 CHECK(source_file IS NOT NULL OR object_key IS NOT NULL) NOT VALID;
