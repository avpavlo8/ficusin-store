-- Classify reviewed delivery leaflets without deleting the source media.
-- Source URLs outlive product_media IDs when external media rows are refreshed.
CREATE TABLE IF NOT EXISTS catalog_supplementary_media (
    product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    object_key TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (product_id, object_key)
);

INSERT INTO catalog_supplementary_media (product_id, object_key, reason)
SELECT media.product_id, media.object_key, 'Reviewed delivery instructions, not a photograph of Alocasia Black Velvet'
FROM product_media media
JOIN products product ON product.id=media.product_id
LEFT JOIN media_mirror mirror ON mirror.source_url=media.object_key
WHERE product.product_code::TEXT='20'
  AND COALESCE(mirror.large_url, media.object_key) IN (
    'https://s3.twcstorage.ru/ficusin-photos/products/e2a5b3e88ffd5baf-large.jpg',
    'https://s3.twcstorage.ru/ficusin-photos/products/898f65cffecd3558-large.jpg',
    'https://s3.twcstorage.ru/ficusin-photos/products/4a2c847899192d9f-large.jpg'
  )
ON CONFLICT (product_id, object_key) DO NOTHING;
