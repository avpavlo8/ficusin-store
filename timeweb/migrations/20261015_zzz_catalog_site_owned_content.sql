-- Owner decision: Saby owns stock; all content and retail prices live on site.
UPDATE products SET saby_fields=ARRAY['stock']::text[]
WHERE saby_id IS NOT NULL OR EXISTS(SELECT 1 FROM product_variants v WHERE v.product_id=products.id AND v.saby_id IS NOT NULL);

-- Keep historical jobs and their results for audit, but leave no paid work pending.
UPDATE catalog_ai_enrichment_jobs SET text_status='failed',text_error='Интеграция ИИ удалена владельцем',updated_at=CURRENT_TIMESTAMP
WHERE text_status IN ('pending','processing');
UPDATE catalog_ai_enrichment_jobs SET image_status='skipped',image_error='Интеграция ИИ удалена владельцем',updated_at=CURRENT_TIMESTAMP
WHERE image_status IN ('pending','processing','failed');
