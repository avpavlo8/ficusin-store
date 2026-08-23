-- 067 intentionally kept a short retry budget. During the first production
-- batch, a burst of transient OpenAI errors exhausted it. Requeue those drafts
-- once under the slower, rate-limit-aware worker introduced with this release.
UPDATE catalog_ai_enrichment_jobs
SET text_status=CASE WHEN text_status='failed' THEN 'pending' ELSE text_status END,
    image_status=CASE WHEN image_status='failed' THEN 'pending' ELSE image_status END,
    text_attempts=CASE WHEN text_status='failed' THEN 0 ELSE text_attempts END,
    image_attempts=CASE WHEN image_status='failed' THEN 0 ELSE image_attempts END,
    updated_at=CURRENT_TIMESTAMP
WHERE text_status='failed' OR image_status='failed';
