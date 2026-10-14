-- Inspection photos can be removed and restored without losing evidence.
ALTER TABLE marketplace_return_photos ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
-- Keep public condition notes separate from private inspection comments.
ALTER TABLE marketplace_returns ADD COLUMN IF NOT EXISTS public_comment TEXT NOT NULL DEFAULT '';
