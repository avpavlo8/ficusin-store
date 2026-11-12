-- New binary chat attachments live in private object storage; old bytes remain readable.
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS object_key TEXT NOT NULL DEFAULT '';
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS attachment_size INTEGER NOT NULL DEFAULT 0;
UPDATE support_messages SET attachment_size=octet_length(image_data) WHERE image_data IS NOT NULL AND attachment_size=0;
