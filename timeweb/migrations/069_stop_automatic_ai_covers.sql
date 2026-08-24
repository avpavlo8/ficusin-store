-- Automatic cover generation was a one-off catalogue fill and is now
-- intentionally disabled. Keep completed images, settle all unfinished image
-- jobs without another paid API call, and leave product drafts unpublished.
UPDATE catalog_ai_enrichment_jobs
SET image_status='skipped',
    image_error='Автоматическая генерация обложек отключена',
    updated_at=CURRENT_TIMESTAMP
WHERE image_status IN ('pending','processing','failed');
