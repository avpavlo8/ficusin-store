ALTER TABLE avito_listings ADD COLUMN IF NOT EXISTS manual_paused BOOLEAN NOT NULL DEFAULT FALSE;

-- Existing archives have no trustworthy reason recorded. Do not spend money
-- restoring them merely because a matching product has stock.
UPDATE avito_listings SET manual_paused=TRUE,desired_published=FALSE
WHERE LOWER(status)='old';
