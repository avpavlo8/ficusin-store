-- Profile photos live in PostgreSQL: the application container has an
-- ephemeral filesystem, so anything written there disappears on the next
-- deploy. The browser downscales the picture before uploading, so these
-- rows stay small (a couple of dozen kilobytes each).

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS avatar_image BYTEA,
    ADD COLUMN IF NOT EXISTS avatar_mime TEXT,
    ADD COLUMN IF NOT EXISTS avatar_updated_at TIMESTAMPTZ;
