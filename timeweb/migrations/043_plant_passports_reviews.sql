ALTER TABLE products
    ADD COLUMN IF NOT EXISTS plant_passport JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS important_warnings TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS product_reviews (
    id BIGSERIAL PRIMARY KEY,
    product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 10 AND 3000),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    moderated_at TIMESTAMPTZ,
    moderated_by BIGINT REFERENCES customers(id),
    UNIQUE (product_id, customer_id, order_id)
);

CREATE TABLE IF NOT EXISTS product_review_photos (
    id BIGSERIAL PRIMARY KEY,
    review_id BIGINT NOT NULL REFERENCES product_reviews(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    image BYTEA NOT NULL CHECK (octet_length(image) <= 5242880),
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS product_reviews_public_idx
    ON product_reviews(product_id, created_at DESC) WHERE status = 'published';
CREATE INDEX IF NOT EXISTS product_reviews_moderation_idx
    ON product_reviews(status, created_at DESC);
