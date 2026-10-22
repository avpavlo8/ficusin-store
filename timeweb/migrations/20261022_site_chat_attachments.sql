-- Keep existing private photo storage and access boundaries for all attachments.
ALTER TABLE support_messages ADD COLUMN attachment_name TEXT NOT NULL DEFAULT '';
ALTER TABLE support_messages ADD COLUMN attachment_kind TEXT NOT NULL DEFAULT '' CHECK (attachment_kind IN ('','image','video','file'));
