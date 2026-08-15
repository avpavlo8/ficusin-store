-- A previous photo pass left some live Saby links at the retry ceiling.
-- Give only still-referenced, not-yet-mirrored supplier photos a fresh set of
-- attempts. Successful S3 mappings and unrelated URLs are never touched.
UPDATE media_mirror mirror
SET attempts = 0,
    failure = '',
    checked_at = CURRENT_TIMESTAMP - INTERVAL '7 hours'
WHERE mirror.card_url IS NULL
  AND mirror.source_url LIKE 'https://disk.sbis.ru/%'
  AND EXISTS (
    SELECT 1 FROM product_media media
    WHERE media.object_key = mirror.source_url
  );
