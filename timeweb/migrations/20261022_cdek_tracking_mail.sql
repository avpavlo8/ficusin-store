-- Existing tracks have already been communicated or handled manually. Do not
-- send a burst of historical mail when enabling once-only tracking letters.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS cdek_tracking_notified_at TIMESTAMPTZ;
UPDATE orders SET cdek_tracking_notified_at=CURRENT_TIMESTAMP
WHERE cdek_track_number<>'' AND cdek_tracking_notified_at IS NULL;
