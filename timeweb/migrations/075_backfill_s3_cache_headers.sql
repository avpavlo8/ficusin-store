-- Objects uploaded before the performance release have no Cache-Control
-- metadata. Keep their working card/large URLs in place, but mark the mirror
-- source for one safe background refresh. The photo worker re-uploads the
-- same deterministic object keys with immutable caching and only then marks
-- mirrored_at again; a failed source never removes the existing copies.
UPDATE media_mirror
SET mirrored_at = NULL,
    attempts = 0,
    failure = '',
    checked_at = CURRENT_TIMESTAMP - INTERVAL '7 hours'
WHERE card_url LIKE 'https://s3.twcstorage.ru/%'
  AND large_url LIKE 'https://s3.twcstorage.ru/%';
