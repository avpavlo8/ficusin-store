-- Run migrate-file-assets --verify against real buckets before this cutover.
-- This transaction also rechecks that every legacy payload has a private key.
-- Originals stay in a detached archive, never in the runtime read path.
LOCK TABLE customers,marketplace_return_photos,procurement_documents,
 support_messages,product_review_photos IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM customers WHERE avatar_image IS NOT NULL AND NULLIF(avatar_object_key,'') IS NULL)
 OR EXISTS(SELECT 1 FROM marketplace_return_photos WHERE image IS NOT NULL AND NULLIF(object_key,'') IS NULL)
 OR EXISTS(SELECT 1 FROM procurement_documents WHERE content IS NOT NULL AND NULLIF(object_key,'') IS NULL)
 OR EXISTS(SELECT 1 FROM support_messages WHERE image_data IS NOT NULL AND NULLIF(object_key,'') IS NULL)
 OR EXISTS(SELECT 1 FROM product_review_photos WHERE (image IS NOT NULL AND NULLIF(object_key,'') IS NULL) OR storage_scope<>'private') THEN
  RAISE EXCEPTION 'Private file cutover blocked: migrate and verify all legacy payloads and public review objects first';
 END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.file_binary_payloads (
 kind TEXT NOT NULL,
 source_id BIGINT NOT NULL,
 destination_key TEXT NOT NULL,
 payload BYTEA NOT NULL,
 sha256 TEXT NOT NULL,
 original_metadata JSONB NOT NULL,
 archived_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(kind,source_id)
);
INSERT INTO legacy_archive.file_binary_payloads(kind,source_id,destination_key,payload,sha256,original_metadata)
 SELECT 'avatars',id,avatar_object_key,avatar_image,encode(sha256(avatar_image),'hex'),to_jsonb(c)-'avatar_image' FROM customers c WHERE avatar_image IS NOT NULL
 UNION ALL SELECT 'returns',id,object_key,image,encode(sha256(image),'hex'),to_jsonb(f)-'image' FROM marketplace_return_photos f WHERE image IS NOT NULL
 UNION ALL SELECT 'procurement',id,object_key,content,encode(sha256(content),'hex'),to_jsonb(d)-'content' FROM procurement_documents d WHERE content IS NOT NULL
 UNION ALL SELECT 'chat',id,object_key,image_data,encode(sha256(image_data),'hex'),to_jsonb(m)-'image_data' FROM support_messages m WHERE image_data IS NOT NULL
 UNION ALL SELECT 'reviews',id,object_key,image,encode(sha256(image),'hex'),to_jsonb(p)-'image' FROM product_review_photos p WHERE image IS NOT NULL;
-- SQL-side equality proof before clearing a single source column.
DO $$ BEGIN
 IF EXISTS(
  WITH originals AS (
   SELECT 'avatars' kind,id source_id,avatar_image payload FROM customers WHERE avatar_image IS NOT NULL
   UNION ALL SELECT 'returns',id,image FROM marketplace_return_photos WHERE image IS NOT NULL
   UNION ALL SELECT 'procurement',id,content FROM procurement_documents WHERE content IS NOT NULL
   UNION ALL SELECT 'chat',id,image_data FROM support_messages WHERE image_data IS NOT NULL
   UNION ALL SELECT 'reviews',id,image FROM product_review_photos WHERE image IS NOT NULL
  ) SELECT 1 FROM originals o LEFT JOIN legacy_archive.file_binary_payloads a USING(kind,source_id)
  WHERE a.payload IS DISTINCT FROM o.payload OR a.sha256 IS DISTINCT FROM encode(sha256(o.payload),'hex')
 ) THEN RAISE EXCEPTION 'Private file archive verification failed'; END IF;
END $$;
UPDATE customers SET avatar_image=NULL WHERE avatar_image IS NOT NULL;
UPDATE marketplace_return_photos SET image=NULL WHERE image IS NOT NULL;
UPDATE procurement_documents SET content=NULL WHERE content IS NOT NULL;
UPDATE support_messages SET image_data=NULL WHERE image_data IS NOT NULL;
UPDATE product_review_photos SET image=NULL WHERE image IS NOT NULL;
-- Retaining the null columns keeps rolling old instances and offline migration
-- tools compatible; constraints prevent new bytea dependencies from appearing.
ALTER TABLE customers ADD CONSTRAINT customer_avatar_s3_only CHECK(avatar_image IS NULL);
ALTER TABLE marketplace_return_photos ADD CONSTRAINT return_photo_s3_only CHECK(image IS NULL);
ALTER TABLE procurement_documents ADD CONSTRAINT procurement_document_s3_only CHECK(content IS NULL);
ALTER TABLE support_messages ADD CONSTRAINT support_attachment_s3_only CHECK(image_data IS NULL);
ALTER TABLE product_review_photos ADD CONSTRAINT review_media_s3_only CHECK(image IS NULL),
 ADD CONSTRAINT review_private_only CHECK(storage_scope='private'),
 ALTER COLUMN storage_scope SET DEFAULT 'private';
